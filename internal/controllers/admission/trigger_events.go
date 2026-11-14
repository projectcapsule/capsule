// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"maps"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

func triggerSourceChanged() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool { return activeTriggers(e.Object) },
		DeleteFunc: func(e event.DeleteEvent) bool {
			spec := triggerSpec(e.Object)

			return spec != nil && len(spec.Triggers) > 0
		},
		GenericFunc: func(event.GenericEvent) bool { return false },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldActive, newActive := activeTriggers(e.ObjectOld), activeTriggers(e.ObjectNew)
			if oldActive != newActive {
				return true
			}

			if !newActive || e.ObjectOld.GetGeneration() == e.ObjectNew.GetGeneration() {
				return false
			}

			oldSpec, newSpec := triggerSpec(e.ObjectOld), triggerSpec(e.ObjectNew)
			if reflect.DeepEqual(oldSpec.Triggers, newSpec.Triggers) {
				return false
			}

			// Trigger names, audiences and list ordering do not change the shared
			// native registration. MatchID is the existing predicate projection.
			return !maps.Equal(triggerGroups(oldSpec), triggerGroups(newSpec))
		},
	}
}

func activeTriggers(obj client.Object) bool {
	spec := triggerSpec(obj)

	return spec != nil && len(spec.Triggers) > 0 && !spec.IsCordoned() && obj.GetDeletionTimestamp().IsZero()
}

func triggerSpec(obj client.Object) *capsulev1beta2.TenantResourceCommonSpec {
	switch resource := obj.(type) {
	case *capsulev1beta2.TenantResource:
		return &resource.Spec.TenantResourceCommonSpec
	case *capsulev1beta2.GlobalTenantResource:
		return &resource.Spec.TenantResourceCommonSpec
	default:
		return nil
	}
}

func triggerGroups(spec *capsulev1beta2.TenantResourceCommonSpec) map[string]struct{} {
	groups := make(map[string]struct{}, len(spec.Triggers))
	for i := range spec.Triggers {
		groups[replication.MatchID(&spec.Triggers[i])] = struct{}{}
	}

	return groups
}
