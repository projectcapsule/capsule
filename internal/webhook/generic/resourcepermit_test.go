// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgocache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	indexes "github.com/projectcapsule/capsule/pkg/runtime/indexers/serviceaccount"
	"github.com/projectcapsule/capsule/pkg/users"
)

func permitAdmissionFixture(t testing.TB, count int, independent bool) (client.WithWatch, admission.Request) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	var parents []client.Object
	for i := range count {
		namespace := fmt.Sprintf("tenant-%d", i)
		parents = append(parents, &capsulev1beta2.ResourcePermit{Name: "permit", Namespace: namespace, UID: types.UID(namespace), Status: capsulev1beta2.ResourcePermitStatus{
			Request: &capsulev1beta2.ResourcePermitStatusRequest{
				Impersonation: &meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: "runner", Namespace: meta.RFC1123SubdomainName(namespace)},
				Resources:     []apiruntime.RenderedResource{{Targets: []runtime.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"shared","namespace":%q}}`, namespace))}}}},
			},
		}})
	}
	index := indexes.ResourcePermitFieldOwner{}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parents...).WithIndex(index.Object(), index.Field(), index.Func()).Build()
	target := protectedResourcePermitResource("system:serviceaccount:tenant-0:runner")
	target.SetAPIVersion("v1")
	target.SetKind("ConfigMap")
	target.SetName("shared")
	target.SetNamespace("tenant-0")
	target.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: meta.ResourcePermitFieldOwner(parents[0]), Operation: metav1.ManagedFieldsOperationApply}})
	if independent {
		target.SetLabels(map[string]string{meta.ProtectedByCapsuleLabel: meta.ValueControllerReplications, meta.ResourcePermitProtectionLabel: meta.ValueTrue})
	}
	raw, err := json.Marshal(target)
	require.NoError(t, err)
	return c, admission.Request{Name: "shared", Namespace: "tenant-0", Kind: metav1.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, Object: runtime.RawExtension{Raw: raw}, OldObject: runtime.RawExtension{Raw: raw}, UserInfo: users.ServiceAccountUserInfo("tenant-0", "runner")}
}

func TestResourcePermitResourceHandler(t *testing.T) {
	t.Setenv(configuration.EnvironmentServiceaccountName, "capsule-controller")
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")
	for _, independent := range []bool{false, true} {
		for _, operation := range []string{"create", "adopt", "update", "delete"} {
			for _, state := range []string{"authorized", "controller", "owner", "other tenant", "self annotation", "no manager", "missing index", "missing parent", "replaced parent", "stale identity", "stale target", "unprotected policy", "read error", "index error", "terminating parent", "forged next owner", "lagging snapshot"} {
				t.Run(fmt.Sprintf("independent=%t/%s/%s", independent, operation, state), func(t *testing.T) {
					c, req := permitAdmissionFixture(t, 2, independent)
					next := &metav1.PartialObjectMetadata{}
					require.NoError(t, json.Unmarshal(req.Object.Raw, next))
					switch state {
					case "controller":
						req.UserInfo = users.ServiceAccountUserInfo("capsule-system", "capsule-controller")
					case "owner":
						req.UserInfo = authenticationv1.UserInfo{Username: "alice"}
					case "other tenant":
						req.UserInfo = users.ServiceAccountUserInfo("tenant-1", "runner")
					case "self annotation":
						req.UserInfo = authenticationv1.UserInfo{Username: "alice"}
						next.Annotations[meta.ResourcePermitServiceAccountAnnotation] = "alice"
					case "no manager":
						next.ManagedFields = nil
					case "forged next owner":
						req.UserInfo = users.ServiceAccountUserInfo("tenant-1", "runner")
						next.ManagedFields[0].Manager = meta.ResourceFieldOwner("resourcepermit/tenant-1")
						next.Annotations[meta.ResourcePermitServiceAccountAnnotation] = req.UserInfo.Username
					}
					var err error
					req.Object.Raw, err = json.Marshal(next)
					require.NoError(t, err)
					if state == "no manager" {
						req.OldObject = req.Object
					}
					if operation == "adopt" {
						old := next.DeepCopy()
						old.Labels = nil
						old.ManagedFields = nil
						req.OldObject.Raw, err = json.Marshal(old)
						require.NoError(t, err)
					}
					reads, lists := 0, 0
					indexed := interceptor.NewClient(c, interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						lists++
						if state == "index error" {
							return errors.New("index unavailable")
						}
						if state == "missing index" {
							return nil
						}
						if err := c.List(ctx, list, opts...); err != nil {
							return err
						}
						if state == "lagging snapshot" {
							for i := range list.(*capsulev1beta2.ResourcePermitList).Items {
								list.(*capsulev1beta2.ResourcePermitList).Items[i].Status.Request = nil
							}
						}
						return nil
					}})
					reader := interceptor.NewClient(c, interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						reads++
						if state == "read error" {
							return errors.New("API unavailable")
						}
						if state == "missing parent" {
							key.Name = "missing"
						}
						if err := c.Get(ctx, key, obj, opts...); err != nil {
							return err
						}
						parent := obj.(*capsulev1beta2.ResourcePermit)
						switch state {
						case "replaced parent":
							parent.UID = "new-uid"
						case "stale identity":
							parent.Status.Request.Impersonation.Name = "different"
						case "stale target":
							parent.Status.Request.Resources = nil
						case "unprotected policy":
							parent.Status.Request.Resources[0].Policy.Protect = new(false)
						case "terminating parent":
							parent.DeletionTimestamp = new(metav1.Now())
						}
						return nil
					}})
					decoder := admission.NewDecoder(c.Scheme())
					call := ResourcePermitResourceHandler().OnUpdate(indexed, reader, decoder, nil)
					if operation == "create" {
						call = ResourcePermitResourceHandler().OnCreate(indexed, reader, decoder, nil)
					}
					if operation == "delete" {
						req.Namespace = ""
						call = ResourcePermitResourceHandler().OnDelete(indexed, reader, decoder, nil)
					}
					response := call(t.Context(), req)
					allowed := state == "authorized" || state == "controller" || state == "terminating parent" || state == "lagging snapshot"
					if allowed {
						require.Nil(t, response)
					} else {
						require.NotNil(t, response)
						require.False(t, response.Allowed)
						code := 403
						if state == "read error" || state == "index error" {
							code = 500
						}
						require.EqualValues(t, code, response.Result.Code)
					}
					if state == "controller" || state == "owner" || state == "self annotation" || state == "no manager" {
						require.Zero(t, lists)
						require.Zero(t, reads)
					}
					if state == "authorized" {
						require.Equal(t, 1, lists)
						require.Equal(t, 1, reads)
					}
				})
			}
		}
	}
}

func BenchmarkResourcePermitProtectionAdmission(b *testing.B) {
	for _, count := range []int{1, 100, 1000} {
		for _, scenario := range []string{"allow", "deny", "create", "adopt", "controller"} {
			b.Run(fmt.Sprintf("tenants=%d/%s", count, scenario), func(b *testing.B) {
				c, req := permitAdmissionFixture(b, count, true)
				parents := &capsulev1beta2.ResourcePermitList{}
				require.NoError(b, c.List(b.Context(), parents))
				// Reuse the client-go index implementation, as in the replication
				// admission benchmarks. Fake List scans every unrelated object.
				index := clientgocache.NewIndexer(clientgocache.MetaNamespaceKeyFunc, clientgocache.Indexers{
					indexes.ResourcePermitFieldOwnerIndex: func(obj any) ([]string, error) {
						return (indexes.ResourcePermitFieldOwner{}).Func()(obj.(client.Object)), nil
					},
				})
				for i := range parents.Items {
					require.NoError(b, index.Add(&parents.Items[i]))
				}
				lists, reads := 0, 0
				c = interceptor.NewClient(c, interceptor.Funcs{
					List: func(_ context.Context, _ client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						lists++
						selector := (&client.ListOptions{}).ApplyOptions(opts).FieldSelector
						if selector == nil {
							b.Fatal("unindexed lookup")
						}
						key, ok := selector.RequiresExactMatch(indexes.ResourcePermitFieldOwnerIndex)
						if !ok {
							b.Fatal("wrong index")
						}
						matches, err := index.ByIndex(indexes.ResourcePermitFieldOwnerIndex, key)
						if err != nil {
							return err
						}
						for _, obj := range matches {
							list.(*capsulev1beta2.ResourcePermitList).Items = append(list.(*capsulev1beta2.ResourcePermitList).Items, *obj.(*capsulev1beta2.ResourcePermit).DeepCopy())
						}
						return nil
					},
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						reads++
						return c.Get(ctx, key, obj, opts...)
					},
				})
				decoder := admission.NewDecoder(c.Scheme())
				call := ResourcePermitResourceHandler().OnUpdate(c, c, decoder, nil)
				switch scenario {
				case "deny":
					req.UserInfo = authenticationv1.UserInfo{Username: "tenant-owner"}
				case "controller":
					b.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")
					b.Setenv(configuration.EnvironmentServiceaccountName, "capsule-controller")
					req.UserInfo = users.ServiceAccountUserInfo("capsule-system", "capsule-controller")
				case "create":
					call = ResourcePermitResourceHandler().OnCreate(c, c, decoder, nil)
				case "adopt":
					req.OldObject.Raw = []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"shared","namespace":"tenant-0"}}`)
				}
				b.ReportAllocs()
				for b.Loop() {
					response := call(b.Context(), req)
					if (response != nil) != (scenario == "deny") {
						b.Fatalf("unexpected decision: %#v", response)
					}
				}
				b.ReportMetric(float64(lists)/float64(b.N), "indexed-lists/op")
				b.ReportMetric(float64(reads)/float64(b.N), "parent-reads/op")
			})
		}
	}
}

func BenchmarkResourcePermitSnapshotSize(b *testing.B) {
	for _, targets := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("targets=%d", targets), func(b *testing.B) {
			c, req := permitAdmissionFixture(b, 1, true)
			parent := &capsulev1beta2.ResourcePermit{}
			require.NoError(b, c.Get(b.Context(), client.ObjectKey{Name: "permit", Namespace: "tenant-0"}, parent))
			matching := parent.Status.Request.Resources[0].Targets[0]
			resources := make([]runtime.RawExtension, 0, targets)
			for i := 1; i < targets; i++ {
				resources = append(resources, runtime.RawExtension{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"other-%d","namespace":"tenant-0"},"data":{"value":%q}}`, i, string(bytes.Repeat([]byte("x"), 512))))})
			}
			resources = append(resources, matching)
			parent.Status.Request.Resources[0].Targets = resources
			require.NoError(b, c.Update(b.Context(), parent))
			call := ResourcePermitResourceHandler().OnUpdate(c, c, admission.NewDecoder(c.Scheme()), nil)
			b.ReportAllocs()
			for b.Loop() {
				if response := call(b.Context(), req); response != nil {
					b.Fatalf("unexpected denial: %#v", response)
				}
			}
		})
	}
}

func protectedResourcePermitResource(serviceAccount string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetLabels(map[string]string{meta.ProtectedByCapsuleLabel: meta.ValueControllerResourcePermit})
	obj.SetAnnotations(map[string]string{meta.ResourcePermitServiceAccountAnnotation: serviceAccount})
	return obj
}
