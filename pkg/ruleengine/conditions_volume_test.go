// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine_test

import (
	"reflect"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/cel/environment"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
)

func TestVolumeConditionContext(t *testing.T) {
	compiler := conditionCache(t)
	volume := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "restored", Labels: map[string]string{"pool": "restore"}}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Name: "destination-copy", Namespace: "openshift-adp"}}}
	original := volume.DeepCopy()
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Namespace: "destination"}}
	condition := []rules.AdmissionCondition{{Expression: `volume != null && has(volume.spec.claimRef) && volume.spec.claimRef.namespace == 'openshift-adp' && volume.spec.claimRef.name.startsWith(object.metadata.namespace + '-')`}}
	request := admissionv1.AdmissionRequest{Namespace: claim.Namespace}
	e := ruleengine.NewConditionEvaluator(compiler, request).WithVolume(volume)
	if matched, err := e.Matches(t.Context(), claim, condition); err != nil || !matched {
		t.Fatalf("matched=%v error=%v", matched, err)
	}
	if matched, err := ruleengine.NewConditionEvaluator(compiler, request).Matches(t.Context(), claim, condition); err != nil || matched {
		t.Fatalf("volume leaked into a different request: matched=%v error=%v", matched, err)
	}
	if matched, err := e.WithVolume(nil).Matches(t.Context(), claim, []rules.AdmissionCondition{{Expression: "volume == null"}}); err != nil || !matched {
		t.Fatalf("cleared volume remained visible: matched=%v error=%v", matched, err)
	}
	if !reflect.DeepEqual(volume, original) {
		t.Fatal("condition changed PV")
	}
	if _, err := compiler.GetOrCompileBoolean("volume == null", environment.NewExpressions); err == nil {
		t.Fatal("volume leaked into quota environment")
	}
}
