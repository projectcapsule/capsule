// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"testing"
	"time"

	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

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

func TestInitializeFromTemplate(t *testing.T) {
	br := &BreakRequest{}
	brt := &BreakRequestTemplate{
		Spec: BreakRequestTemplateSpec{
			Items:           breaktheglass.TemplateItems{"key": {}},
			DefaultDuration: &metav1.Duration{Duration: time.Minute},
			MaxDuration:     metav1.Duration{Duration: time.Hour},
			KeepFor:         5,
		},
	}

	br.InitializeFromTemplate(brt)
	assert.NotNil(t, br.Status.Template)
	assert.Equal(t, brt.Spec.Items, br.Status.Template.Items)
	assert.Equal(t, brt.Spec.DefaultDuration, br.Status.Template.DefaultDuration)
	assert.Equal(t, brt.Spec.MaxDuration, br.Status.Template.MaxDuration)
	assert.Equal(t, brt.Spec.KeepFor, br.Status.Template.KeepFor)
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

func TestDenyRequest(t *testing.T) {
	br := &BreakRequest{}
	entity := &breaktheglass.AccessEntity{Name: "reviewer", Type: breaktheglass.AccessEntityTypeUser}
	err := br.DenyRequest(entity, "Denied")
	require.NoError(t, err)
	assert.Equal(t, RequestPhaseDenied, br.Status.Phase)
	assert.Equal(t, entity, br.Status.Review.Reviewer)
	assert.Equal(t, "Denied", br.Status.Review.Message)
}

func TestRenderItems(t *testing.T) {
	br := &BreakRequest{
		Spec: BreakRequestSpec{
			Params: map[string]runtime.RawExtension{
				"test": {Raw: []byte(`{"key":"value"}`)},
			},
		},
	}
	ti := breaktheglass.TemplateItems{
		"test": {
			ParamSchema:      runtime.RawExtension{Raw: []byte(`{"type":"object","properties":{"key":{"type":"string"}}}`)},
			ManifestTemplate: runtime.RawExtension{Raw: []byte(`{"kind":"ConfigMap"}`)},
		},
	}

	items, err := br.RenderItems(ti)
	require.NoError(t, err)
	assert.NotNil(t, items["test"])
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
			name: "activate not approved",
			br: &BreakRequest{
				Status: BreakRequestStatus{
					Template: &TemplateProperties{
						MaxDuration:     metav1.Duration{Duration: time.Hour},
						DefaultDuration: &metav1.Duration{Duration: time.Minute},
					},
				},
			},
			entity:             &breaktheglass.AccessEntity{Name: "user", Type: breaktheglass.AccessEntityTypeUser},
			wantErr:            "can only activate an approved request",
			expectedPhase:      RequestPhaseActive,
			expectActiveNotNil: true,
			expectActiveUntil:  true,
		},
		{
			name: "activate with approved duration",
			br: &BreakRequest{
				Status: BreakRequestStatus{
					Template: &TemplateProperties{
						MaxDuration:     metav1.Duration{Duration: time.Hour},
						DefaultDuration: &metav1.Duration{Duration: time.Minute},
					},
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
			name: "activate with default duration when approved duration is nil",
			br: &BreakRequest{
				Status: BreakRequestStatus{
					Template: &TemplateProperties{
						MaxDuration:     metav1.Duration{Duration: time.Hour},
						DefaultDuration: &metav1.Duration{Duration: time.Minute},
					},
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
					Template: &TemplateProperties{
						MaxDuration:     metav1.Duration{Duration: time.Hour},
						DefaultDuration: &metav1.Duration{Duration: time.Minute},
					},
					Approved: nil,
					Phase:    RequestPhaseApproved,
				},
			},
			entity:             &breaktheglass.AccessEntity{Name: "user", Type: breaktheglass.AccessEntityTypeUser},
			wantErr:            "",
			expectedPhase:      RequestPhaseActive,
			expectActiveNotNil: true,
			expectActiveUntil:  true,
		},
		{
			name: "activate with nil entity",
			br: &BreakRequest{
				Status: BreakRequestStatus{
					Template: &TemplateProperties{
						MaxDuration:     metav1.Duration{Duration: time.Hour},
						DefaultDuration: &metav1.Duration{Duration: time.Minute},
					},
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
		{
			name: "activate without template",
			br: &BreakRequest{
				Status: BreakRequestStatus{
					Template: nil,
					Approved: &ApprovedProperties{
						Duration: &metav1.Duration{Duration: 30 * time.Minute},
					},
					Phase: RequestPhaseApproved,
				},
			},
			entity:             &breaktheglass.AccessEntity{Name: "user", Type: breaktheglass.AccessEntityTypeUser},
			wantErr:            "template not set",
			expectedPhase:      RequestPhaseActive,
			expectActiveNotNil: true,
			expectActiveUntil:  false,
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
