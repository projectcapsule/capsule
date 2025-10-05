// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

import (
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	FreezeLabel        = "projectcapsule.dev/freeze"
	FreezeLabelTrigger = "true"

	OwnerPromotionLabel        = "owner.projectcapsule.dev/promote"
	OwnerPromotionLabelTrigger = "true"

	CordonedLabel        = "projectcapsule.dev/cordoned"
	CordonedLabelTrigger = "true"
)

func FreezeLabelTriggers(obj client.Object) bool {
	return labelTriggers(obj, FreezeLabel, FreezeLabelTrigger)
}

func FreezeLabelRemove(obj client.Object) {
	labelRemove(obj, FreezeLabel)
}

func OwnerPromotionLabelTriggers(obj client.Object) bool {
	return labelTriggers(obj, OwnerPromotionLabel, OwnerPromotionLabelTrigger)
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
