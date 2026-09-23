// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
)

func TestReplicationPolicyAdmission(t *testing.T) {
	for _, global := range []bool{false, true} {
		for _, protected := range []bool{false, true} {
			for _, authorized := range []bool{false, true} {
				t.Run(fmt.Sprintf("global=%t/protected=%t/authorized=%t", global, protected, authorized), func(t *testing.T) {
					c, req := replicationAdmissionFixture(t, global, protected, 1)
					if authorized {
						req.UserInfo.Username = "system:serviceaccount:tenant-a:runner"
					}
					for _, handler := range []func(context.Context, admission.Request) *admission.Response{ReplicaHandler().OnUpdate(c, c, admission.NewDecoder(c.Scheme()), nil), ReplicaHandler().OnDelete(c, c, admission.NewDecoder(c.Scheme()), nil)} {
						response := handler(t.Context(), req)
						wantDenied := protected && !authorized
						if wantDenied && (response == nil || response.Allowed || response.Result.Code != 403) {
							t.Fatalf("expected policy denial, got %#v", response)
						}
						if !wantDenied && response != nil {
							t.Fatalf("expected allowed chain continuation, got %#v", response)
						}
						req.Namespace = "tenant-b"
						if response := handler(t.Context(), req); response != nil {
							t.Fatalf("policy leaked to other namespace: %#v", response)
						}
						req.Namespace = "tenant-a"
					}
				})
			}
		}
	}
}

func BenchmarkReplicationPolicyAdmission(b *testing.B) {
	for _, n := range []int{1, 100} {
		for _, protected := range []bool{false, true} {
			b.Run(fmt.Sprintf("tenants=%d/protected=%t", n, protected), func(b *testing.B) {
				c, req := replicationAdmissionFixture(b, false, protected, n)
				handler := ReplicaHandler().OnUpdate(c, c, admission.NewDecoder(c.Scheme()), nil)
				b.ReportAllocs()
				for b.Loop() {
					response := handler(b.Context(), req)
					if (response != nil) != protected {
						b.Fatal("unexpected admission decision")
					}
				}
			})
		}
	}
}

// The fake client verifies indexed query semantics, not production cache latency.
func replicationAdmissionFixture(t testing.TB, global, protected bool, tenants int) (client.Client, admission.Request) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	builder := fake.NewClientBuilder().WithScheme(scheme)
	for _, obj := range []client.Object{&capsulev1beta2.GlobalTenantResource{}, &capsulev1beta2.TenantResource{}} {
		index := tenantresource.ProtectedItems{Obj: obj}
		builder.WithIndex(obj, index.Field(), index.Func())
	}
	for i := range tenants {
		namespace := "tenant-a"
		if i > 0 {
			namespace = fmt.Sprintf("other-tenant-%d", i)
		}
		status := capsulev1beta2.TenantResourceCommonStatus{
			ServiceAccount: &meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: "runner", Namespace: meta.RFC1123SubdomainName(namespace)},
			ProcessedItems: meta.ProcessedItems{{Version: "v1", Kind: "ConfigMap", Namespace: namespace, Name: "item", Policy: &apiruntime.ResourceTemplatePolicy{Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(protected)}}},
		}
		if global {
			builder.WithObjects(&capsulev1beta2.GlobalTenantResource{Name: namespace, Status: capsulev1beta2.GlobalTenantResourceStatus{TenantResourceCommonStatus: status}})
		} else {
			builder.WithObjects(&capsulev1beta2.TenantResource{Name: "parent", Namespace: namespace, Status: capsulev1beta2.TenantResourceStatus{TenantResourceCommonStatus: status}})
		}
	}
	raw, err := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "item", "namespace": "tenant-a"}})
	require.NoError(t, err)
	return builder.Build(), admission.Request{OldObject: runtime.RawExtension{Raw: raw}, Kind: metav1.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, Name: "item", Namespace: "tenant-a", UserInfo: authenticationv1.UserInfo{Username: "tenant-owner"}}
}

func TestReplicationProtectionWithoutIndexedParent(t *testing.T) {
	for _, marker := range []string{meta.ProtectedByCapsuleLabel, meta.ReplicationProtectionLabel} {
		for _, operation := range []string{"update", "remove protection", "delete"} {
			t.Run(marker+"/"+operation, func(t *testing.T) {
				c, req := replicationAdmissionFixture(t, false, false, 1)
				value := meta.ValueControllerReplications
				if marker == meta.ReplicationProtectionLabel {
					value = meta.ValueTrue
				}
				req.OldObject.Raw = []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"item","namespace":"tenant-a","labels":{%q:%q}}}`, marker, value))
				req.Object = req.OldObject
				if operation == "remove protection" {
					req.Object.Raw = []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"item","namespace":"tenant-a"}}`)
				}
				reads, lists, namespaceReads := 0, 0, 0
				c = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
					Get: func(_ context.Context, _ client.WithWatch, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
						if ns, ok := obj.(*corev1.Namespace); ok {
							namespaceReads++
							require.Equal(t, client.ObjectKey{Name: req.Namespace}, key)
							ns.Name = key.Name
							return nil
						}
						reads++
						return errors.New("unexpected read")
					},
					List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						lists++
						require.NotNil(t, (&client.ListOptions{}).ApplyOptions(opts).FieldSelector)
						return c.List(ctx, list, opts...)
					},
				})
				handler := ReplicaHandler().OnUpdate(c, c, admission.NewDecoder(c.Scheme()), nil)
				if operation == "delete" {
					handler = ReplicaHandler().OnDelete(c, c, admission.NewDecoder(c.Scheme()), nil)
				}
				response := handler(t.Context(), req)
				require.NotNil(t, response, "fresh protection must survive a stale or empty status index")
				require.False(t, response.Allowed)
				require.EqualValues(t, 403, response.Result.Code)
				require.Contains(t, response.Result.Message, "protected by a capsule replication")
				require.Zero(t, reads)
				if operation == "delete" {
					require.Equal(t, 1, namespaceReads)
				} else {
					require.Zero(t, namespaceReads)
				}
				require.Equal(t, 2, lists)
			})
		}
	}
}

func TestReplicationAdmissionVerifiesServiceAccount(t *testing.T) {
	for _, global := range []bool{false, true} {
		for _, state := range []string{"current", "replaced identity", "different service account", "unprotected", "target removed", "not found", "read error"} {
			t.Run(fmt.Sprintf("global=%t/%s", global, state), func(t *testing.T) {
				c, req := replicationAdmissionFixture(t, global, true, 1)
				req.UserInfo.Username = "system:serviceaccount:tenant-a:runner"
				reads := 0
				reader := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					reads++
					if state == "read error" {
						return errors.New("unavailable")
					}
					if state == "not found" {
						key.Name = "missing"
					}
					if err := c.Get(ctx, key, obj, opts...); err != nil {
						return err
					}
					var status *capsulev1beta2.TenantResourceCommonStatus
					switch obj := obj.(type) {
					case *capsulev1beta2.TenantResource:
						status = &obj.Status.TenantResourceCommonStatus
					case *capsulev1beta2.GlobalTenantResource:
						status = &obj.Status.TenantResourceCommonStatus
					}
					switch state {
					case "replaced identity":
						obj.SetUID("replacement")
					case "different service account":
						status.ServiceAccount.Name = "replacement"
					case "unprotected":
						status.ProcessedItems[0].Policy.Protect = new(false)
					case "target removed":
						status.ProcessedItems = nil
					}
					return nil
				}})
				response := ReplicaHandler().OnUpdate(c, reader, admission.NewDecoder(c.Scheme()), nil)(t.Context(), req)
				if state == "current" {
					require.Nil(t, response)
				} else {
					require.NotNil(t, response)
					require.False(t, response.Allowed)
					if state == "read error" {
						require.Contains(t, response.Result.Message, "unavailable")
					} else {
						require.EqualValues(t, 403, response.Result.Code)
					}
				}
				require.Equal(t, 1, reads)
			})
		}
	}
}

func TestReplicationAdmissionDependencyFailures(t *testing.T) {
	for _, scenario := range []string{"global list", "local list", "malformed old object"} {
		t.Run(scenario, func(t *testing.T) {
			c, req := replicationAdmissionFixture(t, false, false, 1)
			failure := errors.New("index unavailable")
			c = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				_, global := list.(*capsulev1beta2.GlobalTenantResourceList)
				if (global && scenario == "global list") || (!global && scenario == "local list") {
					return failure
				}
				return c.List(ctx, list, opts...)
			}})
			if scenario == "malformed old object" {
				req.OldObject.Raw = []byte("{")
			}
			response := ReplicaHandler().OnUpdate(c, c, admission.NewDecoder(c.Scheme()), nil)(t.Context(), req)
			require.NotNil(t, response)
			require.False(t, response.Allowed)
			if scenario != "malformed old object" {
				require.Contains(t, response.Result.Message, failure.Error())
			}
		})
	}
}

func TestReplicationAdmissionControllerSkipsLookups(t *testing.T) {
	t.Setenv(configuration.EnvironmentServiceaccountName, "capsule-controller")
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")
	request := admission.Request{UserInfo: authenticationv1.UserInfo{Username: "system:serviceaccount:capsule-system:capsule-controller"}}
	for _, handler := range []func(context.Context, admission.Request) *admission.Response{
		ReplicaHandler().OnUpdate(nil, nil, nil, nil), ReplicaHandler().OnDelete(nil, nil, nil, nil),
	} {
		require.Nil(t, handler(t.Context(), request))
	}
}
