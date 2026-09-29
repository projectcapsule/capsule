// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ssa

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgocache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
)

func replicationOwnerFixture(global bool, target *unstructured.Unstructured, tenant string) (client.Object, string) {
	item := meta.ObjectReferenceStatus{ResourceID: gvk.NewResourceID(target, tenant, "0/raw-0")}
	item.LastApply = metav1.Now()
	var parent client.Object
	if global {
		parent = &capsulev1beta2.GlobalTenantResource{
			Name: "global-parent", UID: "global-uid",
			Status: capsulev1beta2.GlobalTenantResourceStatus{ProcessedItems: meta.ProcessedItems{item}},
		}
	} else {
		parent = &capsulev1beta2.TenantResource{
			Name: "local-parent", Namespace: "source-" + tenant, UID: "local-uid",
			Status: capsulev1beta2.TenantResourceStatus{ProcessedItems: meta.ProcessedItems{item}},
		}
	}
	return parent, meta.ReplicationFieldOwnerPrefix(parent.GetName(), parent.GetNamespace()) + "/" + item.FieldOwner("")
}

func ownerClientBuilder(t testing.TB, objects ...client.Object) *fake.ClientBuilder {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).
		WithIndex(&capsulev1beta2.GlobalTenantResource{}, tenantresource.FieldOwnerIndexerFieldName, (tenantresource.FieldOwner{}).Func()).
		WithIndex(&capsulev1beta2.TenantResource{}, tenantresource.FieldOwnerIndexerFieldName, (tenantresource.FieldOwner{}).Func())
}

func TestReplicationOwnerResolver(t *testing.T) {
	for _, global := range []bool{false, true} {
		for _, state := range []string{"live", "stale status", "deleted", "deleting", "recreated", "target removed", "never applied", "other namespace", "other kind", "other name", "cluster scoped", "lookup error", "read error"} {
			t.Run(fmt.Sprintf("global=%t/%s", global, state), func(t *testing.T) {
				target := configMap("shared", nil)
				parent, owner := replicationOwnerFixture(global, target, "tenant-a")
				current := parent.DeepCopyObject().(client.Object)
				var items *meta.ProcessedItems
				switch obj := current.(type) {
				case *capsulev1beta2.GlobalTenantResource:
					items = &obj.Status.ProcessedItems
				case *capsulev1beta2.TenantResource:
					items = &obj.Status.ProcessedItems
				}
				want := state == "live" || state == "stale status" || state == "cluster scoped"
				switch state {
				case "deleting":
					now := metav1.Now()
					current.SetDeletionTimestamp(&now)
					current.SetFinalizers([]string{meta.ControllerFinalizer})
				case "recreated":
					current.SetUID("replacement")
				case "target removed":
					*items = nil
				case "never applied":
					(*items)[0].LastApply = metav1.Time{}
				case "other namespace":
					(*items)[0].Namespace = "tenant-b"
				case "other kind":
					(*items)[0].Kind = "Secret"
				case "other name":
					(*items)[0].Name = "different"
				case "cluster scoped":
					target.SetNamespace("")
					(*items)[0].ClusterScoped = true
					parent = current.DeepCopyObject().(client.Object)
				}
				if state == "stale status" {
					switch obj := parent.(type) {
					case *capsulev1beta2.TenantResource:
						obj.Status.ProcessedItems = nil
					case *capsulev1beta2.GlobalTenantResource:
						obj.Status.ProcessedItems = nil
					}
				}
				target.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: owner}})
				unrelated := target.DeepCopy()
				unrelated.SetNamespace("tenant-b")
				other, _ := replicationOwnerFixture(global, unrelated, "tenant-b")
				other.SetName("other-parent")
				lists, reads := 0, 0
				failure := errors.New("unavailable")
				indexed := ownerClientBuilder(t, parent, other).WithInterceptorFuncs(interceptor.Funcs{
					List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						lists++
						require.Equal(t, tenantresource.FieldOwnerIndexerFieldName+"="+meta.ReplicationFieldOwnerPrefix(parent.GetName(), parent.GetNamespace()), (&client.ListOptions{}).ApplyOptions(opts).FieldSelector.String())
						if state == "lookup error" {
							return failure
						}
						return c.List(ctx, list, opts...)
					},
				}).Build()
				objects := []client.Object{current}
				if state == "deleted" {
					objects = nil
				}
				reader := ownerClientBuilder(t, objects...).WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						reads++
						require.Equal(t, client.ObjectKeyFromObject(parent), key, "must not read unrelated tenant parents")
						if state == "read error" {
							return failure
						}
						return c.Get(ctx, key, obj, opts...)
					},
				}).Build()
				before := parent.DeepCopyObject()
				owners, err := NewReplicationOwnerResolver(indexed, reader)(t.Context(), target, testFieldOwner)
				if state == "lookup error" || state == "read error" {
					require.ErrorIs(t, err, failure)
				} else {
					require.NoError(t, err)
					require.Equal(t, 2, lists)
					require.Equal(t, 1, reads)
					if want {
						require.Equal(t, map[string]struct{}{owner: {}}, owners)
					} else {
						require.Empty(t, owners)
					}
				}
				require.Equal(t, before, parent, "lookup must not mutate cache-owned objects")
			})
		}
	}
}

func TestCleanupRejectsForgedReplicationOwners(t *testing.T) {
	for _, operation := range []string{"disown", "orphan", "skip", "apply"} {
		for _, peer := range []string{"forged", "live", "deleted", "error"} {
			t.Run(operation+"/"+peer, func(t *testing.T) {
				existing := skippedPolicyTarget(testFieldOwner, true)
				labels := existing.GetLabels()
				labels[meta.NewManagedByCapsuleLabel] = testCreatedBy
				existing.SetLabels(labels)
				parent, owner := replicationOwnerFixture(false, existing, "tenant-a")
				fields := existing.GetManagedFields()
				other := fields[0].DeepCopy()
				other.Manager = owner
				if peer == "forged" {
					other.Manager = "2lclct9cwq6mg/default/tenant-a/0/raw-0/"
				}
				existing.SetManagedFields(append(fields, *other))
				indexed := ownerClientBuilder(t, parent).Build()
				var objects []client.Object
				if peer != "deleted" {
					objects = append(objects, parent)
				}
				reader := ownerClientBuilder(t, objects...).Build()
				m := skippedPolicyManager(t)
				m.ReplicationOwners = NewReplicationOwnerResolver(indexed, reader)
				if peer == "error" {
					m.ReplicationOwners = func(context.Context, *unstructured.Unstructured, string) (map[string]struct{}, error) {
						return nil, errors.New("unavailable")
					}
				}
				c := fake.NewClientBuilder().WithObjects(existing).WithReturnManagedFields().Build()
				var err error
				switch operation {
				case "disown":
					err = m.Disown(t.Context(), c, existing, testFieldOwner, nil)
				case "orphan":
					err = m.Orphan(t.Context(), c, existing, testFieldOwner, nil)
				default:
					condition := "false"
					if operation == "apply" {
						condition = "true"
					}
					_, err = m.Apply(t.Context(), c, configMap("guarded", map[string]any{"value": "retained"}), ApplyOptions{FieldOwner: testFieldOwner, Condition: condition, Adopt: true})
				}
				if peer == "error" {
					require.ErrorContains(t, err, "unavailable")
				} else {
					require.NoError(t, err)
				}
				actual := existing.DeepCopy()
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), actual))
				retain := peer == "live" || peer == "error"
				require.Equal(t, retain, actual.GetLabels()[meta.ProtectedByCapsuleLabel] == testCreatedBy)
				if operation == "disown" || operation == "orphan" {
					require.Equal(t, retain, actual.GetLabels()[meta.NewManagedByCapsuleLabel] == testCreatedBy)
				}
				require.Equal(t, existing.Object["data"], actual.Object["data"])
				require.Equal(t, "original", actual.GetLabels()["example.org/keep"])
			})
		}
	}
}

func TestReplicationOwnerLookupGate(t *testing.T) {
	for _, manager := range []string{testFieldOwner, "kubectl", meta.ResourceFieldOwner("resourcepermit/peer")} {
		t.Run(manager, func(t *testing.T) {
			m := Manager{ReplicationOwners: func(context.Context, *unstructured.Unstructured, string) (map[string]struct{}, error) {
				t.Fatal("unexpected replication lookup")
				return nil, nil
			}}
			_, err := m.resourceFieldOwners(t.Context(), skippedPolicyTarget(manager, true), testFieldOwner)
			require.NoError(t, err)
		})
	}
}

func BenchmarkVerifiedProtectionCleanup(b *testing.B) {
	for _, workload := range []struct{ unrelated, items int }{{0, 1}, {100, 1}, {1000, 1}, {0, 100}, {0, 1000}} {
		for _, live := range []bool{false, true} {
			if !live && workload.items > 1 {
				continue
			}
			b.Run(fmt.Sprintf("unrelated=%d/items=%d/live=%t", workload.unrelated, workload.items, live), func(b *testing.B) {
				target := skippedPolicyTarget(testFieldOwner, true)
				parent, owner := replicationOwnerFixture(false, target, "tenant-a")
				objects := []client.Object{}
				if live {
					objects = append(objects, parent)
				}
				for i := range workload.unrelated {
					obj := parent.DeepCopyObject().(*capsulev1beta2.TenantResource)
					obj.Name = fmt.Sprintf("unrelated-%d", i)
					obj.UID = types.UID(obj.Name)
					obj.Status.ProcessedItems[0].Namespace = fmt.Sprintf("tenant-b-%d", i)
					obj.Status.ProcessedItems[0].Tenant = "tenant-b"
					objects = append(objects, obj)
				}
				local := parent.(*capsulev1beta2.TenantResource)
				for i := 1; i < workload.items; i++ {
					item := local.Status.ProcessedItems[0]
					item.Name = fmt.Sprintf("other-target-%d", i)
					local.Status.ProcessedItems = append(local.Status.ProcessedItems, item)
				}
				// Use the same client-go index implementation as the manager cache:
				// the fake client's List implementation scans all stored objects.
				index := clientgocache.NewIndexer(clientgocache.MetaNamespaceKeyFunc, clientgocache.Indexers{
					tenantresource.FieldOwnerIndexerFieldName: func(obj any) ([]string, error) {
						return (tenantresource.FieldOwner{}).Func()(obj.(client.Object)), nil
					},
				})
				for _, obj := range objects {
					require.NoError(b, index.Add(obj))
				}
				lists, reads := 0, 0
				lookup := ownerClientBuilder(b, objects...).WithInterceptorFuncs(interceptor.Funcs{
					List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						lists++
						selector := (&client.ListOptions{}).ApplyOptions(opts).FieldSelector
						if selector == nil {
							b.Fatal("unindexed lookup")
						}
						key, ok := selector.RequiresExactMatch(tenantresource.FieldOwnerIndexerFieldName)
						if !ok {
							b.Fatal("incorrect index")
						}
						if local, ok := list.(*capsulev1beta2.TenantResourceList); ok {
							matches, err := index.ByIndex(tenantresource.FieldOwnerIndexerFieldName, key)
							if err != nil {
								return err
							}
							for _, obj := range matches {
								local.Items = append(local.Items, *obj.(*capsulev1beta2.TenantResource).DeepCopy())
							}
						}
						return nil
					},
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						reads++
						return c.Get(ctx, key, obj, opts...)
					},
				}).Build()
				field := target.GetManagedFields()[0]
				field.Manager = owner
				target.SetManagedFields(append(target.GetManagedFields(), field))
				m := skippedPolicyManager(b)
				m.ReplicationOwners = NewReplicationOwnerResolver(lookup, lookup)
				b.ReportAllocs()
				for b.Loop() {
					b.StopTimer()
					c := fake.NewClientBuilder().WithObjects(target).WithReturnManagedFields().Build()
					b.StartTimer()
					if err := m.Orphan(b.Context(), c, target, testFieldOwner, nil); err != nil {
						b.Fatal(err)
					}
					b.StopTimer()
					actual := target.DeepCopy()
					if err := c.Get(b.Context(), client.ObjectKeyFromObject(target), actual); err != nil {
						b.Fatal(err)
					}
					if (actual.GetLabels()[meta.ProtectedByCapsuleLabel] != "") != live {
						b.Fatal("incorrect protection")
					}
					b.StartTimer()
				}
				b.ReportMetric(float64(lists)/float64(b.N), "indexed-lists/op")
				b.ReportMetric(float64(reads)/float64(b.N), "parent-reads/op")
			})
		}
	}
}

func TestReplicationOwnerResolverRequiredForSharedTarget(t *testing.T) {
	_, err := (Manager{}).resourceFieldOwners(t.Context(), skippedPolicyTarget("abc/default/tenant/0/raw-0/", true), testFieldOwner)
	require.ErrorContains(t, err, "replication owner resolver is not configured")
}

func knownReplicationOwners(owners ...string) ReplicationOwnerResolver {
	known := map[string]struct{}{}
	for _, owner := range owners {
		known[owner] = struct{}{}
	}
	return func(context.Context, *unstructured.Unstructured, string) (map[string]struct{}, error) {
		return known, nil
	}
}
