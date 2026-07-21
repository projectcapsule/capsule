// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

const (
	ServiceAccountIndexerFieldName string = "spec.serviceaccount"
	ProcessedIndexerFieldName      string = "status.items"
	CreatedIndexerFieldName        string = "status.items.created"
	NamespaceIndexerFieldName      string = "metadata.namespace"
	TriggersIndexerFieldName       string = "spec.triggers"
)

// TriggerGKKey returns the index key used for a trigger kind. Triggers select
// kinds, optionally by bare API group (matching any version), so the key
// intentionally carries only group and kind; the sinks apply the per-trigger
// version match on top of the index lookup.
func TriggerGKKey(gk schema.GroupKind) string {
	const sep = "\x1f"

	return fmt.Sprintf("%s%s%s", gk.Group, sep, gk.Kind)
}

// triggerIndexKeys extracts the per-GroupKind index keys for a resource's
// triggers. Shared by the Global and Namespaced trigger indexers.
func triggerIndexKeys(spec capsulev1beta2.TenantResourceCommonSpec) []string {
	vks := spec.TriggerVersionKinds()

	seen := make(map[string]struct{}, len(vks))
	out := make([]string, 0, len(vks))

	for _, vk := range vks {
		key := TriggerGKKey(vk.GroupVersionKind().GroupKind())
		if _, ok := seen[key]; ok {
			continue
		}

		seen[key] = struct{}{}

		out = append(out, key)
	}

	return out
}
