// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	apimeta "github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestOptionalBreakRequestFieldsAreOmitted(t *testing.T) {
	t.Parallel()

	values := map[string]struct {
		value  any
		fields []string
	}{
		"status": {
			value:  BreakRequestStatus{},
			fields: []string{"review", "resources", "request", "failure", "active", "keepUntil", "transitions"},
		},
		"active period": {
			value:  ActivePeriod{},
			fields: []string{"from", "until"},
		},
		"request properties": {
			value:  BreakRequestStatusRequest{},
			fields: []string{"template", "impersonation", "approvals", "keepFor", "duration", "startTime", "resources"},
		},
	}

	for name, testCase := range values {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			raw, err := json.Marshal(testCase.value)
			require.NoError(t, err)

			serialized := map[string]any{}
			require.NoError(t, json.Unmarshal(raw, &serialized))
			for _, field := range testCase.fields {
				assert.NotContains(t, serialized, field)
			}
		})
	}
}

func TestTransitionAuditTrail(t *testing.T) {
	t.Parallel()

	requestor := &breaktheglass.AccessEntity{
		Name:   "alice",
		Type:   breaktheglass.AccessEntityTypeUser,
		Groups: []string{"developers"},
	}
	createdAt := metav1.NewTime(time.Date(2026, time.September, 2, 8, 0, 0, 0, time.UTC))
	br := &BreakRequest{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: createdAt}}

	require.NoError(t, br.SetCreated(requestor))
	require.NoError(t, br.SetRequestedBy(requestor))
	require.NoError(t, br.ApproveRequest(
		&breaktheglass.AccessEntity{Type: breaktheglass.AccessEntityTypeSystem},
		&BreakRequestStatusRequest{},
		"Auto Approved",
	))
	require.NoError(t, br.ActiveRequest(nil))

	require.Len(t, br.Status.Transitions, 4)
	assert.Equal(t, RequestPhaseCreated, br.Status.Transitions[0].Type)
	assert.Equal(t, requestor.Name, br.Status.Transitions[0].Actor.Name)
	assert.Equal(t, requestor.Type, br.Status.Transitions[0].Actor.Type)
	assert.Equal(t, createdAt, br.Status.Transitions[0].Timestamp)
	actorJSON, err := json.Marshal(br.Status.Transitions[0].Actor)
	require.NoError(t, err)
	assert.NotContains(t, string(actorJSON), "groups")
	assert.Equal(t, RequestPhaseRequested, br.Status.Transitions[1].Type)
	assert.Equal(t, requestor.Name, br.Status.Transitions[1].Actor.Name)
	assert.Equal(t, requestor.Type, br.Status.Transitions[1].Actor.Type)
	assert.Equal(t, RequestPhaseApproved, br.Status.Transitions[2].Type)
	assert.Equal(t, BreakRequestTransitionActor{
		Name: capsuleControllerActorName,
		Type: breaktheglass.AccessEntityTypeSystem,
	}, br.Status.Transitions[2].Actor)
	assert.Equal(t, RequestPhaseActive, br.Status.Transitions[3].Type)
	assert.Empty(t, br.Status.Conditions)
}

func TestBreakRequestResolvedDataIsNestedUnderRequest(t *testing.T) {
	t.Parallel()

	status := BreakRequestStatus{Request: &BreakRequestStatusRequest{
		Template: &ResolvedBreakRequestTemplateReference{
			BreakRequestTemplateReference: BreakRequestTemplateReference{
				Kind: GlobalBreakRequestTemplateKind,
				Name: "emergency-access",
			},
			ResourceVersion: "42",
		},
		Impersonation: &apimeta.NamespacedRFC1123ObjectReferenceWithNamespace{
			Name:      "runner",
			Namespace: "capsule-system",
		},
	}}

	raw, err := json.Marshal(status)
	require.NoError(t, err)

	serialized := map[string]any{}
	require.NoError(t, json.Unmarshal(raw, &serialized))
	assert.NotContains(t, serialized, "approved")
	assert.NotContains(t, serialized, "template")
	assert.NotContains(t, serialized, "serviceAccount")

	request, ok := serialized["request"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, request, "template")
	assert.Contains(t, request, "impersonation")
}

func TestBreakRequestFailureRetryLifecycle(t *testing.T) {
	t.Parallel()

	requestor := &breaktheglass.AccessEntity{Name: "alice", Type: breaktheglass.AccessEntityTypeUser}
	br := &BreakRequest{Status: BreakRequestStatus{
		Phase:   RequestPhaseApproved,
		Request: &BreakRequestStatusRequest{},
		Review: &ReviewInfo{
			Reviewer: requestor,
			Verdict:  RequestVerdictApproved,
		},
	}}

	require.NoError(t, br.FailRequest(
		RequestFailureStageActivation,
		RequestPhaseApproved,
		"ResourceApplyFailed",
		"configmaps is forbidden",
	))
	assert.Equal(t, RequestPhaseFailed, br.Status.Phase)
	require.NotNil(t, br.Status.Failure)
	assert.Equal(t, RequestFailureStageActivation, br.Status.Failure.Stage)

	require.NoError(t, br.RetryRequest(requestor))
	assert.Equal(t, RequestPhaseRetrying, br.Status.Phase)
	require.NoError(t, br.CompleteRetry())
	assert.Equal(t, RequestPhaseApproved, br.Status.Phase)
	assert.Nil(t, br.Status.Failure)
	assert.Equal(t, requestor, br.Status.Review.Reviewer)

	require.Len(t, br.Status.Transitions, 3)
	assert.Equal(t, RequestPhaseFailed, br.Status.Transitions[0].Type)
	assert.Equal(t, RequestPhaseRetrying, br.Status.Transitions[1].Type)
	assert.Equal(t, RequestPhaseApproved, br.Status.Transitions[2].Type)
	assert.Equal(t, requestor.Name, br.Status.Transitions[1].Actor.Name)
	assert.Equal(t, requestor.Type, br.Status.Transitions[1].Actor.Type)
}

func TestExpireRequestTracksActor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		entity      *breaktheglass.AccessEntity
		wantReason  string
		wantMessage string
	}{
		{
			name:        "automatic expiration",
			wantReason:  "ExpiredBySystem",
			wantMessage: "Access request expired automatically",
		},
		{
			name: "user expiration",
			entity: &breaktheglass.AccessEntity{
				Name: "alice",
				Type: breaktheglass.AccessEntityTypeUser,
			},
			wantReason:  "ExpiredByUser",
			wantMessage: "Access request expired by alice",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			br := &BreakRequest{Status: BreakRequestStatus{Phase: RequestPhaseActive}}
			require.NoError(t, br.ExpireRequest(testCase.entity))

			transition := br.LatestTransition(RequestPhaseExpired)
			require.NotNil(t, transition)
			assert.Equal(t, testCase.wantReason, transition.Reason)
			assert.Equal(t, testCase.wantMessage, transition.Message)
			if testCase.entity == nil {
				assert.Equal(t, breaktheglass.AccessEntityTypeSystem, transition.Actor.Type)
				assert.Equal(t, capsuleControllerActorName, transition.Actor.Name)
			} else {
				assert.Equal(t, testCase.entity.Name, transition.Actor.Name)
				assert.Equal(t, testCase.entity.Type, transition.Actor.Type)
			}
		})
	}
}

func TestSetReviewer(t *testing.T) {
	reviewer := &breaktheglass.AccessEntity{Type: breaktheglass.AccessEntityTypeUser, Name: "test-user"}
	tests := []struct {
		name             string
		ar               *BreakRequest
		entity           *breaktheglass.AccessEntity
		conditionMessage string
		verdict          RequestVerdict
		expectedReview   *ReviewInfo
	}{
		{
			name:             "set reviewer successfully",
			ar:               &BreakRequest{},
			entity:           reviewer,
			conditionMessage: "Approved",
			verdict:          RequestVerdictApproved,
			expectedReview: &ReviewInfo{
				Reviewer: reviewer,
				Message:  "Approved",
				Verdict:  RequestVerdictApproved,
			},
		},
		{
			name:             "nil entity does not set reviewer",
			ar:               &BreakRequest{},
			entity:           nil,
			conditionMessage: "No review",
			verdict:          RequestVerdictDenied,
			expectedReview:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setReviewer(tt.ar, tt.entity, tt.conditionMessage, tt.verdict)
			assert.Equal(t, tt.expectedReview, tt.ar.Status.Review)
		})
	}
}

func TestTransitionRequestPhase(t *testing.T) {
	request := &BreakRequest{}
	now := metav1.Now()
	tests := []struct {
		name        string
		phase       RequestPhase
		initPhase   RequestPhase
		expectError bool
	}{
		{
			name:        "create an uninitialized request",
			phase:       RequestPhaseCreated,
			initPhase:   "",
			expectError: false,
		},
		{
			name:        "valid transition",
			phase:       RequestPhaseRequested,
			initPhase:   "",
			expectError: false,
		},
		{
			name:        "deny approved request",
			phase:       RequestPhaseDenied,
			initPhase:   RequestPhaseApproved,
			expectError: true,
		},
		{
			name:        "activate unapproved request",
			phase:       RequestPhaseActive,
			initPhase:   RequestPhaseRequested,
			expectError: true,
		},
		{
			name:        "expire a requested request",
			phase:       RequestPhaseExpired,
			initPhase:   RequestPhaseRequested,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request.Status.Phase = tt.initPhase
			err := request.transitionRequestPhase(tt.phase, "test", "reason", now, nil)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.phase, request.Status.Phase)
			}
		})
	}
}

func TestApproveRequest(t *testing.T) {
	br := &BreakRequest{}
	entity := &breaktheglass.AccessEntity{Name: "reviewer", Type: breaktheglass.AccessEntityTypeUser}
	props := &BreakRequestStatusRequest{Duration: &metav1.Duration{Duration: time.Hour}}
	err := br.ApproveRequest(entity, props, "Approved")
	require.NoError(t, err)
	assert.Equal(t, RequestPhaseApproved, br.Status.Phase)
	assert.Equal(t, entity, br.Status.Review.Reviewer)
	assert.Equal(t, props.Duration, br.Status.Request.Duration)
}

func TestGenerateRequestStatusResolvesLifecycleDefaults(t *testing.T) {
	keepFor := breaktheglass.ExtendedDuration(5 * time.Minute)
	resources := []apiruntime.RenderedResource{{
		Targets: []runtime.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap"}`)}},
	}}
	brt := &GlobalBreakRequestTemplate{Spec: GlobalBreakRequestTemplateSpec{
		DefaultDuration: &metav1.Duration{Duration: time.Minute},
		MaxDuration:     &metav1.Duration{Duration: time.Hour},
		KeepFor:         &keepFor,
		Approvals: breaktheglass.ApprovalSpec{
			Auto:       true,
			Conditions: []string{"true"},
		},
	}}
	br := &BreakRequest{Status: BreakRequestStatus{Request: &BreakRequestStatusRequest{
		Resources: resources,
	}}}

	properties, err := br.GenerateRequestStatus(brt)
	require.NoError(t, err)
	require.NotNil(t, properties.Duration)
	assert.Equal(t, time.Minute, properties.Duration.Duration)
	require.NotNil(t, properties.KeepFor)
	assert.Equal(t, keepFor, *properties.KeepFor)
	require.NotNil(t, properties.StartTime)
	assert.Equal(t, resources, properties.Resources)
	require.NotSame(t, &resources[0], &properties.Resources[0])
	require.NotNil(t, properties.Approvals)
	assert.Equal(t, brt.Spec.Approvals, *properties.Approvals)
	brt.Spec.Approvals.Conditions[0] = "false"
	assert.Equal(t, "true", properties.Approvals.Conditions[0])

	br.Spec.Duration = &metav1.Duration{Duration: 2 * time.Hour}
	_, err = br.GenerateRequestStatus(brt)
	require.ErrorContains(t, err, "exceeds template maxDuration")
}

func TestDenyRequest(t *testing.T) {
	br := &BreakRequest{}
	entity := &breaktheglass.AccessEntity{Name: "reviewer", Type: breaktheglass.AccessEntityTypeUser}
	err := br.DenyRequest(entity, "Denied")
	require.NoError(t, err)
	assert.Equal(t, RequestPhaseDenied, br.Status.Phase)
	assert.Equal(t, entity, br.Status.Review.Reviewer)
	assert.Equal(t, "Denied", br.Status.Review.Message)
}

func TestRenderResources(t *testing.T) {
	br := &BreakRequest{
		Spec: BreakRequestSpec{
			Params: &runtime.RawExtension{Raw: []byte(`{"key":"value"}`)},
		},
	}
	schema := runtime.RawExtension{Raw: []byte(`{"type":"object","properties":{"key":{"type":"string"}}}`)}
	resource := apiruntime.ResourceTemplate{
		Policy:  apiruntime.ResourceTemplatePolicy{Creation: apiruntime.ResourceCreationPolicyMerge, Force: true},
		Targets: []runtime.RawExtension{{Raw: []byte(`{"kind":"ConfigMap"}`)}},
	}

	items, err := br.RenderResources(&schema, []apiruntime.ResourceTemplate{resource})
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.Equal(t, resource.Policy, items[0].Policy)
	assert.Len(t, items[0].Targets, 1)
}
func TestActiveRequest(t *testing.T) {
	tests := []struct {
		name               string
		br                 *BreakRequest
		entity             *breaktheglass.AccessEntity
		wantErr            string
		expectedPhase      RequestPhase
		expectActiveNotNil bool
		expectActiveUntil  bool
	}{
		{
			name:               "activate not approved",
			br:                 &BreakRequest{},
			entity:             &breaktheglass.AccessEntity{Name: "user", Type: breaktheglass.AccessEntityTypeUser},
			wantErr:            "can only activate an approved request",
			expectedPhase:      RequestPhaseActive,
			expectActiveNotNil: false,
			expectActiveUntil:  false,
		},
		{
			name: "activate with approved duration",
			br: &BreakRequest{
				Status: BreakRequestStatus{
					Request: &BreakRequestStatusRequest{
						Duration: &metav1.Duration{Duration: 30 * time.Minute},
					},
					Phase: RequestPhaseApproved,
				},
			},
			entity:             &breaktheglass.AccessEntity{Name: "user", Type: breaktheglass.AccessEntityTypeUser},
			wantErr:            "",
			expectedPhase:      RequestPhaseActive,
			expectActiveNotNil: true,
			expectActiveUntil:  true,
		},
		{
			name: "activate with requested duration when approved duration is nil",
			br: &BreakRequest{
				Spec: BreakRequestSpec{Duration: &metav1.Duration{Duration: time.Minute}},
				Status: BreakRequestStatus{
					Request: &BreakRequestStatusRequest{
						Duration: nil,
					},
					Phase: RequestPhaseApproved,
				},
			},
			entity:             &breaktheglass.AccessEntity{Name: "user", Type: breaktheglass.AccessEntityTypeUser},
			wantErr:            "",
			expectedPhase:      RequestPhaseActive,
			expectActiveNotNil: true,
			expectActiveUntil:  true,
		},
		{
			name: "activate without request properties",
			br: &BreakRequest{
				Status: BreakRequestStatus{
					Request: nil,
					Phase:   RequestPhaseApproved,
				},
			},
			entity:             &breaktheglass.AccessEntity{Name: "user", Type: breaktheglass.AccessEntityTypeUser},
			wantErr:            "",
			expectedPhase:      RequestPhaseActive,
			expectActiveNotNil: true,
			expectActiveUntil:  false,
		},
		{
			name: "activate with nil entity",
			br: &BreakRequest{
				Status: BreakRequestStatus{
					Request: &BreakRequestStatusRequest{
						Duration: &metav1.Duration{Duration: 30 * time.Minute},
					},
					Phase: RequestPhaseApproved,
				},
			},
			entity:             nil,
			wantErr:            "",
			expectedPhase:      RequestPhaseActive,
			expectActiveNotNil: true,
			expectActiveUntil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.br.ActiveRequest(tt.entity)
			if tt.wantErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedPhase, tt.br.Status.Phase)
				if tt.expectActiveNotNil {
					assert.NotNil(t, tt.br.Status.Active)
					if tt.expectActiveUntil {
						assert.True(t, tt.br.Status.Active.ActiveUntil.Time.After(time.Now()))
					}
				}
			}
		})
	}
}
