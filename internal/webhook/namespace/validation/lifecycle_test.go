// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
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
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

type lifecycleConfiguration struct{ configuration.Configuration }

func (lifecycleConfiguration) Administrators() rbac.UserListSpec {
	return rbac.UserListSpec{{Name: "admin", Kind: rbac.UserOwner}}
}
func (lifecycleConfiguration) Users() rbac.UserListSpec       { return nil }
func (lifecycleConfiguration) UserGroups() []string           { return nil }
func (lifecycleConfiguration) IgnoreUserWithGroups() []string { return nil }
func (lifecycleConfiguration) GetUsersByStatus() rbac.UserListSpec {
	return rbac.UserListSpec{{Name: "alice", Kind: rbac.UserOwner}, {Name: "system:serviceaccount:owners:alice", Kind: rbac.ServiceAccountOwner}}
}

func lifecycleAdmissionFixture(t testing.TB, count int, mode string) (func(context.Context, admission.Request) *admission.Response, admission.Request, *int) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	tnt := &capsulev1beta2.Tenant{Name: "tenant-0", UID: "tenant-0-uid", Finalizers: []string{meta.ControllerFinalizer}}
	if mode == "unprotected" {
		tnt.Finalizers = nil
	}
	if strings.HasPrefix(mode, "terminating") {
		now := metav1.Now()
		tnt.DeletionTimestamp = &now
	}
	if mode == "terminating-recorded-name" {
		tnt.Status.Spaces = []*capsulev1beta2.TenantStatusNamespaceItem{{Name: "workloads", UID: "previous-namespace-uid"}}
	}
	objects := []client.Object{tnt}
	for i := 1; i < count; i++ {
		objects = append(objects, &capsulev1beta2.Tenant{Name: fmt.Sprintf("tenant-%d", i), UID: types.UID(fmt.Sprintf("tenant-%d-uid", i))})
	}
	reads := 0
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			reads++
			return c.Get(ctx, key, obj, opts...)
		},
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			t.Fatal("namespace admission must not list resources")
			return nil
		},
	}).Build()
	ns := namespaceWithTenantReference("workloads", tnt.Name, string(tnt.UID))
	if mode == "unmanaged" {
		ns = &corev1.Namespace{Name: "unmanaged"}
	}
	raw, err := json.Marshal(ns)
	if err != nil {
		t.Fatal(err)
	}
	req := admission.Request{Operation: admissionv1.Create, Object: runtime.RawExtension{Raw: raw}, UserInfo: authenticationv1.UserInfo{Username: "admin"}}
	h := NamespaceHandler(lifecycleConfiguration{})
	decoder := admission.NewDecoder(scheme)
	handler := func(ctx context.Context, req admission.Request) *admission.Response {
		return h.OnCreate(cl, cl, decoder, nil)(ctx, req)
	}
	if mode == "terminating-existing-update" || mode == "terminating-migration" {
		old := ns.DeepCopy()
		if mode == "terminating-migration" {
			old = &corev1.Namespace{Name: ns.Name}
		}
		oldRaw, err := json.Marshal(old)
		if err != nil {
			t.Fatal(err)
		}
		req.Operation = admissionv1.Update
		req.OldObject = runtime.RawExtension{Raw: oldRaw}
		handler = func(ctx context.Context, req admission.Request) *admission.Response {
			return h.OnUpdate(cl, cl, decoder, nil)(ctx, req)
		}
	}
	return handler, req, &reads
}

func TestNamespaceAssignmentLifecycle(t *testing.T) {
	for _, mode := range []string{"active", "unprotected", "terminating", "terminating-recorded-name", "terminating-existing-update", "terminating-migration", "unmanaged"} {
		t.Run(mode, func(t *testing.T) {
			handler, req, reads := lifecycleAdmissionFixture(t, 2, mode)
			response := handler(t.Context(), req)
			deny := mode == "unprotected" || mode == "terminating" || mode == "terminating-recorded-name" || mode == "terminating-migration"
			if denied := response != nil && !response.Allowed; denied != deny {
				t.Fatalf("mode=%s response=%v", mode, response)
			}
			if deny {
				reason := "terminating"
				if mode == "unprotected" {
					reason = "lifecycle protection is not ready"
				}
				if !strings.Contains(response.Result.Message, reason) {
					t.Fatal(response.Result.Message)
				}
			}
			want := 1
			if mode == "unmanaged" {
				want = 0
			}
			if *reads != want {
				t.Fatalf("GET calls=%d want=%d", *reads, want)
			}
		})
	}
}

func TestNamespaceAssignmentLifecycleForTenantOwners(t *testing.T) {
	for _, identity := range []authenticationv1.UserInfo{
		{Username: "alice"},
		{Username: "system:serviceaccount:owners:alice", Groups: []string{"system:serviceaccounts", "system:serviceaccounts:owners", "system:authenticated"}},
	} {
		for _, mode := range []string{"active", "unprotected", "terminating"} {
			t.Run(identity.Username+"/"+mode, func(t *testing.T) {
				handler, req, reads := lifecycleAdmissionFixture(t, 2, mode)
				req.UserInfo = identity
				response := handler(t.Context(), req)
				if denied := response != nil && !response.Allowed; denied != (mode != "active") {
					t.Fatalf("response=%v", response)
				}
				if *reads != 1 {
					t.Fatalf("GET calls=%d", *reads)
				}
			})
		}
	}
}

func BenchmarkNamespaceLifecycleAdmission(b *testing.B) {
	for _, count := range []int{1, 1000} {
		for _, mode := range []string{"active", "terminating", "unmanaged"} {
			b.Run(fmt.Sprintf("tenants=%d/%s", count, mode), func(b *testing.B) {
				handler, req, reads := lifecycleAdmissionFixture(b, count, mode)
				b.ReportAllocs()
				for b.Loop() {
					response := handler(b.Context(), req)
					if deny := response != nil && !response.Allowed; deny != (mode == "terminating") {
						b.Fatalf("response=%v", response)
					}
				}
				b.ReportMetric(float64(*reads)/float64(b.N), "GET/op")
			})
		}
	}
}
