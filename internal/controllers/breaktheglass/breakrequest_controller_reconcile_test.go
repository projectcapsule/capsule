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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mc "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
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
	mtConfigMapRendered = runtime.RawExtension{Raw: []byte(`
{
  "apiVersion": "v1",
  "kind": "ConfigMap",
  "metadata": {
    "name": "test-configmap"
  },
  "data": {
    "test": "test-value"
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
	matchBrt := gm.AssignableToTypeOf(&capsulev1beta2.GlobalBreakRequestTemplate{})
	matchLocalBrt := gm.AssignableToTypeOf(&capsulev1beta2.BreakRequestTemplate{})
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
					Template: capsulev1beta2.GlobalBreakRequestTemplateReference{
						Kind: capsulev1beta2.GlobalBreakRequestTemplateKind,
						Name: templateName,
					},
				},
			},
			mocks: func(cl *mc.MockClient, scl *mc.MockSubResourceWriter) {
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBr).Return(nil)
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBrt).
					Do(func(_ any, _ any, brt *capsulev1beta2.GlobalBreakRequestTemplate, _ ...any) {
						brt.ResourceVersion = "1234"
						brt.Spec.Resources = []apiruntime.ResourceTemplate{{
							Targets: []runtime.RawExtension{mtConfigMapRendered},
						}}
					})
				scl.EXPECT().Update(gm.Any(), matchBr, gm.Any()).Return(nil)
			},
			verify: func(t *testing.T, br *capsulev1beta2.BreakRequest) {
				assert.Len(t, br.Status.Conditions, 2)
				assert.Equal(t, capsulev1beta2.RequestPhaseRequested, br.Status.Phase)
				require.NotNil(t, br.Status.Template)
				assert.Equal(t, capsulev1beta2.GlobalBreakRequestTemplateKind, br.Status.Template.Kind)
				assert.Equal(t, templateName, br.Status.Template.Name)
				assert.Equal(t, "1234", br.Status.Template.ResourceVersion)
				require.NotNil(t, br.Status.Approved)
				require.Len(t, br.Status.Approved.Resources, 1)
				require.Len(t, br.Status.Approved.Resources[0].Targets, 1)
				ready := findCondition(br.Status.Conditions, meta.ReadyCondition)
				require.NotNil(t, ready)
				assert.Equal(t, v1.ConditionTrue, ready.Status)
			},
		},
		{
			name: "newly created with namespace-local template",
			br: &capsulev1beta2.BreakRequest{
				ObjectMeta: v1.ObjectMeta{Name: resourceName, Namespace: "team-a"},
				Spec: capsulev1beta2.BreakRequestSpec{Template: capsulev1beta2.BreakRequestTemplateReference{
					Kind: capsulev1beta2.BreakRequestTemplateKind,
					Name: templateName,
				}},
			},
			mocks: func(cl *mc.MockClient, scl *mc.MockSubResourceWriter) {
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBr).Return(nil)
				cl.EXPECT().
					Get(
						gm.Any(),
						client.ObjectKey{Namespace: "team-a", Name: templateName},
						matchLocalBrt,
					).
					Do(func(_ any, _ any, brt *capsulev1beta2.BreakRequestTemplate, _ ...any) {
						brt.Namespace = "team-a"
						brt.ResourceVersion = "local-1234"
						brt.Spec.Resources = []apiruntime.ResourceTemplate{{
							Targets: []runtime.RawExtension{mtConfigMapRendered},
						}}
					})
				scl.EXPECT().Update(gm.Any(), matchBr, gm.Any()).Return(nil)
			},
			verify: func(t *testing.T, br *capsulev1beta2.BreakRequest) {
				require.NotNil(t, br.Status.Template)
				assert.Equal(t, capsulev1beta2.BreakRequestTemplateKind, br.Status.Template.Kind)
				assert.Equal(t, templateName, br.Status.Template.Name)
				assert.Equal(t, "local-1234", br.Status.Template.ResourceVersion)
				require.NotNil(t, br.Status.Approved)
				require.Len(t, br.Status.Approved.Resources, 1)
				require.Len(t, br.Status.Approved.Resources[0].Targets, 1)
			},
		},
		{
			name: "rendering failure is reported as not ready",
			br: &capsulev1beta2.BreakRequest{
				ObjectMeta: v1.ObjectMeta{Name: resourceName, Namespace: "default"},
				Spec: capsulev1beta2.BreakRequestSpec{
					Template: capsulev1beta2.GlobalBreakRequestTemplateReference{
						Kind: capsulev1beta2.GlobalBreakRequestTemplateKind,
						Name: templateName,
					},
				},
			},
			mocks: func(cl *mc.MockClient, scl *mc.MockSubResourceWriter) {
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBrt).
					Do(func(_ any, _ any, brt *capsulev1beta2.GlobalBreakRequestTemplate, _ ...any) {
						brt.Spec.ParamSchema = &psString
						brt.Spec.Resources = []apiruntime.ResourceTemplate{{
							Targets: []runtime.RawExtension{mtConfigMapParameterized},
						}}
					})
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBr).Return(nil)
				scl.EXPECT().Update(gm.Any(), matchBr, gm.Any()).Return(nil)
			},
			verify: func(t *testing.T, br *capsulev1beta2.BreakRequest) {
				assert.Empty(t, br.Status.Phase)
				require.NotNil(t, br.Status.Approved)
				assert.Empty(t, br.Status.Approved.Resources)
				ready := findCondition(br.Status.Conditions, meta.ReadyCondition)
				require.NotNil(t, ready)
				assert.Equal(t, v1.ConditionFalse, ready.Status)
				assert.Equal(t, templateRenderingFailedReason, ready.Reason)
				assert.Contains(t, ready.Message, "invalid params")
			},
			wantErr: true,
		},
		{
			name: "approved but not yet to start",
			br: &capsulev1beta2.BreakRequest{
				ObjectMeta: v1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: capsulev1beta2.BreakRequestSpec{
					Template: capsulev1beta2.GlobalBreakRequestTemplateReference{
						Kind: capsulev1beta2.GlobalBreakRequestTemplateKind,
						Name: templateName,
					},
				},
				Status: capsulev1beta2.BreakRequestStatus{
					Phase: capsulev1beta2.RequestPhaseApproved,
					Conditions: []v1.Condition{
						{
							LastTransitionTime: v1.Now(),
							Message:            "rendered resources are ready",
							Reason:             meta.SucceededReason,
							Status:             v1.ConditionTrue,
							Type:               meta.ReadyCondition,
						},
						{
							LastTransitionTime: v1.Now(),
							Message:            "Access request approved",
							Reason:             "ApprovedByUser",
							Status:             "True",
							Type:               "Approved",
						},
					},
					Approved: &capsulev1beta2.ApprovedProperties{
						StartTime: ptr.To(v1.NewTime(time.Now().Add(time.Hour))),
					},
				},
			},
			mocks: func(cl *mc.MockClient, scl *mc.MockSubResourceWriter) {
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBrt).Return(nil)
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBr).Return(nil).Times(3)
				cl.EXPECT().Update(gm.Any(), matchBr, gm.Any()).Return(nil)
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
					Template: capsulev1beta2.GlobalBreakRequestTemplateReference{
						Kind: capsulev1beta2.GlobalBreakRequestTemplateKind,
						Name: templateName,
					},
					Params: &runtime.RawExtension{Raw: []byte(`{"testValue": "test-value"}`)},
				},
				Status: capsulev1beta2.BreakRequestStatus{
					Phase: capsulev1beta2.RequestPhaseApproved,
					Conditions: []v1.Condition{
						{
							LastTransitionTime: v1.Now(),
							Message:            "rendered resources are ready",
							Reason:             meta.SucceededReason,
							Status:             v1.ConditionTrue,
							Type:               meta.ReadyCondition,
						},
						{
							LastTransitionTime: v1.Now(),
							Message:            "Access request approved",
							Reason:             "ApprovedByUser",
							Status:             "True",
							Type:               "Approved",
						},
					},
					Approved: &capsulev1beta2.ApprovedProperties{
						StartTime: ptr.To(v1.Now()),
						Resources: []apiruntime.RenderedResource{{
							Targets: []runtime.RawExtension{mtConfigMapRendered},
						}},
					},
				},
			},
			mocks: func(cl *mc.MockClient, scl *mc.MockSubResourceWriter) {
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBrt).Return(nil)
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBr).Return(nil).Times(3)
				cl.EXPECT().Update(gm.Any(), matchBr, gm.Any()).Return(nil)
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchUs).
					Return(apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "test-configmap"))
				cl.EXPECT().Patch(gm.Any(), matchUs, gm.Any(), gm.Any()).Return(nil).Times(2)
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchUs).Return(nil)
				scl.EXPECT().Update(gm.Any(), matchBr, gm.Any()).Return(nil)
			},
			verify: func(t *testing.T, br *capsulev1beta2.BreakRequest) {
				assert.Equal(t, capsulev1beta2.RequestPhaseActive, br.Status.Phase)
				require.NotNil(t, br.Status.Approved)
				assert.Len(t, br.Status.Approved.Resources, 1)
				assert.Len(t, br.Status.Approved.Resources[0].Targets, 1)
				assert.Equal(t, uint(1), br.Status.Size)
				require.Len(t, br.Status.ProcessedItems, 1)

				managed := br.Status.ProcessedItems[0]
				assert.Equal(t, "ConfigMap", managed.Kind)
				assert.Equal(t, "test-configmap", managed.Name)
				assert.Equal(t, "default", managed.Namespace)
				assert.Equal(t, v1.ConditionTrue, managed.Status)
				assert.Equal(t, meta.ReadyCondition, managed.Type)
				assert.True(t, managed.Created)
				assert.False(t, managed.ClusterScoped)

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

				obj := br.Status.Approved.Resources[0].Targets[0].Object
				co, ok := obj.(client.Object)
				assert.True(t, ok)
				assert.Empty(t, co.GetOwnerReferences())
				assert.Equal(t, meta.ValueAppBreakTheGlassManager, co.GetLabels()[meta.AppManagedByLabel])
			},
		},
		{
			name: "approved target apply fails",
			br: &capsulev1beta2.BreakRequest{
				ObjectMeta: v1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: capsulev1beta2.BreakRequestSpec{
					Template: capsulev1beta2.GlobalBreakRequestTemplateReference{
						Kind: capsulev1beta2.GlobalBreakRequestTemplateKind,
						Name: templateName,
					},
					Params: &runtime.RawExtension{Raw: []byte(`{"testValue": "test-value"}`)},
				},
				Status: capsulev1beta2.BreakRequestStatus{
					Phase: capsulev1beta2.RequestPhaseApproved,
					Conditions: []v1.Condition{{
						LastTransitionTime: v1.Now(),
						Message:            "rendered resources are ready",
						Reason:             meta.SucceededReason,
						Status:             v1.ConditionTrue,
						Type:               meta.ReadyCondition,
					}},
					Approved: &capsulev1beta2.ApprovedProperties{
						StartTime: ptr.To(v1.Now()),
						Resources: []apiruntime.RenderedResource{{
							Targets: []runtime.RawExtension{mtConfigMapRendered},
						}},
					},
				},
			},
			mocks: func(cl *mc.MockClient, scl *mc.MockSubResourceWriter) {
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBrt).Return(nil)
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBr).Return(nil).Times(3)
				cl.EXPECT().Update(gm.Any(), matchBr, gm.Any()).Return(nil)
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchUs).
					Return(apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "test-configmap"))
				cl.EXPECT().Patch(gm.Any(), matchUs, gm.Any(), gm.Any()).Return(assert.AnError)
				scl.EXPECT().Update(gm.Any(), matchBr, gm.Any()).Return(nil)
			},
			verify: func(t *testing.T, br *capsulev1beta2.BreakRequest) {
				assert.Equal(t, capsulev1beta2.RequestPhaseApproved, br.Status.Phase)
				assert.Equal(t, uint(1), br.Status.Size)
				require.Len(t, br.Status.ProcessedItems, 1)
				assert.Equal(t, v1.ConditionFalse, br.Status.ProcessedItems[0].Status)
				assert.Contains(t, br.Status.ProcessedItems[0].Message, "apply failed")
				assert.True(t, br.Status.ProcessedItems[0].Created)
				ready := findCondition(br.Status.Conditions, meta.ReadyCondition)
				require.NotNil(t, ready)
				assert.Equal(t, v1.ConditionFalse, ready.Status)
				assert.Equal(t, resourceApplyFailedReason, ready.Reason)
			},
			wantErr: true,
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

func TestBreakRequestReconcilerLoadsNamespacedTemplateLocally(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	require.NoError(t, capsulev1beta2.AddToScheme(s))

	teamA := &capsulev1beta2.BreakRequestTemplate{
		ObjectMeta: v1.ObjectMeta{Name: templateName, Namespace: "team-a"},
	}
	teamB := &capsulev1beta2.BreakRequestTemplate{
		ObjectMeta: v1.ObjectMeta{Name: templateName, Namespace: "team-b"},
	}
	r := &BreakRequestReconciler{Client: fake.NewClientBuilder().WithScheme(s).WithObjects(teamA, teamB).Build()}
	br := &capsulev1beta2.BreakRequest{
		ObjectMeta: v1.ObjectMeta{Namespace: "team-a"},
		Spec: capsulev1beta2.BreakRequestSpec{Template: capsulev1beta2.BreakRequestTemplateReference{
			Kind: capsulev1beta2.BreakRequestTemplateKind,
			Name: templateName,
		}},
	}

	loaded, err := r.loadTemplate(context.Background(), br)
	require.NoError(t, err)
	local, ok := loaded.(*capsulev1beta2.BreakRequestTemplate)
	require.True(t, ok)
	assert.Equal(t, "team-a", local.Namespace)
}

func findCondition(conditions []v1.Condition, conditionType string) *v1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}

	return nil
}
