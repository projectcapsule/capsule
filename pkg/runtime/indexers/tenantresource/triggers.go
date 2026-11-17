// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"slices"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

// Triggers selects explicit resource/operation subscriptions without full scans.
type Triggers struct{ Obj client.Object }

func (i Triggers) Object() client.Object { return i.Obj }
func (i Triggers) Field() string         { return replication.TriggerIndexerField }
func (i Triggers) Func() client.IndexerFunc {
	return func(obj client.Object) []string {
		var spec *capsulev1beta2.TenantResourceCommonSpec

		switch resource := obj.(type) {
		case *capsulev1beta2.TenantResource:
			spec = &resource.Spec.TenantResourceCommonSpec
		case *capsulev1beta2.GlobalTenantResource:
			spec = &resource.Spec.TenantResourceCommonSpec
		default:
			return nil
		}

		if len(spec.Triggers) == 0 || spec.IsCordoned() || !obj.GetDeletionTimestamp().IsZero() {
			return nil
		}

		keys := []string{replication.TriggerEnabledKey}

		if len(spec.Triggers) > 32 {
			return nil
		}

		for idx := range spec.Triggers {
			trigger := &spec.Triggers[idx]
			for _, resource := range replication.TriggerResources(trigger) {
				for _, operation := range []admissionv1.OperationType{admissionv1.Create, admissionv1.Update, admissionv1.Delete} {
					if slices.Contains(trigger.Operations, operation) || slices.Contains(trigger.Operations, admissionv1.OperationAll) {
						keys = append(keys, replication.ResourceKey(resource, operation))
					}
				}
			}
		}

		return keys
	}
}
