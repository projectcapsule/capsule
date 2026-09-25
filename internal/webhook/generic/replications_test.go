// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
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
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/processor"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
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
	builder := fake.NewClientBuilder().WithScheme(scheme).WithReturnManagedFields()
	for _, obj := range []client.Object{&capsulev1beta2.GlobalTenantResource{}, &capsulev1beta2.TenantResource{}} {
		index := tenantresource.ProtectedItems{Obj: obj}
		builder.WithIndex(obj, index.Field(), index.Func())
		owner := tenantresource.FieldOwner{Obj: obj}
		builder.WithIndex(obj, owner.Field(), owner.Func())
	}
	for i := range tenants {
		namespace := "tenant-a"
		if i > 0 {
			namespace = fmt.Sprintf("other-tenant-%d", i)
		}
		status := capsulev1beta2.TenantResourceCommonStatus{
			ServiceAccount: &meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: "runner", Namespace: meta.RFC1123SubdomainName(namespace)},
			ProcessedItems: meta.ProcessedItems{{LastApply: metav1.Now(), Version: "v1", Kind: "ConfigMap", Namespace: namespace, Name: "item", Tenant: namespace, Origin: "0/raw-0", Policy: &apiruntime.ResourceTemplatePolicy{Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(protected)}}},
		}
		if global {
			builder.WithObjects(&capsulev1beta2.GlobalTenantResource{Name: namespace, Status: capsulev1beta2.GlobalTenantResourceStatus{TenantResourceCommonStatus: status}})
		} else {
			builder.WithObjects(&capsulev1beta2.TenantResource{Name: "parent", Namespace: namespace, Status: capsulev1beta2.TenantResourceStatus{TenantResourceCommonStatus: status}})
		}
	}
	parentName, parentNamespace := "parent", "tenant-a"
	if global {
		parentName, parentNamespace = "tenant-a", ""
	}
	owner := meta.ReplicationFieldOwnerPrefix(parentName, parentNamespace) + "/tenant-a/tenant-a/0/raw-0/"
	raw, err := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "item", "namespace": "tenant-a", "managedFields": []metav1.ManagedFieldsEntry{{Manager: owner}}}})
	require.NoError(t, err)
	return builder.Build(), admission.Request{OldObject: runtime.RawExtension{Raw: raw}, Object: runtime.RawExtension{Raw: raw}, Kind: metav1.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, Name: "item", Namespace: "tenant-a", UserInfo: authenticationv1.UserInfo{Username: "tenant-owner"}}
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
		ReplicaHandler().OnCreate(nil, nil, nil, nil), ReplicaHandler().OnUpdate(nil, nil, nil, nil), ReplicaHandler().OnDelete(nil, nil, nil, nil),
	} {
		require.Nil(t, handler(t.Context(), request))
	}
}

func TestReplicationLegacyTrackingRequiresCurrentPolicy(t *testing.T) {
	for _, global := range []bool{false, true} {
		for _, marker := range []string{meta.CreatedByCapsuleLabel, meta.NewManagedByCapsuleLabel} {
			for _, state := range []string{"unprotected", "legacy adopted", "empty index", "legacy", "protected", "recreated", "missing", "read error", "different target", "never applied", "other protected manager"} {
				t.Run(fmt.Sprintf("global=%t/%s/%s", global, marker, state), func(t *testing.T) {
					c, req := replicationAdmissionFixture(t, global, false, 2)
					parentName, parentNamespace := "parent", "tenant-a"
					if global {
						parentName, parentNamespace = "tenant-a", ""
					}
					id := gvk.ResourceID{Version: "v1", Kind: "ConfigMap", Namespace: "tenant-a", Name: "item", Tenant: "tenant-a", Origin: "0/raw-0"}
					owner := meta.ReplicationFieldOwnerPrefix(parentName, parentNamespace) + "/" + id.FieldOwner("")
					req.OldObject.Raw = []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"item","namespace":"tenant-a","labels":{%q:"replications"},"managedFields":[{"manager":%q}]}}`, marker, owner))
					if state == "other protected manager" {
						status := capsulev1beta2.TenantResourceCommonStatus{ProcessedItems: meta.ProcessedItems{{ResourceID: id, LastApply: metav1.Now(), Policy: &apiruntime.ResourceTemplatePolicy{Protect: new(true)}}}}
						var peer client.Object
						if global {
							peer = &capsulev1beta2.GlobalTenantResource{Name: parentName + "-peer", Status: capsulev1beta2.GlobalTenantResourceStatus{TenantResourceCommonStatus: status}}
						} else {
							peer = &capsulev1beta2.TenantResource{Name: parentName + "-peer", Namespace: parentNamespace, Status: capsulev1beta2.TenantResourceStatus{TenantResourceCommonStatus: status}}
						}
						require.NoError(t, c.Create(t.Context(), peer))
						old := &metav1.PartialObjectMetadata{}
						require.NoError(t, json.Unmarshal(req.OldObject.Raw, old))
						old.ManagedFields = append(old.ManagedFields, metav1.ManagedFieldsEntry{Manager: meta.ReplicationFieldOwnerPrefix(peer.GetName(), peer.GetNamespace()) + "/" + id.FieldOwner("")})
						var err error
						req.OldObject.Raw, err = json.Marshal(old)
						require.NoError(t, err)
					}
					// Removing the marker in the submitted object must not bypass protection.
					req.Object.Raw = []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"item","namespace":"tenant-a"}}`)
					indexed := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						if state == "empty index" {
							return nil
						}
						if state == "other protected manager" {
							selector := (&client.ListOptions{}).ApplyOptions(opts).FieldSelector
							if _, protectedIndex := selector.RequiresExactMatch(tenantresource.ProtectedIndexerFieldName); protectedIndex {
								return nil
							}
						}
						return c.List(ctx, list, opts...)
					}})
					reader := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if _, namespace := obj.(*corev1.Namespace); namespace {
							return nil
						}
						if state == "read error" {
							return errors.New("parent unavailable")
						}
						if state == "missing" {
							key.Name = "missing"
						}
						if err := c.Get(ctx, key, obj, opts...); err != nil {
							return err
						}
						var items meta.ProcessedItems
						switch parent := obj.(type) {
						case *capsulev1beta2.GlobalTenantResource:
							items = parent.Status.ProcessedItems
						case *capsulev1beta2.TenantResource:
							items = parent.Status.ProcessedItems
						}
						switch state {
						case "legacy", "legacy adopted":
							items[0].Policy = nil
							items[0].Created = state == "legacy"
						case "protected":
							items[0].Policy.Protect = new(true)
						case "never applied":
							items[0].LastApply = metav1.Time{}
						case "different target":
							items[0].Namespace = "tenant-b"
						case "recreated":
							obj.SetUID("replacement")
						}
						return nil
					}})
					for _, call := range []func(context.Context, admission.Request) *admission.Response{
						ReplicaHandler().OnUpdate(indexed, reader, admission.NewDecoder(c.Scheme()), nil),
						ReplicaHandler().OnDelete(indexed, reader, admission.NewDecoder(c.Scheme()), nil),
					} {
						response := call(t.Context(), req)
						if state == "unprotected" || state == "legacy adopted" {
							require.Nil(t, response)
							continue
						}
						require.NotNil(t, response)
						require.False(t, response.Allowed)
						if state == "read error" {
							require.Contains(t, response.Result.Message, "parent unavailable")
						} else {
							require.EqualValues(t, 403, response.Result.Code)
							require.Contains(t, response.Result.Message, "protected by a capsule replication")
						}
					}
				})
			}
		}
	}
}

func TestReplicationTrackingWithForgedManager(t *testing.T) {
	for _, global := range []bool{false, true} {
		for _, tc := range []struct {
			name, username string
			unknownOnly    bool
			staleIdentity  bool
			explicitMarker bool
			allow          bool
		}{
			{name: "verified manager", username: "system:serviceaccount:tenant-a:runner", allow: true},
			{name: "tenant owner", username: "tenant-owner"},
			{name: "other tenant manager", username: "system:serviceaccount:other-tenant-1:runner"},
			{name: "unknown parent", username: "system:serviceaccount:tenant-a:runner", unknownOnly: true},
			{name: "replaced parent", username: "system:serviceaccount:tenant-a:runner", staleIdentity: true},
			{name: "pending protected status", username: "system:serviceaccount:tenant-a:runner", explicitMarker: true},
		} {
			t.Run(fmt.Sprintf("global=%t/%s", global, tc.name), func(t *testing.T) {
				c, req := replicationAdmissionFixture(t, global, false, 2)
				req.UserInfo.Username = tc.username
				id := gvk.ResourceID{Version: "v1", Kind: "ConfigMap", Namespace: "tenant-a", Name: "item", Tenant: "tenant-a", Origin: "0/raw-0"}
				parentName, parentNamespace := "parent", "tenant-a"
				if global {
					parentName, parentNamespace = "tenant-a", ""
				}
				old := &metav1.PartialObjectMetadata{}
				require.NoError(t, json.Unmarshal(req.OldObject.Raw, old))
				old.Labels = map[string]string{meta.NewManagedByCapsuleLabel: meta.ValueControllerReplications}
				old.ManagedFields = []metav1.ManagedFieldsEntry{{Manager: "2lclct9cwq6mg/" + id.FieldOwner("")}}
				if !tc.unknownOnly {
					old.ManagedFields = append(old.ManagedFields, metav1.ManagedFieldsEntry{Manager: meta.ReplicationFieldOwnerPrefix(parentName, parentNamespace) + "/" + id.FieldOwner("")})
				}
				if tc.explicitMarker {
					old.Labels[meta.ReplicationProtectionLabel] = meta.ValueTrue
				}
				var err error
				req.OldObject.Raw, err = json.Marshal(old)
				require.NoError(t, err)
				reads := 0
				reader := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, namespace := obj.(*corev1.Namespace); namespace {
						return nil
					}
					reads++
					if err := c.Get(ctx, key, obj, opts...); err != nil {
						return err
					}
					if tc.staleIdentity {
						obj.SetUID("replacement")
					}
					return nil
				}})
				for _, handler := range []func(context.Context, admission.Request) *admission.Response{
					ReplicaHandler().OnUpdate(c, reader, admission.NewDecoder(c.Scheme()), nil),
					ReplicaHandler().OnDelete(c, reader, admission.NewDecoder(c.Scheme()), nil),
				} {
					reads = 0
					response := handler(t.Context(), req)
					if tc.allow {
						require.Nil(t, response)
						require.Equal(t, 1, reads, "reuse the authoritative parent read")
					} else {
						require.NotNil(t, response)
						require.EqualValues(t, 403, response.Result.Code)
					}
				}
			})
		}
	}
}

func TestReplicationPruneAdoptedTarget(t *testing.T) {
	for _, tc := range []struct {
		name      string
		protected bool
		legacy    bool
		failure   types.PatchType
	}{
		{name: "explicit unprotected"},
		{name: "protected", protected: true},
		{name: "protected metadata failure retries", protected: true, failure: types.JSONPatchType},
		{name: "protected prune failure retries", protected: true, failure: types.ApplyPatchType},
		{name: "legacy adopted", legacy: true},
		{name: "metadata failure retries", failure: types.JSONPatchType},
		{name: "prune failure retries", failure: types.ApplyPatchType},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, req := replicationAdmissionFixture(t, false, tc.protected, 2)
			parent := &capsulev1beta2.TenantResource{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: "tenant-a", Name: "parent"}, parent))
			if tc.legacy {
				parent.Status.ProcessedItems[0].Policy = nil
				require.NoError(t, c.Update(t.Context(), parent))
			}
			prefix := meta.ReplicationFieldOwnerPrefix(parent.Name, parent.Namespace)
			owner := prefix + "/" + parent.Status.ProcessedItems[0].FieldOwner("")
			require.NoError(t, c.Create(t.Context(), &corev1.Namespace{Name: "tenant-a"}))
			seed := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Namespace: "tenant-a", Name: "item", Data: map[string]string{"external": "retained"}}
			require.NoError(t, c.Patch(t.Context(), seed, client.Apply, client.FieldOwner("external")))
			desired := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Namespace: "tenant-a", Name: "item", Data: map[string]string{"managed": "removed"}}
			require.NoError(t, c.Patch(t.Context(), desired, client.Apply, client.FieldOwner(owner)))
			require.NoError(t, c.Patch(t.Context(), desired, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"labels":{"projectcapsule.dev/managed-by":"replications"}}}`)), client.FieldOwner(meta.ResourceControllerFieldOwnerPrefix())))

			if tc.protected {
				require.NoError(t, c.Patch(t.Context(), desired, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"labels":{"protection.projectcapsule.dev/replications":"true"}}}`)), client.FieldOwner(meta.ResourceControllerFieldOwnerPrefix())))
			}
			req.UserInfo.Username = "system:serviceaccount:tenant-a:runner"
			handler := ReplicaHandler().OnUpdate(c, c, admission.NewDecoder(c.Scheme()), nil)
			failure := tc.failure
			reads, writes := 0, 0
			admitted := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
				Get: func(ctx context.Context, underlying client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					reads++
					return underlying.Get(ctx, key, obj, opts...)
				},
				Patch: func(ctx context.Context, underlying client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					writes++
					if patch.Type() == failure {
						return errors.New("injected cleanup failure")
					}
					old := &unstructured.Unstructured{}
					old.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
					if err := underlying.Get(ctx, client.ObjectKeyFromObject(obj), old); err != nil {
						return err
					}
					var err error
					req.OldObject.Raw, err = json.Marshal(old)
					if err != nil {
						return err
					}
					preview := fake.NewClientBuilder().WithScheme(c.Scheme()).WithReturnManagedFields().WithObjects(old).Build()
					next := obj.DeepCopyObject().(client.Object)
					if err := preview.Patch(ctx, next, patch, opts...); err != nil {
						return err
					}
					req.Object.Raw, err = json.Marshal(next)
					if err != nil {
						return err
					}
					if response := handler(ctx, req); response != nil && !response.Allowed {
						return apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, obj.GetName(), errors.New(response.Result.Message))
					}
					return underlying.Patch(ctx, obj, patch, opts...)
				},
			})
			mapper := k8smeta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion})
			mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), k8smeta.RESTScopeNamespace)
			p := processor.Processor{GatherClient: c, Mapper: mapper}
			items := parent.DeepCopy().Status.ProcessedItems
			opts := processor.ProcessorOptions{FieldOwnerPrefix: prefix, Prune: true}
			if failure != "" {
				require.Error(t, p.Reconcile(t.Context(), logr.Discard(), admitted, &items, processor.Accumulator{}, opts))
				require.Len(t, items, 1, "failed cleanup must remain tracked for retry")
				require.Contains(t, items[0].Message, "injected cleanup failure")
				retained := &corev1.ConfigMap{}
				require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(seed), retained))
				require.Equal(t, map[string]string{"external": "retained", "managed": "removed"}, retained.Data)
				failure = ""
			}
			reads, writes = 0, 0
			require.NoError(t, p.Reconcile(t.Context(), logr.Discard(), admitted, &items, processor.Accumulator{}, opts))
			require.Equal(t, 2, reads, "cleanup order must not add target reads")
			if tc.failure == "" {
				require.Equal(t, 2, writes, "cleanup needs one metadata patch and one SSA prune")
			}
			require.Empty(t, items)
			actual := &corev1.ConfigMap{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(seed), actual))
			require.Equal(t, map[string]string{"external": "retained"}, actual.Data)
			require.NotContains(t, actual.Labels, meta.NewManagedByCapsuleLabel)
		})
	}
}

func TestReplicationRecreatedProtectedTarget(t *testing.T) {
	for _, global := range []bool{false, true} {
		t.Run(fmt.Sprintf("global=%t", global), func(t *testing.T) {
			c, req := replicationAdmissionFixture(t, global, true, 1)
			var peer client.Object
			var id gvk.ResourceID
			if global {
				parent := &capsulev1beta2.GlobalTenantResource{}
				require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: "tenant-a"}, parent))
				id = parent.Status.ProcessedItems[0].ResourceID
				parent.Name = "replacement-parent"
				parent.ResourceVersion = ""
				parent.UID = "replacement-parent-uid"
				parent.Status.ServiceAccount.Name = "replacement-runner"
				peer = parent
			} else {
				parent := &capsulev1beta2.TenantResource{}
				require.NoError(t, c.Get(t.Context(), client.ObjectKey{Namespace: "tenant-a", Name: "parent"}, parent))
				id = parent.Status.ProcessedItems[0].ResourceID
				parent.Name = "replacement-parent"
				parent.ResourceVersion = ""
				parent.UID = "replacement-parent-uid"
				parent.Status.ServiceAccount.Name = "replacement-runner"
				peer = parent
			}
			require.NoError(t, c.Create(t.Context(), peer))
			owner := meta.ReplicationFieldOwnerPrefix(peer.GetName(), peer.GetNamespace()) + "/" + id.FieldOwner("")
			// The server supplies the replacement's current metadata. Only the new
			// replication owns it; the former replication still has an old status item.
			req.OldObject.Raw = []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"item","namespace":"tenant-a","uid":"replacement-target","labels":{%q:"true"},"managedFields":[{"manager":%q}]}}`, meta.ReplicationProtectionLabel, owner))
			req.Object = req.OldObject
			call := ReplicaHandler().OnUpdate(c, c, admission.NewDecoder(c.Scheme()), nil)
			req.UserInfo.Username = "system:serviceaccount:tenant-a:replacement-runner"
			require.Nil(t, call(t.Context(), req), "current owning replication must be allowed")
			req.UserInfo.Username = "tenant-owner"
			response := call(t.Context(), req)
			require.NotNil(t, response, "ordinary users must be denied")
			require.False(t, response.Allowed)
			req.UserInfo.Username = "system:serviceaccount:tenant-a:runner"
			response = call(t.Context(), req)
			require.NotNil(t, response, "stale status must not authorize the former replication on the replacement")
			require.EqualValues(t, 403, response.Result.Code)
			forged := &metav1.PartialObjectMetadata{}
			require.NoError(t, json.Unmarshal(req.Object.Raw, forged))
			parentName, parentNamespace := "parent", "tenant-a"
			if global {
				parentName, parentNamespace = "tenant-a", ""
			}
			forged.ManagedFields = append(forged.ManagedFields, metav1.ManagedFieldsEntry{Manager: meta.ReplicationFieldOwnerPrefix(parentName, parentNamespace) + "/" + id.FieldOwner("")})
			var err error
			req.Object.Raw, err = json.Marshal(forged)
			require.NoError(t, err)
			response = call(t.Context(), req)
			require.NotNil(t, response, "a forged new field manager must not restore the former identity's authority")
			require.EqualValues(t, 403, response.Result.Code)
		})
	}
}
