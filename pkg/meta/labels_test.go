// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestFreezeLabel(t *testing.T) {
	ns := &corev1.Namespace{}
	ns.SetLabels(map[string]string{})

	// absent
	if FreezeLabelTriggers(ns) {
		t.Errorf("expected FreezeLabelTriggers to be false when label is absent")
	}

	// set to trigger
	ns.Labels[FreezeLabel] = FreezeLabelTrigger
	if !FreezeLabelTriggers(ns) {
		t.Errorf("expected FreezeLabelTriggers to be true when label is set to trigger")
	}

	ns.Labels[FreezeLabel] = "false"
	if FreezeLabelTriggers(ns) {
		t.Errorf("expected FreezeLabelTriggers to be false when label is not set to trigger")
	}

	// remove
	FreezeLabelRemove(ns)
	if _, ok := ns.Labels[FreezeLabel]; ok {
		t.Errorf("expected FreezeLabel to be removed")
	}
}

func TestOwnerPromotionLabel(t *testing.T) {
	ns := &corev1.Namespace{}
	ns.SetLabels(map[string]string{})

	if OwnerPromotionLabelTriggers(ns) {
		t.Errorf("expected OwnerPromotionLabelTriggers to be false when label is absent")
	}

	ns.Labels[OwnerPromotionLabel] = OwnerPromotionLabelTrigger
	if !OwnerPromotionLabelTriggers(ns) {
		t.Errorf("expected OwnerPromotionLabelTriggers to be true when label is set to trigger")
	}

	ns.Labels[OwnerPromotionLabel] = "false"
	if OwnerPromotionLabelTriggers(ns) {
		t.Errorf("expected OwnerPromotionLabelTriggers to be false when label is not set to trigger")
	}

	OwnerPromotionLabelRemove(ns)
	if _, ok := ns.Labels[OwnerPromotionLabel]; ok {
		t.Errorf("expected OwnerPromotionLabel to be removed")
	}
}
