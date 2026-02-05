// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

type LabelsMatchingPredicate struct {
	Match map[string]string
}

func (p LabelsMatchingPredicate) Create(e event.CreateEvent) bool {
	return p.matches(e.Object)
}

func (p LabelsMatchingPredicate) Delete(e event.DeleteEvent) bool {
	return p.matches(e.Object)
}

func (p LabelsMatchingPredicate) Generic(e event.GenericEvent) bool {
	return p.matches(e.Object)
}

func (p LabelsMatchingPredicate) Update(e event.UpdateEvent) bool {
	return p.matches(e.ObjectNew)
}

func (p LabelsMatchingPredicate) matches(obj client.Object) bool {
	if obj == nil {
		return false
	}

	if len(p.Match) == 0 {
		return true
	}

	labels := obj.GetLabels()
	if labels == nil {
		return false
	}
	for k, v := range p.Match {
		if labels[k] != v {
			return false
		}
	}

	return true
}

func LabelsMatching(match map[string]string) builder.Predicates {
	return builder.WithPredicates(LabelsMatchingPredicate{Match: match})
}
