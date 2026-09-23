// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
	"github.com/projectcapsule/capsule/pkg/users"
)

type replicaHandler struct{}

func ReplicaHandler() handlers.Handler {
	return &replicaHandler{}
}

func (h *replicaHandler) OnCreate(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *replicaHandler) OnDelete(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handler(ctx, c, reader, decoder, req)
	}
}

func (h *replicaHandler) OnUpdate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handler(ctx, c, reader, decoder, req)
	}
}

func (h *replicaHandler) handler(
	ctx context.Context,
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	req admission.Request,
) *admission.Response {
	// Replicated objects are applied with the replication's impersonated client,
	// but controllers reconcile their own metadata, finalizers, and status with
	// the Capsule manager client. This is especially relevant when the replicated
	// object is itself a TenantResource or GlobalTenantResource.
	if users.IsControllerServiceAccount(req.UserInfo.Username) {
		return nil
	}

	// Checking if the object is managed by a TenantResource, local or global
	ref := gvk.ResourceID{
		Group:     req.Kind.Group,
		Version:   req.Kind.Version,
		Kind:      req.Kind.Kind,
		Name:      req.Name,
		Namespace: req.Namespace,
	}

	gvkKey := ref.GetGVKKey("")

	global := &capsulev1beta2.GlobalTenantResourceList{}
	if err := c.List(
		ctx,
		global,
		client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(tenantresource.ProtectedIndexerFieldName, gvkKey),
		},
	); err != nil {
		return ad.ErroredResponse(err)
	}

	if len(global.Items) > 0 {
		for i := range global.Items {
			if !isAllowedServiceAccount(req.UserInfo.Username, global.Items[i].Status.ServiceAccount) {
				continue
			}

			allowed, err := replicationServiceAccountAllowed(ctx, reader, &global.Items[i], gvkKey, req.UserInfo.Username)
			if err != nil {
				return ad.ErroredResponse(err)
			}

			if allowed {
				return nil
			}
		}

		return ad.Denyf(
			"resource %s is managed by a global capsule replication %s",
			req.Name,
			global.Items[0].GetName(),
		)
	}

	local := &capsulev1beta2.TenantResourceList{}
	if err := c.List(
		ctx,
		local,
		client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(tenantresource.ProtectedIndexerFieldName, gvkKey),
		},
	); err != nil {
		return ad.ErroredResponse(err)
	}

	if len(local.Items) > 0 {
		for i := range local.Items {
			if !isAllowedServiceAccount(req.UserInfo.Username, local.Items[i].Status.ServiceAccount) {
				continue
			}

			allowed, err := replicationServiceAccountAllowed(ctx, reader, &local.Items[i], gvkKey, req.UserInfo.Username)
			if err != nil {
				return ad.ErroredResponse(err)
			}

			if allowed {
				return nil
			}
		}

		return ad.Denyf(
			"resource %s is managed by a tenant capsule replication %s/%s",
			req.Name,
			local.Items[0].GetName(),
			local.Items[0].GetNamespace(),
		)
	}

	// Protection is written to the target before its parent's status and cache
	// index catch up. The API server's old object closes that window, including
	// attempts to remove the marker in the same update. An unknown owner must
	// retry after the index catches up; an empty index cannot authorize a write.
	old := &metav1.PartialObjectMetadata{}
	if err := decoder.DecodeRaw(req.OldObject, old); err != nil {
		return ad.ErroredResponse(err)
	}

	if old.Labels[meta.ReplicationProtectionLabel] == meta.ValueTrue ||
		old.Labels[meta.ProtectedByCapsuleLabel] == meta.ValueControllerReplications {
		return ad.Denyf("resource %s is protected by a capsule replication; its managing parent is not yet available", req.Name)
	}

	return nil
}

// The status index only selects candidates. Verify an authorization against
// the current parent identity, target policy, and ServiceAccount before allowing.
func replicationServiceAccountAllowed(ctx context.Context, reader client.Reader, parent client.Object, targetKey, username string) (bool, error) {
	uid := parent.GetUID()

	if err := reader.Get(ctx, client.ObjectKeyFromObject(parent), parent); err != nil {
		return false, client.IgnoreNotFound(err)
	}

	if parent.GetUID() != uid || !slices.Contains((tenantresource.ProtectedItems{}).Func()(parent), targetKey) {
		return false, nil
	}

	switch parent := parent.(type) {
	case *capsulev1beta2.GlobalTenantResource:
		return isAllowedServiceAccount(username, parent.Status.ServiceAccount), nil
	case *capsulev1beta2.TenantResource:
		return isAllowedServiceAccount(username, parent.Status.ServiceAccount), nil
	}

	return false, nil
}

func isAllowedServiceAccount(username string, sa *meta.NamespacedRFC1123ObjectReferenceWithNamespace) bool {
	if sa == nil {
		return false
	}

	ns, name, err := serviceaccount.SplitUsername(username)
	if err != nil {
		return false
	}

	return name == sa.Name.String() && ns == sa.Namespace.String()
}
