// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	discoveryfake "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	namespaceindex "github.com/projectcapsule/capsule/pkg/runtime/indexers/namespace"
)

type cleanupTestConfiguration struct{ configuration.Configuration }

func (cleanupTestConfiguration) AllowServiceAccountPromotion() bool { return false }
func (cleanupTestConfiguration) Administrators() rbac.UserListSpec  { return nil }

func namespaceCleanupFixture(t testing.TB, objects ...client.Object) (*Manager, *dynamicfake.FakeDynamicClient) {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, networkingv1.AddToScheme, rbacv1.AddToScheme, capsulev1beta2.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithStatusSubresource(&capsulev1beta2.Tenant{}).
		WithIndex(&corev1.Namespace{}, namespaceindex.OwnerReferenceIndex, func(obj client.Object) []string {
			for _, ref := range obj.GetOwnerReferences() {
				if ref.Kind == "Tenant" {
					return []string{ref.Name}
				}
			}
			return nil
		}).Build()
	discovery := &benchmarkDiscovery{FakeDiscovery: &discoveryfake.FakeDiscovery{Fake: &ktesting.Fake{}}}
	discovery.Resources = []*metav1.APIResourceList{{GroupVersion: "v1", APIResources: []metav1.APIResource{{Name: "configmaps", Kind: "ConfigMap", Namespaced: true, Verbs: metav1.Verbs{"get", "list", "delete", "patch"}}}}}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{{Group: "example.com", Version: "v1", Resource: "widgets"}: "WidgetList"})
	return &Manager{Client: cl, reader: cl, DynamicClient: dyn, DiscoveryClient: discovery, Metrics: metrics.NewTenantRecorder(), Configuration: cleanupTestConfiguration{}, Log: logr.Discard()}, dyn
}

func cleanupOwnedNamespace(tnt *capsulev1beta2.Tenant, name string, terminating bool) *corev1.Namespace {
	ns := &corev1.Namespace{Name: name, UID: types.UID(name + "-uid"), Labels: map[string]string{meta.TenantLabel: tnt.Name}, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: tnt.Name, UID: tnt.UID}}}
	if terminating {
		stamp := metav1.NewTime(time.Now().Add(-time.Hour))
		ns.DeletionTimestamp = &stamp
		ns.Finalizers = []string{"example.com/hold"}
	}
	return ns
}

func TestNamespaceCleanupControllerScopeAndLifecycle(t *testing.T) {
	for _, mode := range []string{"active", "unowned", "stale-owner", "missing-tenant", "missing-namespace", "grace", "pods", "cleanup", "failure"} {
		t.Run(mode, func(t *testing.T) {
			tnt := &capsulev1beta2.Tenant{Name: "tenant-a", UID: "tenant-a-uid"}
			ns := cleanupOwnedNamespace(tnt, "cleanup", true)
			objects := []client.Object{tnt, ns}
			switch mode {
			case "active":
				ns.DeletionTimestamp = nil
				ns.Finalizers = nil
			case "unowned":
				ns.Labels = nil
				ns.OwnerReferences = nil
			case "stale-owner":
				ns.OwnerReferences[0].UID = "deleted-tenant-uid"
			case "missing-tenant":
				objects = []client.Object{ns}
			case "missing-namespace":
				objects = []client.Object{tnt}
			case "grace":
				stamp := metav1.Now()
				ns.DeletionTimestamp = &stamp
			case "pods":
				objects = append(objects, &corev1.Pod{Name: "pending", Namespace: ns.Name})
			}
			manager, dyn := namespaceCleanupFixture(t, objects...)
			if mode == "failure" {
				dyn.PrependReactor("list", "configmaps", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, errors.New("API unavailable") })
			}
			result, err := manager.reconcileNamespaceCleanup(context.Background(), reconcile.Request{NamespacedName: client.ObjectKey{Name: ns.Name}})
			if (err != nil) != (mode == "failure") {
				t.Fatalf("error=%v", err)
			}
			wantWork := mode == "cleanup" || mode == "failure"
			if (len(dyn.Actions()) > 0) != wantWork {
				t.Fatalf("unexpected cleanup work: %v", dyn.Actions())
			}
			wantRetry := mode == "grace" || mode == "pods" || mode == "cleanup"
			if (result.RequeueAfter > 0) != wantRetry {
				t.Fatalf("requeue=%s", result.RequeueAfter)
			}
		})
	}
}

func TestNamespaceCleanupControllerDiscoveryVerbs(t *testing.T) {
	for _, tt := range []struct {
		name  string
		verbs metav1.Verbs
		clean bool
	}{
		{name: "supported", verbs: metav1.Verbs{"get", "list", "delete", "patch"}, clean: true},
		{name: "missing delete", verbs: metav1.Verbs{"get", "list", "patch", "update"}},
		{name: "missing get", verbs: metav1.Verbs{"list", "delete", "patch"}},
		{name: "update without patch", verbs: metav1.Verbs{"get", "list", "delete", "update"}},
		{name: "missing list", verbs: metav1.Verbs{"get", "delete", "patch"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tnt := &capsulev1beta2.Tenant{Name: "tenant-a", UID: "tenant-a-uid"}
			other := &capsulev1beta2.Tenant{Name: "tenant-b", UID: "tenant-b-uid"}
			ns := cleanupOwnedNamespace(tnt, "cleanup", true)
			foreign := cleanupOwnedNamespace(other, "preserved", false)
			manager, dyn := namespaceCleanupFixture(t, tnt, other, ns, foreign)
			gvr := schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}
			discovery := manager.DiscoveryClient.(*benchmarkDiscovery)
			discovery.Resources = append(discovery.Resources, &metav1.APIResourceList{
				GroupVersion: "example.com/v1",
				APIResources: []metav1.APIResource{{Name: "widgets", Kind: "Widget", Namespaced: true, Verbs: tt.verbs}},
			})
			widget := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "example.com/v1", "kind": "Widget",
				"metadata": map[string]any{"name": "held", "namespace": ns.Name, "uid": "held-uid", "resourceVersion": "1", "finalizers": []any{"example.com/hold"}},
			}}
			preserved := widget.DeepCopy()
			preserved.SetNamespace(foreign.Name)
			preserved.SetUID("foreign-uid")
			for _, obj := range []runtime.Object{widget, preserved, &corev1.ConfigMap{Name: "supported", Namespace: ns.Name, UID: "configmap-uid"}} {
				if err := dyn.Tracker().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			// Emulate verb support and deletion that waits for finalizers; the fake
			// dynamic client otherwise accepts unsupported operations and deletes immediately.
			dyn.PrependReactor("*", "widgets", func(action ktesting.Action) (bool, runtime.Object, error) {
				if !slices.Contains(tt.verbs, action.GetVerb()) {
					return true, nil, apierrors.NewMethodNotSupported(gvr.GroupResource(), action.GetVerb())
				}
				if action.GetVerb() == "delete" {
					current := widget.DeepCopy()
					stamp := metav1.Now()
					current.SetDeletionTimestamp(&stamp)
					current.SetResourceVersion("2")
					return true, nil, dyn.Tracker().Update(gvr, current, ns.Name)
				}
				return false, nil, nil
			})
			// Exercise discovery and its cached result; unsupported resources must
			// not cause either reconciliation to fail or prevent supported cleanup.
			for range 2 {
				if _, err := manager.reconcileNamespaceCleanup(t.Context(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(ns)}); err != nil {
					t.Fatal(err)
				}
			}
			for _, action := range dyn.Actions() {
				if action.GetNamespace() != ns.Name || (!tt.clean && action.GetResource() == gvr) {
					t.Fatalf("unexpected cleanup request: %v", action)
				}
			}
			configmaps := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
			if _, err := dyn.Tracker().Get(configmaps, ns.Name, "supported"); !apierrors.IsNotFound(err) {
				t.Fatalf("supported resource was not deleted: %v", err)
			}
			current, err := dyn.Tracker().Get(gvr, ns.Name, widget.GetName())
			if err != nil {
				t.Fatal(err)
			}
			if tt.clean {
				obj := current.(*unstructured.Unstructured)
				if obj.GetDeletionTimestamp() == nil || len(obj.GetFinalizers()) != 0 {
					t.Fatalf("supported resource was not cleaned: %v", obj)
				}
			} else if !reflect.DeepEqual(current, widget) {
				t.Fatalf("unsupported resource changed: %v", current)
			}
			current, err = dyn.Tracker().Get(gvr, foreign.Name, preserved.GetName())
			if err != nil || !reflect.DeepEqual(current, preserved) {
				t.Fatalf("other tenant's resource changed: %v, %v", current, err)
			}
		})
	}
}

func TestTenantProvisioningDoesNotWaitForNamespaceCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tnt := &capsulev1beta2.Tenant{Name: "tenant-a", UID: "tenant-a-uid", Spec: capsulev1beta2.TenantSpec{
		Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: "alice", ClusterRoles: []string{"admin"}}},
		Rules:  []*rules.NamespaceRuleBodyTenant{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "reader"}}, Permissions: rules.NamespaceRulePermissionBody{Bindings: []rbac.AdditionalRoleBindingsSpec{{ClusterRoleName: "view", Subjects: []rbacv1.Subject{{Kind: "User", Name: "profile-reader"}}}}}}},
	}}
	otherTenant := &capsulev1beta2.Tenant{Name: "tenant-b", UID: "tenant-b-uid"}
	old := cleanupOwnedNamespace(tnt, "old", true)
	selected := cleanupOwnedNamespace(tnt, "selected", false)
	selected.Labels["profile"] = "reader"
	unselected := cleanupOwnedNamespace(tnt, "unselected", false)
	foreign := cleanupOwnedNamespace(otherTenant, "foreign", false)
	foreign.Labels["profile"] = "reader"
	manager, dyn := namespaceCleanupFixture(t, tnt, otherTenant, old, selected, unselected, foreign)
	started, release := make(chan struct{}), make(chan struct{})
	dyn.PrependReactor("list", "configmaps", func(ktesting.Action) (bool, runtime.Object, error) {
		close(started)
		select {
		case <-release:
			return true, &unstructured.UnstructuredList{}, nil
		case <-ctx.Done():
			return true, nil, ctx.Err()
		}
	})
	cleanupDone := make(chan error, 1)
	go func() {
		_, err := manager.reconcileNamespaceCleanup(ctx, reconcile.Request{NamespacedName: client.ObjectKey{Name: old.Name}})
		cleanupDone <- err
	}()
	defer func() {
		close(release)
		if err := <-cleanupDone; err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("cleanup did not start")
	}
	done := make(chan error, 1)
	go func() { done <- manager.reconcile(ctx, logr.Discard(), tnt.DeepCopy()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("Tenant provisioning waited for cleanup")
	}
	assertBindingSubject(t, ctx, manager.Client, selected.Name, "profile-reader", true)
	assertBindingSubject(t, ctx, manager.Client, unselected.Name, "profile-reader", false)
	assertBindingSubject(t, ctx, manager.Client, foreign.Name, "alice", false)
	assertBindingSubject(t, ctx, manager.Client, old.Name, "alice", false)
	assertBindingSubject(t, ctx, manager.Client, unselected.Name, "alice", true)
}

func TestRoleBindingsPrecedeCustomQuotaUsageRecount(t *testing.T) {
	ctx := context.Background()
	tnt := &capsulev1beta2.Tenant{Name: "tenant-a", UID: "tenant-a-uid", Annotations: map[string]string{capsulev1beta2.LimitAnnotationForResource("widgets.example.com_v1"): "10"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: "alice", ClusterRoles: []string{"admin"}}}}}
	ns := cleanupOwnedNamespace(tnt, "new", false)
	manager, dyn := namespaceCleanupFixture(t, tnt, ns)
	counted := false
	dyn.PrependReactor("list", "widgets", func(ktesting.Action) (bool, runtime.Object, error) {
		counted = true
		assertBindingSubject(t, ctx, manager.Client, ns.Name, "alice", true)
		return true, &unstructured.UnstructuredList{}, nil
	})
	if err := manager.reconcile(ctx, logr.Discard(), tnt.DeepCopy()); err != nil {
		t.Fatal(err)
	}
	if !counted {
		t.Fatal("usage recount was skipped")
	}
}

func assertBindingSubject(t testing.TB, ctx context.Context, c client.Client, namespace, subject string, want bool) {
	t.Helper()
	list := &rbacv1.RoleBindingList{}
	if err := c.List(ctx, list, client.InNamespace(namespace)); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, binding := range list.Items {
		for _, s := range binding.Subjects {
			if s.Name == subject {
				found = true
			}
		}
	}
	if found != want {
		t.Errorf("namespace %s subject %s binding=%v, want %v", namespace, subject, found, want)
	}
}
