// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"context"
	"encoding/json"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/webhook/test"
	breaktheglassapi "github.com/projectcapsule/capsule/pkg/api/breaktheglass"
)

func TestBreakRequestMutationHandlerOnCreate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		username   string
		groups     []string
		entityType breaktheglassapi.AccessEntityType
	}{
		{
			name:       "user",
			username:   "alice",
			groups:     []string{"developers", "on-call"},
			entityType: breaktheglassapi.AccessEntityTypeUser,
		},
		{
			name:       "service account",
			username:   "system:serviceaccount:team-a:reviewer",
			groups:     []string{"system:serviceaccounts", "system:serviceaccounts:team-a"},
			entityType: breaktheglassapi.AccessEntityTypeServiceAccount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			br := &capsulev1beta2.BreakRequest{
				Spec: capsulev1beta2.BreakRequestSpec{
					Template: capsulev1beta2.BreakRequestTemplateReference{
						Kind: capsulev1beta2.BreakRequestTemplateKind,
						Name: "template",
					},
					Requestor: breaktheglassapi.AccessEntity{Name: "spoofed"},
				},
			}
			raw, err := json.Marshal(br)
			require.NoError(t, err)

			decoder := &test.Decoder[*capsulev1beta2.BreakRequest]{Object: br}
			req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Object: runtime.RawExtension{Raw: raw},
				UserInfo: authenticationv1.UserInfo{
					Username: tt.username,
					Groups:   tt.groups,
				},
			}}

			resp := BreakRequestMutationHandler(log.Log.WithName("test")).OnCreate(nil, nil, decoder, nil)(context.Background(), req)
			require.NotNil(t, resp)
			assert.True(t, resp.Allowed)
			require.Len(t, resp.Patches, 1)
			assert.Equal(t, "/spec/requestor", resp.Patches[0].Path)
			mutated := applyResponsePatches(t, raw, resp)
			assert.Equal(t, breaktheglassapi.AccessEntity{
				Name:   tt.username,
				Type:   tt.entityType,
				Groups: tt.groups,
			}, mutated.Spec.Requestor)
		})
	}
}

func TestBreakRequestMutationHandlerOnApproval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		reviewer     *breaktheglassapi.AccessEntity
		wantReviewer breaktheglassapi.AccessEntity
		wantPath     string
		wantPatch    bool
	}{
		{
			name: "records authenticated reviewer and groups",
			reviewer: &breaktheglassapi.AccessEntity{
				Name: "spoofed",
				Type: breaktheglassapi.AccessEntityTypeUser,
			},
			wantReviewer: breaktheglassapi.AccessEntity{
				Name:   "alice",
				Type:   breaktheglassapi.AccessEntityTypeUser,
				Groups: []string{"reviewers"},
			},
			wantPath:  "/status/review/reviewer",
			wantPatch: true,
		},
		{
			name:     "creates missing review without patching unrelated status fields",
			reviewer: nil,
			wantReviewer: breaktheglassapi.AccessEntity{
				Name:   "alice",
				Type:   breaktheglassapi.AccessEntityTypeUser,
				Groups: []string{"reviewers"},
			},
			wantPath:  "/status/review",
			wantPatch: true,
		},
		{
			name: "preserves controller system reviewer",
			reviewer: &breaktheglassapi.AccessEntity{
				Name: "capsule-controller",
				Type: breaktheglassapi.AccessEntityTypeSystem,
			},
			wantReviewer: breaktheglassapi.AccessEntity{
				Name: "capsule-controller",
				Type: breaktheglassapi.AccessEntityTypeSystem,
			},
			wantPatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			oldBr := &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
				Phase: capsulev1beta2.RequestPhaseRequested,
			}}
			var review *capsulev1beta2.ReviewInfo
			if tt.reviewer != nil {
				review = &capsulev1beta2.ReviewInfo{Reviewer: tt.reviewer.DeepCopy()}
			}

			newBr := &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
				Phase:  capsulev1beta2.RequestPhaseApproved,
				Review: review,
			}}
			raw, err := json.Marshal(newBr)
			require.NoError(t, err)

			decoder := &test.Decoder[*capsulev1beta2.BreakRequest]{Object: newBr, OldObject: oldBr}
			req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Object:      runtime.RawExtension{Raw: raw},
				OldObject:   runtime.RawExtension{Raw: []byte(`{}`)},
				SubResource: "status",
				UserInfo: authenticationv1.UserInfo{
					Username: "alice",
					Groups:   []string{"reviewers"},
				},
			}}

			resp := BreakRequestMutationHandler(log.Log.WithName("test")).OnUpdate(nil, nil, decoder, nil)(context.Background(), req)
			if tt.wantPatch {
				require.NotNil(t, resp)
				assert.True(t, resp.Allowed)
				require.Len(t, resp.Patches, 1)
				assert.Equal(t, tt.wantPath, resp.Patches[0].Path)
				assert.NotContains(t, resp.Patches[0].Path, "keepUntil")
				mutated := applyResponsePatches(t, raw, resp)
				require.NotNil(t, mutated.Status.Review)
				require.NotNil(t, mutated.Status.Review.Reviewer)
				assert.Equal(t, tt.wantReviewer, *mutated.Status.Review.Reviewer)
			} else {
				assert.Nil(t, resp)
				assert.Equal(t, tt.wantReviewer, *newBr.Status.Review.Reviewer)
			}
		})
	}
}

func TestBreakRequestMutationHandlerIgnoresApprovalOutsideStatusSubresource(t *testing.T) {
	t.Parallel()

	oldBr := &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
		Phase: capsulev1beta2.RequestPhaseRequested,
	}}
	newBr := &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
		Phase: capsulev1beta2.RequestPhaseApproved,
	}}
	raw, err := json.Marshal(newBr)
	require.NoError(t, err)

	decoder := &test.Decoder[*capsulev1beta2.BreakRequest]{Object: newBr, OldObject: oldBr}
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Object:    runtime.RawExtension{Raw: raw},
		OldObject: runtime.RawExtension{Raw: []byte(`{}`)},
		UserInfo: authenticationv1.UserInfo{
			Username: "alice",
			Groups:   []string{"reviewers"},
		},
	}}

	resp := BreakRequestMutationHandler(log.Log.WithName("test")).OnUpdate(nil, nil, decoder, nil)(context.Background(), req)
	assert.Nil(t, resp)
}

func applyResponsePatches(
	t *testing.T,
	raw []byte,
	response *admission.Response,
) *capsulev1beta2.BreakRequest {
	t.Helper()

	encodedPatch, err := json.Marshal(response.Patches)
	require.NoError(t, err)
	patch, err := jsonpatch.DecodePatch(encodedPatch)
	require.NoError(t, err)
	mutatedRaw, err := patch.Apply(raw)
	require.NoError(t, err)

	mutated := &capsulev1beta2.BreakRequest{}
	require.NoError(t, json.Unmarshal(mutatedRaw, mutated))

	return mutated
}
