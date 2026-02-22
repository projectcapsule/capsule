// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"fmt"

	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/tenant"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
)

type replicaHandler struct{}

func ReplicaHandler() handlers.Handler {
	return &replicaHandler{}
}

func (h *replicaHandler) OnCreate(client.Client, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *replicaHandler) OnDelete(client client.Client, _ admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handler(ctx, client, req, recorder)
	}
}

func (h *replicaHandler) OnUpdate(client client.Client, _ admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handler(ctx, client, req, recorder)
	}
}

func (h *replicaHandler) handler(ctx context.Context, clt client.Client, req admission.Request, recorder events.EventRecorder) *admission.Response {
	tnt, err := tenant.TenantByStatusNamespace(ctx, clt, req.Namespace)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == nil {
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
	if err := clt.List(
		ctx,
		global,
		client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(tenantresource.ProcessedIndexerFieldName, gvkKey),
		},
	); err != nil {
		return utils.ErroredResponse(err)
	}

	if len(global.Items) > 0 {
		if isAllowedServiceAccount(req.UserInfo.Username, global.Items[0].Status.ServiceAccount) {
			return nil
		}

		resp := admission.Denied(fmt.Sprintf("resource %s is managed by a global capsule replication '%s'", req.Name, global.Items[0].GetName()))
		return &resp
	}

	local := &capsulev1beta2.TenantResourceList{}
	if err := clt.List(
		ctx,
		local,
		client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(tenantresource.ProcessedIndexerFieldName, gvkKey),
		},
	); err != nil {
		return utils.ErroredResponse(err)
	}

	if len(local.Items) > 0 {
		if isAllowedServiceAccount(req.UserInfo.Username, local.Items[0].Status.ServiceAccount) {
			return nil
		}

		resp := admission.Denied(fmt.Sprintf("resource %s is managed by a tenant capsule replication %s/%s", req.Name, local.Items[0].GetName(), local.Items[0].GetNamespace()))
		return &resp
	}

	return nil
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
