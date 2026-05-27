// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"sigs.k8s.io/controller-runtime/pkg/event"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

type TenantStatusOwnersChangedPredicate struct{}

func (TenantStatusOwnersChangedPredicate) Create(event.CreateEvent) bool   { return false }
func (TenantStatusOwnersChangedPredicate) Delete(event.DeleteEvent) bool   { return false }
func (TenantStatusOwnersChangedPredicate) Generic(event.GenericEvent) bool { return false }

func (TenantStatusOwnersChangedPredicate) Update(e event.UpdateEvent) bool {
	oldObj, ok1 := e.ObjectOld.(*capsulev1beta2.Tenant)
	newObj, ok2 := e.ObjectNew.(*capsulev1beta2.Tenant)

	if !ok1 || !ok2 {
		return false
	}

	return ownersChanged(oldObj.Status.Owners, newObj.Status.Owners)
}

func ownersChanged(a, b rbac.OwnerStatusListSpec) bool {
	if len(a) != len(b) {
		return true
	}

	for i := range a {
		if a[i].Name == b[i].Name && a[i].Kind == b[i].Kind {
			return true
		}
	}

	return false
}
