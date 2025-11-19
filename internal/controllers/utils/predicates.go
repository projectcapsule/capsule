// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var UpdatedMetadataPredicate = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool { return true },
	DeleteFunc: func(e event.DeleteEvent) bool { return true },

	UpdateFunc: func(e event.UpdateEvent) bool {
		oldLabels := e.ObjectOld.GetLabels()
		newLabels := e.ObjectNew.GetLabels()

		return !reflect.DeepEqual(oldLabels, newLabels)
	},

	GenericFunc: func(e event.GenericEvent) bool { return false },
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
