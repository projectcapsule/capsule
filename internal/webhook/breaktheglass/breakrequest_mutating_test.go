// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/webhook/test"
	breaktheglassapi "github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	apimeta "github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
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
					Template: capsulev1beta2.GlobalBreakRequestTemplateReference{
						Kind: capsulev1beta2.GlobalBreakRequestTemplateKind,
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
	t.Setenv(configuration.EnvironmentServiceaccountName, "capsule-controller")
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")

	tests := []struct {
		name         string
		username     string
		groups       []string
		reviewer     *breaktheglassapi.AccessEntity
		wantReviewer breaktheglassapi.AccessEntity
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
			wantPatch: true,
		},
		{
			name:     "preserves controller system reviewer",
			username: "system:serviceaccount:capsule-system:capsule-controller",
			groups:   []string{"system:serviceaccounts", "system:serviceaccounts:capsule-system"},
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
		{
			name: "replaces a spoofed system reviewer",
			reviewer: &breaktheglassapi.AccessEntity{
				Name: "capsule-controller",
				Type: breaktheglassapi.AccessEntityTypeSystem,
			},
			wantReviewer: breaktheglassapi.AccessEntity{
				Name:   "alice",
				Type:   breaktheglassapi.AccessEntityTypeUser,
				Groups: []string{"reviewers"},
			},
			wantPatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldBr := &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
				Phase:   capsulev1beta2.RequestPhaseRequested,
				Request: &capsulev1beta2.BreakRequestStatusRequest{},
			}}
			var review *capsulev1beta2.ReviewInfo
			if tt.reviewer != nil {
				review = &capsulev1beta2.ReviewInfo{Reviewer: tt.reviewer.DeepCopy()}
			}

			newBr := &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
				Phase:   capsulev1beta2.RequestPhaseApproved,
				Review:  review,
				Request: &capsulev1beta2.BreakRequestStatusRequest{},
			}}
			raw, err := json.Marshal(newBr)
			require.NoError(t, err)

			decoder := &test.Decoder[*capsulev1beta2.BreakRequest]{Object: newBr, OldObject: oldBr}
			username := tt.username
			groups := tt.groups
			if username == "" {
				username = "alice"
				groups = []string{"reviewers"}
			}

			req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Object:      runtime.RawExtension{Raw: raw},
				OldObject:   runtime.RawExtension{Raw: []byte(`{}`)},
				SubResource: "status",
				UserInfo: authenticationv1.UserInfo{
					Username: username,
					Groups:   groups,
				},
			}}

			resp := BreakRequestMutationHandler(log.Log.WithName("test")).OnUpdate(nil, nil, decoder, nil)(context.Background(), req)
			if tt.wantPatch {
				require.NotNil(t, resp)
				assert.True(t, resp.Allowed)
				require.Len(t, resp.Patches, 1)
				assert.Equal(t, "/status", resp.Patches[0].Path)
				mutated := applyResponsePatches(t, raw, resp)
				assert.Equal(t, capsulev1beta2.RequestPhaseApproved, mutated.Status.Phase)
				require.NotNil(t, mutated.Status.Review)
				require.NotNil(t, mutated.Status.Review.Reviewer)
				assert.Equal(t, tt.wantReviewer, *mutated.Status.Review.Reviewer)
				assert.Equal(t, capsulev1beta2.RequestVerdictApproved, mutated.Status.Review.Verdict)
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

func TestBreakRequestMutationHandlerAppliesApprovalFromPhaseOnly(t *testing.T) {
	t.Parallel()

	resources := []apiruntime.RenderedResource{{
		Targets: []runtime.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap"}`)}},
	}}
	serviceAccount := &apimeta.NamespacedRFC1123ObjectReferenceWithNamespace{
		Name:      "runner",
		Namespace: "operations",
	}
	template := &capsulev1beta2.ResolvedBreakRequestTemplateReference{
		BreakRequestTemplateReference: capsulev1beta2.BreakRequestTemplateReference{
			Kind: capsulev1beta2.GlobalBreakRequestTemplateKind,
			Name: "template",
		},
		ResourceVersion: "1234",
	}
	duration := &metav1.Duration{Duration: time.Hour}
	oldBr := &capsulev1beta2.BreakRequest{
		Spec: capsulev1beta2.BreakRequestSpec{Template: capsulev1beta2.GlobalBreakRequestTemplateReference{
			Kind: capsulev1beta2.GlobalBreakRequestTemplateKind,
			Name: "template",
		}},
		Status: capsulev1beta2.BreakRequestStatus{
			Phase: capsulev1beta2.RequestPhaseRequested,
			Request: &capsulev1beta2.BreakRequestStatusRequest{
				Duration:      duration,
				Resources:     resources,
				Impersonation: serviceAccount,
				Template:      template,
			},
		},
	}
	newBr := &capsulev1beta2.BreakRequest{
		Spec:   oldBr.Spec,
		Status: capsulev1beta2.BreakRequestStatus{Phase: capsulev1beta2.RequestPhaseApproved},
	}
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

	resp := BreakRequestMutationHandler(log.Log.WithName("test")).OnUpdate(
		nil,
		nil,
		decoder,
		nil,
	)(context.Background(), req)
	require.NotNil(t, resp)
	assert.True(t, resp.Allowed)

	mutated := applyResponsePatches(t, raw, resp)
	assert.Equal(t, capsulev1beta2.RequestPhaseApproved, mutated.Status.Phase)
	assert.Equal(t, resources, mutated.Status.Request.Resources)
	assert.Equal(t, duration, mutated.Status.Request.Duration)
	assert.Equal(t, serviceAccount, mutated.Status.Request.Impersonation)
	assert.Equal(t, template, mutated.Status.Request.Template)
	require.NotNil(t, mutated.Status.Review)
	require.NotNil(t, mutated.Status.Review.Reviewer)
	assert.Equal(t, "alice", mutated.Status.Review.Reviewer.Name)
	assert.Equal(t, capsulev1beta2.RequestVerdictApproved, mutated.Status.Review.Verdict)
	approved := mutated.LatestTransition(capsulev1beta2.RequestPhaseApproved)
	require.NotNil(t, approved)
	assert.Equal(t, "ApprovedByUser", approved.Reason)
	assert.Equal(t, "Access request approved", approved.Message)
	assert.Equal(t, "alice", approved.Actor.Name)
	assert.Equal(t, capsulev1beta2.RequestPhaseApproved, approved.Type)
}

func TestBreakRequestMutationHandlerAppliesExpirationFromStoredStatus(t *testing.T) {
	t.Parallel()

	keepFor := breaktheglassapi.ExtendedDuration(10 * time.Minute)
	resources := []apiruntime.RenderedResource{{
		Targets: []runtime.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap"}`)}},
	}}
	serviceAccount := &apimeta.NamespacedRFC1123ObjectReferenceWithNamespace{
		Name:      "runner",
		Namespace: "operations",
	}
	template := &capsulev1beta2.ResolvedBreakRequestTemplateReference{
		BreakRequestTemplateReference: capsulev1beta2.BreakRequestTemplateReference{
			Kind: capsulev1beta2.GlobalBreakRequestTemplateKind,
			Name: "template",
		},
		ResourceVersion: "1234",
	}
	oldBr := &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
		Phase: capsulev1beta2.RequestPhaseActive,
		Request: &capsulev1beta2.BreakRequestStatusRequest{
			KeepFor:       &keepFor,
			Resources:     resources,
			Impersonation: serviceAccount,
			Template:      template,
		},
		Active: &capsulev1beta2.ActivePeriod{
			ActiveFrom: ptrTo(metav1.Now()),
		},
	}}
	newBr := &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
		Phase: capsulev1beta2.RequestPhaseExpired,
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
			Groups:   []string{"developers"},
		},
	}}

	resp := BreakRequestMutationHandler(log.Log.WithName("test")).OnUpdate(nil, nil, decoder, nil)(context.Background(), req)
	require.NotNil(t, resp)
	assert.True(t, resp.Allowed)

	mutated := applyResponsePatches(t, raw, resp)
	assert.Equal(t, capsulev1beta2.RequestPhaseExpired, mutated.Status.Phase)
	assert.Equal(t, resources, mutated.Status.Request.Resources)
	assert.Equal(t, serviceAccount, mutated.Status.Request.Impersonation)
	assert.Equal(t, template, mutated.Status.Request.Template)
	require.NotNil(t, mutated.Status.KeepUntil)
	assert.True(t, mutated.Status.KeepUntil.After(time.Now()))
	expired := mutated.LatestTransition(capsulev1beta2.RequestPhaseExpired)
	require.NotNil(t, expired)
	assert.Equal(t, "ExpiredByUser", expired.Reason)
	assert.Equal(t, "Access request expired by alice", expired.Message)
	assert.Equal(t, "alice", expired.Actor.Name)
	assert.Equal(t, capsulev1beta2.RequestPhaseExpired, expired.Type)

	// Validating admission receives the object after mutation. The reconstructed
	// status must therefore pass the controller-owned resource and impersonation
	// checks even though the client submitted only the target phase.
	validationDecoder := &test.Decoder[*capsulev1beta2.BreakRequest]{
		Object:    mutated,
		OldObject: oldBr,
	}
	validationResponse := BreakRequestValidationHandler(log.Log.WithName("test"), nil).OnUpdate(
		nil,
		nil,
		validationDecoder,
		nil,
	)(context.Background(), req)
	assert.Nil(t, validationResponse)
}

func TestBreakRequestMutationHandlerAppliesRequesterRetryFromStoredFailure(t *testing.T) {
	t.Parallel()

	resources := []apiruntime.RenderedResource{{Targets: []runtime.RawExtension{{Raw: []byte(
		`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"target"}}`,
	)}}}}
	oldBr := &capsulev1beta2.BreakRequest{
		Spec: capsulev1beta2.BreakRequestSpec{Requestor: breaktheglassapi.AccessEntity{
			Name: "alice",
			Type: breaktheglassapi.AccessEntityTypeUser,
		}},
		Status: capsulev1beta2.BreakRequestStatus{
			Phase: capsulev1beta2.RequestPhaseFailed,
			Failure: &capsulev1beta2.BreakRequestFailure{
				Stage:      capsulev1beta2.RequestFailureStagePreflight,
				RetryPhase: capsulev1beta2.RequestPhaseRequested,
				Reason:     "ResourceDryRunFailed",
				Message:    "configmaps is forbidden",
			},
			Request: &capsulev1beta2.BreakRequestStatusRequest{Resources: resources},
		},
	}
	newBr := oldBr.DeepCopy()
	newBr.Status = capsulev1beta2.BreakRequestStatus{Phase: capsulev1beta2.RequestPhaseRetrying}
	raw, err := json.Marshal(newBr)
	require.NoError(t, err)

	decoder := &test.Decoder[*capsulev1beta2.BreakRequest]{Object: newBr, OldObject: oldBr}
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Object:      runtime.RawExtension{Raw: raw},
		OldObject:   runtime.RawExtension{Raw: []byte(`{}`)},
		SubResource: "status",
		UserInfo: authenticationv1.UserInfo{
			Username: "alice",
			Groups:   []string{"system:authenticated"},
		},
	}}

	resp := BreakRequestMutationHandler(log.Log.WithName("test")).OnUpdate(nil, nil, decoder, nil)(context.Background(), req)
	require.NotNil(t, resp)
	assert.True(t, resp.Allowed)

	mutated := applyResponsePatches(t, raw, resp)
	assert.Equal(t, capsulev1beta2.RequestPhaseRetrying, mutated.Status.Phase)
	assert.Equal(t, oldBr.Status.Failure, mutated.Status.Failure)
	assert.Equal(t, resources, mutated.Status.Request.Resources)
	retryTransition := mutated.LatestTransition(capsulev1beta2.RequestPhaseRetrying)
	require.NotNil(t, retryTransition)
	assert.Equal(t, "RetryRequestedByUser", retryTransition.Reason)
	assert.Contains(t, retryTransition.Message, "alice")
	assert.Equal(t, "alice", retryTransition.Actor.Name)
}

func TestBreakRequestMutationHandlerRejectsInvalidTransition(t *testing.T) {
	t.Parallel()

	oldBr := &capsulev1beta2.BreakRequest{Status: capsulev1beta2.BreakRequestStatus{
		Phase:   capsulev1beta2.RequestPhaseRequested,
		Request: &capsulev1beta2.BreakRequestStatusRequest{},
	}}
	newBr := oldBr.DeepCopy()
	newBr.Status.Phase = capsulev1beta2.RequestPhaseActive
	raw, err := json.Marshal(newBr)
	require.NoError(t, err)

	decoder := &test.Decoder[*capsulev1beta2.BreakRequest]{Object: newBr, OldObject: oldBr}
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Object:      runtime.RawExtension{Raw: raw},
		OldObject:   runtime.RawExtension{Raw: []byte(`{}`)},
		SubResource: "status",
		UserInfo: authenticationv1.UserInfo{
			Username: "alice",
		},
	}}

	resp := BreakRequestMutationHandler(log.Log.WithName("test")).OnUpdate(nil, nil, decoder, nil)(context.Background(), req)
	test.VerifyResponse(
		t,
		resp,
		403,
		"invalid BreakRequest transition: can only activate an approved request",
	)
}

func ptrTo[T any](value T) *T {
	return &value
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
