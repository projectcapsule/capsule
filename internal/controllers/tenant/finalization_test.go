// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"errors"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func deletingLifecycleTenant() *capsulev1beta2.Tenant {
	now := metav1.Now()
	return &capsulev1beta2.Tenant{Name: "tenant-a", UID: "tenant-a-uid", DeletionTimestamp: &now, Finalizers: []string{meta.ControllerFinalizer}}
}

func TestTenantFinalizationFindsUnrecordedNamespaces(t *testing.T) {
	for _, recorded := range []bool{false, true} {
		for _, labelled := range []bool{false, true} {
			t.Run(fmt.Sprintf("recorded=%t/labelled=%t", recorded, labelled), func(t *testing.T) {
				ctx := t.Context()
				tnt := deletingLifecycleTenant()
				ns := cleanupOwnedNamespace(tnt, "held", true)
				if !labelled {
					ns.Labels = nil
				}
				if recorded {
					tnt.Status.Spaces = []*capsulev1beta2.TenantStatusNamespaceItem{{Name: ns.Name, UID: ns.UID}}
				}
				other := &capsulev1beta2.Tenant{Name: "tenant-b", UID: "tenant-b-uid"}
				foreign := cleanupOwnedNamespace(other, "foreign", false)
				manager, _ := namespaceCleanupFixture(t, tnt, ns, other, foreign)
				// The namespace informer has not seen this child. The authoritative reader has.
				cached := manager.Client.(client.WithWatch)
				manager.Client = interceptor.NewClient(cached, interceptor.Funcs{List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, ok := list.(*corev1.NamespaceList); ok {
						return nil
					}
					return cl.List(ctx, list, opts...)
				}})
				req := reconcile.Request{Name: tnt.Name}
				for range 2 {
					result, err := manager.Reconcile(ctx, req)
					if err != nil {
						t.Fatal(err)
					}
					current := &capsulev1beta2.Tenant{}
					if err := manager.reader.Get(ctx, client.ObjectKeyFromObject(tnt), current); err != nil {
						t.Fatalf("Tenant disappeared before namespace: %v", err)
					}
					if !controllerutil.ContainsFinalizer(current, meta.ControllerFinalizer) || result.RequeueAfter == 0 {
						t.Fatalf("finalizers=%v result=%v", current.Finalizers, result)
					}
					if len(current.Status.Spaces) != 1 || current.Status.Spaces[0].Name != ns.Name {
						t.Fatalf("spaces=%+v", current.Status.Spaces)
					}
					if current.Status.Spaces[0].Conditions == nil {
						t.Fatal("discovered namespace conditions must serialize as an array")
					}
				}
				held := &corev1.Namespace{}
				if err := manager.reader.Get(ctx, client.ObjectKeyFromObject(ns), held); err != nil {
					t.Fatal(err)
				}
				held.Finalizers = nil
				if err := manager.Update(ctx, held); err != nil {
					t.Fatal(err)
				}
				if _, err := manager.Reconcile(ctx, req); err != nil {
					t.Fatal(err)
				}
				if err := manager.reader.Get(ctx, client.ObjectKeyFromObject(tnt), &capsulev1beta2.Tenant{}); !apierrors.IsNotFound(err) {
					t.Fatalf("Tenant not finalized: %v", err)
				}
				if err := manager.reader.Get(ctx, client.ObjectKeyFromObject(foreign), foreign); err != nil || foreign.DeletionTimestamp != nil {
					t.Fatalf("foreign namespace affected: %v", err)
				}
			})
		}
	}
}

func TestTenantFinalizationDoesNotDeleteForeignOrRecreatedNamespaces(t *testing.T) {
	for _, mode := range []string{"foreign", "old-tenant-uid", "forged-label", "unowned"} {
		t.Run(mode, func(t *testing.T) {
			tnt := deletingLifecycleTenant()
			ns := cleanupOwnedNamespace(tnt, "reused-name", false)
			switch mode {
			case "foreign":
				ns.OwnerReferences[0].Name = "tenant-b"
				ns.OwnerReferences[0].UID = "tenant-b-uid"
			case "old-tenant-uid":
				ns.OwnerReferences[0].UID = "previous-tenant-uid"
			case "forged-label":
				ns.OwnerReferences[0].APIVersion = "example.com/v1"
			case "unowned":
				ns.OwnerReferences = nil
			}
			tnt.Status.Spaces = []*capsulev1beta2.TenantStatusNamespaceItem{{Name: ns.Name, UID: "previous-namespace-uid"}}
			manager, _ := namespaceCleanupFixture(t, tnt, ns)
			if _, err := manager.Reconcile(t.Context(), reconcile.Request{Name: tnt.Name}); err != nil {
				t.Fatal(err)
			}
			if err := manager.Get(t.Context(), client.ObjectKeyFromObject(ns), ns); err != nil || ns.DeletionTimestamp != nil {
				t.Fatalf("unowned namespace touched: %v", err)
			}
			if err := manager.Get(t.Context(), client.ObjectKeyFromObject(tnt), &capsulev1beta2.Tenant{}); !apierrors.IsNotFound(err) {
				t.Fatalf("finalization: %v", err)
			}
		})
	}
}

func TestTenantFinalizationFailsClosed(t *testing.T) {
	for _, mode := range []string{"namespace-get", "namespace-delete", "namespace-list", "quota-list"} {
		t.Run(mode, func(t *testing.T) {
			tnt := deletingLifecycleTenant()
			ns := cleanupOwnedNamespace(tnt, "owned", false)
			objects := []client.Object{tnt}
			if mode == "namespace-get" || mode == "namespace-delete" {
				tnt.Status.Spaces = []*capsulev1beta2.TenantStatusNamespaceItem{{Name: ns.Name}}
				objects = append(objects, ns)
			}
			manager, _ := namespaceCleanupFixture(t, objects...)
			failed := errors.New("API unavailable")
			wrapped := interceptor.NewClient(manager.Client.(client.WithWatch), interceptor.Funcs{
				Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*corev1.Namespace); ok && mode == "namespace-get" {
						return failed
					}
					return cl.Get(ctx, key, obj, opts...)
				},
				Delete: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					if mode == "namespace-delete" {
						return failed
					}
					return cl.Delete(ctx, obj, opts...)
				},
				List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, ok := list.(*metav1.PartialObjectMetadataList); ok && mode == "namespace-list" {
						return failed
					}
					if _, ok := list.(*capsulev1beta2.GlobalResourceQuotaList); ok && mode == "quota-list" {
						return failed
					}
					return cl.List(ctx, list, opts...)
				},
			})
			manager.Client = wrapped
			manager.reader = wrapped
			_, err := manager.Reconcile(t.Context(), reconcile.Request{Name: tnt.Name})
			// Some existing deletion retries return RequeueAfter together with status errors.
			current := &capsulev1beta2.Tenant{}
			if getErr := wrapped.Get(t.Context(), client.ObjectKeyFromObject(tnt), current); getErr != nil {
				t.Fatalf("Tenant lost after failure %v: %v", err, getErr)
			}
			if !controllerutil.ContainsFinalizer(current, meta.ControllerFinalizer) {
				t.Fatal("finalized on API failure")
			}
			condition := current.Status.Conditions.GetConditionByType(meta.ReadyCondition)
			if condition == nil || condition.Status != metav1.ConditionFalse {
				t.Fatalf("failure not reported: %v", condition)
			}
		})
	}
}

func TestTenantFinalizationPaginatesAuthoritativeMetadata(t *testing.T) {
	tnt := deletingLifecycleTenant()
	manager, _ := namespaceCleanupFixture(t, tnt)
	calls := 0
	manager.reader = interceptor.NewClient(manager.Client.(client.WithWatch), interceptor.Funcs{List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
		page, ok := list.(*metav1.PartialObjectMetadataList)
		if !ok {
			return cl.List(ctx, list, opts...)
		}
		options := (&client.ListOptions{}).ApplyOptions(opts)
		if options.Limit != 500 || options.LabelSelector != nil || options.FieldSelector != nil || options.Raw != nil {
			t.Fatalf("non-authoritative or unbounded list: %+v", options)
		}
		if page.GroupVersionKind() != corev1.SchemeGroupVersion.WithKind("NamespaceList") {
			t.Fatalf("wrong kind %v", page.GroupVersionKind())
		}
		calls++
		switch calls {
		case 1:
			if options.Continue != "" {
				t.Fatal("unexpected continuation")
			}
			page.Continue = "next"
		case 2:
			if options.Continue != "next" {
				t.Fatal("missing continuation")
			}
			ns := cleanupOwnedNamespace(tnt, "late-child", true)
			page.Items = []metav1.PartialObjectMetadata{{ObjectMeta: ns.ObjectMeta}}
		default:
			t.Fatal("unexpected extra list")
		}
		return nil
	}})
	if _, err := manager.Reconcile(t.Context(), reconcile.Request{Name: tnt.Name}); err != nil {
		t.Fatal(err)
	}
	current := &capsulev1beta2.Tenant{}
	if err := manager.Get(t.Context(), client.ObjectKeyFromObject(tnt), current); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(current.Status.Spaces) != 1 || current.Status.Spaces[0].Name != "late-child" {
		t.Fatalf("calls=%d spaces=%v", calls, current.Status.Spaces)
	}
}

func TestTenantDeletionUsesNamespaceIdentityPreconditions(t *testing.T) {
	tnt := deletingLifecycleTenant()
	ns := cleanupOwnedNamespace(tnt, "owned", false)
	tnt.Status.Spaces = []*capsulev1beta2.TenantStatusNamespaceItem{{Name: ns.Name}}
	manager, _ := namespaceCleanupFixture(t, tnt, ns)
	deletes := 0
	manager.Client = interceptor.NewClient(manager.Client.(client.WithWatch), interceptor.Funcs{Delete: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
		if _, ok := obj.(*corev1.Namespace); ok {
			deletes++
			options := (&client.DeleteOptions{}).ApplyOptions(opts)
			if options.Preconditions == nil || options.Preconditions.UID == nil || *options.Preconditions.UID != ns.UID || options.Preconditions.ResourceVersion == nil || *options.Preconditions.ResourceVersion != obj.GetResourceVersion() {
				t.Fatalf("unsafe delete: %+v", options)
			}
			// The namespace changed ownership after the read. Kubernetes rejects the
			// resourceVersion precondition rather than deleting the reassigned object.
			return apierrors.NewConflict(corev1.Resource("namespaces"), ns.Name, errors.New("ownership changed"))
		}
		return cl.Delete(ctx, obj, opts...)
	}})
	if _, err := manager.Reconcile(t.Context(), reconcile.Request{Name: tnt.Name}); err != nil {
		t.Fatal(err)
	}
	if deletes != 1 {
		t.Fatalf("deletes=%d", deletes)
	}
	if err := manager.Get(t.Context(), client.ObjectKeyFromObject(tnt), &capsulev1beta2.Tenant{}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Get(t.Context(), client.ObjectKeyFromObject(ns), &corev1.Namespace{}); err != nil {
		t.Fatal(err)
	}
}

func TestTenantFinalizationAvoidsScansWhileWaiting(t *testing.T) {
	tnt := deletingLifecycleTenant()
	ns := cleanupOwnedNamespace(tnt, "held", true)
	tnt.Status.Spaces = []*capsulev1beta2.TenantStatusNamespaceItem{{Name: ns.Name, UID: ns.UID}}
	manager, _ := namespaceCleanupFixture(t, tnt, ns)
	gets := 0
	manager.reader = interceptor.NewClient(manager.Client.(client.WithWatch), interceptor.Funcs{
		Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*corev1.Namespace); ok {
				gets++
			}
			return cl.Get(ctx, key, obj, opts...)
		},
		List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*metav1.PartialObjectMetadataList); ok {
				t.Fatal("waiting on known namespaces must not rescan the cluster")
			}
			return cl.List(ctx, list, opts...)
		},
	})
	for range 3 {
		if _, err := manager.Reconcile(t.Context(), reconcile.Request{Name: tnt.Name}); err != nil {
			t.Fatal(err)
		}
	}
	if gets != 3 {
		t.Fatalf("namespace GET calls=%d want=3", gets)
	}
}

func TestTenantReconcileObservesDeletionDespiteStaleCache(t *testing.T) {
	tnt := deletingLifecycleTenant()
	ns := cleanupOwnedNamespace(tnt, "held", true)
	manager, _ := namespaceCleanupFixture(t, tnt, ns)
	manager.Client = interceptor.NewClient(manager.Client.(client.WithWatch), interceptor.Funcs{
		Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			err := cl.Get(ctx, key, obj, opts...)
			if cached, ok := obj.(*capsulev1beta2.Tenant); ok && err == nil {
				cached.DeletionTimestamp = nil
				cached.Finalizers = nil
			}
			return err
		},
	})
	if _, err := manager.Reconcile(t.Context(), reconcile.Request{Name: tnt.Name}); err != nil {
		t.Fatal(err)
	}
	current := &capsulev1beta2.Tenant{}
	if err := manager.reader.Get(t.Context(), client.ObjectKeyFromObject(tnt), current); err != nil {
		t.Fatal(err)
	}
	if current.Status.State != capsulev1beta2.TenantStateTerminating || len(current.Status.Spaces) != 1 || !controllerutil.ContainsFinalizer(current, meta.ControllerFinalizer) {
		t.Fatalf("stale lifecycle state used: %+v", current)
	}
}

func TestTenantFinalizationDeletesUnrecordedActiveNamespace(t *testing.T) {
	tnt := deletingLifecycleTenant()
	ns := cleanupOwnedNamespace(tnt, "not-yet-terminating", false)
	ns.Finalizers = []string{"example.com/hold"}
	manager, _ := namespaceCleanupFixture(t, tnt, ns)
	for range 2 {
		if _, err := manager.Reconcile(t.Context(), reconcile.Request{Name: tnt.Name}); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.Get(t.Context(), client.ObjectKeyFromObject(ns), ns); err != nil {
		t.Fatal(err)
	}
	if ns.DeletionTimestamp == nil {
		t.Fatal("discovered namespace was not deleted")
	}
	if err := manager.Get(t.Context(), client.ObjectKeyFromObject(tnt), tnt); err != nil {
		t.Fatal(err)
	}
	if !controllerutil.ContainsFinalizer(tnt, meta.ControllerFinalizer) {
		t.Fatal("Tenant finalized while namespace still exists")
	}
}
