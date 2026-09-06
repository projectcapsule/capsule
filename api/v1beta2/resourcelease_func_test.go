// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"encoding/json"
	"testing"
	"time"

	apimeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/resourcelease"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestOptionalResourceLeaseFieldsAreOmitted(t *testing.T) {
	t.Parallel()

	values := map[string]struct {
		value  any
		fields []string
	}{
		"status": {
			value:  ResourceLeaseStatus{},
			fields: []string{"review", "resources", "request", "failure", "active", "keepUntil", "transitions"},
		},
		"active period": {
			value:  ActivePeriod{},
			fields: []string{"from", "until"},
		},
		"request properties": {
			value:  ResourceLeaseStatusRequest{},
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

	requestor := &resourcelease.AccessEntity{
		Name:   "alice",
		Type:   resourcelease.AccessEntityTypeUser,
		Groups: []string{"developers"},
	}
	createdAt := metav1.NewTime(time.Date(2026, time.September, 2, 8, 0, 0, 0, time.UTC))
	br := &ResourceLease{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: createdAt}}

	require.NoError(t, br.SetCreated(requestor))
	require.NoError(t, br.SetRequestedBy(requestor))
	require.NoError(t, br.ApproveLease(
		&resourcelease.AccessEntity{Type: resourcelease.AccessEntityTypeSystem},
		&ResourceLeaseStatusRequest{},
		"Auto Approved",
	))
	require.NoError(t, br.ActivateLease(nil))

	require.Len(t, br.Status.Transitions, 4)
	assert.Equal(t, ResourceLeasePhaseCreated, br.Status.Transitions[0].Type)
	assert.Equal(t, requestor.Name, br.Status.Transitions[0].Actor.Name)
	assert.Equal(t, requestor.Type, br.Status.Transitions[0].Actor.Type)
	assert.Equal(t, createdAt, br.Status.Transitions[0].Timestamp)
	actorJSON, err := json.Marshal(br.Status.Transitions[0].Actor)
	require.NoError(t, err)
	assert.NotContains(t, string(actorJSON), "groups")
	assert.Equal(t, ResourceLeasePhaseRequested, br.Status.Transitions[1].Type)
	assert.Equal(t, requestor.Name, br.Status.Transitions[1].Actor.Name)
	assert.Equal(t, requestor.Type, br.Status.Transitions[1].Actor.Type)
	assert.Equal(t, ResourceLeasePhaseApproved, br.Status.Transitions[2].Type)
	assert.Equal(t, ResourceLeaseTransitionActor{
		Name: capsuleControllerActorName,
		Type: resourcelease.AccessEntityTypeSystem,
	}, br.Status.Transitions[2].Actor)
	assert.Equal(t, ResourceLeasePhaseActive, br.Status.Transitions[3].Type)
	assert.Empty(t, br.Status.Conditions)
}

func TestResourceLeaseResolvedDataIsNestedUnderRequest(t *testing.T) {
	t.Parallel()

	status := ResourceLeaseStatus{Request: &ResourceLeaseStatusRequest{
		Template: &ResolvedResourceLeaseTemplateReference{
			ResourceLeaseTemplateReference: ResourceLeaseTemplateReference{
				Kind: GlobalResourceLeaseTemplateKind,
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

func TestResourceLeaseFailureRetryLifecycle(t *testing.T) {
	t.Parallel()

	requestor := &resourcelease.AccessEntity{Name: "alice", Type: resourcelease.AccessEntityTypeUser}
	br := &ResourceLease{Status: ResourceLeaseStatus{
		Phase:   ResourceLeasePhaseApproved,
		Request: &ResourceLeaseStatusRequest{},
		Review: &ReviewInfo{
			Reviewer: requestor,
			Verdict:  ResourceLeaseVerdictApproved,
		},
	}}

	require.NoError(t, br.FailLease(
		ResourceLeaseFailureStageActivation,
		ResourceLeasePhaseApproved,
		"ResourceApplyFailed",
		"configmaps is forbidden",
	))
	assert.Equal(t, ResourceLeasePhaseFailed, br.Status.Phase)
	require.NotNil(t, br.Status.Failure)
	assert.Equal(t, ResourceLeaseFailureStageActivation, br.Status.Failure.Stage)

	require.NoError(t, br.RetryLease(requestor))
	assert.Equal(t, ResourceLeasePhaseRetrying, br.Status.Phase)
	require.NoError(t, br.CompleteRetry())
	assert.Equal(t, ResourceLeasePhaseApproved, br.Status.Phase)
	assert.Nil(t, br.Status.Failure)
	assert.Equal(t, requestor, br.Status.Review.Reviewer)

	require.Len(t, br.Status.Transitions, 3)
	assert.Equal(t, ResourceLeasePhaseFailed, br.Status.Transitions[0].Type)
	assert.Equal(t, ResourceLeasePhaseRetrying, br.Status.Transitions[1].Type)
	assert.Equal(t, ResourceLeasePhaseApproved, br.Status.Transitions[2].Type)
	assert.Equal(t, requestor.Name, br.Status.Transitions[1].Actor.Name)
	assert.Equal(t, requestor.Type, br.Status.Transitions[1].Actor.Type)
}

func TestExpireLeaseTracksActor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		entity      *resourcelease.AccessEntity
		wantReason  string
		wantMessage string
	}{
		{
			name:        "automatic expiration",
			wantReason:  "ExpiredBySystem",
			wantMessage: "Resource lease expired automatically",
		},
		{
			name: "user expiration",
			entity: &resourcelease.AccessEntity{
				Name: "alice",
				Type: resourcelease.AccessEntityTypeUser,
			},
			wantReason:  "ExpiredByUser",
			wantMessage: "Resource lease expired by alice",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			br := &ResourceLease{Status: ResourceLeaseStatus{Phase: ResourceLeasePhaseActive}}
			require.NoError(t, br.ExpireLease(testCase.entity))

			transition := br.LatestTransition(ResourceLeasePhaseExpired)
			require.NotNil(t, transition)
			assert.Equal(t, testCase.wantReason, transition.Reason)
			assert.Equal(t, testCase.wantMessage, transition.Message)
			if testCase.entity == nil {
				assert.Equal(t, resourcelease.AccessEntityTypeSystem, transition.Actor.Type)
				assert.Equal(t, capsuleControllerActorName, transition.Actor.Name)
			} else {
				assert.Equal(t, testCase.entity.Name, transition.Actor.Name)
				assert.Equal(t, testCase.entity.Type, transition.Actor.Type)
			}
		})
	}
}

func TestSetReviewer(t *testing.T) {
	reviewer := &resourcelease.AccessEntity{Type: resourcelease.AccessEntityTypeUser, Name: "test-user"}
	tests := []struct {
		name             string
		ar               *ResourceLease
		entity           *resourcelease.AccessEntity
		conditionMessage string
		verdict          ResourceLeaseVerdict
		expectedReview   *ReviewInfo
	}{
		{
			name:             "set reviewer successfully",
			ar:               &ResourceLease{},
			entity:           reviewer,
			conditionMessage: "Approved",
			verdict:          ResourceLeaseVerdictApproved,
			expectedReview: &ReviewInfo{
				Reviewer: reviewer,
				Message:  "Approved",
				Verdict:  ResourceLeaseVerdictApproved,
			},
		},
		{
			name:             "nil entity does not set reviewer",
			ar:               &ResourceLease{},
			entity:           nil,
			conditionMessage: "No review",
			verdict:          ResourceLeaseVerdictDenied,
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

func TestTransitionResourceLeasePhase(t *testing.T) {
	request := &ResourceLease{}
	now := metav1.Now()
	tests := []struct {
		name        string
		phase       ResourceLeasePhase
		initPhase   ResourceLeasePhase
		expectError bool
	}{
		{
			name:        "create an uninitialized request",
			phase:       ResourceLeasePhaseCreated,
			initPhase:   "",
			expectError: false,
		},
		{
			name:        "valid transition",
			phase:       ResourceLeasePhaseRequested,
			initPhase:   "",
			expectError: false,
		},
		{
			name:        "deny approved request",
			phase:       ResourceLeasePhaseDenied,
			initPhase:   ResourceLeasePhaseApproved,
			expectError: true,
		},
		{
			name:        "activate unapproved request",
			phase:       ResourceLeasePhaseActive,
			initPhase:   ResourceLeasePhaseRequested,
			expectError: true,
		},
		{
			name:        "expire a requested request",
			phase:       ResourceLeasePhaseExpired,
			initPhase:   ResourceLeasePhaseRequested,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request.Status.Phase = tt.initPhase
			err := request.transitionResourceLeasePhase(tt.phase, "test", "reason", now, nil)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.phase, request.Status.Phase)
			}
		})
	}
}

func TestApproveLease(t *testing.T) {
	br := &ResourceLease{}
	entity := &resourcelease.AccessEntity{Name: "reviewer", Type: resourcelease.AccessEntityTypeUser}
	props := &ResourceLeaseStatusRequest{Duration: &metav1.Duration{Duration: time.Hour}}
	err := br.ApproveLease(entity, props, "Approved")
	require.NoError(t, err)
	assert.Equal(t, ResourceLeasePhaseApproved, br.Status.Phase)
	assert.Equal(t, entity, br.Status.Review.Reviewer)
	assert.Equal(t, props.Duration, br.Status.Request.Duration)
}

func TestGenerateRequestStatusResolvesLifecycleDefaults(t *testing.T) {
	keepFor := resourcelease.ExtendedDuration(5 * time.Minute)
	resources := []apiruntime.RenderedResource{{
		Targets: []runtime.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap"}`)}},
	}}
	brt := &GlobalResourceLeaseTemplate{Spec: GlobalResourceLeaseTemplateSpec{
		DefaultDuration: &metav1.Duration{Duration: time.Minute},
		MaxDuration:     &metav1.Duration{Duration: time.Hour},
		KeepFor:         &keepFor,
		Approvals: resourcelease.ApprovalSpec{
			Auto:       true,
			Conditions: []string{"true"},
		},
	}}
	br := &ResourceLease{Status: ResourceLeaseStatus{Request: &ResourceLeaseStatusRequest{
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

func TestDenyLease(t *testing.T) {
	br := &ResourceLease{}
	entity := &resourcelease.AccessEntity{Name: "reviewer", Type: resourcelease.AccessEntityTypeUser}
	err := br.DenyLease(entity, "Denied")
	require.NoError(t, err)
	assert.Equal(t, ResourceLeasePhaseDenied, br.Status.Phase)
	assert.Equal(t, entity, br.Status.Review.Reviewer)
	assert.Equal(t, "Denied", br.Status.Review.Message)
}

func TestRenderResources(t *testing.T) {
	br := &ResourceLease{
		Spec: ResourceLeaseSpec{
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
func TestActivateLease(t *testing.T) {
	tests := []struct {
		name               string
		br                 *ResourceLease
		entity             *resourcelease.AccessEntity
		wantErr            string
		expectedPhase      ResourceLeasePhase
		expectActiveNotNil bool
		expectActiveUntil  bool
	}{
		{
			name:               "activate not approved",
			br:                 &ResourceLease{},
			entity:             &resourcelease.AccessEntity{Name: "user", Type: resourcelease.AccessEntityTypeUser},
			wantErr:            "can only activate an approved request",
			expectedPhase:      ResourceLeasePhaseActive,
			expectActiveNotNil: false,
			expectActiveUntil:  false,
		},
		{
			name: "activate with approved duration",
			br: &ResourceLease{
				Status: ResourceLeaseStatus{
					Request: &ResourceLeaseStatusRequest{
						Duration: &metav1.Duration{Duration: 30 * time.Minute},
					},
					Phase: ResourceLeasePhaseApproved,
				},
			},
			entity:             &resourcelease.AccessEntity{Name: "user", Type: resourcelease.AccessEntityTypeUser},
			wantErr:            "",
			expectedPhase:      ResourceLeasePhaseActive,
			expectActiveNotNil: true,
			expectActiveUntil:  true,
		},
		{
			name: "activate with requested duration when approved duration is nil",
			br: &ResourceLease{
				Spec: ResourceLeaseSpec{Duration: &metav1.Duration{Duration: time.Minute}},
				Status: ResourceLeaseStatus{
					Request: &ResourceLeaseStatusRequest{
						Duration: nil,
					},
					Phase: ResourceLeasePhaseApproved,
				},
			},
			entity:             &resourcelease.AccessEntity{Name: "user", Type: resourcelease.AccessEntityTypeUser},
			wantErr:            "",
			expectedPhase:      ResourceLeasePhaseActive,
			expectActiveNotNil: true,
			expectActiveUntil:  true,
		},
		{
			name: "activate without request properties",
			br: &ResourceLease{
				Status: ResourceLeaseStatus{
					Request: nil,
					Phase:   ResourceLeasePhaseApproved,
				},
			},
			entity:             &resourcelease.AccessEntity{Name: "user", Type: resourcelease.AccessEntityTypeUser},
			wantErr:            "",
			expectedPhase:      ResourceLeasePhaseActive,
			expectActiveNotNil: true,
			expectActiveUntil:  false,
		},
		{
			name: "activate with nil entity",
			br: &ResourceLease{
				Status: ResourceLeaseStatus{
					Request: &ResourceLeaseStatusRequest{
						Duration: &metav1.Duration{Duration: 30 * time.Minute},
					},
					Phase: ResourceLeasePhaseApproved,
				},
			},
			entity:             nil,
			wantErr:            "",
			expectedPhase:      ResourceLeasePhaseActive,
			expectActiveNotNil: true,
			expectActiveUntil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.br.ActivateLease(tt.entity)
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
