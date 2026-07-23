// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package handlers_test

import (
	"context"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

func TestTypedTenantWithRulesetHandlerSkipsDelete(t *testing.T) {
	t.Parallel()

	handler := &handlers.TypedTenantWithRulesetHandler[*corev1.ConfigMap]{
		Factory: func() *corev1.ConfigMap { return &corev1.ConfigMap{} },
	}
	request := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Delete,
		Namespace: "solar-system",
	}}

	if response := handler.OnDelete(nil, nil, nil, nil)(context.Background(), request); response != nil {
		t.Fatalf("OnDelete() response = %#v, want nil", response)
	}
}
