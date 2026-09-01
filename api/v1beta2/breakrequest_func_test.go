// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
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
			fields: []string{"review", "resources", "approved", "active", "keepUntil"},
		},
		"active period": {
			value:  ActivePeriod{},
			fields: []string{"from", "until"},
		},
		"approved properties": {
			value:  ApprovedProperties{},
			fields: []string{"keepFor", "duration", "startTime", "resources"},
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
	props := &ApprovedProperties{Duration: &metav1.Duration{Duration: time.Hour}}
	err := br.ApproveRequest(entity, props, "Approved")
	require.NoError(t, err)
	assert.Equal(t, RequestPhaseApproved, br.Status.Phase)
	assert.Equal(t, entity, br.Status.Review.Reviewer)
	assert.Equal(t, props.Duration, br.Status.Approved.Duration)
}

func TestGenerateApprovedPropertiesResolvesLifecycleDefaults(t *testing.T) {
	keepFor := breaktheglass.ExtendedDuration(5 * time.Minute)
	resources := []apiruntime.RenderedResource{{
		Targets: []runtime.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap"}`)}},
	}}
	brt := &GlobalBreakRequestTemplate{Spec: GlobalBreakRequestTemplateSpec{
		DefaultDuration: &metav1.Duration{Duration: time.Minute},
		MaxDuration:     &metav1.Duration{Duration: time.Hour},
		KeepFor:         &keepFor,
	}}
	br := &BreakRequest{Status: BreakRequestStatus{Approved: &ApprovedProperties{
		Resources: resources,
	}}}

	properties, err := br.GenerateApprovedProperties(brt)
	require.NoError(t, err)
	require.NotNil(t, properties.Duration)
	assert.Equal(t, time.Minute, properties.Duration.Duration)
	require.NotNil(t, properties.KeepFor)
	assert.Equal(t, keepFor, *properties.KeepFor)
	require.NotNil(t, properties.StartTime)
	assert.Equal(t, resources, properties.Resources)
	require.NotSame(t, &resources[0], &properties.Resources[0])

	br.Spec.Duration = &metav1.Duration{Duration: 2 * time.Hour}
	_, err = br.GenerateApprovedProperties(brt)
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
					Approved: &ApprovedProperties{
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
					Approved: &ApprovedProperties{
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
			name: "activate without approved properties",
			br: &BreakRequest{
				Status: BreakRequestStatus{
					Approved: nil,
					Phase:    RequestPhaseApproved,
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
					Approved: &ApprovedProperties{
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
