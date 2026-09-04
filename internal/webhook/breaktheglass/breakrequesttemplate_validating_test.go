// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/webhook/test"
	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestBreakRequestTemplateValidationHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template *capsulev1beta2.BreakRequestTemplate
		expected int32
		message  string
	}{
		{
			name: "valid template",
			template: &capsulev1beta2.BreakRequestTemplate{Spec: capsulev1beta2.BreakRequestTemplateSpec{
				Approvals: breaktheglass.ApprovalSpec{Conditions: []string{`requestor.name == "alice"`}},
				Resources: []apiruntime.ResourceTemplate{{Targets: []runtime.RawExtension{{Object: &corev1.ConfigMap{}}}}},
			}},
		},
		{
			name: "invalid condition",
			template: &capsulev1beta2.BreakRequestTemplate{Spec: capsulev1beta2.BreakRequestTemplateSpec{
				Approvals: breaktheglass.ApprovalSpec{Conditions: []string{`unknown.name == "alice"`}},
				Resources: []apiruntime.ResourceTemplate{{Targets: []runtime.RawExtension{{Object: &corev1.ConfigMap{}}}}},
			}},
			expected: http.StatusForbidden,
			message:  "approval conditions are invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoder := &test.Decoder[*capsulev1beta2.BreakRequestTemplate]{Object: tt.template}
			handler := BreakRequestTemplateValidationHandler(ctrl.Log.WithName("test"))
			response := handler.OnCreate(nil, nil, decoder, nil)(context.Background(), admission.Request{})
			if tt.expected == 0 {
				assert.Nil(t, response)
				return
			}

			test.VerifyResponse(t, response, tt.expected, tt.message)
		})
	}
}
