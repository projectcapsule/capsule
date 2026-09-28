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
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
	"github.com/projectcapsule/capsule/pkg/users"
)

type replicaHandler struct{}

func ReplicaHandler() handlers.Handler {
	return &replicaHandler{}
}

func (h *replicaHandler) OnCreate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validateMarkers(ctx, c, reader, decoder, req, nil)
	}
}

func (h *replicaHandler) OnDelete(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return allowTerminatingNamespaceDeletion(ctx, reader, req, h.handler(ctx, c, reader, decoder, req, false))
	}
}

func (h *replicaHandler) OnUpdate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handler(ctx, c, reader, decoder, req, true)
	}
}

func (h *replicaHandler) handler(
	ctx context.Context,
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	req admission.Request,
	update bool,
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

	var old *metav1.PartialObjectMetadata

	decodeOld := func() error {
		if old == nil {
			old = &metav1.PartialObjectMetadata{}

			return decoder.DecodeRaw(req.OldObject, old)
		}

		return nil
	}

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

			if err := decodeOld(); err != nil {
				return ad.ErroredResponse(err)
			}

			allowed, err := replicationServiceAccountAllowed(ctx, reader, &global.Items[i], old, gvkKey, req.UserInfo.Username)
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

			if err := decodeOld(); err != nil {
				return ad.ErroredResponse(err)
			}

			allowed, err := replicationServiceAccountAllowed(ctx, reader, &local.Items[i], old, gvkKey, req.UserInfo.Username)
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
	if err := decodeOld(); err != nil {
		return ad.ErroredResponse(err)
	}

	if old.Labels[meta.ReplicationProtectionLabel] == meta.ValueTrue ||
		old.Labels[meta.ProtectedByCapsuleLabel] == meta.ValueControllerReplications {
		return ad.Denyf("resource %s is protected by a capsule replication; its managing parent is not yet available", req.Name)
	}

	if old.Labels[meta.CreatedByCapsuleLabel] == meta.ValueControllerReplications ||
		old.Labels[meta.NewManagedByCapsuleLabel] == meta.ValueControllerReplications {
		// Current unprotected targets also carry tracking labels. Only an
		// authoritative lifecycle status can distinguish them from legacy targets
		// whose protection has not reached the status index yet.
		unprotected, err := replicationKnownUnprotected(ctx, c, reader, old, gvkKey, req.UserInfo.Username)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		if !unprotected {
			return ad.Denyf("resource %s is protected by a capsule replication; its managing parent is not yet available", req.Name)
		}
	}

	if update {
		return h.validateMarkers(ctx, c, reader, decoder, req, old)
	}

	return nil
}

// Stable parent identities avoid using another lagging status index to authorize
// a legacy target. Every replication manager must be accounted for before allowing
// arbitrary users. A verified managing ServiceAccount can also proceed when an
// unrelated field manager imitates a replication with no corresponding parent.
func replicationKnownUnprotected(ctx context.Context, indexed, reader client.Reader, target client.Object, targetKey, username string) (bool, error) {
	prefixes := ssa.ReplicationOwnerPrefixes(target, "")
	if len(prefixes) == 0 {
		return false, nil
	}

	allKnown, managingServiceAccount := true, false

	for prefix := range prefixes {
		parents, err := ssa.ReplicationOwnerCandidates(ctx, indexed, prefix)
		if err != nil {
			return false, err
		}

		if len(parents) == 0 {
			allKnown = false

			continue
		}

		for _, parent := range parents {
			allowed, err := replicationParentUnprotected(ctx, reader, parent, target, targetKey)
			if err != nil || !allowed {
				return false, err
			}

			// The read above verified the current UID, applied target, exact
			// field manager, and unprotected policy. Reuse that same snapshot.
			switch parent := parent.(type) {
			case *capsulev1beta2.GlobalTenantResource:
				managingServiceAccount = managingServiceAccount || isAllowedServiceAccount(username, parent.Status.ServiceAccount)
			case *capsulev1beta2.TenantResource:
				managingServiceAccount = managingServiceAccount || isAllowedServiceAccount(username, parent.Status.ServiceAccount)
			}
		}
	}

	return allKnown || managingServiceAccount, nil
}

func replicationParentUnprotected(ctx context.Context, reader client.Reader, parent, target client.Object, targetKey string) (bool, error) {
	uid := parent.GetUID()

	if err := reader.Get(ctx, client.ObjectKeyFromObject(parent), parent); err != nil {
		return false, client.IgnoreNotFound(err)
	}

	if parent.GetUID() != uid {
		return false, nil
	}

	matched, protected := replicationTargetProtection(parent, target, targetKey)

	return matched && !protected, nil
}

// A stale status entry must not authorize the previous manager of a replacement
// object. Match the applied item and its exact field owner on the API server's
// old object, rather than a caller-provided new managedFields entry.
func replicationTargetProtection(parent, target client.Object, targetKey string) (matched, protected bool) {
	var items meta.ProcessedItems

	switch parent := parent.(type) {
	case *capsulev1beta2.GlobalTenantResource:
		items = parent.Status.ProcessedItems
	case *capsulev1beta2.TenantResource:
		items = parent.Status.ProcessedItems
	}

	prefix := meta.ReplicationFieldOwnerPrefix(parent.GetName(), parent.GetNamespace()) + "/"

	for _, item := range items {
		ref := item.ResourceID
		if item.ClusterScoped {
			ref.Namespace = ""
		}

		owner := prefix + item.FieldOwner("")

		if (!item.Created && item.LastApply.IsZero()) || ref.GetGVKKey("") != targetKey || !slices.ContainsFunc(target.GetManagedFields(), func(field metav1.ManagedFieldsEntry) bool { return field.Manager == owner }) {
			continue
		}

		matched = true

		itemProtected := item.Created
		if item.Policy != nil {
			itemProtected = item.Policy.IsProtected()
		}

		if itemProtected {
			return true, true
		}
	}

	return matched, protected
}

// The status index only selects candidates. Verify an authorization against
// the current parent identity, target policy, and ServiceAccount before allowing.
func replicationServiceAccountAllowed(ctx context.Context, reader client.Reader, parent, target client.Object, targetKey, username string) (bool, error) {
	uid := parent.GetUID()

	if err := reader.Get(ctx, client.ObjectKeyFromObject(parent), parent); err != nil {
		return false, client.IgnoreNotFound(err)
	}

	_, protected := replicationTargetProtection(parent, target, targetKey)
	if parent.GetUID() != uid || !protected {
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

// Initial application precedes processed-item status. Resolve the parent's
// stable identity from the new object's SSA managers and verify its execution
// ServiceAccount authoritatively. A field-manager name alone grants no privilege.
func (h *replicaHandler) validateMarkers(ctx context.Context, indexed, reader client.Reader, decoder admission.Decoder, req admission.Request, old *metav1.PartialObjectMetadata) *admission.Response {
	if users.IsControllerServiceAccount(req.UserInfo.Username) {
		return nil
	}

	target := &metav1.PartialObjectMetadata{}
	if err := decoder.DecodeRaw(req.Object, target); err != nil {
		return ad.ErroredResponse(err)
	}

	var previous map[string]string
	if old != nil {
		previous = old.Labels
	}

	added := false
	for key, value := range map[string]string{
		meta.ReplicationProtectionLabel: meta.ValueTrue,
		meta.ProtectedByCapsuleLabel:    meta.ValueControllerReplications,
		meta.CreatedByCapsuleLabel:      meta.ValueControllerReplications,
		meta.NewManagedByCapsuleLabel:   meta.ValueControllerReplications,
	} {
		added = added || (target.Labels[key] == value && previous[key] != value)
	}

	if !added {
		return nil
	}
	// Ordinary users cannot establish controller metadata, even with forged SSA
	// managers. Avoid any parent reads for these common denials.
	denied := ad.Denyf("resource %s replication metadata can only be established by its managing ServiceAccount", req.Name)
	if _, _, err := serviceaccount.SplitUsername(req.UserInfo.Username); err != nil || reader == nil {
		return denied
	}

	for prefix := range ssa.ReplicationOwnerPrefixes(target, "") {
		parents, err := ssa.ReplicationOwnerCandidates(ctx, indexed, prefix)
		if err != nil {
			return ad.ErroredResponse(err)
		}

		for _, parent := range parents {
			uid := parent.GetUID()

			if err := reader.Get(ctx, client.ObjectKeyFromObject(parent), parent); err != nil {
				if client.IgnoreNotFound(err) != nil {
					return ad.ErroredResponse(err)
				}

				continue
			}

			if parent.GetUID() != uid || !parent.GetDeletionTimestamp().IsZero() {
				continue
			}

			var sa *meta.NamespacedRFC1123ObjectReferenceWithNamespace

			switch parent := parent.(type) {
			case *capsulev1beta2.GlobalTenantResource:
				sa = parent.Status.ServiceAccount
			case *capsulev1beta2.TenantResource:
				sa = parent.Status.ServiceAccount
			}

			if isAllowedServiceAccount(req.UserInfo.Username, sa) {
				return nil
			}
		}
	}

	return denied
}
