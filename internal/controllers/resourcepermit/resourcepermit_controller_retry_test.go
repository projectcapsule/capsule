// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepermit

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/resourcepermit"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestResourcePermitActivationRetry(t *testing.T) {
	t.Parallel()

	for _, finalizerPresent := range []bool{true, false} {
		t.Run(fmt.Sprintf("finalizer present=%t", finalizerPresent), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, capsulev1beta2.AddToScheme(scheme))
			reviewer := &resourcepermit.AccessEntity{Name: "alice", Type: resourcepermit.AccessEntityTypeUser}
			template := &capsulev1beta2.GlobalResourcePermitTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "retry-template"},
				Spec: capsulev1beta2.GlobalResourcePermitTemplateSpec{
					Approvals: resourcepermit.ApprovalSpec{Approvers: capsulerbac.UserListSpec{{
						Kind: capsulerbac.UserOwner, Name: reviewer.Name,
					}}},
				},
			}
			target := &corev1.ConfigMap{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
				ObjectMeta: metav1.ObjectMeta{Name: "retry-target", Namespace: "permit-test"},
				Data:       map[string]string{"key": "value"},
			}
			permit := &capsulev1beta2.ResourcePermit{
				ObjectMeta: metav1.ObjectMeta{
					Name: "retry-permit", Namespace: target.Namespace, UID: "retry-permit-uid",
					Finalizers: []string{"tests.projectcapsule.dev/retain"},
				},
				Spec: capsulev1beta2.ResourcePermitSpec{
					Template: capsulev1beta2.GlobalResourcePermitTemplateReference{
						Kind: capsulev1beta2.GlobalResourcePermitTemplateKind, Name: template.Name,
					},
				},
			}
			require.NoError(t, permit.ApprovePermit(reviewer, &capsulev1beta2.ResourcePermitStatusRequest{
				Approvals: &template.Spec.Approvals,
				Resources: []apiruntime.RenderedResource{{Targets: []runtime.RawExtension{{Object: target.DeepCopy()}}}},
			}, "approved before permissions were revoked"))
			permit.SetReady(metav1.ConditionTrue, meta.SucceededReason, "preflight succeeded")
			approvedReview := permit.Status.Review.DeepCopy()
			base := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(permit).
				WithObjects(template, permit).Build()
			denyApply := true
			cl := interceptor.NewClient(base, interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if denyApply && obj.GetObjectKind().GroupVersionKind() == corev1.SchemeGroupVersion.WithKind("ConfigMap") {
						return apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, obj.GetName(), fmt.Errorf("write access revoked"))
					}

					return c.Patch(ctx, obj, patch, opts...)
				},
			})
			r := &ResourcePermitReconciler{Client: cl}
			key := client.ObjectKeyFromObject(permit)
			current := &capsulev1beta2.ResourcePermit{}
			require.NoError(t, cl.Get(ctx, key, current))
			_, err := r.reconcile(ctx, ctrl.Log, current)
			require.ErrorContains(t, err, "write access revoked")
			require.NoError(t, cl.Get(ctx, key, current))
			require.Equal(t, capsulev1beta2.ResourcePermitPhaseFailed, current.Status.Phase)
			require.NotNil(t, current.Status.Failure)
			require.Equal(t, capsulev1beta2.ResourcePermitFailureStageActivation, current.Status.Failure.Stage)
			require.Equal(t, resourceApplyFailedReason, current.Status.Failure.Reason)
			require.True(t, controllerutil.ContainsFinalizer(current, meta.ControllerFinalizer))
			require.True(t, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(target), &corev1.ConfigMap{})))

			if !finalizerPresent {
				controllerutil.RemoveFinalizer(current, meta.ControllerFinalizer)
				require.NoError(t, cl.Update(ctx, current))
			}
			denyApply = false
			require.NoError(t, current.RetryPermit(reviewer))
			require.NoError(t, cl.Status().Update(ctx, current))
			require.NoError(t, cl.Get(ctx, key, current))
			_, err = r.reconcile(ctx, ctrl.Log, current)
			require.NoError(t, err, "restoring write access must allow the approved snapshot to activate")
			require.NoError(t, cl.Get(ctx, key, current))
			require.Equal(t, capsulev1beta2.ResourcePermitPhaseActive, current.Status.Phase)
			require.Nil(t, current.Status.Failure)
			require.Equal(t, approvedReview, current.Status.Review)
			require.True(t, k8smeta.IsStatusConditionTrue(current.Status.Conditions, meta.ReadyCondition))
			require.ElementsMatch(t, []string{"tests.projectcapsule.dev/retain", meta.ControllerFinalizer}, current.Finalizers)
			require.Len(t, current.Status.ProcessedItems, 1)
			require.Equal(t, metav1.ConditionTrue, current.Status.ProcessedItems[0].Status)
			retryApproval := current.LatestTransition(capsulev1beta2.ResourcePermitPhaseApproved)
			require.NotNil(t, retryApproval)
			require.Equal(t, "RetrySucceeded", retryApproval.Reason)
			actual := &corev1.ConfigMap{}
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(target), actual))
			require.Equal(t, target.Data, actual.Data)
		})
	}
}
