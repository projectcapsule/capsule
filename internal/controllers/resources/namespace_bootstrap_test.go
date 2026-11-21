// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	tpl "github.com/projectcapsule/capsule/pkg/template"
)

func TestNamespaceBootstrapUsesLiveTargetOwnership(t *testing.T) {
	for _, change := range []string{"unchanged", "moved", "recreated", "terminating", "missing", "read-error", "label-only", "stale-owner", "label-owner-disagree"} {
		t.Run(change, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, capsulev1beta2.AddToScheme(scheme))
			a := &capsulev1beta2.Tenant{Name: "a", UID: "a-uid"}
			b := &capsulev1beta2.Tenant{Name: "b", UID: "b-uid"}
			stale := &corev1.Namespace{Name: "target", UID: "old-target", Labels: map[string]string{meta.TenantLabel: a.Name}, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: a.Name, UID: a.UID}}}
			live := stale.DeepCopy()
			wantTenant := "a"
			switch change {
			case "moved", "recreated":
				live.Labels[meta.TenantLabel] = b.Name
				live.OwnerReferences[0].Name, live.OwnerReferences[0].UID = b.Name, b.UID
				wantTenant = "b"
				if change == "recreated" {
					live.UID = "new-target"
				}
			case "terminating":
				live.Finalizers = []string{meta.ControllerFinalizer}
				live.DeletionTimestamp = new(metav1.Now())
				wantTenant = "a"
			case "missing", "read-error":
				wantTenant = ""
			case "label-only":
				live.OwnerReferences = nil
			case "stale-owner":
				live.OwnerReferences[0].UID = "previous-a"
			case "label-owner-disagree":
				live.Labels[meta.TenantLabel] = b.Name
				wantTenant = "b"
			}
			cached := fake.NewClientBuilder().WithScheme(scheme).WithObjects(a, b, stale).Build()
			liveBuilder := fake.NewClientBuilder().WithScheme(scheme)
			if change != "missing" {
				liveBuilder.WithObjects(live)
			}
			var resolved []string
			cachedNamespaceReads, liveReads, sourceLists := 0, 0, 0
			cacheClient := interceptor.NewClient(cached, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					switch obj.(type) {
					case *corev1.Namespace:
						cachedNamespaceReads++
					case *capsulev1beta2.Tenant:
						resolved = append(resolved, key.Name)
					}
					return c.Get(ctx, key, obj, opts...)
				},
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					sourceLists++
					return c.List(ctx, list, opts...)
				},
			})
			reader := interceptor.NewClient(liveBuilder.Build(), interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				liveReads++
				if change == "read-error" {
					return errors.New("live namespace unavailable")
				}
				return c.Get(ctx, key, obj, opts...)
			}})
			r := &NamespaceTrigger{client: cacheClient, reader: reader}
			_, err := r.reconcileNamespace(t.Context(), reconcile.Request{Name: stale.Name})
			if change == "read-error" {
				require.ErrorContains(t, err, "live namespace unavailable")
			} else {
				require.NoError(t, err)
			}
			require.Zero(t, cachedNamespaceReads)
			require.Equal(t, 1, liveReads)
			if change != "unchanged" && change != "moved" && change != "recreated" {
				require.Zero(t, sourceLists, "invalid, missing or terminating ownership must not gather replication sources")
			}
			if wantTenant == "" {
				require.Empty(t, resolved)
			} else {
				require.Equal(t, []string{wantTenant}, resolved, "the target's current owner must select the replication tenant")
			}
		})
	}
}

func TestTenantNamespaceAdditionsKeepAuthoritativeFallbacks(t *testing.T) {
	for _, change := range []string{"addition", "removal", "labels", "replacement", "termination", "reorder"} {
		t.Run(change, func(t *testing.T) {
			old := &capsulev1beta2.Tenant{Name: "a", UID: "uid", Labels: map[string]string{"selected": "true"}}
			old.Status.Namespaces = []string{"a-0", "a-1"}
			after := old.DeepCopy()
			after.Status.Namespaces = append(after.Status.Namespaces, "a-2")
			switch change {
			case "removal":
				after.Status.Namespaces = []string{"a-0"}
			case "labels":
				after.Labels["selected"] = "false"
			case "replacement":
				after.UID = "replaced"
			case "termination":
				after.DeletionTimestamp = new(metav1.Now())
			case "reorder":
				after.Status.Namespaces = []string{"a-1", "a-0"}
			}
			require.Equal(t, change == "addition", namespaceAdditionsOnly(old, after))
		})
	}
	scheme := runtime.NewScheme()
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	var objects []client.Object
	for _, kind := range []string{"raw", "generator", "copied", "health", "tenant-scope", "unselected"} {
		obj := &capsulev1beta2.GlobalTenantResource{Name: kind, Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: api.ResourceScopeNamespace, TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"selected": "true"}}}}
		obj.Spec.Resources = []capsulev1beta2.ResourceSpec{{}}
		switch kind {
		case "generator":
			obj.Spec.Resources[0].Generators = []capsulev1beta2.TemplateItemSpec{{Template: "{{ .tenant.status.namespaces }}"}}
		case "copied":
			obj.Spec.Resources[0].NamespacedItems = []tpl.ResourceReference{{}}
		case "health":
			obj.Spec.TenantResourceCommonSpec = healthSpec()
		case "tenant-scope":
			obj.Spec.Scope = api.ResourceScopeTenant
		case "unselected":
			obj.Spec.TenantSelector.MatchLabels["selected"] = "other"
		}
		objects = append(objects, obj)
	}
	r := &globalResourceController{client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build(), log: logr.Discard()}
	old := &capsulev1beta2.Tenant{Name: "a", UID: "uid", Labels: map[string]string{"selected": "true"}}
	after := old.DeepCopy()
	after.Status.Namespaces = []string{"a-0"}
	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer q.ShutDown()
	r.tenantHandler().Update(t.Context(), event.UpdateEvent{ObjectOld: old, ObjectNew: after}, q)
	var got []string
	for q.Len() > 0 {
		item, _ := q.Get()
		got = append(got, item.Name)
		q.Done(item)
	}
	require.ElementsMatch(t, []string{"generator", "copied", "health", "tenant-scope"}, got)
}

func TestNamespaceBootstrapFairnessAndConcurrentNotifications(t *testing.T) {
	var pending namespaceBootstraps
	for i := range 40 {
		pending.add("a", fmt.Sprintf("a-%d", i))
	}
	pending.add("b", "b-0")
	require.Equal(t, "a-0", pending.first("a").name)
	require.Equal(t, "b-0", pending.first("b").name)
	first := pending.first("a")
	pending.add("a", "a-0")
	require.True(t, pending.done("a", first), "notification during apply must survive completion")
	require.Equal(t, "a-0", pending.first("a").name)
	require.True(t, pending.done("a", pending.first("a")))
	require.Equal(t, "a-1", pending.first("a").name)
	require.False(t, pending.done("b", pending.first("b")))
	require.Empty(t, pending.first("b").name)
	require.Len(t, pending.pending, 1, "completed tenant queues must be removed")
}

func TestNamespaceBootstrapProfileLabelEvents(t *testing.T) {
	for _, change := range []string{"label-added", "label-changed", "label-removed", "unchanged", "annotations-only", "terminating", "unowned"} {
		t.Run(change, func(t *testing.T) {
			r := &NamespaceTrigger{}
			queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
			defer queue.ShutDown()
			before := &corev1.Namespace{Name: "tenant-a-target", Labels: map[string]string{meta.TenantLabel: "tenant-a", "projectcapsule.dev/profile": "excluded"}}
			after := before.DeepCopy()
			want := true
			switch change {
			case "label-added":
				after.Labels["projectcapsule.dev/selected"] = "true"
			case "label-changed":
				after.Labels["projectcapsule.dev/profile"] = "selected"
			case "label-removed":
				delete(after.Labels, "projectcapsule.dev/profile")
			case "annotations-only":
				after.Annotations = map[string]string{"projectcapsule.dev/note": "updated"}
				want = false
			case "terminating":
				after.Labels["projectcapsule.dev/profile"] = "selected"
				after.DeletionTimestamp = new(metav1.Now())
				want = false
			case "unowned":
				delete(after.Labels, meta.TenantLabel)
				want = false
			case "unchanged":
				want = false
			}
			r.bootstrapHandler().Update(t.Context(), event.UpdateEvent{ObjectOld: before, ObjectNew: after}, queue)
			require.Equal(t, want, queue.Len() == 1)
			if want {
				require.Equal(t, after.Name, r.bootstraps.first("tenant-a").name)
				require.Empty(t, r.bootstraps.first("tenant-b").name)
			}
		})
	}
}

func BenchmarkNamespaceBootstrapProfileEvents(b *testing.B) {
	for _, count := range []int{1, 64} {
		for _, changed := range []bool{false, true} {
			b.Run(fmt.Sprintf("labels=%d/changed=%t", count, changed), func(b *testing.B) {
				before := &corev1.Namespace{Name: "target", Labels: map[string]string{meta.TenantLabel: "tenant-a"}}
				for i := range count {
					before.Labels[fmt.Sprintf("projectcapsule.dev/label-%d", i)] = "value"
				}
				after := before.DeepCopy()
				if changed {
					after.Labels["projectcapsule.dev/label-0"] = "selected"
				}
				r := &NamespaceTrigger{}
				h := r.bootstrapHandler()
				queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
				defer queue.ShutDown()
				e := event.UpdateEvent{ObjectOld: before, ObjectNew: after}
				h.Update(b.Context(), e, queue)
				b.ReportAllocs()
				for b.Loop() {
					h.Update(b.Context(), e, queue)
				}
			})
		}
	}
}

func TestNamespaceBootstrapFailedSourceFallsBackWithoutBlockingTenant(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	tnt := &capsulev1beta2.Tenant{Name: "tenant", UID: "tenant-uid"}
	namespace := &corev1.Namespace{Name: "target", Labels: map[string]string{meta.TenantLabel: tnt.Name}, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: tnt.Name, UID: tnt.UID}}}
	source := &capsulev1beta2.GlobalTenantResource{Name: "broken", UID: "source-uid", Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: api.ResourceScopeNamespace, TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Resources: []capsulev1beta2.ResourceSpec{{RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(`{}`)}}}}}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tnt, namespace, source).Build()
	mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion})
	r := &NamespaceTrigger{client: c, reader: c, collector: NewCollector(c, mapper)}
	r.globalClients = impersonatedClientLoader[*capsulev1beta2.GlobalTenantResource]{client: c, resolve: func(configuration.Configuration, logr.Logger, *capsulev1beta2.GlobalTenantResource) *meta.NamespacedRFC1123ObjectReferenceWithNamespace {
		return nil
	}}
	r.bootstraps.add(tnt.Name, namespace.Name)
	r.bootstraps.add(tnt.Name, "next")
	result, err := r.Reconcile(t.Context(), reconcile.Request{Namespace: "bootstrap", Name: tnt.Name})
	require.NoError(t, err)
	require.Positive(t, result.RequeueAfter)
	require.Equal(t, "next", r.bootstraps.first(tnt.Name).name)
	fresh := &capsulev1beta2.GlobalTenantResource{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(source), fresh))
	require.NotEmpty(t, fresh.Annotations[meta.ReconcileAnnotation], "failed source must have a durable full-reconcile request")
}

func TestNamespaceBootstrapConcurrentDeduplication(t *testing.T) {
	var pending namespaceBootstraps
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for i := range 100 {
				pending.add("tenant", fmt.Sprint(i))
			}
		})
	}
	wg.Wait()
	require.Len(t, pending.pending["tenant"].order, 100)
	for pending.first("tenant").name != "" {
		pending.done("tenant", pending.first("tenant"))
	}
	require.Empty(t, pending.pending)
}

func TestNamespaceBootstrapBoundsTenantQueueAndRetainsParallelism(t *testing.T) {
	r := &NamespaceTrigger{bootstrapWorkers: 4}
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer queue.ShutDown()
	for _, tenant := range []string{"a", "b"} {
		for i := range 1000 {
			r.enqueueBootstrap(tenant, fmt.Sprintf("namespace-%d", i), queue)
		}
	}
	require.Equal(t, 8, queue.Len(), "a burst queues at most the configured worker count per Tenant")
	require.Len(t, r.bootstraps.pending, 8)
	for queue.Len() > 0 {
		request, _ := queue.Get()
		key := bootstrapQueueKey(request)
		first := r.bootstraps.first(key)
		require.NotEmpty(t, first.name)
		require.True(t, r.bootstraps.done(key, first))
		queue.Done(request)
	}
}

func TestNamespaceBootstrapHonorsDependencyCheckpoints(t *testing.T) {
	for _, global := range []bool{false, true} {
		for _, state := range []string{"ready", "not-ready", "missing", "stale-health", "foreign-namespace"} {
			t.Run(fmt.Sprintf("global=%t/%s", global, state), func(t *testing.T) {
				scheme := runtime.NewScheme()
				require.NoError(t, capsulev1beta2.AddToScheme(scheme))
				var source, dependency client.Object = &capsulev1beta2.GlobalTenantResource{Name: "source"}, &capsulev1beta2.GlobalTenantResource{Name: "dependency", Generation: 2}
				if !global {
					source = &capsulev1beta2.TenantResource{Name: "source", Namespace: "tenant-a"}
					dependency = &capsulev1beta2.TenantResource{Name: "dependency", Namespace: "tenant-a", Generation: 2}
				}
				spec, _ := healthSource(source)
				spec.DependsOn = []meta.LocalRFC1123ObjectReference{{Name: "dependency"}}
				depSpec, depStatus := healthSource(dependency)
				depStatus.ObservedGeneration = dependency.GetGeneration()
				depStatus.Conditions = meta.ConditionList{{Type: meta.ReadyCondition, Status: metav1.ConditionTrue}}
				switch state {
				case "not-ready":
					depStatus.Conditions[0].Status = metav1.ConditionFalse
				case "stale-health":
					depSpec.HealthChecks = &capsulev1beta2.TenantResourceHealthChecks{Wait: true}
					depStatus.ObservedGeneration = 1
				case "foreign-namespace":
					if global {
						dependency.SetName("other")
					} else {
						dependency.SetNamespace("tenant-b")
					}
				}
				builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&capsulev1beta2.Tenant{Name: "tenant-a", UID: "tenant-a"}, &capsulev1beta2.Tenant{Name: "tenant-b", UID: "tenant-b"})
				if state != "missing" {
					builder.WithObjects(dependency)
				}
				r := &NamespaceTrigger{client: builder.Build()}
				err := r.bootstrapDependenciesReady(t.Context(), source)
				if state == "ready" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
					if global {
						err = r.replicateGlobal(t.Context(), logr.Discard(), source.(*capsulev1beta2.GlobalTenantResource), capsulev1beta2.Tenant{}, &corev1.Namespace{Name: "new"})
					} else {
						err = r.replicateNamespaced(t.Context(), logr.Discard(), source.(*capsulev1beta2.TenantResource), capsulev1beta2.Tenant{}, &corev1.Namespace{Name: "new"})
					}
					require.ErrorContains(t, err, "dependency")
				}
			})
		}
	}
}

func TestNamespaceBootstrapSkipsUnselectedUninitializedSources(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &NamespaceTrigger{client: c, reader: c, collector: NewCollector(c, k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion}))}
	r.globalClients = impersonatedClientLoader[*capsulev1beta2.GlobalTenantResource]{client: c, resolve: func(configuration.Configuration, logr.Logger, *capsulev1beta2.GlobalTenantResource) *meta.NamespacedRFC1123ObjectReferenceWithNamespace {
		return nil
	}}
	r.namespacedClients = impersonatedClientLoader[*capsulev1beta2.TenantResource]{client: c, resolve: func(configuration.Configuration, logr.Logger, *capsulev1beta2.TenantResource) *meta.NamespacedRFC1123ObjectReferenceWithNamespace {
		return nil
	}}
	spec := capsulev1beta2.TenantResourceCommonSpec{Resources: []capsulev1beta2.ResourceSpec{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "selected"}}}}}
	tenant := capsulev1beta2.Tenant{Name: "a", UID: "a"}
	namespace := &corev1.Namespace{Name: "a-new", Labels: map[string]string{meta.TenantLabel: tenant.Name}}
	require.NoError(t, r.replicateGlobal(t.Context(), logr.Discard(), &capsulev1beta2.GlobalTenantResource{Name: "source", Spec: capsulev1beta2.GlobalTenantResourceSpec{TenantResourceCommonSpec: spec}}, tenant, namespace))
	require.NoError(t, r.replicateNamespaced(t.Context(), logr.Discard(), &capsulev1beta2.TenantResource{Name: "source", Namespace: "a-source", Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: spec}}, tenant, namespace))
}

func TestNamespaceBootstrapValidatesHealthBeforeApplying(t *testing.T) {
	conditions, err := cache.NewCELCache()
	require.NoError(t, err)
	spec := capsulev1beta2.TenantResourceCommonSpec{HealthChecks: &capsulev1beta2.TenantResourceHealthChecks{Wait: true, Expressions: []capsulev1beta2.TenantResourceHealthCheckExpression{{APIVersion: "v1", Kind: "ConfigMap", Current: "object.("}}}}
	r := &NamespaceTrigger{conditions: conditions}
	// No loader is installed: invalid checks must stop before resolving clients
	// or writing targets, even when a previous valid revision has a finalizer.
	global := &capsulev1beta2.GlobalTenantResource{Name: "source", Finalizers: []string{meta.ControllerFinalizer}, Spec: capsulev1beta2.GlobalTenantResourceSpec{TenantResourceCommonSpec: spec}}
	local := &capsulev1beta2.TenantResource{Name: "source", Namespace: "tenant-a", Finalizers: []string{meta.ControllerFinalizer}, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: spec}}
	require.ErrorContains(t, r.replicateGlobal(t.Context(), logr.Discard(), global, capsulev1beta2.Tenant{}, &corev1.Namespace{Name: "new"}), "healthChecks.expressions[0]")
	require.ErrorContains(t, r.replicateNamespaced(t.Context(), logr.Discard(), local, capsulev1beta2.Tenant{}, &corev1.Namespace{Name: "new"}), "healthChecks.expressions[0]")
}

func BenchmarkNamespaceBootstrapValidation(b *testing.B) {
	for _, mode := range []string{"disabled", "wait", "expressions"} {
		b.Run(mode, func(b *testing.B) {
			conditions, err := cache.NewCELCache()
			require.NoError(b, err)
			source := &capsulev1beta2.GlobalTenantResource{}
			spec := &source.Spec.TenantResourceCommonSpec
			if mode != "disabled" {
				spec.HealthChecks = &capsulev1beta2.TenantResourceHealthChecks{Wait: true}
			}
			if mode == "expressions" {
				spec.HealthChecks.Expressions = []capsulev1beta2.TenantResourceHealthCheckExpression{{APIVersion: "v1", Kind: "ConfigMap", Current: "true"}}
			}
			require.NoError(b, validateReplicationChecks(source, spec, false, nil, conditions))
			b.ReportAllocs()
			for b.Loop() {
				if err := validateReplicationChecks(source, spec, false, nil, conditions); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkNamespaceBootstrapScheduling(b *testing.B) {
	for _, count := range []int{1, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			var pending namespaceBootstraps
			for i := range count {
				pending.add("other-tenant", fmt.Sprint(i))
			}
			b.ReportAllocs()
			for b.Loop() {
				pending.add("tenant", "namespace")
				pending.done("tenant", pending.first("tenant"))
			}
		})
	}
}
