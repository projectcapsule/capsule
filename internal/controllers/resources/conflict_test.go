// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/processor"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func TestGlobalReplicationFirstReconcileConflicts(t *testing.T) {
	for _, tc := range []struct {
		name        string
		failures    int
		fieldOwner  bool
		wantReady   bool
		attempts    int
		staleCache  string
		readFailure bool
	}{
		{name: "transient", failures: 1, wantReady: true, attempts: 2},
		{name: "stale-notfound", failures: 1, wantReady: true, attempts: 2, staleCache: "notfound"},
		{name: "stale-version", failures: 1, wantReady: true, attempts: 2, staleCache: "version"},
		{name: "failed-observation", failures: 1, attempts: 1, readFailure: true},
		{name: "exhausted", failures: 4, attempts: 4},
		{name: "field-ownership", failures: 1, fieldOwner: true, attempts: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, capsulev1beta2.AddToScheme(scheme))
			mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion})
			mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), k8smeta.RESTScopeNamespace)
			parent := &capsulev1beta2.GlobalTenantResource{
				APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "GlobalTenantResource", Name: "conflict-" + tc.name, UID: "parent-uid", Generation: 1,
				Spec: capsulev1beta2.GlobalTenantResourceSpec{
					Scope: api.ResourceScopeNamespace, TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"selected": "true"}},
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						Cordoned:        new(false),
						PruningOnDelete: new(true),
						Settings:        capsulev1beta2.TenantResourceCommonSpecSettings{Adopt: new(false), Force: new(false)},
						ResyncPeriod:    metav1.Duration{Duration: time.Hour},
						Resources:       []capsulev1beta2.ResourceSpec{{RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"target"},"data":{"key":"value"}}`)}}}},
					},
				},
			}
			cfg := &capsulev1beta2.CapsuleConfiguration{Name: "default"}
			objects := []client.Object{parent, cfg}
			for _, name := range []string{"tenant-a", "tenant-b"} {
				tenant := &capsulev1beta2.Tenant{Name: name, UID: types.UID(name), Status: capsulev1beta2.TenantStatus{Namespaces: []string{name}}}
				if name == "tenant-a" {
					tenant.Labels = map[string]string{"selected": "true"}
				}
				objects = append(objects, tenant, &corev1.Namespace{Name: name, Labels: map[string]string{meta.TenantLabel: name}, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: name, UID: tenant.UID}}})
			}
			attempts, failedReads, targetReads := 0, 0, 0
			c := fake.NewClientBuilder().WithScheme(scheme).WithRESTMapper(mapper).WithObjects(objects...).WithStatusSubresource(parent).WithReturnManagedFields().WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if key.Name == "target" {
						targetReads++
					}
					if tc.readFailure && key.Name == "target" && attempts > 0 && failedReads == 0 {
						failedReads++
						return apierrors.NewServiceUnavailable("target read unavailable")
					}
					return c.Get(ctx, key, obj, opts...)
				},
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					obj.SetUID("target-uid")
					obj.SetCreationTimestamp(metav1.Now())
					return c.Create(ctx, obj, opts...)
				},
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if patch.Type() == types.ApplyPatchType {
						attempts++
						if attempts <= tc.failures {
							err := apierrors.NewConflict(corev1.Resource("configmaps"), obj.GetName(), errors.New("concurrent writer"))
							if tc.fieldOwner {
								err.ErrStatus.Details.Causes = []metav1.StatusCause{{Type: metav1.CauseTypeFieldManagerConflict}}
							}
							return err
						}
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
			}).Build()
			staleReads := 0
			cached := interceptor.NewClient(c, interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, target := obj.(*unstructured.Unstructured); target && tc.staleCache != "" {
					staleReads++
					if tc.staleCache == "notfound" {
						return apierrors.NewNotFound(corev1.Resource("configmaps"), key.Name)
					}
					if err := c.Get(ctx, key, obj, opts...); err != nil {
						return err
					}
					obj.SetResourceVersion("stale")
					return nil
				}
				return c.Get(ctx, key, obj, opts...)
			}})
			configuration := configuration.NewCapsuleConfiguration(t.Context(), c, c, nil, cfg.Name)
			r := &globalResourceController{
				client: cached, reader: c, log: logr.Discard(), configuration: configuration,
				processor: processor.Processor{Configuration: configuration, GatherClient: c, Mapper: mapper, AllowCrossNamespaceSelection: true},
				collector: NewCollector(c, mapper), metrics: metrics.NewGlobalTenantResourceRecorder(),
				clients: impersonatedClientLoader[*capsulev1beta2.GlobalTenantResource]{client: replicationClient{Client: cached, reader: c}, configuration: configuration, resolve: globalServiceAccount},
			}
			t.Cleanup(func() { r.metrics.DeleteMetrics(parent.Name) })
			_, err := r.Reconcile(t.Context(), reconcile.Request{Name: parent.Name})
			require.NoError(t, err, "handled item failures are reported in status")
			require.Equal(t, tc.attempts, attempts)
			require.Zero(t, staleReads, "target reads must bypass the manager cache")
			expectedReads := tc.attempts
			if tc.readFailure {
				expectedReads++
			}
			require.Equal(t, expectedReads, targetReads, "each attempt must perform exactly one authoritative target read")
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
			require.Equal(t, parent.Generation, parent.Status.ObservedGeneration)
			require.Equal(t, tc.wantReady, meta.IsStatusConditionTrue(parent.Status.Conditions, meta.ReadyCondition))
			require.Len(t, parent.Status.ProcessedItems, 1)
			item := parent.Status.ProcessedItems[0]
			require.Equal(t, "tenant-a", item.Tenant)
			require.Equal(t, tc.wantReady, item.Status == metav1.ConditionTrue)
			if !tc.wantReady {
				if tc.readFailure {
					require.Contains(t, item.Message, "target read unavailable")
					require.Equal(t, 1, failedReads)
				} else {
					require.Contains(t, item.Message, "concurrent writer")
				}
				require.False(t, item.LastApply.IsZero(), "partial creation must remain tracked after failed applies")
				parent.Spec.Resources = nil
				require.NoError(t, c.Update(t.Context(), parent))
				_, err = r.Reconcile(t.Context(), reconcile.Request{Name: parent.Name})
				require.NoError(t, err)
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
				require.Empty(t, parent.Status.ProcessedItems)
				require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKey{Namespace: "tenant-a", Name: "target"}, &corev1.ConfigMap{})), "removing a failed item must clean up its created target")
			}
			require.True(t, apierrors.IsNotFound(c.Get(t.Context(), client.ObjectKey{Namespace: "tenant-b", Name: "target"}, &corev1.ConfigMap{})))
		})
	}
}
