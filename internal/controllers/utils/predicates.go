// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

var CapsuleConfigSpecChangedPredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		oldObj, ok1 := e.ObjectOld.(*capsulev1beta2.CapsuleConfiguration)
		newObj, ok2 := e.ObjectNew.(*capsulev1beta2.CapsuleConfiguration)
		if !ok1 || !ok2 {
			return false
		}

		if len(oldObj.Spec.Administrators) != len(newObj.Spec.Administrators) {
			return true
		}

		return false
	},

	CreateFunc:  func(e event.CreateEvent) bool { return false },
	DeleteFunc:  func(e event.DeleteEvent) bool { return false },
	GenericFunc: func(e event.GenericEvent) bool { return false },
}

var PromotedServiceaccountPredicate = predicate.TypedFuncs[client.Object]{
	CreateFunc: func(e event.TypedCreateEvent[client.Object]) bool {
		v, ok := e.Object.GetLabels()[meta.OwnerPromotionLabel]

		return ok && v == meta.OwnerPromotionLabelTrigger
	},

	DeleteFunc: func(e event.TypedDeleteEvent[client.Object]) bool {
		v, ok := e.Object.GetLabels()[meta.OwnerPromotionLabel]

		return ok && v == meta.OwnerPromotionLabelTrigger
	},

	UpdateFunc: func(e event.TypedUpdateEvent[client.Object]) bool {
		oldVal, oldOK := e.ObjectOld.GetLabels()[meta.OwnerPromotionLabel]
		newVal, newOK := e.ObjectNew.GetLabels()[meta.OwnerPromotionLabel]

		return oldOK != newOK || oldVal != newVal
	},

	GenericFunc: func(event.TypedGenericEvent[client.Object]) bool {
		return false
	},
}

var UpdatedMetadataPredicate = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool { return true },
	DeleteFunc: func(e event.DeleteEvent) bool { return true },

	UpdateFunc: func(e event.UpdateEvent) bool {
		return !labelsEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels())
	},

	GenericFunc: func(e event.GenericEvent) bool { return false },
}

func labelsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}

	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}

	return true
}

func LabelsChanged(keys []string, oldLabels, newLabels map[string]string) bool {
	for _, key := range keys {
		oldVal, oldOK := oldLabels[key]
		newVal, newOK := newLabels[key]

		if oldOK != newOK || oldVal != newVal {
			return true
		}
	}

	return false
}

func NamesMatchingPredicate(names ...string) builder.Predicates {
	return builder.WithPredicates(predicate.NewPredicateFuncs(func(object client.Object) bool {
		for _, name := range names {
			if object.GetName() == name {
				return true
			}
		}

		return false
	}))
}
