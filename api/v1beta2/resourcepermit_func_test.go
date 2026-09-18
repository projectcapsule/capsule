// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"encoding/json"
	"testing"
	"time"

	apimeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/resourcepermit"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestOptionalResourcePermitFieldsAreOmitted(t *testing.T) {
	t.Parallel()

	values := map[string]struct {
		value  any
		fields []string
	}{
		"status": {
			value:  ResourcePermitStatus{},
			fields: []string{"review", "resources", "request", "failure", "active", "keepUntil", "transitions"},
		},
		"active period": {
			value:  ActivePeriod{},
			fields: []string{"from", "until"},
		},
		"request properties": {
			value:  ResourcePermitStatusRequest{},
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

	requestor := &resourcepermit.AccessEntity{
		Name:   "alice",
		Type:   resourcepermit.AccessEntityTypeUser,
		Groups: []string{"developers"},
	}
	createdAt := metav1.NewTime(time.Date(2026, time.September, 2, 8, 0, 0, 0, time.UTC))
	br := &ResourcePermit{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: createdAt}}

	require.NoError(t, br.SetCreated(requestor))
	require.NoError(t, br.SetRequestedBy(requestor))
	require.NoError(t, br.ApprovePermit(
		&resourcepermit.AccessEntity{Type: resourcepermit.AccessEntityTypeSystem},
		&ResourcePermitStatusRequest{},
		"Auto Approved",
	))
	require.NoError(t, br.ActivatePermit(nil))

	require.Len(t, br.Status.Transitions, 4)
	assert.Equal(t, ResourcePermitPhaseCreated, br.Status.Transitions[0].Type)
	assert.Equal(t, requestor.Name, br.Status.Transitions[0].Actor.Name)
	assert.Equal(t, requestor.Type, br.Status.Transitions[0].Actor.Type)
	assert.Equal(t, createdAt, br.Status.Transitions[0].Timestamp)
	actorJSON, err := json.Marshal(br.Status.Transitions[0].Actor)
	require.NoError(t, err)
	assert.NotContains(t, string(actorJSON), "groups")
	assert.Equal(t, ResourcePermitPhaseRequested, br.Status.Transitions[1].Type)
	assert.Equal(t, requestor.Name, br.Status.Transitions[1].Actor.Name)
	assert.Equal(t, requestor.Type, br.Status.Transitions[1].Actor.Type)
	assert.Equal(t, ResourcePermitPhaseApproved, br.Status.Transitions[2].Type)
	assert.Equal(t, ResourcePermitTransitionActor{
		Name: capsuleControllerActorName,
		Type: resourcepermit.AccessEntityTypeSystem,
	}, br.Status.Transitions[2].Actor)
	assert.Equal(t, ResourcePermitPhaseActive, br.Status.Transitions[3].Type)
	assert.Empty(t, br.Status.Conditions)
}

func TestResourcePermitResolvedDataIsNestedUnderRequest(t *testing.T) {
	t.Parallel()

	status := ResourcePermitStatus{Request: &ResourcePermitStatusRequest{
		Template: &ResolvedResourcePermitTemplateReference{
			ResourcePermitTemplateReference: ResourcePermitTemplateReference{
				Kind: GlobalResourcePermitTemplateKind,
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

func TestResourcePermitFailureRetryLifecycle(t *testing.T) {
	t.Parallel()

	requestor := &resourcepermit.AccessEntity{Name: "alice", Type: resourcepermit.AccessEntityTypeUser}
	br := &ResourcePermit{Status: ResourcePermitStatus{
		Phase:   ResourcePermitPhaseApproved,
		Request: &ResourcePermitStatusRequest{},
		Review: &ReviewInfo{
			Reviewer: requestor,
			Verdict:  ResourcePermitVerdictApproved,
		},
	}}

	require.NoError(t, br.FailPermit(
		ResourcePermitFailureStageActivation,
		ResourcePermitPhaseApproved,
		"ResourceApplyFailed",
		"configmaps is forbidden",
	))
	assert.Equal(t, ResourcePermitPhaseFailed, br.Status.Phase)
	require.NotNil(t, br.Status.Failure)
	assert.Equal(t, ResourcePermitFailureStageActivation, br.Status.Failure.Stage)

	require.NoError(t, br.RetryPermit(requestor))
	assert.Equal(t, ResourcePermitPhaseRetrying, br.Status.Phase)
	require.NoError(t, br.CompleteRetry())
	assert.Equal(t, ResourcePermitPhaseApproved, br.Status.Phase)
	assert.Nil(t, br.Status.Failure)
	assert.Equal(t, requestor, br.Status.Review.Reviewer)

	require.Len(t, br.Status.Transitions, 3)
	assert.Equal(t, ResourcePermitPhaseFailed, br.Status.Transitions[0].Type)
	assert.Equal(t, ResourcePermitPhaseRetrying, br.Status.Transitions[1].Type)
	assert.Equal(t, ResourcePermitPhaseApproved, br.Status.Transitions[2].Type)
	assert.Equal(t, requestor.Name, br.Status.Transitions[1].Actor.Name)
	assert.Equal(t, requestor.Type, br.Status.Transitions[1].Actor.Type)
}

func TestExpirePermitTracksActor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		entity      *resourcepermit.AccessEntity
		wantReason  string
		wantMessage string
	}{
		{
			name:        "automatic expiration",
			wantReason:  "ExpiredBySystem",
			wantMessage: "Resource permit expired automatically",
		},
		{
			name: "user expiration",
			entity: &resourcepermit.AccessEntity{
				Name: "alice",
				Type: resourcepermit.AccessEntityTypeUser,
			},
			wantReason:  "ExpiredByUser",
			wantMessage: "Resource permit expired by alice",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			br := &ResourcePermit{Status: ResourcePermitStatus{Phase: ResourcePermitPhaseActive}}
			require.NoError(t, br.ExpirePermit(testCase.entity))

			transition := br.LatestTransition(ResourcePermitPhaseExpired)
			require.NotNil(t, transition)
			assert.Equal(t, testCase.wantReason, transition.Reason)
			assert.Equal(t, testCase.wantMessage, transition.Message)
			if testCase.entity == nil {
				assert.Equal(t, resourcepermit.AccessEntityTypeSystem, transition.Actor.Type)
				assert.Equal(t, capsuleControllerActorName, transition.Actor.Name)
			} else {
				assert.Equal(t, testCase.entity.Name, transition.Actor.Name)
				assert.Equal(t, testCase.entity.Type, transition.Actor.Type)
			}
		})
	}
}

func TestSetReviewer(t *testing.T) {
	reviewer := &resourcepermit.AccessEntity{Type: resourcepermit.AccessEntityTypeUser, Name: "test-user"}
	tests := []struct {
		name             string
		ar               *ResourcePermit
		entity           *resourcepermit.AccessEntity
		conditionMessage string
		verdict          ResourcePermitVerdict
		expectedReview   *ReviewInfo
	}{
		{
			name:             "set reviewer successfully",
			ar:               &ResourcePermit{},
			entity:           reviewer,
			conditionMessage: "Approved",
			verdict:          ResourcePermitVerdictApproved,
			expectedReview: &ReviewInfo{
				Reviewer: reviewer,
				Message:  "Approved",
				Verdict:  ResourcePermitVerdictApproved,
			},
		},
		{
			name:             "nil entity does not set reviewer",
			ar:               &ResourcePermit{},
			entity:           nil,
			conditionMessage: "No review",
			verdict:          ResourcePermitVerdictDenied,
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

func TestTransitionResourcePermitPhase(t *testing.T) {
	request := &ResourcePermit{}
	now := metav1.Now()
	tests := []struct {
		name        string
		phase       ResourcePermitPhase
		initPhase   ResourcePermitPhase
		expectError bool
	}{
		{
			name:        "create an uninitialized request",
			phase:       ResourcePermitPhaseCreated,
			initPhase:   "",
			expectError: false,
		},
		{
			name:        "valid transition",
			phase:       ResourcePermitPhaseRequested,
			initPhase:   "",
			expectError: false,
		},
		{
			name:        "deny approved request",
			phase:       ResourcePermitPhaseDenied,
			initPhase:   ResourcePermitPhaseApproved,
			expectError: true,
		},
		{
			name:        "activate unapproved request",
			phase:       ResourcePermitPhaseActive,
			initPhase:   ResourcePermitPhaseRequested,
			expectError: true,
		},
		{
			name:        "expire a requested request",
			phase:       ResourcePermitPhaseExpired,
			initPhase:   ResourcePermitPhaseRequested,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request.Status.Phase = tt.initPhase
			err := request.transitionResourcePermitPhase(tt.phase, "test", "reason", now, nil)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.phase, request.Status.Phase)
			}
		})
	}
}

func TestApprovePermit(t *testing.T) {
	br := &ResourcePermit{}
	entity := &resourcepermit.AccessEntity{Name: "reviewer", Type: resourcepermit.AccessEntityTypeUser}
	props := &ResourcePermitStatusRequest{Duration: &metav1.Duration{Duration: time.Hour}}
	err := br.ApprovePermit(entity, props, "Approved")
	require.NoError(t, err)
	assert.Equal(t, ResourcePermitPhaseApproved, br.Status.Phase)
	assert.Equal(t, entity, br.Status.Review.Reviewer)
	assert.Equal(t, props.Duration, br.Status.Request.Duration)
}

func TestGenerateRequestStatusResolvesLifecycleDefaults(t *testing.T) {
	keepFor := resourcepermit.ExtendedDuration(5 * time.Minute)
	resources := []apiruntime.RenderedResource{{
		Targets: []runtime.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap"}`)}},
	}}
	brt := &GlobalResourcePermitTemplate{Spec: GlobalResourcePermitTemplateSpec{
		DefaultDuration: &metav1.Duration{Duration: time.Minute},
		MaxDuration:     &metav1.Duration{Duration: time.Hour},
		KeepFor:         &keepFor,
		Approvals: resourcepermit.ApprovalSpec{
			Auto:       true,
			Conditions: []string{"true"},
		},
	}}
	br := &ResourcePermit{Status: ResourcePermitStatus{Request: &ResourcePermitStatusRequest{
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

func TestDenyPermit(t *testing.T) {
	br := &ResourcePermit{}
	entity := &resourcepermit.AccessEntity{Name: "reviewer", Type: resourcepermit.AccessEntityTypeUser}
	err := br.DenyPermit(entity, "Denied")
	require.NoError(t, err)
	assert.Equal(t, ResourcePermitPhaseDenied, br.Status.Phase)
	assert.Equal(t, entity, br.Status.Review.Reviewer)
	assert.Equal(t, "Denied", br.Status.Review.Message)
}

func TestRenderResources(t *testing.T) {
	br := &ResourcePermit{
		Spec: ResourcePermitSpec{
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
func TestActivatePermit(t *testing.T) {
	tests := []struct {
		name               string
		br                 *ResourcePermit
		entity             *resourcepermit.AccessEntity
		wantErr            string
		expectedPhase      ResourcePermitPhase
		expectActiveNotNil bool
		expectActiveUntil  bool
	}{
		{
			name:               "activate not approved",
			br:                 &ResourcePermit{},
			entity:             &resourcepermit.AccessEntity{Name: "user", Type: resourcepermit.AccessEntityTypeUser},
			wantErr:            "can only activate an approved request",
			expectedPhase:      ResourcePermitPhaseActive,
			expectActiveNotNil: false,
			expectActiveUntil:  false,
		},
		{
			name: "activate with approved duration",
			br: &ResourcePermit{
				Status: ResourcePermitStatus{
					Request: &ResourcePermitStatusRequest{
						Duration: &metav1.Duration{Duration: 30 * time.Minute},
					},
					Phase: ResourcePermitPhaseApproved,
				},
			},
			entity:             &resourcepermit.AccessEntity{Name: "user", Type: resourcepermit.AccessEntityTypeUser},
			wantErr:            "",
			expectedPhase:      ResourcePermitPhaseActive,
			expectActiveNotNil: true,
			expectActiveUntil:  true,
		},
		{
			name: "activate with requested duration when approved duration is nil",
			br: &ResourcePermit{
				Spec: ResourcePermitSpec{Duration: &metav1.Duration{Duration: time.Minute}},
				Status: ResourcePermitStatus{
					Request: &ResourcePermitStatusRequest{
						Duration: nil,
					},
					Phase: ResourcePermitPhaseApproved,
				},
			},
			entity:             &resourcepermit.AccessEntity{Name: "user", Type: resourcepermit.AccessEntityTypeUser},
			wantErr:            "",
			expectedPhase:      ResourcePermitPhaseActive,
			expectActiveNotNil: true,
			expectActiveUntil:  true,
		},
		{
			name: "activate without request properties",
			br: &ResourcePermit{
				Status: ResourcePermitStatus{
					Request: nil,
					Phase:   ResourcePermitPhaseApproved,
				},
			},
			entity:             &resourcepermit.AccessEntity{Name: "user", Type: resourcepermit.AccessEntityTypeUser},
			wantErr:            "",
			expectedPhase:      ResourcePermitPhaseActive,
			expectActiveNotNil: true,
			expectActiveUntil:  false,
		},
		{
			name: "activate with nil entity",
			br: &ResourcePermit{
				Status: ResourcePermitStatus{
					Request: &ResourcePermitStatusRequest{
						Duration: &metav1.Duration{Duration: 30 * time.Minute},
					},
					Phase: ResourcePermitPhaseApproved,
				},
			},
			entity:             nil,
			wantErr:            "",
			expectedPhase:      ResourcePermitPhaseActive,
			expectActiveNotNil: true,
			expectActiveUntil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.br.ActivatePermit(tt.entity)
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
