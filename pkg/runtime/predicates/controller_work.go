// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"reflect"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capmeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/utils"
)

func ClassChanged() predicate.Predicate {
	return predicate.Or(
		predicate.GenerationChangedPredicate{},
		UpdatedMetadataPredicate{},
		DeletionChangedPredicate{},
	)
}

// TenantManagedResourceChangedPredicate admits drift in fields reconciled by
// the Tenant controller while filtering status-only updates. Several built-in
// resources do not reliably increment metadata.generation when their desired
// fields change, so GenerationChangedPredicate alone cannot detect tampering.
type TenantManagedResourceChangedPredicate struct{ predicate.Funcs }

func (TenantManagedResourceChangedPredicate) Create(event.CreateEvent) bool   { return true }
func (TenantManagedResourceChangedPredicate) Delete(event.DeleteEvent) bool   { return true }
func (TenantManagedResourceChangedPredicate) Generic(event.GenericEvent) bool { return false }
func (TenantManagedResourceChangedPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	if !utils.MapEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels()) ||
		!utils.MapEqual(e.ObjectOld.GetAnnotations(), e.ObjectNew.GetAnnotations()) ||
		!reflect.DeepEqual(e.ObjectOld.GetOwnerReferences(), e.ObjectNew.GetOwnerReferences()) ||
		!reflect.DeepEqual(e.ObjectOld.GetFinalizers(), e.ObjectNew.GetFinalizers()) ||
		(e.ObjectOld.GetDeletionTimestamp() == nil) != (e.ObjectNew.GetDeletionTimestamp() == nil) {
		return true
	}

	switch oldObject := e.ObjectOld.(type) {
	case *corev1.LimitRange:
		newObject, ok := e.ObjectNew.(*corev1.LimitRange)

		return ok && !reflect.DeepEqual(oldObject.Spec, newObject.Spec)
	case *corev1.ResourceQuota:
		newObject, ok := e.ObjectNew.(*corev1.ResourceQuota)

		return ok && !reflect.DeepEqual(oldObject.Spec, newObject.Spec)
	case *networkingv1.NetworkPolicy:
		newObject, ok := e.ObjectNew.(*networkingv1.NetworkPolicy)

		return ok && !reflect.DeepEqual(oldObject.Spec, newObject.Spec)
	case *rbacv1.RoleBinding:
		newObject, ok := e.ObjectNew.(*rbacv1.RoleBinding)

		return ok && (!reflect.DeepEqual(oldObject.RoleRef, newObject.RoleRef) ||
			!reflect.DeepEqual(oldObject.Subjects, newObject.Subjects))
	default:
		return false
	}
}

type NamespaceTenantStateChangedPredicate struct{ predicate.Funcs }

func (NamespaceTenantStateChangedPredicate) Create(event.CreateEvent) bool   { return true }
func (NamespaceTenantStateChangedPredicate) Delete(event.DeleteEvent) bool   { return true }
func (NamespaceTenantStateChangedPredicate) Generic(event.GenericEvent) bool { return false }
func (NamespaceTenantStateChangedPredicate) Update(e event.UpdateEvent) bool {
	oldNamespace, oldOK := e.ObjectOld.(*corev1.Namespace)

	newNamespace, newOK := e.ObjectNew.(*corev1.Namespace)

	if !oldOK || !newOK {
		return false
	}

	return !reflect.DeepEqual(oldNamespace.Labels, newNamespace.Labels) ||
		!reflect.DeepEqual(oldNamespace.Annotations, newNamespace.Annotations) ||
		!reflect.DeepEqual(oldNamespace.OwnerReferences, newNamespace.OwnerReferences) ||
		(oldNamespace.DeletionTimestamp == nil) != (newNamespace.DeletionTimestamp == nil)
}

type QuantityLedgerWorkChangedPredicate struct{ predicate.Funcs }

func (QuantityLedgerWorkChangedPredicate) Create(event.CreateEvent) bool   { return true }
func (QuantityLedgerWorkChangedPredicate) Delete(event.DeleteEvent) bool   { return true }
func (QuantityLedgerWorkChangedPredicate) Generic(event.GenericEvent) bool { return false }
func (QuantityLedgerWorkChangedPredicate) Update(e event.UpdateEvent) bool {
	oldLedger, oldOK := e.ObjectOld.(*capsulev1beta2.QuantityLedger)

	newLedger, newOK := e.ObjectNew.(*capsulev1beta2.QuantityLedger)

	if !oldOK || !newOK {
		return false
	}

	return oldLedger.Generation != newLedger.Generation ||
		!reflect.DeepEqual(oldLedger.Status.Reservations, newLedger.Status.Reservations) ||
		!reflect.DeepEqual(oldLedger.Status.PendingDeletes, newLedger.Status.PendingDeletes)
}

type ProvisionerSubjectsChangedPredicate struct{ predicate.Funcs }

func (ProvisionerSubjectsChangedPredicate) Create(event.CreateEvent) bool   { return true }
func (ProvisionerSubjectsChangedPredicate) Delete(event.DeleteEvent) bool   { return false }
func (ProvisionerSubjectsChangedPredicate) Generic(event.GenericEvent) bool { return false }
func (ProvisionerSubjectsChangedPredicate) Update(e event.UpdateEvent) bool {
	oldConfig, oldOK := e.ObjectOld.(*capsulev1beta2.CapsuleConfiguration)

	newConfig, newOK := e.ObjectNew.(*capsulev1beta2.CapsuleConfiguration)

	if !oldOK || !newOK {
		return false
	}

	return !reflect.DeepEqual(oldConfig.Spec.Administrators, newConfig.Spec.Administrators) ||
		!reflect.DeepEqual(oldConfig.Status.Users, newConfig.Status.Users) ||
		oldConfig.Spec.AllowServiceAccountPromotion != newConfig.Spec.AllowServiceAccountPromotion ||
		!reflect.DeepEqual(oldConfig.Spec.RBAC, newConfig.Spec.RBAC)
}

type ResourcePoolNamespacesChangedPredicate struct{ predicate.Funcs }

func (ResourcePoolNamespacesChangedPredicate) Create(event.CreateEvent) bool   { return true }
func (ResourcePoolNamespacesChangedPredicate) Delete(event.DeleteEvent) bool   { return true }
func (ResourcePoolNamespacesChangedPredicate) Generic(event.GenericEvent) bool { return false }
func (ResourcePoolNamespacesChangedPredicate) Update(e event.UpdateEvent) bool {
	oldPool, oldOK := e.ObjectOld.(*capsulev1beta2.ResourcePool)

	newPool, newOK := e.ObjectNew.(*capsulev1beta2.ResourcePool)

	if !oldOK || !newOK {
		return false
	}

	return !reflect.DeepEqual(oldPool.Status.Namespaces, newPool.Status.Namespaces) ||
		(oldPool.DeletionTimestamp == nil) != (newPool.DeletionTimestamp == nil)
}

// ResourceQuotaUsageChangedPredicate admits only usage changes from the quota
// controller. ResourcePool claim Bound conditions are derived from status.used,
// while status.hard and other status updates do not affect claim scheduling.
type ResourceQuotaUsageChangedPredicate struct{ predicate.Funcs }

func (ResourceQuotaUsageChangedPredicate) Create(event.CreateEvent) bool   { return false }
func (ResourceQuotaUsageChangedPredicate) Delete(event.DeleteEvent) bool   { return false }
func (ResourceQuotaUsageChangedPredicate) Generic(event.GenericEvent) bool { return false }
func (ResourceQuotaUsageChangedPredicate) Update(e event.UpdateEvent) bool {
	oldQuota, oldOK := e.ObjectOld.(*corev1.ResourceQuota)

	newQuota, newOK := e.ObjectNew.(*corev1.ResourceQuota)

	if !oldOK || !newOK {
		return false
	}

	return !reflect.DeepEqual(oldQuota.Status.Used, newQuota.Status.Used)
}

type DependencyStateChangedPredicate struct{ predicate.Funcs }

func (DependencyStateChangedPredicate) Create(event.CreateEvent) bool   { return true }
func (DependencyStateChangedPredicate) Delete(event.DeleteEvent) bool   { return true }
func (DependencyStateChangedPredicate) Generic(event.GenericEvent) bool { return false }
func (DependencyStateChangedPredicate) Update(e event.UpdateEvent) bool {
	return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() ||
		(e.ObjectOld.GetDeletionTimestamp() == nil) != (e.ObjectNew.GetDeletionTimestamp() == nil) ||
		!reflect.DeepEqual(resourceReadyCondition(e.ObjectOld), resourceReadyCondition(e.ObjectNew))
}

type TenantSelectionChangedPredicate struct{ predicate.Funcs }

func (TenantSelectionChangedPredicate) Create(event.CreateEvent) bool   { return true }
func (TenantSelectionChangedPredicate) Delete(event.DeleteEvent) bool   { return true }
func (TenantSelectionChangedPredicate) Generic(event.GenericEvent) bool { return false }
func (TenantSelectionChangedPredicate) Update(e event.UpdateEvent) bool {
	oldTenant, oldOK := e.ObjectOld.(*capsulev1beta2.Tenant)

	newTenant, newOK := e.ObjectNew.(*capsulev1beta2.Tenant)

	if !oldOK || !newOK {
		return false
	}

	return !utils.MapEqual(oldTenant.Labels, newTenant.Labels) ||
		!reflect.DeepEqual(oldTenant.Status.Namespaces, newTenant.Status.Namespaces) ||
		(oldTenant.DeletionTimestamp == nil) != (newTenant.DeletionTimestamp == nil)
}

type TenantPodOptionsChangedPredicate struct{ predicate.Funcs }

func (TenantPodOptionsChangedPredicate) Create(event.CreateEvent) bool   { return false }
func (TenantPodOptionsChangedPredicate) Delete(event.DeleteEvent) bool   { return false }
func (TenantPodOptionsChangedPredicate) Generic(event.GenericEvent) bool { return false }
func (TenantPodOptionsChangedPredicate) Update(e event.UpdateEvent) bool {
	oldTenant, oldOK := e.ObjectOld.(*capsulev1beta2.Tenant)
	newTenant, newOK := e.ObjectNew.(*capsulev1beta2.Tenant)

	return oldOK && newOK && !reflect.DeepEqual(oldTenant.Spec.PodOptions, newTenant.Spec.PodOptions)
}

type TenantServiceOptionsChangedPredicate struct{ predicate.Funcs }

func (TenantServiceOptionsChangedPredicate) Create(event.CreateEvent) bool   { return false }
func (TenantServiceOptionsChangedPredicate) Delete(event.DeleteEvent) bool   { return false }
func (TenantServiceOptionsChangedPredicate) Generic(event.GenericEvent) bool { return false }
func (TenantServiceOptionsChangedPredicate) Update(e event.UpdateEvent) bool {
	oldTenant, oldOK := e.ObjectOld.(*capsulev1beta2.Tenant)
	newTenant, newOK := e.ObjectNew.(*capsulev1beta2.Tenant)

	return oldOK && newOK && !reflect.DeepEqual(oldTenant.Spec.ServiceOptions, newTenant.Spec.ServiceOptions)
}

type TenantNamespacesChangedPredicate struct{ predicate.Funcs }

func (TenantNamespacesChangedPredicate) Create(event.CreateEvent) bool   { return true }
func (TenantNamespacesChangedPredicate) Delete(event.DeleteEvent) bool   { return true }
func (TenantNamespacesChangedPredicate) Generic(event.GenericEvent) bool { return false }
func (TenantNamespacesChangedPredicate) Update(e event.UpdateEvent) bool {
	oldTenant, oldOK := e.ObjectOld.(*capsulev1beta2.Tenant)
	newTenant, newOK := e.ObjectNew.(*capsulev1beta2.Tenant)

	return oldOK && newOK && !reflect.DeepEqual(oldTenant.Status.Namespaces, newTenant.Status.Namespaces)
}

type NamespaceMetadataChangedPredicate struct{ predicate.Funcs }

func (NamespaceMetadataChangedPredicate) Create(event.CreateEvent) bool   { return true }
func (NamespaceMetadataChangedPredicate) Delete(event.DeleteEvent) bool   { return true }
func (NamespaceMetadataChangedPredicate) Generic(event.GenericEvent) bool { return false }
func (NamespaceMetadataChangedPredicate) Update(e event.UpdateEvent) bool {
	oldNamespace, oldOK := e.ObjectOld.(*corev1.Namespace)
	newNamespace, newOK := e.ObjectNew.(*corev1.Namespace)

	return oldOK && newOK && (!reflect.DeepEqual(oldNamespace.Labels, newNamespace.Labels) ||
		!reflect.DeepEqual(oldNamespace.Annotations, newNamespace.Annotations))
}

func resourceReadyCondition(obj any) any {
	switch typed := obj.(type) {
	case *capsulev1beta2.GlobalTenantResource:
		return typed.Status.Conditions.GetConditionByType(capmeta.ReadyCondition)
	case *capsulev1beta2.TenantResource:
		return typed.Status.Conditions.GetConditionByType(capmeta.ReadyCondition)
	default:
		return nil
	}
}
