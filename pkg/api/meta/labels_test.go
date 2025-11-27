// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestFreezeLabel(t *testing.T) {
	ns := &corev1.Namespace{}
	ns.SetLabels(map[string]string{})

	// absent
	if meta.FreezeLabelTriggers(ns) {
		t.Errorf("expected FreezeLabelTriggers to be false when label is absent")
	}

	// set to trigger
	ns.Labels[meta.FreezeLabel] = meta.FreezeLabelTrigger
	if !meta.FreezeLabelTriggers(ns) {
		t.Errorf("expected FreezeLabelTriggers to be true when label is set to trigger")
	}

	ns.Labels[meta.FreezeLabel] = "false"
	if meta.FreezeLabelTriggers(ns) {
		t.Errorf("expected FreezeLabelTriggers to be false when label is not set to trigger")
	}

	// remove
	meta.FreezeLabelRemove(ns)
	if _, ok := ns.Labels[meta.FreezeLabel]; ok {
		t.Errorf("expected FreezeLabel to be removed")
	}
}

func TestOwnerPromotionLabel(t *testing.T) {
	ns := &corev1.Namespace{}
	ns.SetLabels(map[string]string{})

	if meta.OwnerPromotionLabelTriggers(ns) {
		t.Errorf("expected OwnerPromotionLabelTriggers to be false when label is absent")
	}

	ns.Labels[meta.OwnerPromotionLabel] = meta.OwnerPromotionLabelTrigger
	if !meta.OwnerPromotionLabelTriggers(ns) {
		t.Errorf("expected OwnerPromotionLabelTriggers to be true when label is set to trigger")
	}

	ns.Labels[meta.OwnerPromotionLabel] = "false"
	if meta.OwnerPromotionLabelTriggers(ns) {
		t.Errorf("expected OwnerPromotionLabelTriggers to be false when label is not set to trigger")
	}

	meta.OwnerPromotionLabelRemove(ns)
	if _, ok := ns.Labels[meta.OwnerPromotionLabel]; ok {
		t.Errorf("expected OwnerPromotionLabel to be removed")
	}
}
