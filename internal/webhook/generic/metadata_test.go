// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func tenantAssignmentFixture(t testing.TB, tenants int) (client.Client, *int) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	objects := make([]client.Object, 0, tenants*2)
	for i := range tenants {
		name := fmt.Sprintf("tenant-%d", i)
		tnt := &capsulev1beta2.Tenant{Name: name, UID: types.UID(name)}
		ns := &corev1.Namespace{Name: name + "-ns", UID: types.UID(name + "-ns"),
			OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: name, UID: tnt.UID}},
		}
		objects = append(objects, tnt, ns)
	}
	reads := new(int)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			*reads++
			return c.Get(ctx, key, obj, opts...)
		},
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			t.Fatal("tenant label assignment must not list unrelated objects")
			return nil
		},
	}).Build()
	return c, reads
}

func tenantAssignmentObject(namespace, tenant string) *corev1.ConfigMap {
	stamp := metav1.Now()
	return &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: "held", Namespace: namespace,
		UID: "held-uid", ResourceVersion: "42", DeletionTimestamp: &stamp,
		Finalizers: []string{"example.com/hold"},
		Labels:     map[string]string{meta.ManagedByCapsuleLabel: tenant, meta.NewTenantLabel: tenant, "env": "e2e"},
		Data:       map[string]string{"payload": "preserved"},
	}
}

func tenantAssignmentRequest(t testing.TB, old, obj *corev1.ConfigMap) admission.Request {
	t.Helper()
	oldRaw, err := json.Marshal(old)
	require.NoError(t, err)
	raw, err := json.Marshal(obj)
	require.NoError(t, err)
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Update, Namespace: obj.Namespace, Name: obj.Name,
		Kind:     metav1.GroupVersionKind{Version: "v1", Kind: "ConfigMap"},
		Resource: metav1.GroupVersionResource{Version: "v1", Resource: "configmaps"},
		Object:   runtime.RawExtension{Raw: raw}, OldObject: runtime.RawExtension{Raw: oldRaw},
	}}
}

func TestTenantAssignmentFinalizerCompletion(t *testing.T) {
	for _, mode := range []string{"last-finalizer", "last-of-many", "active", "still-finalized", "already-unfinalized", "missing-uid", "different-uid", "different-name", "different-namespace", "different-timestamp", "positive-grace", "old-positive-grace", "status", "label-change", "missing-label", "inconsistent-labels", "malformed", "malformed-old", "read-error", "missing-namespace", "no-namespace"} {
		t.Run(mode, func(t *testing.T) {
			c, reads := tenantAssignmentFixture(t, 2)
			old := tenantAssignmentObject("tenant-0-ns", "tenant-0")
			obj := old.DeepCopy()
			obj.Finalizers = nil
			skip := mode == "last-finalizer" || mode == "last-of-many"
			patch := false
			switch mode {
			case "last-of-many":
				old.Finalizers = append(old.Finalizers, "example.com/second")
			case "active":
				old.DeletionTimestamp, obj.DeletionTimestamp = nil, nil
			case "still-finalized":
				obj.Finalizers = []string{"capsule.projectcapsule.dev/finalizer"}
			case "already-unfinalized":
				old.Finalizers = nil
			case "missing-uid":
				old.UID, obj.UID = "", ""
			case "different-uid":
				obj.UID = "replacement"
			case "different-name":
				old.Name = "other"
			case "different-namespace":
				old.Namespace = "tenant-1-ns"
			case "different-timestamp":
				stamp := metav1.NewTime(old.DeletionTimestamp.AddDate(0, 0, -1))
				old.DeletionTimestamp = &stamp
			case "positive-grace", "old-positive-grace":
				grace := int64(30)
				old.DeletionGracePeriodSeconds = &grace
				if mode == "positive-grace" {
					obj.DeletionGracePeriodSeconds = &grace
				}
			case "label-change":
				obj.Labels[meta.NewTenantLabel] = "tenant-1"
				patch = true
			case "missing-label":
				delete(old.Labels, meta.ManagedByCapsuleLabel)
				delete(obj.Labels, meta.ManagedByCapsuleLabel)
				patch = true
			case "inconsistent-labels":
				old.Labels[meta.NewTenantLabel], obj.Labels[meta.NewTenantLabel] = "tenant-1", "tenant-1"
				patch = true
			case "missing-namespace":
				old.Namespace, obj.Namespace = "missing", "missing"
				obj.Finalizers = old.Finalizers
			case "read-error":
				obj.Finalizers = old.Finalizers
				c = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
					*reads++
					return errors.New("namespace API unavailable")
				}})
			}
			req := tenantAssignmentRequest(t, old, obj)
			switch mode {
			case "status":
				req.SubResource = "status"
			case "malformed":
				req.Object.Raw = []byte("{")
			case "malformed-old":
				req.OldObject.Raw = []byte("{")
			case "no-namespace":
				req.Namespace = ""
			}
			before, oldBefore := string(req.Object.Raw), string(req.OldObject.Raw)
			response := TenantAssignmentHandler().OnUpdate(c, c, admission.NewDecoder(c.Scheme()), nil)(t.Context(), req)
			require.Equal(t, before, string(req.Object.Raw))
			require.Equal(t, oldBefore, string(req.OldObject.Raw))
			if skip || mode == "no-namespace" || strings.HasPrefix(mode, "malformed") {
				require.Zero(t, *reads)
			} else {
				require.Equal(t, 1, *reads)
			}
			switch {
			case strings.HasPrefix(mode, "malformed") || mode == "read-error":
				require.NotNil(t, response)
				require.False(t, response.Allowed)
				if mode == "read-error" {
					require.Contains(t, response.Result.Message, "namespace API unavailable")
				}
			case patch:
				require.NotNil(t, response)
				require.True(t, response.Allowed)
				require.NotEmpty(t, response.Patches)
				for _, p := range response.Patches {
					require.Equal(t, "tenant-0", p.Value)
				}
			default:
				require.Nil(t, response)
			}
		})
	}
}

func TestTenantAssignmentCreateStillUsesNamespaceOwnership(t *testing.T) {
	c, reads := tenantAssignmentFixture(t, 2)
	for _, tenant := range []string{"tenant-0", "tenant-1"} {
		obj := tenantAssignmentObject(tenant+"-ns", "forged-tenant")
		obj.DeletionTimestamp, obj.Finalizers = nil, nil
		req := tenantAssignmentRequest(t, obj, obj)
		req.Operation = admissionv1.Create
		response := TenantAssignmentHandler().OnCreate(c, c, admission.NewDecoder(c.Scheme()), nil)(t.Context(), req)
		require.NotNil(t, response)
		for _, p := range response.Patches {
			require.Equal(t, tenant, p.Value)
		}
	}
	require.Equal(t, 2, *reads)
}

func BenchmarkTenantAssignmentAdmission(b *testing.B) {
	for _, tenants := range []int{1, 1000} {
		for _, size := range []int{0, 16 * 1024} {
			for _, mode := range []string{"create", "active-update", "last-finalizer", "retained-finalizer", "retag"} {
				b.Run(fmt.Sprintf("tenants=%d/payload=%d/%s", tenants, size, mode), func(b *testing.B) {
					c, reads := tenantAssignmentFixture(b, tenants)
					old := tenantAssignmentObject("tenant-0-ns", "tenant-0")
					old.Data["payload"] = strings.Repeat("x", size)
					obj := old.DeepCopy()
					obj.Finalizers = nil
					switch mode {
					case "create", "active-update":
						old.DeletionTimestamp, obj.DeletionTimestamp = nil, nil
					case "retained-finalizer":
						obj.Finalizers = old.Finalizers
					case "retag":
						obj.Labels[meta.NewTenantLabel] = "tenant-elsewhere"
					}
					req := tenantAssignmentRequest(b, old, obj)
					h := TenantAssignmentHandler()
					handle := h.OnUpdate(c, c, admission.NewDecoder(c.Scheme()), nil)
					if mode == "create" {
						req.Operation = admissionv1.Create
						handle = h.OnCreate(c, c, admission.NewDecoder(c.Scheme()), nil)
					}
					b.ReportAllocs()
					for b.Loop() {
						response := handle(b.Context(), req)
						if mode == "retag" {
							if response == nil || !response.Allowed || len(response.Patches) == 0 {
								b.Fatal("missing label repair")
							}
						} else if response != nil {
							b.Fatalf("unexpected response: %+v", response)
						}
					}
					want := b.N
					if mode == "last-finalizer" {
						want = 0
					}
					if *reads != want {
						b.Fatalf("namespace GETs=%d, want=%d", *reads, want)
					}
					b.ReportMetric(float64(*reads)/float64(b.N), "namespace-GET/op")
				})
			}
		}
	}
}
