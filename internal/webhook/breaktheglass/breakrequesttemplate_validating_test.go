// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	gm "go.uber.org/mock/gomock"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mc "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/internal/webhook/test"
)

func TestBreakRequestTemplateValidationHandler(t *testing.T) {
	ctx := context.Background()
	log := ctrl.Log.WithName("test")

	tests := []struct {
		name     string
		brt      *capsulev1beta2.BreakRequestTemplate
		setup    func(cl *mc.MockClient)
		expected int32
		errMsg   string
	}{
		{
			name: "deny if autoApprove is false but approvalCondition is set",
			brt: &capsulev1beta2.BreakRequestTemplate{
				Spec: capsulev1beta2.BreakRequestTemplateSpec{
					AutoApprove:       false,
					ApprovalCondition: "foo",
					Templates:         []runtime.RawExtension{{Object: &corev1.ConfigMap{}}},
				},
			},
			expected: http.StatusForbidden,
			errMsg:   "approvalCondition should not be set when autoApprove is false",
		},
		{
			name: "allow if autoApprove is true and condition is empty",
			brt: &capsulev1beta2.BreakRequestTemplate{
				Spec: capsulev1beta2.BreakRequestTemplateSpec{
					AutoApprove: true,
					Templates:   []runtime.RawExtension{{Object: &corev1.ConfigMap{}}},
				},
			},
			setup: func(cl *mc.MockClient) {
				cl.EXPECT().Create(gm.Any(), gm.Any(), gm.Any()).DoAndReturn(func(ctx context.Context, review *authorizationv1.SelfSubjectAccessReview, _ ...client.CreateOption) error {
					review.Status.Allowed = true
					return nil
				}).AnyTimes()
			},
			expected: 0,
		},
		{
			name: "deny if approvalCondition is invalid",
			brt: &capsulev1beta2.BreakRequestTemplate{
				Spec: capsulev1beta2.BreakRequestTemplateSpec{
					AutoApprove:       true,
					ApprovalCondition: "foo.spec.reason == 'test'",
					Templates:         []runtime.RawExtension{{Object: &corev1.ConfigMap{}}},
				},
			},
			expected: http.StatusForbidden,
			errMsg:   "approvalCondition is invalid: ERROR: <input>:1:1: undeclared reference to 'foo'",
		},
		{
			name: "allow if approvalCondition is valid",
			brt: &capsulev1beta2.BreakRequestTemplate{
				Spec: capsulev1beta2.BreakRequestTemplateSpec{
					AutoApprove:       true,
					ApprovalCondition: "request.spec.reason == 'test'",
					Templates:         []runtime.RawExtension{{Object: &corev1.ConfigMap{}}},
				},
			},
			setup: func(cl *mc.MockClient) {
				cl.EXPECT().Create(gm.Any(), gm.Any(), gm.Any()).DoAndReturn(func(ctx context.Context, review *authorizationv1.SelfSubjectAccessReview, _ ...client.CreateOption) error {
					review.Status.Allowed = true
					return nil
				}).AnyTimes()
			},
			expected: 0,
		},
		{
			name: "allow if item schema is valid",
			brt: &capsulev1beta2.BreakRequestTemplate{
				Spec: capsulev1beta2.BreakRequestTemplateSpec{
					Templates:   []runtime.RawExtension{{Object: &corev1.ConfigMap{}}},
					ParamSchema: runtime.RawExtension{Raw: []byte(`{"type": "string"}`)},
				},
			},
			setup: func(cl *mc.MockClient) {
				cl.EXPECT().Create(gm.Any(), gm.Any(), gm.Any()).DoAndReturn(func(ctx context.Context, review *authorizationv1.SelfSubjectAccessReview, _ ...client.CreateOption) error {
					review.Status.Allowed = true
					return nil
				}).AnyTimes()
			},
			expected: 0,
		},
		{
			name: "deny if item schema is invalid",
			brt: &capsulev1beta2.BreakRequestTemplate{
				Spec: capsulev1beta2.BreakRequestTemplateSpec{
					Templates:   []runtime.RawExtension{{Object: &corev1.ConfigMap{}}},
					ParamSchema: runtime.RawExtension{Raw: []byte(`"type": `)},
				},
			},
			expected: http.StatusForbidden,
			errMsg:   `invalid templates: paramSchema is invalid: failed to validate OpenAPI schemaData: schema invalid`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gm.NewController(t)
			defer mockCtrl.Finish()

			cl := mc.NewMockClient(mockCtrl)
			decoder := &test.Decoder[*capsulev1beta2.BreakRequestTemplate]{
				Object: tt.brt,
			}
			validator := BreakRequestTemplateValidationHandler(log)

			if tt.setup != nil {
				tt.setup(cl)
			}

			resp := validator.OnCreate(cl, nil, decoder, nil)(ctx, admission.Request{})
			if tt.expected == 0 {
				assert.Nil(t, resp)
			} else {
				test.VerifyResponse(t, resp, tt.expected, tt.errMsg)
			}

			resp = validator.OnUpdate(cl, nil, decoder, nil)(ctx, admission.Request{})
			if tt.expected == 0 {
				assert.Nil(t, resp)
			} else {
				test.VerifyResponse(t, resp, tt.expected, tt.errMsg)
			}
		})
	}
}
