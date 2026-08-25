// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	webhooktest "github.com/projectcapsule/capsule/internal/webhook/test"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/users"
)

func TestBreakTheGlassResourceHandler(t *testing.T) {
	t.Setenv(configuration.EnvironmentServiceaccountName, "capsule-controller")
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")

	handler := BreakTheGlassResourceHandler()

	t.Run("controller may update", func(t *testing.T) {
		resp := handler.OnUpdate(nil, nil, nil, nil)(context.Background(), admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				UserInfo: users.ServiceAccountUserInfo("capsule-system", "capsule-controller"),
			},
		})
		if resp != nil {
			t.Fatalf("expected controller request to be allowed, got %#v", resp)
		}
	})

	t.Run("user may not update", func(t *testing.T) {
		resp := handler.OnUpdate(nil, nil, nil, nil)(context.Background(), admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				UserInfo: authenticationv1.UserInfo{Username: "alice"},
			},
		})
		webhooktest.VerifyResponse(t, resp, 403, "can only be changed by the Capsule controller")
	})

	t.Run("user may not delete", func(t *testing.T) {
		resp := handler.OnDelete(nil, nil, nil, nil)(context.Background(), admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				UserInfo: authenticationv1.UserInfo{Username: "alice"},
			},
		})
		webhooktest.VerifyResponse(t, resp, 403, "can only be changed by the Capsule controller")
	})
}
