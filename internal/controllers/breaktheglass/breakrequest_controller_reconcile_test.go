// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gm "go.uber.org/mock/gomock"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mc "github.com/projectcapsule/capsule/internal/mocks/client"
)

const (
	resourceName = "test-resource"
	templateName = "test-template"
)

var (
	mtConfigMapParameterized = runtime.RawExtension{Raw: []byte(`
{
  "kind": "ConfigMap",
  "metadata": {
    "name": "test-configmap"
  },
  "data": {
    "test": "{{.testValue}}"
  }
}`)}

	psString = runtime.RawExtension{
		Raw: []byte(`{"type": "object", "required": ["testValue"], "properties": {"testValue": {"type": "string"}}}`),
	}
)

func TestBreakRequestReconciler_reconcile(t *testing.T) {
	s := scheme.Scheme
	_ = capsulev1beta2.AddToScheme(s)

	matchBr := gm.AssignableToTypeOf(&capsulev1beta2.BreakRequest{})
	matchBrt := gm.AssignableToTypeOf(&capsulev1beta2.BreakRequestTemplate{})
	matchUs := gm.AssignableToTypeOf(&unstructured.Unstructured{})

	tests := []struct {
		name    string
		br      *capsulev1beta2.BreakRequest
		mocks   func(cl *mc.MockClient, scl *mc.MockSubResourceWriter)
		verify  func(t *testing.T, br *capsulev1beta2.BreakRequest)
		wantErr bool
	}{
		{
			name: "newly created",
			br: &capsulev1beta2.BreakRequest{
				ObjectMeta: v1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: capsulev1beta2.BreakRequestSpec{
					TemplateName: templateName,
				},
			},
			mocks: func(cl *mc.MockClient, scl *mc.MockSubResourceWriter) {
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBr).Return(nil)
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBrt).Return(nil)
				scl.EXPECT().Update(gm.Any(), matchBr, gm.Any()).Return(nil)
			},
			verify: func(t *testing.T, br *capsulev1beta2.BreakRequest) {
				assert.Len(t, br.Status.Conditions, 1)
				assert.Equal(t, capsulev1beta2.RequestPhaseRequested, br.Status.Phase)
			},
		},
		{
			name: "approved but not yet to start",
			br: &capsulev1beta2.BreakRequest{
				ObjectMeta: v1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: capsulev1beta2.BreakRequestSpec{
					TemplateName: templateName,
				},
				Status: capsulev1beta2.BreakRequestStatus{
					Phase: capsulev1beta2.RequestPhaseApproved,
					Conditions: []v1.Condition{
						{
							LastTransitionTime: v1.Now(),
							Message:            "Access request approved",
							Reason:             "ApprovedByUser",
							Status:             "True",
							Type:               "Approved",
						},
					},
					Approved: &capsulev1beta2.ApprovedProperties{
						StartTime: v1.NewTime(time.Now().Add(time.Hour)),
					},
				},
			},
			mocks: func(cl *mc.MockClient, scl *mc.MockSubResourceWriter) {
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBr).Return(nil)
				scl.EXPECT().Update(gm.Any(), matchBr, gm.Any()).Return(nil)
			},
			verify: func(t *testing.T, br *capsulev1beta2.BreakRequest) {
				assert.Equal(t, capsulev1beta2.RequestPhaseApproved, br.Status.Phase)
				found := false
				for _, c := range br.Status.Conditions {
					if c.Type == "Approved" {
						found = true
						break
					}
				}
				assert.True(t, found)
			},
		},
		{
			name: "approved and ready",
			br: &capsulev1beta2.BreakRequest{
				ObjectMeta: v1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: capsulev1beta2.BreakRequestSpec{
					TemplateName: templateName,
					Params:       &runtime.RawExtension{Raw: []byte(`{"testValue": "test-value"}`)},
				},
				Status: capsulev1beta2.BreakRequestStatus{
					Phase: capsulev1beta2.RequestPhaseApproved,
					Conditions: []v1.Condition{
						{
							LastTransitionTime: v1.Now(),
							Message:            "Access request approved",
							Reason:             "ApprovedByUser",
							Status:             "True",
							Type:               "Approved",
						},
					},
					Approved: &capsulev1beta2.ApprovedProperties{
						StartTime: v1.Now(),
					},
					Template: &capsulev1beta2.TemplateProperties{
						Templates:   []runtime.RawExtension{mtConfigMapParameterized},
						ParamSchema: psString,
					},
				},
			},
			mocks: func(cl *mc.MockClient, scl *mc.MockSubResourceWriter) {
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBr).Return(nil)
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchUs).Return(nil)
				cl.EXPECT().Update(gm.Any(), matchUs, gm.Any()).Return(nil)
				scl.EXPECT().Update(gm.Any(), matchBr, gm.Any()).Return(nil)
			},
			verify: func(t *testing.T, br *capsulev1beta2.BreakRequest) {
				assert.Equal(t, capsulev1beta2.RequestPhaseActive, br.Status.Phase)
				assert.Len(t, br.Status.Approved.Templates, 1)

				foundApproved := false
				foundActive := false
				for _, c := range br.Status.Conditions {
					if c.Type == "Approved" {
						foundApproved = true
					}
					if c.Type == "Active" {
						foundActive = true
					}
				}
				assert.True(t, foundApproved)
				assert.True(t, foundActive)

				obj := br.Status.Approved.Templates[0].Object
				co, ok := obj.(client.Object)
				assert.True(t, ok)
				assert.Len(t, co.GetOwnerReferences(), 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gm.NewController(t)
			defer mockCtrl.Finish()

			cl := mc.NewMockClient(mockCtrl)
			scl := mc.NewMockSubResourceWriter(mockCtrl)

			cl.EXPECT().Status().Return(scl).AnyTimes()
			cl.EXPECT().Scheme().Return(s).AnyTimes()

			if tt.mocks != nil {
				tt.mocks(cl, scl)
			}

			r := &BreakRequestReconciler{
				Client:   cl,
				scheme:   s,
				recorder: &events.FakeRecorder{},
				Log:      ctrl.Log,
			}

			_, err := r.reconcile(context.Background(), ctrl.Log, tt.br)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if tt.verify != nil {
				tt.verify(t, tt.br)
			}
		})
	}
}
