// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

type PromotedServiceaccountPredicate struct{}

func (PromotedServiceaccountPredicate) Generic(event.GenericEvent) bool { return false }

func (PromotedServiceaccountPredicate) Create(e event.CreateEvent) bool {
	if e.Object == nil {
		return false
	}

	v, ok := e.Object.GetLabels()[meta.OwnerPromotionLabel]

	return ok && v == meta.ValueTrue
}

func (PromotedServiceaccountPredicate) Delete(e event.DeleteEvent) bool {
	if e.Object == nil {
		return false
	}

	v, ok := e.Object.GetLabels()[meta.OwnerPromotionLabel]

	return ok && v == meta.ValueTrue
}

func (PromotedServiceaccountPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	oldVal, oldOK := e.ObjectOld.GetLabels()[meta.OwnerPromotionLabel]
	newVal, newOK := e.ObjectNew.GetLabels()[meta.OwnerPromotionLabel]

	return oldOK != newOK || oldVal != newVal
}
