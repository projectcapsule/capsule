// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ResourcesLabel = "capsule.clastix.io/resources"

	TenantNameLabel = "kubernetes.io/metadata.name"

	TenantLabel    = "capsule.clastix.io/tenant"
	NewTenantLabel = "projectcapsule.dev/tenant"

	ResourcePoolLabel = "projectcapsule.dev/pool"

	FreezeLabel = "projectcapsule.dev/freeze"

	OwnerPromotionLabel = "owner.projectcapsule.dev/promote"

	CordonedLabel = "projectcapsule.dev/cordoned"

	CapsuleNameLabel = "projectcapsule.dev/name"

	CreatedByCapsuleLabel = "projectcapsule.dev/created-by"
	CustomResourcesLabel  = "projectcapsule.dev/custom-resources"

	NewManagedByCapsuleLabel = "projectcapsule.dev/managed-by"
	ManagedByCapsuleLabel    = "capsule.clastix.io/managed-by"

	LimitRangeLabel    = "capsule.clastix.io/limit-range"
	NetworkPolicyLabel = "capsule.clastix.io/network-policy"
	ResourceQuotaLabel = "capsule.clastix.io/resource-quota"
	RolebindingLabel   = "capsule.clastix.io/role-binding"
)

func FreezeLabelTriggers(obj client.Object) bool {
	return labelTriggers(obj, FreezeLabel, ValueTrue)
}

func FreezeLabelRemove(obj client.Object) {
	labelRemove(obj, FreezeLabel)
}

func OwnerPromotionLabelTriggers(obj client.Object) bool {
	return labelTriggers(obj, OwnerPromotionLabel, ValueTrue)
}

func OwnerPromotionLabelRemove(obj client.Object) {
	labelRemove(obj, OwnerPromotionLabel)
}

func labelRemove(obj client.Object, anno string) {
	annotations := obj.GetLabels()

	if _, ok := annotations[anno]; ok {
		delete(annotations, anno)

		obj.SetLabels(annotations)
	}
}

func labelTriggers(obj client.Object, anno string, trigger string) bool {
	annotations := obj.GetLabels()

	if val, ok := annotations[anno]; ok {
		if strings.ToLower(val) == trigger {
			return true
		}
	}

	return false
}

// SetFilteredLabels Removes given labels by key.
func SetFilteredLabels(obj *unstructured.Unstructured, filter map[string]struct{}) {
	if obj == nil || len(filter) == 0 {
		return
	}

	labels := obj.GetLabels()
	if labels == nil {
		return
	}

	for k := range labels {
		if _, reserved := filter[k]; reserved {
			delete(labels, k)
		}
	}

	obj.SetLabels(labels)
}

// LabelsChanged indicates if the given label keys have changed.
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
