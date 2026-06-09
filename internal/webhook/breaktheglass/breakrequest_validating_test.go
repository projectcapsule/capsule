// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	gm "go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mc "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/internal/webhook/test"
)

func TestBreakRequestValidationHandler(t *testing.T) {
	defaultTemplateName := "foo"
	alternateTemplateName := "bar"
	ctx := context.Background()
	log := ctrl.Log.WithName("test")

	t.Run("OnCreate", func(t *testing.T) {
		tests := []struct {
			name     string
			br       *capsulev1beta2.BreakRequest
			setup    func(reader *mc.MockReader)
			expected int32
			errMsg   string
		}{
			{
				name: "deny if template not found",
				br: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						TemplateName: defaultTemplateName,
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().
						Get(gm.Any(), client.ObjectKey{Name: defaultTemplateName}, gm.Any(), gm.Any()).
						Return(errors.New("not found"))
				},
				expected: http.StatusInternalServerError,
				errMsg:   "error loading template foo: not found",
			},
			{
				name: "allow if template found",
				br: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						TemplateName: alternateTemplateName,
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().
						Get(gm.Any(), client.ObjectKey{Name: alternateTemplateName}, gm.Any(), gm.Any()).
						Return(nil)
				},
				expected: 0, // allowed
			},
			{
				name: "deny if duration exceeds maxDuration",
				br: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						TemplateName: defaultTemplateName,
						Duration:     metav1.Duration{Duration: time.Hour},
					},
				},
				setup: func(reader *mc.MockReader) {
					reader.EXPECT().
						Get(gm.Any(), client.ObjectKey{Name: defaultTemplateName}, gm.Any(), gm.Any()).
						Do(func(_ any, _ any, brt *capsulev1beta2.BreakRequestTemplate, _ ...any) {
							brt.Spec.MaxDuration.Duration = time.Minute
						})
				},
				expected: http.StatusForbidden,
				errMsg:   "requested duration 1h0m0s exceeds template maxDuration 1m0s",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mockCtrl := gm.NewController(t)
				defer mockCtrl.Finish()
				reader := mc.NewMockReader(mockCtrl)
				decoder := &test.Decoder[*capsulev1beta2.BreakRequest]{
					Object: tt.br,
				}
				validator := BreakRequestValidationHandler(log)

				if tt.setup != nil {
					tt.setup(reader)
				}

				resp := validator.OnCreate(nil, reader, decoder, nil)(ctx, admission.Request{})
				if tt.expected == 0 {
					assert.Nil(t, resp)
				} else {
					test.VerifyResponse(t, resp, tt.expected, tt.errMsg)
				}
			})
		}
	})

	t.Run("OnUpdate", func(t *testing.T) {
		tests := []struct {
			name     string
			oldBr    *capsulev1beta2.BreakRequest
			newBr    *capsulev1beta2.BreakRequest
			expected int32
			errMsg   string
		}{
			{
				name: "allow if templateName not changed",
				oldBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						TemplateName: defaultTemplateName,
					},
				},
				newBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						TemplateName: defaultTemplateName,
					},
				},
				expected: 0,
			},
			{
				name: "deny if templateName changed",
				oldBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						TemplateName: defaultTemplateName,
					},
				},
				newBr: &capsulev1beta2.BreakRequest{
					Spec: capsulev1beta2.BreakRequestSpec{
						TemplateName: alternateTemplateName,
					},
				},
				expected: http.StatusForbidden,
				errMsg:   "templateName cannot be changed. old: foo, new: bar",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				decoder := &test.Decoder[*capsulev1beta2.BreakRequest]{
					Object:    tt.newBr,
					OldObject: tt.oldBr,
				}
				validator := BreakRequestValidationHandler(log)

				resp := validator.OnUpdate(nil, nil, decoder, nil)(ctx, admission.Request{})
				if tt.expected == 0 {
					assert.Nil(t, resp)
				} else {
					test.VerifyResponse(t, resp, tt.expected, tt.errMsg)
				}
			})
		}
	})
}
