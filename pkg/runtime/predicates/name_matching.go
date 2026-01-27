// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"slices"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

type NamesMatchingPredicate struct {
	Names []string
}

func (p NamesMatchingPredicate) Create(e event.CreateEvent) bool   { return p.matches(e.Object) }
func (p NamesMatchingPredicate) Delete(e event.DeleteEvent) bool   { return p.matches(e.Object) }
func (p NamesMatchingPredicate) Generic(e event.GenericEvent) bool { return p.matches(e.Object) }
func (p NamesMatchingPredicate) Update(e event.UpdateEvent) bool   { return p.matches(e.ObjectNew) }

func (p NamesMatchingPredicate) matches(obj client.Object) bool {
	if obj == nil {
		return false
	}

	return slices.Contains(p.Names, obj.GetName())
}

func NamesMatching(names ...string) builder.Predicates {
	return builder.WithPredicates(NamesMatchingPredicate{Names: names})
}
