// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcelease

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gm "go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2/klogr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	mc "github.com/projectcapsule/capsule/internal/mocks/client"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/resourcelease"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
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

func TestResourceLeaseReconciler_reconcile(t *testing.T) {
	s := scheme.Scheme
	_ = capsulev1beta2.AddToScheme(s)

	matchBr := gm.AssignableToTypeOf(&capsulev1beta2.ResourceLease{})
	matchBrt := gm.AssignableToTypeOf(&capsulev1beta2.GlobalResourceLeaseTemplate{})
	matchLocalBrt := gm.AssignableToTypeOf(&capsulev1beta2.ResourceLeaseTemplate{})
	matchUs := gm.AssignableToTypeOf(&unstructured.Unstructured{})

	tests := []struct {
		name    string
		br      *capsulev1beta2.ResourceLease
		mocks   func(cl *mc.MockClient, scl *mc.MockSubResourceWriter)
		verify  func(t *testing.T, br *capsulev1beta2.ResourceLease)
		wantErr bool
	}{
		{
			name: "newly created",
			br: &capsulev1beta2.ResourceLease{
				ObjectMeta: v1.ObjectMeta{
					Name:              resourceName,
					Namespace:         "default",
					CreationTimestamp: v1.NewTime(time.Date(2026, time.September, 2, 8, 0, 0, 0, time.UTC)),
				},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Requestor: resourcelease.AccessEntity{
						Name: "alice",
						Type: resourcelease.AccessEntityTypeUser,
					},
					Template: capsulev1beta2.GlobalResourceLeaseTemplateReference{
						Kind: capsulev1beta2.GlobalResourceLeaseTemplateKind,
						Name: templateName,
					},
				},
			},
			mocks: func(cl *mc.MockClient, scl *mc.MockSubResourceWriter) {
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBr).Return(nil)
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBrt).
					Do(func(_ any, _ any, brt *capsulev1beta2.GlobalResourceLeaseTemplate, _ ...any) {
						brt.ResourceVersion = "1234"
						brt.Spec.Resources = []apiruntime.ResourceTemplate{{
							Targets: []runtime.RawExtension{mtConfigMapRendered},
						}}
					})
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchUs).
					Return(apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "test-configmap"))
				cl.EXPECT().Patch(gm.Any(), matchUs, gm.Any(), gm.Any(), gm.Any()).Return(nil)
				scl.EXPECT().Update(gm.Any(), matchBr, gm.Any()).Return(nil)
			},
			verify: func(t *testing.T, br *capsulev1beta2.ResourceLease) {
				assert.Len(t, br.Status.Conditions, 1)
				assert.Equal(t, capsulev1beta2.ResourceLeasePhaseRequested, br.Status.Phase)
				require.Len(t, br.Status.Transitions, 2)
				assert.Equal(t, capsulev1beta2.ResourceLeasePhaseCreated, br.Status.Transitions[0].Type)
				assert.Equal(t, "alice", br.Status.Transitions[0].Actor.Name)
				assert.Equal(t, br.CreationTimestamp, br.Status.Transitions[0].Timestamp)
				assert.Equal(t, capsulev1beta2.ResourceLeasePhaseRequested, br.Status.Transitions[1].Type)
				require.NotNil(t, br.Status.Request.Template)
				assert.Equal(t, capsulev1beta2.GlobalResourceLeaseTemplateKind, br.Status.Request.Template.Kind)
				assert.Equal(t, templateName, br.Status.Request.Template.Name)
				assert.Equal(t, "1234", br.Status.Request.Template.ResourceVersion)
				require.NotNil(t, br.Status.Request)
				require.Len(t, br.Status.Request.Resources, 1)
				require.Len(t, br.Status.Request.Resources[0].Targets, 1)
				ready := findCondition(br.Status.Conditions, meta.ReadyCondition)
				require.NotNil(t, ready)
				assert.Equal(t, v1.ConditionTrue, ready.Status)
			},
		},
		{
			name: "newly created with namespace-local template",
			br: &capsulev1beta2.ResourceLease{
				ObjectMeta: v1.ObjectMeta{Name: resourceName, Namespace: "team-a"},
				Spec: capsulev1beta2.ResourceLeaseSpec{Template: capsulev1beta2.ResourceLeaseTemplateReference{
					Kind: capsulev1beta2.ResourceLeaseTemplateKind,
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
					Do(func(_ any, _ any, brt *capsulev1beta2.ResourceLeaseTemplate, _ ...any) {
						brt.Namespace = "team-a"
						brt.ResourceVersion = "local-1234"
						brt.Spec.Resources = []apiruntime.ResourceTemplate{{
							Targets: []runtime.RawExtension{mtConfigMapRendered},
						}}
					})
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchUs).
					Return(apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "test-configmap"))
				cl.EXPECT().Patch(gm.Any(), matchUs, gm.Any(), gm.Any(), gm.Any()).Return(nil)
				scl.EXPECT().Update(gm.Any(), matchBr, gm.Any()).Return(nil)
			},
			verify: func(t *testing.T, br *capsulev1beta2.ResourceLease) {
				require.NotNil(t, br.Status.Request.Template)
				assert.Equal(t, capsulev1beta2.ResourceLeaseTemplateKind, br.Status.Request.Template.Kind)
				assert.Equal(t, templateName, br.Status.Request.Template.Name)
				assert.Equal(t, "local-1234", br.Status.Request.Template.ResourceVersion)
				require.NotNil(t, br.Status.Request)
				require.Len(t, br.Status.Request.Resources, 1)
				require.Len(t, br.Status.Request.Resources[0].Targets, 1)
			},
		},
		{
			name: "rendering failure is reported as not ready",
			br: &capsulev1beta2.ResourceLease{
				ObjectMeta: v1.ObjectMeta{Name: resourceName, Namespace: "default"},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: capsulev1beta2.GlobalResourceLeaseTemplateReference{
						Kind: capsulev1beta2.GlobalResourceLeaseTemplateKind,
						Name: templateName,
					},
				},
			},
			mocks: func(cl *mc.MockClient, scl *mc.MockSubResourceWriter) {
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBrt).
					Do(func(_ any, _ any, brt *capsulev1beta2.GlobalResourceLeaseTemplate, _ ...any) {
						brt.Spec.ParamSchema = &psString
						brt.Spec.Resources = []apiruntime.ResourceTemplate{{
							Targets: []runtime.RawExtension{mtConfigMapParameterized},
						}}
					})
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBr).Return(nil)
				scl.EXPECT().Update(gm.Any(), matchBr, gm.Any()).Return(nil)
			},
			verify: func(t *testing.T, br *capsulev1beta2.ResourceLease) {
				assert.Equal(t, capsulev1beta2.ResourceLeasePhaseCreated, br.Status.Phase)
				require.Len(t, br.Status.Transitions, 1)
				assert.Equal(t, capsulev1beta2.ResourceLeasePhaseCreated, br.Status.Transitions[0].Type)
				require.NotNil(t, br.Status.Request)
				assert.Empty(t, br.Status.Request.Resources)
				ready := findCondition(br.Status.Conditions, meta.ReadyCondition)
				require.NotNil(t, ready)
				assert.Equal(t, v1.ConditionFalse, ready.Status)
				assert.Equal(t, templateRenderingFailedReason, ready.Reason)
				assert.Contains(t, ready.Message, "invalid params")
			},
			wantErr: true,
		},
		{
			name: "dry-run failure is recoverable before review",
			br: &capsulev1beta2.ResourceLease{
				ObjectMeta: v1.ObjectMeta{Name: resourceName, Namespace: "default"},
				Spec: capsulev1beta2.ResourceLeaseSpec{Template: capsulev1beta2.ResourceLeaseTemplateReference{
					Kind: capsulev1beta2.GlobalResourceLeaseTemplateKind,
					Name: templateName,
				}},
			},
			mocks: func(cl *mc.MockClient, scl *mc.MockSubResourceWriter) {
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBrt).
					Do(func(_ any, _ any, brt *capsulev1beta2.GlobalResourceLeaseTemplate, _ ...any) {
						brt.Spec.Resources = []apiruntime.ResourceTemplate{{
							Targets: []runtime.RawExtension{mtConfigMapRendered},
						}}
					})
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBr).Return(nil)
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchUs).
					Return(apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "test-configmap"))
				cl.EXPECT().Patch(gm.Any(), matchUs, gm.Any(), gm.Any(), gm.Any()).Return(assert.AnError)
				scl.EXPECT().Update(gm.Any(), matchBr, gm.Any()).Return(nil)
			},
			verify: func(t *testing.T, br *capsulev1beta2.ResourceLease) {
				assert.Equal(t, capsulev1beta2.ResourceLeasePhaseFailed, br.Status.Phase)
				require.NotNil(t, br.Status.Failure)
				assert.Equal(t, capsulev1beta2.ResourceLeaseFailureStagePreflight, br.Status.Failure.Stage)
				assert.Equal(t, capsulev1beta2.ResourceLeasePhaseRequested, br.Status.Failure.RetryPhase)
				assert.Equal(t, resourceDryRunFailedReason, br.Status.Failure.Reason)
				assert.Empty(t, br.Status.ProcessedItems)
				ready := findCondition(br.Status.Conditions, meta.ReadyCondition)
				require.NotNil(t, ready)
				assert.Equal(t, v1.ConditionFalse, ready.Status)
				assert.Equal(t, resourceDryRunFailedReason, ready.Reason)
			},
			wantErr: true,
		},
		{
			name: "failed request remains not ready while waiting for retry",
			br: &capsulev1beta2.ResourceLease{
				ObjectMeta: v1.ObjectMeta{Name: resourceName, Namespace: "default"},
				Status: capsulev1beta2.ResourceLeaseStatus{
					Phase: capsulev1beta2.ResourceLeasePhaseFailed,
					Failure: &capsulev1beta2.ResourceLeaseFailure{
						Stage:      capsulev1beta2.ResourceLeaseFailureStagePreflight,
						RetryPhase: capsulev1beta2.ResourceLeasePhaseRequested,
						Reason:     resourceDryRunFailedReason,
						Message:    "service account cannot patch ConfigMaps",
					},
					Conditions: []v1.Condition{{
						Type:               meta.ReadyCondition,
						Status:             v1.ConditionFalse,
						Reason:             resourceDryRunFailedReason,
						Message:            "service account cannot patch ConfigMaps",
						LastTransitionTime: v1.Now(),
					}},
				},
			},
			mocks: func(cl *mc.MockClient, scl *mc.MockSubResourceWriter) {
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBr).Return(nil)
				scl.EXPECT().Update(gm.Any(), matchBr, gm.Any()).Return(nil)
			},
			verify: func(t *testing.T, br *capsulev1beta2.ResourceLease) {
				assert.Equal(t, capsulev1beta2.ResourceLeasePhaseFailed, br.Status.Phase)
				ready := findCondition(br.Status.Conditions, meta.ReadyCondition)
				require.NotNil(t, ready)
				assert.Equal(t, v1.ConditionFalse, ready.Status)
				assert.Equal(t, resourceDryRunFailedReason, ready.Reason)
				assert.Equal(t, "service account cannot patch ConfigMaps", ready.Message)
			},
		},
		{
			name: "successful preflight retry returns to review",
			br: &capsulev1beta2.ResourceLease{
				ObjectMeta: v1.ObjectMeta{Name: resourceName, Namespace: "default"},
				Status: capsulev1beta2.ResourceLeaseStatus{
					Phase: capsulev1beta2.ResourceLeasePhaseRetrying,
					Failure: &capsulev1beta2.ResourceLeaseFailure{
						Stage:      capsulev1beta2.ResourceLeaseFailureStagePreflight,
						RetryPhase: capsulev1beta2.ResourceLeasePhaseRequested,
						Reason:     resourceDryRunFailedReason,
						Message:    "forbidden",
					},
					Request: &capsulev1beta2.ResourceLeaseStatusRequest{Resources: []apiruntime.RenderedResource{{
						Targets: []runtime.RawExtension{mtConfigMapRendered},
					}}},
				},
			},
			mocks: func(cl *mc.MockClient, scl *mc.MockSubResourceWriter) {
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchBr).Return(nil)
				cl.EXPECT().Get(gm.Any(), gm.Any(), matchUs).
					Return(apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "test-configmap"))
				cl.EXPECT().Patch(gm.Any(), matchUs, gm.Any(), gm.Any(), gm.Any()).Return(nil)
				scl.EXPECT().Update(gm.Any(), matchBr, gm.Any()).Return(nil)
			},
			verify: func(t *testing.T, br *capsulev1beta2.ResourceLease) {
				assert.Equal(t, capsulev1beta2.ResourceLeasePhaseRequested, br.Status.Phase)
				assert.Nil(t, br.Status.Failure)
				assert.Empty(t, br.Status.ProcessedItems)
				ready := findCondition(br.Status.Conditions, meta.ReadyCondition)
				require.NotNil(t, ready)
				assert.Equal(t, v1.ConditionTrue, ready.Status)
			},
		},
		{
			name: "approved but not yet to start",
			br: &capsulev1beta2.ResourceLease{
				ObjectMeta: v1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: capsulev1beta2.GlobalResourceLeaseTemplateReference{
						Kind: capsulev1beta2.GlobalResourceLeaseTemplateKind,
						Name: templateName,
					},
				},
				Status: capsulev1beta2.ResourceLeaseStatus{
					Phase: capsulev1beta2.ResourceLeasePhaseApproved,
					Conditions: []v1.Condition{
						{
							LastTransitionTime: v1.Now(),
							Message:            "rendered resources are ready",
							Reason:             meta.SucceededReason,
							Status:             v1.ConditionTrue,
							Type:               meta.ReadyCondition,
						},
					},
					Transitions: []capsulev1beta2.ResourceLeaseTransition{{
						Type:      capsulev1beta2.ResourceLeasePhaseApproved,
						Timestamp: v1.Now(),
						Reason:    "ApprovedByUser",
					}},
					Request: &capsulev1beta2.ResourceLeaseStatusRequest{
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
			verify: func(t *testing.T, br *capsulev1beta2.ResourceLease) {
				assert.Equal(t, capsulev1beta2.ResourceLeasePhaseApproved, br.Status.Phase)
				approved := br.LatestTransition(capsulev1beta2.ResourceLeasePhaseApproved)
				require.NotNil(t, approved)
				assert.Equal(t, "ApprovedByUser", approved.Reason)
			},
		},
		{
			name: "approved and ready",
			br: &capsulev1beta2.ResourceLease{
				ObjectMeta: v1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: capsulev1beta2.GlobalResourceLeaseTemplateReference{
						Kind: capsulev1beta2.GlobalResourceLeaseTemplateKind,
						Name: templateName,
					},
					Params: &runtime.RawExtension{Raw: []byte(`{"testValue": "test-value"}`)},
				},
				Status: capsulev1beta2.ResourceLeaseStatus{
					Phase: capsulev1beta2.ResourceLeasePhaseApproved,
					Conditions: []v1.Condition{
						{
							LastTransitionTime: v1.Now(),
							Message:            "rendered resources are ready",
							Reason:             meta.SucceededReason,
							Status:             v1.ConditionTrue,
							Type:               meta.ReadyCondition,
						},
					},
					Transitions: []capsulev1beta2.ResourceLeaseTransition{{
						Type:      capsulev1beta2.ResourceLeasePhaseApproved,
						Timestamp: v1.Now(),
						Reason:    "ApprovedByUser",
					}},
					Request: &capsulev1beta2.ResourceLeaseStatusRequest{
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
			verify: func(t *testing.T, br *capsulev1beta2.ResourceLease) {
				assert.Equal(t, capsulev1beta2.ResourceLeasePhaseActive, br.Status.Phase)
				require.NotNil(t, br.Status.Request)
				assert.Len(t, br.Status.Request.Resources, 1)
				assert.Len(t, br.Status.Request.Resources[0].Targets, 1)
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

				require.Len(t, br.Status.Transitions, 2)
				assert.Equal(t, capsulev1beta2.ResourceLeasePhaseApproved, br.Status.Transitions[0].Type)
				assert.Equal(t, capsulev1beta2.ResourceLeasePhaseActive, br.Status.Transitions[1].Type)

				obj := br.Status.Request.Resources[0].Targets[0].Object
				co, ok := obj.(client.Object)
				assert.True(t, ok)
				assert.Empty(t, co.GetOwnerReferences())
				assert.Equal(t, meta.ValueAppResourceLeaseManager, co.GetLabels()[meta.AppManagedByLabel])
			},
		},
		{
			name: "approved target apply fails",
			br: &capsulev1beta2.ResourceLease{
				ObjectMeta: v1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: capsulev1beta2.GlobalResourceLeaseTemplateReference{
						Kind: capsulev1beta2.GlobalResourceLeaseTemplateKind,
						Name: templateName,
					},
					Params: &runtime.RawExtension{Raw: []byte(`{"testValue": "test-value"}`)},
				},
				Status: capsulev1beta2.ResourceLeaseStatus{
					Phase: capsulev1beta2.ResourceLeasePhaseApproved,
					Conditions: []v1.Condition{{
						LastTransitionTime: v1.Now(),
						Message:            "rendered resources are ready",
						Reason:             meta.SucceededReason,
						Status:             v1.ConditionTrue,
						Type:               meta.ReadyCondition,
					}},
					Request: &capsulev1beta2.ResourceLeaseStatusRequest{
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
			verify: func(t *testing.T, br *capsulev1beta2.ResourceLease) {
				assert.Equal(t, capsulev1beta2.ResourceLeasePhaseFailed, br.Status.Phase)
				require.NotNil(t, br.Status.Failure)
				assert.Equal(t, capsulev1beta2.ResourceLeaseFailureStageActivation, br.Status.Failure.Stage)
				assert.Equal(t, capsulev1beta2.ResourceLeasePhaseApproved, br.Status.Failure.RetryPhase)
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

			r := &ResourceLeaseReconciler{
				Client: cl,
				Log:    ctrl.Log,
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

func TestRecordTransitionEventOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		phase   capsulev1beta2.ResourceLeasePhase
		reason  string
		action  string
		message string
		actor   capsulev1beta2.ResourceLeaseTransitionActor
	}{
		{
			name:    "review requested",
			phase:   capsulev1beta2.ResourceLeasePhaseRequested,
			reason:  evt.ReasonResourceLeaseReviewNeeded,
			action:  evt.ActionPendingReview,
			message: "Pending Review",
			actor: capsulev1beta2.ResourceLeaseTransitionActor{
				Name: "alice",
				Type: resourcelease.AccessEntityTypeUser,
			},
		},
		{
			name:    "approval",
			phase:   capsulev1beta2.ResourceLeasePhaseApproved,
			reason:  evt.ReasonResourceLeaseApproved,
			action:  evt.ActionApproved,
			message: "Resource lease approved by alice",
			actor: capsulev1beta2.ResourceLeaseTransitionActor{
				Name: "alice",
				Type: resourcelease.AccessEntityTypeUser,
			},
		},
		{
			name:    "denial",
			phase:   capsulev1beta2.ResourceLeasePhaseDenied,
			reason:  evt.ReasonResourceLeaseDenied,
			action:  evt.ActionDenied,
			message: "Resource lease denied by alice",
			actor: capsulev1beta2.ResourceLeaseTransitionActor{
				Name: "alice@example.com",
				Type: resourcelease.AccessEntityTypeUser,
			},
		},
		{
			name:    "activation",
			phase:   capsulev1beta2.ResourceLeasePhaseActive,
			reason:  evt.ReasonResourceLeaseActivated,
			action:  evt.ActionActivated,
			message: "Resource lease activated",
			actor: capsulev1beta2.ResourceLeaseTransitionActor{
				Name: "capsule-controller",
				Type: resourcelease.AccessEntityTypeSystem,
			},
		},
		{
			name:    "expiration",
			phase:   capsulev1beta2.ResourceLeasePhaseExpired,
			reason:  evt.ReasonResourceLeaseExpired,
			action:  evt.ActionExpired,
			message: "Resource lease expired by alice",
			actor: capsulev1beta2.ResourceLeaseTransitionActor{
				Name: "alice",
				Type: resourcelease.AccessEntityTypeUser,
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			eventScheme := runtime.NewScheme()
			require.NoError(t, eventsv1.AddToScheme(eventScheme))
			require.NoError(t, capsulev1beta2.AddToScheme(eventScheme))
			eventClient := fake.NewClientBuilder().WithScheme(eventScheme).Build()
			recorder := evt.NewEventRecorder(eventClient, klogr.New(), nil, nil)
			br := &capsulev1beta2.ResourceLease{
				TypeMeta: v1.TypeMeta{
					APIVersion: capsulev1beta2.GroupVersion.String(),
					Kind:       "ResourceLease",
				},
				ObjectMeta: v1.ObjectMeta{
					Name:      "temporary-lease",
					Namespace: "team-a",
					UID:       "request-uid",
				},
				Status: capsulev1beta2.ResourceLeaseStatus{
					Phase: testCase.phase,
					Transitions: []capsulev1beta2.ResourceLeaseTransition{{
						Type:      testCase.phase,
						Timestamp: v1.Now(),
						Actor:     testCase.actor,
						Reason:    string(testCase.phase) + "ByUser",
						Message:   testCase.message,
					}},
				},
			}
			r := &ResourceLeaseReconciler{recorder: recorder}

			ctx := context.Background()
			r.recordTransitionEvent(ctx, br, testCase.phase, testCase.reason, testCase.action)
			r.recordTransitionEvent(ctx, br, testCase.phase, testCase.reason, testCase.action)

			transition := br.LatestTransition(testCase.phase)
			require.NotNil(t, transition)
			assert.NotNil(t, transition.EventTime)

			var eventList eventsv1.EventList
			require.Eventually(t, func() bool {
				eventList.Items = nil

				return eventClient.List(ctx, &eventList, client.InNamespace(br.Namespace)) == nil &&
					len(eventList.Items) == 1
			}, time.Second, 10*time.Millisecond)

			event := eventList.Items[0]
			assert.Equal(t, testCase.reason, event.Reason)
			assert.Equal(t, testCase.action, event.Action)
			assert.Equal(t, testCase.message, event.Note)
			assert.Equal(t, eventActorLabelValue(testCase.actor.Name), event.Labels[meta.EventActorLabel])
			assert.Equal(t, testCase.actor.Type.String(), event.Labels[meta.EventActorKindLabel])
			assert.Equal(t, br.UID, event.Regarding.UID)

			eventList.Items = nil
			require.NoError(t, eventClient.List(ctx, &eventList, client.InNamespace(br.Namespace)))
			assert.Len(t, eventList.Items, 1)
		})
	}
}

func TestEventActorLabelValue(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"alice":                                 "alice",
		"alice@example.com":                     "alice_example.com-ff8d9819",
		"system:serviceaccount:team-a:reviewer": "system_serviceaccount_team-a_reviewer-9a2364e2",
		"@@@":                                   "actor-2ec847d8",
	}

	for actor, expected := range tests {
		assert.Equal(t, expected, eventActorLabelValue(actor))
	}
}

func TestReconcileDeleteSkipsRetentionForTerminatingNamespace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	deletionTime := v1.Now()
	keepUntil := v1.NewTime(time.Now().Add(time.Hour))

	tests := []struct {
		name          string
		namespace     *corev1.Namespace
		wantRequeue   bool
		wantFinalizer bool
	}{
		{
			name: "retains request while namespace is active",
			namespace: &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: "team-a"},
				Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
			},
			wantRequeue:   true,
			wantFinalizer: true,
		},
		{
			name: "removes finalizer while namespace is terminating",
			namespace: &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{
					Name:              "team-a",
					DeletionTimestamp: &deletionTime,
					Finalizers:        []string{"test.projectcapsule.dev/hold"},
				},
				Status: corev1.NamespaceStatus{Phase: corev1.NamespaceTerminating},
			},
			wantFinalizer: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			s := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(s))
			require.NoError(t, capsulev1beta2.AddToScheme(s))

			br := &capsulev1beta2.ResourceLease{
				ObjectMeta: v1.ObjectMeta{
					Name:              "temporary-lease",
					Namespace:         testCase.namespace.Name,
					DeletionTimestamp: &deletionTime,
					Finalizers:        []string{meta.ControllerFinalizer},
				},
				Status: capsulev1beta2.ResourceLeaseStatus{KeepUntil: &keepUntil},
			}
			cl := fake.NewClientBuilder().WithScheme(s).WithObjects(testCase.namespace, br).Build()
			r := &ResourceLeaseReconciler{Client: cl}

			result, err := r.reconcileDelete(ctx, ctrl.Log, br)
			require.NoError(t, err)
			assert.Equal(t, testCase.wantRequeue, result.RequeueAfter > 0)
			assert.Equal(t, testCase.wantFinalizer, controllerutil.ContainsFinalizer(br, meta.ControllerFinalizer))
		})
	}
}

func TestDefaultTargetNamespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		target   string
		request  string
		expected string
	}{
		{
			name:     "defaults omitted namespace",
			request:  "request-namespace",
			expected: "request-namespace",
		},
		{
			name:     "preserves rendered namespace",
			target:   "selected-namespace",
			request:  "request-namespace",
			expected: "selected-namespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			obj := &unstructured.Unstructured{}
			obj.SetNamespace(tt.target)
			defaultTargetNamespace(obj, tt.request)
			assert.Equal(t, tt.expected, obj.GetNamespace())
		})
	}
}

func TestResourceLeaseReconcilerLoadsNamespacedTemplateLocally(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	require.NoError(t, capsulev1beta2.AddToScheme(s))

	teamA := &capsulev1beta2.ResourceLeaseTemplate{
		ObjectMeta: v1.ObjectMeta{Name: templateName, Namespace: "team-a"},
	}
	teamB := &capsulev1beta2.ResourceLeaseTemplate{
		ObjectMeta: v1.ObjectMeta{Name: templateName, Namespace: "team-b"},
	}
	r := &ResourceLeaseReconciler{Client: fake.NewClientBuilder().WithScheme(s).WithObjects(teamA, teamB).Build()}
	br := &capsulev1beta2.ResourceLease{
		ObjectMeta: v1.ObjectMeta{Namespace: "team-a"},
		Spec: capsulev1beta2.ResourceLeaseSpec{Template: capsulev1beta2.ResourceLeaseTemplateReference{
			Kind: capsulev1beta2.ResourceLeaseTemplateKind,
			Name: templateName,
		}},
	}

	loaded, err := r.loadTemplate(context.Background(), br)
	require.NoError(t, err)
	local, ok := loaded.(*capsulev1beta2.ResourceLeaseTemplate)
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
