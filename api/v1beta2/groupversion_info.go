// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

// Package v1beta2 contains API Schema definitions for the capsule v1beta2 API group
// +kubebuilder:object:generate=true
// +groupName=capsule.clastix.io
package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "capsule.clastix.io", Version: "v1beta2"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&CapsuleConfiguration{},
		&CapsuleConfigurationList{},
		&CustomQuota{},
		&CustomQuotaList{},
		&GlobalCustomQuota{},
		&GlobalCustomQuotaList{},
		&GlobalTenantResource{},
		&GlobalTenantResourceList{},
		&QuantityLedger{},
		&QuantityLedgerList{},
		&ResourcePool{},
		&ResourcePoolList{},
		&ResourcePoolClaim{},
		&ResourcePoolClaimList{},
		&RuleStatus{},
		&RuleStatusList{},
		&Tenant{},
		&TenantList{},
		&TenantOwner{},
		&TenantOwnerList{},
		&TenantResource{},
		&TenantResourceList{},
	)

	metav1.AddToGroupVersion(scheme, GroupVersion)

	return nil
}
