// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func namespaceTerminationFixture(t testing.TB, tenants, metadata int, state string) (client.WithWatch, *corev1.Namespace, *int) {
	t.Helper()
	objects := make([]client.Object, 0, tenants)
	for i := range tenants {
		objects = append(objects, &capsulev1beta2.Tenant{
			Name: fmt.Sprintf("tenant-%d", i), UID: types.UID(fmt.Sprintf("tenant-%d-uid", i)),
			Finalizers: []string{meta.ControllerFinalizer},
		})
	}
	if state == "missing" {
		objects = objects[1:]
	}
	if state == "recreated" {
		objects[0].SetUID("replacement-tenant-uid")
	}
	reads := 0
	cl := fake.NewClientBuilder().WithScheme(namespaceValidationScheme(t)).WithObjects(objects...).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			reads++
			if state == "unavailable" {
				return errors.New("API unavailable")
			}
			return c.Get(ctx, key, obj, opts...)
		},
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			t.Fatal("namespace lifecycle admission must not list resources")
			return nil
		},
	}).Build()
	ns := namespaceWithTenantReference("workloads", "tenant-0", "tenant-0-uid")
	ns.UID = "namespace-uid"
	now := metav1.Now()
	ns.DeletionTimestamp = &now
	ns.Spec.Finalizers = []corev1.FinalizerName{corev1.FinalizerKubernetes, "example.com/hold"}
	ns.Annotations = make(map[string]string, metadata)
	for i := range metadata {
		ns.Annotations[fmt.Sprintf("example.com/key-%d", i)] = "preserved"
	}
	return cl, ns, &reads
}

func TestNamespaceTerminationDoesNotDependOnTenant(t *testing.T) {
	for _, state := range []string{"live", "missing", "recreated", "unavailable"} {
		for _, subresource := range []string{"", "status", "finalize"} {
			for _, identity := range []string{"system:serviceaccount:kube-system:namespace-controller", "system:kube-controller-manager", "admin"} {
				t.Run(state+"/"+subresource+"/"+identity, func(t *testing.T) {
					cl, oldNs, reads := namespaceTerminationFixture(t, 2, 16, state)
					oldNs.Finalizers = []string{"example.com/metadata-hold"}
					newNs := oldNs.DeepCopy()
					switch subresource {
					case "":
						newNs.Finalizers = nil
					case "status":
						newNs.Status.Phase = corev1.NamespaceTerminating
					case "finalize":
						newNs.Spec.Finalizers = []corev1.FinalizerName{"example.com/hold"}
					}
					req := namespaceUpdateRequest(t, oldNs, newNs, subresource)
					req.UserInfo.Username = identity
					if strings.HasPrefix(identity, "system:serviceaccount:kube-system:") {
						req.UserInfo.Groups = []string{"system:serviceaccounts", "system:serviceaccounts:kube-system", "system:authenticated"}
					}
					response := NamespaceHandler(lifecycleConfiguration{}).OnUpdate(cl, cl, admission.NewDecoder(cl.Scheme()), nil)(t.Context(), req)
					if response != nil || *reads != 0 {
						t.Fatalf("response=%v GETs=%d; lifecycle-only updates must continue the chain without Tenant reads", response, *reads)
					}
				})
			}
		}
	}
}

func TestNamespaceTerminationPreservesGuards(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*corev1.Namespace, *corev1.Namespace)
		reason string
	}{
		{name: "tenant label", change: func(_, ns *corev1.Namespace) { ns.Labels[meta.TenantLabel] = "tenant-1" }, reason: "ownership can not change"},
		{name: "tenant UID", change: func(_, ns *corev1.Namespace) { ns.OwnerReferences[0].UID = "replacement-uid" }, reason: "ownership can not change"},
		{name: "labels", change: func(_, ns *corev1.Namespace) { ns.Labels["profile"] = "other" }, reason: "not found"},
		{name: "annotations", change: func(_, ns *corev1.Namespace) { ns.Annotations["example.com/key-0"] = "changed" }, reason: "not found"},
		{name: "other owner", change: func(_, ns *corev1.Namespace) {
			ns.OwnerReferences = append(ns.OwnerReferences, metav1.OwnerReference{APIVersion: "v1", Kind: "ConfigMap", Name: "other", UID: "other-uid"})
		}, reason: "not found"},
		{name: "metadata finalizer", change: func(_, ns *corev1.Namespace) { ns.Finalizers = []string{"example.com/hold"} }, reason: "not found"},
		{name: "metadata with finalizer removal", change: func(old, ns *corev1.Namespace) {
			old.Finalizers = []string{"example.com/hold"}
			ns.Annotations["example.com/key-0"] = "changed"
		}, reason: "not found"},
		{name: "new spec finalizer", change: func(_, ns *corev1.Namespace) { ns.Spec.Finalizers = append(ns.Spec.Finalizers, "example.com/new") }, reason: "not found"},
		{name: "duplicate spec finalizer", change: func(_, ns *corev1.Namespace) { ns.Spec.Finalizers = append(ns.Spec.Finalizers, ns.Spec.Finalizers[0]) }, reason: "not found"},
		{name: "active namespace", change: func(old, ns *corev1.Namespace) { old.DeletionTimestamp = nil; ns.DeletionTimestamp = nil }, reason: "not found"},
		{name: "forged termination", change: func(old, _ *corev1.Namespace) { old.DeletionTimestamp = nil }, reason: "not found"},
	} {
		for _, subresource := range []string{"", "status", "finalize"} {
			t.Run(tc.name+"/"+subresource, func(t *testing.T) {
				cl, oldNs, _ := namespaceTerminationFixture(t, 2, 1, "missing")
				newNs := oldNs.DeepCopy()
				tc.change(oldNs, newNs)
				req := namespaceUpdateRequest(t, oldNs, newNs, subresource)
				req.UserInfo.Username = "system:serviceaccount:kube-system:namespace-controller"
				response := NamespaceHandler(lifecycleConfiguration{}).OnUpdate(cl, cl, admission.NewDecoder(cl.Scheme()), nil)(t.Context(), req)
				if response == nil || response.Allowed || !strings.Contains(response.Result.Message, tc.reason) {
					t.Fatalf("response=%v; want denial containing %q", response, tc.reason)
				}
			})
		}
	}
	for _, subresource := range []string{"", "status", "finalize"} {
		t.Run("tenant owner/"+subresource, func(t *testing.T) {
			cl, oldNs, _ := namespaceTerminationFixture(t, 2, 1, "missing")
			req := namespaceUpdateRequest(t, oldNs, oldNs, subresource)
			req.UserInfo.Username = "alice"
			response := NamespaceHandler(lifecycleConfiguration{}).OnUpdate(cl, cl, admission.NewDecoder(cl.Scheme()), nil)(t.Context(), req)
			if response == nil || response.Allowed || !strings.Contains(response.Result.Message, "not found") {
				t.Fatalf("tenant owners must still pass ownership checks: %v", response)
			}
		})
	}
}

func TestNamespaceTerminationStillEnforcesLiveTenantGuards(t *testing.T) {
	for _, subresource := range []string{"", "status", "finalize"} {
		t.Run(subresource, func(t *testing.T) {
			cl, oldNs, _ := namespaceTerminationFixture(t, 2, 1, "live")
			decoder := admission.NewDecoder(cl.Scheme())
			recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
			req := namespaceUpdateRequest(t, oldNs, oldNs, subresource)
			req.UserInfo.Username = "alice"
			response := NamespaceHandler(lifecycleConfiguration{}).OnUpdate(cl, cl, decoder, recorder)(t.Context(), req)
			if response == nil || response.Allowed || !strings.Contains(response.Result.Message, "denied patch request") {
				t.Fatalf("unrelated tenant owner must not finalize another Tenant's namespace: %v", response)
			}

			tnt := &capsulev1beta2.Tenant{}
			if err := cl.Get(t.Context(), client.ObjectKey{Name: "tenant-0"}, tnt); err != nil {
				t.Fatal(err)
			}
			tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{RequiredMetadata: &capsulev1beta2.RequiredMetadata{Annotations: map[string]string{"example.com/key-0": "^preserved$"}}}
			if err := cl.Update(t.Context(), tnt); err != nil {
				t.Fatal(err)
			}
			newNs := oldNs.DeepCopy()
			newNs.Annotations["example.com/key-0"] = "changed"
			req = namespaceUpdateRequest(t, oldNs, newNs, subresource)
			req.UserInfo.Username = "system:kube-controller-manager"
			response = NamespaceHandler(lifecycleConfiguration{}, RequiredMetadataHandler()).OnUpdate(cl, cl, decoder, recorder)(t.Context(), req)
			if response == nil || response.Allowed || !strings.Contains(response.Result.Message, "does not match regex") {
				t.Fatalf("lifecycle subresources must not bypass metadata validation: %v", response)
			}
		})
	}
}

func BenchmarkNamespaceTerminationAdmission(b *testing.B) {
	for _, tenants := range []int{1, 1000} {
		for _, size := range []int{16, 256} {
			for _, mode := range []string{"status", "finalize", "metadata-finalizers", "metadata", "ownership-deny"} {
				b.Run(fmt.Sprintf("tenants=%d/metadata=%d/%s", tenants, size, mode), func(b *testing.B) {
					cl, oldNs, reads := namespaceTerminationFixture(b, tenants, size, "live")
					if mode == "metadata-finalizers" {
						oldNs.Finalizers = []string{"example.com/hold"}
					}
					newNs := oldNs.DeepCopy()
					subresource := "status"
					switch mode {
					case "status":
						newNs.Status.Phase = corev1.NamespaceTerminating
					case "finalize":
						subresource = "finalize"
						newNs.Spec.Finalizers = nil
					case "metadata-finalizers":
						subresource = ""
						newNs.Finalizers = nil
					case "metadata":
						newNs.Annotations["example.com/key-0"] = "changed"
					case "ownership-deny":
						newNs.OwnerReferences[0].UID = "other-uid"
					}
					req := namespaceUpdateRequest(b, oldNs, newNs, subresource)
					req.UserInfo.Username = "system:kube-controller-manager"
					handler := NamespaceHandler(lifecycleConfiguration{})
					decoder := admission.NewDecoder(cl.Scheme())
					b.ReportAllocs()
					for b.Loop() {
						// The router constructs the callback per request. Reusing one
						// would retain its request-local Tenant reader across iterations.
						response := handler.OnUpdate(cl, cl, decoder, nil)(b.Context(), req)
						if denied := response != nil && !response.Allowed; denied != (mode == "ownership-deny") {
							b.Fatalf("response=%v", response)
						}
					}
					if *reads != 0 && *reads != b.N {
						b.Fatalf("request-local reads were reused across requests: GETs=%d requests=%d", *reads, b.N)
					}
					b.ReportMetric(float64(*reads)/float64(b.N), "GET/op")
				})
			}
		}
	}
}
