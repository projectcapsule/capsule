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

	if v := e.Object.GetLabels()[meta.OwnerPromotionLabel]; v == meta.ValueTrue {
		return true
	}

	if v := e.Object.GetLabels()[meta.ServiceAccountPromotionLabel]; v == meta.ValueTrue {
		return true
	}

	return false
}

func (PromotedServiceaccountPredicate) Delete(e event.DeleteEvent) bool {
	if e.Object == nil {
		return false
	}

	if v := e.Object.GetLabels()[meta.OwnerPromotionLabel]; v == meta.ValueTrue {
		return true
	}

	if v := e.Object.GetLabels()[meta.ServiceAccountPromotionLabel]; v == meta.ValueTrue {
		return true
	}

	return false
}

func (PromotedServiceaccountPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	oldLabels := e.ObjectOld.GetLabels()
	newLabels := e.ObjectNew.GetLabels()

	return oldLabels[meta.OwnerPromotionLabel] != newLabels[meta.OwnerPromotionLabel] ||
		oldLabels[meta.ServiceAccountPromotionLabel] != newLabels[meta.ServiceAccountPromotionLabel]
}
