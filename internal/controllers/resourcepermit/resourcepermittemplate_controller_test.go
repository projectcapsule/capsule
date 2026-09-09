// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepermit

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

const validPermitResourceTemplate = `apiVersion: v1
kind: ConfigMap
metadata:
  name: permitted-config
`

func TestPermitTemplateReadinessLifecycle(t *testing.T) {
	t.Parallel()

	for _, global := range []bool{false, true} {
		name := "namespaced"
		if global {
			name = "global"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			object := newReadinessTemplate(global)
			cl := permitTemplateTestClient(t, object)
			r, registry := permitTemplateTestReconciler(global, cl)
			request := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(object)}

			_, err := r.Reconcile(ctx, request)
			require.NoError(t, err)
			require.NoError(t, cl.Get(ctx, request.NamespacedName, object))
			ready := requireTemplateReady(t, object, metav1.ConditionTrue)
			require.Equal(t, meta.SucceededReason, ready.Reason)
			requireTemplateMetric(t, registry, 1)

			// A controller restart must recreate the metric without writing an
			// unchanged status or moving the condition's transition timestamp.
			resourceVersion := object.GetResourceVersion()
			transitionTime := ready.LastTransitionTime
			r, registry = permitTemplateTestReconciler(global, cl)
			_, err = r.Reconcile(ctx, request)
			require.NoError(t, err)
			require.NoError(t, cl.Get(ctx, request.NamespacedName, object))
			require.Equal(t, resourceVersion, object.GetResourceVersion())
			require.Equal(t, transitionTime, requireTemplateReady(t, object, metav1.ConditionTrue).LastTransitionTime)
			requireTemplateMetric(t, registry, 1)

			setReadinessTemplateResources(object, "{{")
			object.SetGeneration(3)
			require.NoError(t, cl.Update(ctx, object))
			_, err = r.Reconcile(ctx, request)
			require.ErrorContains(t, err, "invalid resources")
			require.NoError(t, cl.Get(ctx, request.NamespacedName, object))
			ready = requireTemplateReady(t, object, metav1.ConditionFalse)
			require.Equal(t, meta.FailedReason, ready.Reason)
			require.Contains(t, ready.Message, "invalid resources")
			requireTemplateMetric(t, registry, 0)
			if template, ok := object.(*capsulev1beta2.GlobalResourcePermitTemplate); ok {
				require.Empty(t, template.Status.Namespaces, "failed templates must not publish stale namespace access")
			}

			setReadinessTemplateResources(object, validPermitResourceTemplate)
			object.SetGeneration(4)
			require.NoError(t, cl.Update(ctx, object))
			_, err = r.Reconcile(ctx, request)
			require.NoError(t, err)
			require.NoError(t, cl.Get(ctx, request.NamespacedName, object))
			requireTemplateReady(t, object, metav1.ConditionTrue)
			requireTemplateMetric(t, registry, 1)

			require.NoError(t, cl.Delete(ctx, object))
			_, err = r.Reconcile(ctx, request)
			require.NoError(t, err)
			families, err := registry.Gather()
			require.NoError(t, err)
			require.Empty(t, families, "deleted templates must not leave metrics behind")
		})
	}
}

func TestGlobalPermitTemplateNamespaceFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	object := newReadinessTemplate(true).(*capsulev1beta2.GlobalResourcePermitTemplate)
	object.Spec.NamespaceSelectors = []selectors.NamespaceSelector{{
		LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"resource-permit": "enabled"}},
	}}
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a", Labels: map[string]string{"resource-permit": "enabled"}}}
	base := permitTemplateTestClient(t, object, namespace)
	listErr := errors.New("namespace listing unavailable")
	failList := false
	cl := interceptor.NewClient(base, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*corev1.NamespaceList); ok && failList {
				return listErr
			}
			return c.List(ctx, list, opts...)
		},
	})
	r, registry := permitTemplateTestReconciler(true, cl)
	request := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(object)}
	_, err := r.Reconcile(ctx, request)
	require.NoError(t, err)
	require.NoError(t, cl.Get(ctx, request.NamespacedName, object))
	require.Equal(t, []string{"team-a"}, object.Status.Namespaces)

	failList = true
	_, err = r.Reconcile(ctx, request)
	require.ErrorIs(t, err, listErr)
	require.NoError(t, cl.Get(ctx, request.NamespacedName, object))
	ready := requireTemplateReady(t, object, metav1.ConditionFalse)
	require.Contains(t, ready.Message, listErr.Error())
	require.Empty(t, object.Status.Namespaces)
	requireTemplateMetric(t, registry, 0)

	failList = false
	_, err = r.Reconcile(ctx, request)
	require.NoError(t, err)
	require.NoError(t, cl.Get(ctx, request.NamespacedName, object))
	requireTemplateReady(t, object, metav1.ConditionTrue)
	require.Equal(t, []string{"team-a"}, object.Status.Namespaces)
	requireTemplateMetric(t, registry, 1)
}

func TestPermitTemplateStatusRetriesConflicts(t *testing.T) {
	t.Parallel()
	for _, global := range []bool{false, true} {
		object := newReadinessTemplate(global)
		base := permitTemplateTestClient(t, object)
		updates := 0
		cl := interceptor.NewClient(base, interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, c client.Client, subresource string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				updates++
				if updates == 1 {
					return apierrors.NewConflict(schema.GroupResource{Group: "capsule.clastix.io", Resource: "templates"}, obj.GetName(), errors.New("conflict"))
				}
				return c.SubResource(subresource).Update(ctx, obj, opts...)
			},
		})
		r, registry := permitTemplateTestReconciler(global, cl)
		_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(object)})
		require.NoError(t, err)
		require.Equal(t, 2, updates)
		requireTemplateMetric(t, registry, 1)
	}
}

func TestPermitTemplateStatusSkipsStaleObjects(t *testing.T) {
	t.Parallel()
	for _, global := range []bool{false, true} {
		for _, recreated := range []bool{false, true} {
			object := newReadinessTemplate(global)
			stale := object.DeepCopyObject().(client.Object)
			if recreated {
				object.SetUID("replacement-uid")
			} else {
				object.SetGeneration(stale.GetGeneration() + 1)
			}
			cl := permitTemplateTestClient(t, object)
			if global {
				r := &GlobalResourcePermitTemplateReconciler{Client: cl}
				updated, err := r.updateStatus(context.Background(), stale.(*capsulev1beta2.GlobalResourcePermitTemplate), []string{"*"}, nil)
				require.NoError(t, err)
				require.Nil(t, updated)
			} else {
				r := &ResourcePermitTemplateReconciler{Client: cl}
				updated, err := r.updateStatus(context.Background(), stale.(*capsulev1beta2.ResourcePermitTemplate), nil)
				require.NoError(t, err)
				require.Nil(t, updated)
			}
			require.NoError(t, cl.Get(context.Background(), client.ObjectKeyFromObject(object), object))
			_, conditions := readinessTemplateStatus(object)
			require.Empty(t, conditions)
		}
	}
}

func newReadinessTemplate(global bool) client.Object {
	var object client.Object = &capsulev1beta2.ResourcePermitTemplate{}
	object.SetNamespace("team-a")
	if global {
		object = &capsulev1beta2.GlobalResourcePermitTemplate{}
	}
	object.SetName("example")
	object.SetUID("template-uid")
	object.SetGeneration(2)
	setReadinessTemplateResources(object, validPermitResourceTemplate)
	return object
}

func setReadinessTemplateResources(object client.Object, template string) {
	resources := []apiruntime.ResourceTemplate{{Template: template}}
	switch object := object.(type) {
	case *capsulev1beta2.ResourcePermitTemplate:
		object.Spec.Resources = resources
	case *capsulev1beta2.GlobalResourcePermitTemplate:
		object.Spec.Resources = resources
	}
}

func permitTemplateTestClient(t *testing.T, objects ...client.Object) client.WithWatch {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&capsulev1beta2.ResourcePermitTemplate{}, &capsulev1beta2.GlobalResourcePermitTemplate{}).
		WithObjects(objects...).Build()
}

func permitTemplateTestReconciler(global bool, cl client.Client) (reconcile.Reconciler, *prometheus.Registry) {
	registry := prometheus.NewRegistry()
	if global {
		recorder := metrics.NewGlobalResourcePermitTemplateRecorder()
		registry.MustRegister(recorder.Collectors()...)
		return &GlobalResourcePermitTemplateReconciler{Client: cl, Metrics: recorder}, registry
	}
	recorder := metrics.NewResourcePermitTemplateRecorder()
	registry.MustRegister(recorder.Collectors()...)
	return &ResourcePermitTemplateReconciler{Client: cl, Metrics: recorder}, registry
}

func readinessTemplateStatus(object client.Object) (int64, meta.ConditionList) {
	switch object := object.(type) {
	case *capsulev1beta2.ResourcePermitTemplate:
		return object.Status.ObservedGeneration, object.Status.Conditions
	case *capsulev1beta2.GlobalResourcePermitTemplate:
		return object.Status.ObservedGeneration, object.Status.Conditions
	default:
		panic("unexpected template type")
	}
}

func requireTemplateReady(t *testing.T, object client.Object, status metav1.ConditionStatus) meta.Condition {
	t.Helper()
	observed, conditions := readinessTemplateStatus(object)
	require.Equal(t, object.GetGeneration(), observed)
	ready := conditions.GetConditionByType(meta.ReadyCondition)
	require.NotNil(t, ready)
	require.Equal(t, status, ready.Status)
	require.Equal(t, object.GetGeneration(), ready.ObservedGeneration)
	require.False(t, ready.LastTransitionTime.IsZero())
	return *ready
}

func requireTemplateMetric(t *testing.T, registry *prometheus.Registry, value float64) {
	t.Helper()
	families, err := registry.Gather()
	require.NoError(t, err)
	require.Len(t, families, 1)
	require.Len(t, families[0].Metric, 1)
	require.Equal(t, value, families[0].Metric[0].GetGauge().GetValue())
}
