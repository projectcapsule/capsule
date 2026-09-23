// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ssa

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
)

// ReplicationOwnerResolver resolves recorded replication managers for a target.
type ReplicationOwnerResolver func(context.Context, *unstructured.Unstructured, string) (map[string]struct{}, error)

// NewReplicationOwnerResolver indexes parent identities, which are immutable and
// present in the manager cache before the replication controllers can apply any
// targets. Unlike a target-status index, this cannot miss a newly recorded item
// on an existing parent. Read current status directly; cache status is only a
// candidate source and must not decide whether protection can be removed.
// Neither reader is the impersonated client used to apply the target.
func NewReplicationOwnerResolver(indexed, reader client.Reader) ReplicationOwnerResolver {
	return func(ctx context.Context, target *unstructured.Unstructured, departing string) (map[string]struct{}, error) {
		key := gvk.NewResourceID(target, "", "").GetGVKKey("")
		owners := map[string]struct{}{}

		for prefix := range replicationOwnerPrefixes(target, departing) {
			parents, err := replicationOwnerCandidates(ctx, indexed, prefix)
			if err != nil {
				return nil, err
			}

			for _, parent := range parents {
				if err := addReplicationOwners(ctx, reader, parent, key, owners); err != nil {
					return nil, err
				}
			}
		}

		return owners, nil
	}
}

func replicationOwnerPrefixes(target *unstructured.Unstructured, departing string) map[string]struct{} {
	prefixes := map[string]struct{}{}

	for _, field := range target.GetManagedFields() {
		if prefix := replicationOwnerPrefix(field.Manager); prefix != "" && field.Manager != departing {
			prefixes[prefix] = struct{}{}
		}
	}

	return prefixes
}

func replicationOwnerCandidates(ctx context.Context, indexed client.Reader, prefix string) ([]client.Object, error) {
	global := &capsulev1beta2.GlobalTenantResourceList{}
	if err := indexed.List(ctx, global, client.MatchingFields{tenantresource.FieldOwnerIndexerFieldName: prefix}); err != nil {
		return nil, err
	}

	local := &capsulev1beta2.TenantResourceList{}
	if err := indexed.List(ctx, local, client.MatchingFields{tenantresource.FieldOwnerIndexerFieldName: prefix}); err != nil {
		return nil, err
	}

	parents := make([]client.Object, 0, len(global.Items)+len(local.Items))
	for i := range global.Items {
		parents = append(parents, &global.Items[i])
	}

	for i := range local.Items {
		parents = append(parents, &local.Items[i])
	}

	return parents, nil
}

func addReplicationOwners(ctx context.Context, reader client.Reader, parent client.Object, targetKey string, owners map[string]struct{}) error {
	uid := parent.GetUID()

	if err := reader.Get(ctx, client.ObjectKeyFromObject(parent), parent); err != nil {
		return client.IgnoreNotFound(err)
	}

	if parent.GetUID() != uid || !parent.GetDeletionTimestamp().IsZero() {
		return nil
	}

	prefix := meta.ReplicationFieldOwnerPrefix(parent.GetName(), parent.GetNamespace())

	for _, item := range replicationProcessedItems(parent) {
		ref := item.ResourceID
		if item.ClusterScoped {
			ref.Namespace = ""
		}

		if ref.GetGVKKey("") == targetKey && (item.Created || !item.LastApply.IsZero()) {
			owners[prefix+"/"+item.FieldOwner("")] = struct{}{}
		}
	}

	return nil
}

func replicationProcessedItems(parent client.Object) meta.ProcessedItems {
	switch obj := parent.(type) {
	case *capsulev1beta2.GlobalTenantResource:
		return obj.Status.ProcessedItems
	case *capsulev1beta2.TenantResource:
		return obj.Status.ProcessedItems
	}

	return nil
}

func (m Manager) resourceFieldOwners(ctx context.Context, target *unstructured.Unstructured, departing string) (map[string]struct{}, error) {
	// Ordinary field managers and the departing owner need no reverse lookup.
	// The legacy format is only a lookup gate, never proof of ownership.
	fields := target.GetManagedFields()
	for _, field := range fields {
		if field.Manager != departing && replicationOwnerPrefix(field.Manager) != "" {
			if m.ReplicationOwners == nil {
				return nil, fmt.Errorf("replication owner resolver is not configured")
			}

			known, err := m.ReplicationOwners(ctx, target, departing)
			if err != nil {
				return nil, fmt.Errorf("resolving replication field owners: %w", err)
			}

			return meta.CapsuleResourceFieldOwners(fields, known), nil
		}
	}

	return meta.CapsuleResourceFieldOwners(fields, nil), nil
}

// This parser only selects candidate parents. Exact target, tenant, and origin
// ownership must still match the parent's authoritative processed-item status.
func replicationOwnerPrefix(manager string) string {
	parts := strings.SplitN(manager, "/", 4)
	if len(parts) != 4 || len(parts[3]) < 2 || !strings.HasSuffix(parts[3], "/") {
		return ""
	}

	hash, err := strconv.ParseUint(parts[0], 36, 64)
	if err != nil || strconv.FormatUint(hash, 36) != parts[0] {
		return ""
	}

	return parts[0]
}
