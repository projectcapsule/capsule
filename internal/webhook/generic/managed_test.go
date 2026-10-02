// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/users"
)

func managedAdmissionFixture(t testing.TB, tenants int) (handlers.Handler, client.Client, *int) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, capsulev1beta2.AddToScheme(scheme))
	config := &capsulev1beta2.CapsuleConfiguration{Name: "capsule", Spec: capsulev1beta2.CapsuleConfigurationSpec{
		Administrators: rbac.UserListSpec{{Kind: rbac.UserOwner, Name: "admin"}},
	}}
	objects := []client.Object{config}
	for i := range tenants {
		objects = append(objects, &capsulev1beta2.Tenant{Name: fmt.Sprintf("tenant-%d", i)})
	}
	reads := new(int)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			*reads++
			return c.Get(ctx, key, obj, opts...)
		},
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			t.Fatal("managed user/controller admission must not scan tenants")
			return nil
		},
	}).Build()
	return ManagedValidatingHandler(configuration.NewCapsuleConfiguration(t.Context(), c, c, nil, config.Name)), c, reads
}

func TestManagedLabelProtectionIdentities(t *testing.T) {
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")
	t.Setenv(configuration.EnvironmentServiceaccountName, "capsule")
	h, c, reads := managedAdmissionFixture(t, 2)
	for _, namespace := range []string{"", "tenant-a"} {
		for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update, admissionv1.Delete} {
			for _, actor := range []struct {
				name  string
				allow bool
			}{
				{"owner", false}, {"admin", true}, {"", false},
				{users.ServiceAccountUsername("capsule-system", "capsule"), true},
				{users.ServiceAccountUsername("tenant-a", "capsule"), false},
			} {
				t.Run(fmt.Sprintf("namespace=%s/%s/%s", namespace, operation, actor.name), func(t *testing.T) {
					if namespace != "" {
						require.NoError(t, client.IgnoreAlreadyExists(c.Create(t.Context(), &corev1.Namespace{Name: namespace})))
					}
					req := admission.Request{Namespace: namespace, Operation: operation, UserInfo: authenticationv1.UserInfo{Username: actor.name}}
					var call handlers.Func
					switch operation {
					case admissionv1.Create:
						call = h.OnCreate(c, c, nil, nil)
					case admissionv1.Update:
						call = h.OnUpdate(c, c, nil, nil)
					case admissionv1.Delete:
						call = h.OnDelete(c, c, nil, nil)
					}
					*reads = 0
					response := call(t.Context(), req)
					if actor.allow {
						require.Nil(t, response)
					} else {
						require.NotNil(t, response)
						require.False(t, response.Allowed)
						require.Contains(t, response.Result.Message, "Labeling resources as controller managed")
					}
					if actor.name == users.ServiceAccountUsername("capsule-system", "capsule") {
						require.Zero(t, *reads)
					}
				})
			}
		}
	}
	t.Run("legacy managed resources can be deleted during namespace termination", func(t *testing.T) {
		ns := &corev1.Namespace{Name: "terminating", Status: corev1.NamespaceStatus{Phase: corev1.NamespaceTerminating}}
		require.NoError(t, c.Create(t.Context(), ns))
		req := admission.Request{Namespace: ns.Name, Operation: admissionv1.Delete, UserInfo: authenticationv1.UserInfo{Username: "namespace-controller"}}
		require.Nil(t, h.OnDelete(c, c, nil, nil)(t.Context(), req))
	})
}

func BenchmarkManagedLabelGuard(b *testing.B) {
	b.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")
	b.Setenv(configuration.EnvironmentServiceaccountName, "capsule")
	for _, tenants := range []int{1, 1000} {
		for _, actor := range []string{"owner", "admin", users.ServiceAccountUsername("capsule-system", "capsule")} {
			b.Run(fmt.Sprintf("tenants=%d/%s", tenants, actor), func(b *testing.B) {
				h, c, reads := managedAdmissionFixture(b, tenants)
				req := admission.Request{Kind: metav1.GroupVersionKind{Version: "v1", Kind: "Namespace"}, UserInfo: authenticationv1.UserInfo{Username: actor}}
				call := h.OnCreate(c, c, nil, nil)
				b.ReportAllocs()
				for b.Loop() {
					response := call(b.Context(), req)
					if (response != nil && !response.Allowed) != (actor == "owner") {
						b.Fatal("unexpected admission result")
					}
				}
				b.ReportMetric(float64(*reads)/float64(b.N), "GET/op")
			})
		}
	}
}
