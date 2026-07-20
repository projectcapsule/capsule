// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestErroredResponse(t *testing.T) {
	t.Parallel()

	resp := handlers.ErroredResponse(errors.New("boom"))
	if resp == nil {
		t.Fatalf("ErroredResponse() = nil")
	}
	if resp.Allowed {
		t.Fatalf("ErroredResponse().Allowed = true, want false")
	}
	if resp.Result == nil || resp.Result.Code != 500 {
		t.Fatalf("ErroredResponse().Result = %#v, want status 500", resp.Result)
	}
}

func TestResolveAdmissionUser(t *testing.T) {
	ctx := context.Background()
	cl := handlersFakeClient(t,
		&capsulev1beta2.CapsuleConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: "capsule"},
			Spec: capsulev1beta2.CapsuleConfigurationSpec{
				Administrators:       rbac.UserListSpec{{Kind: rbac.UserOwner, Name: "admin"}},
				IgnoreUserWithGroups: []string{"ignored"},
			},
			Status: capsulev1beta2.CapsuleConfigurationStatus{
				Users: rbac.UserListSpec{{Kind: rbac.GroupOwner, Name: "capsule-users"}},
			},
		},
	)
	cfg := configuration.NewCapsuleConfiguration(ctx, cl, cl, &rest.Config{}, "capsule")

	t.Run("controller service account is admin", func(t *testing.T) {
		t.Setenv(configuration.EnvironmentServiceaccountName, "controller")
		t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")

		user := handlers.ResolveAdmissionUser(ctx, cl, requestFor("system:serviceaccount:capsule-system:controller", nil), cfg)
		if !user.IsAdmin() {
			t.Fatalf("ResolveAdmissionUser() = %#v, want admin", user)
		}
	})

	t.Run("ignored group remains unknown", func(t *testing.T) {
		user := handlers.ResolveAdmissionUser(ctx, cl, requestFor("admin", []string{"ignored"}), cfg)
		if !user.IsUnknown() {
			t.Fatalf("ResolveAdmissionUser() = %#v, want unknown", user)
		}
	})

	t.Run("configured administrator is admin", func(t *testing.T) {
		user := handlers.ResolveAdmissionUser(ctx, cl, requestFor("admin", nil), cfg)
		if !user.IsAdmin() {
			t.Fatalf("ResolveAdmissionUser() = %#v, want admin", user)
		}
	})

	t.Run("configured capsule group is capsule user", func(t *testing.T) {
		user := handlers.ResolveAdmissionUser(ctx, cl, requestFor("alice", []string{"capsule-users"}), cfg)
		if !user.IsCapsule() {
			t.Fatalf("ResolveAdmissionUser() = %#v, want capsule user", user)
		}
	})
}

func TestInCapsuleGroupsWrapper(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := handlersFakeClient(t, &capsulev1beta2.CapsuleConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "capsule"},
		Status: capsulev1beta2.CapsuleConfigurationStatus{
			Users: rbac.UserListSpec{{Kind: rbac.GroupOwner, Name: "capsule-users"}},
		},
	})
	cfg := configuration.NewCapsuleConfiguration(ctx, cl, cl, &rest.Config{}, "capsule")
	spy := &spyHandler{}
	wrapped := handlers.InCapsuleGroups(cfg, spy)

	if resp := wrapped.OnCreate(cl, cl, nil, nil)(ctx, requestFor("alice", []string{"other"})); resp != nil {
		t.Fatalf("OnCreate() = %#v, want nil for non-capsule user", resp)
	}
	if spy.createCalls != 0 {
		t.Fatalf("inner handler called for non-capsule user")
	}

	resp := wrapped.OnCreate(cl, cl, nil, nil)(ctx, requestFor("alice", []string{"capsule-users"}))
	if resp == nil || resp.Allowed {
		t.Fatalf("OnCreate() = %#v, want inner denial response", resp)
	}
	if spy.createCalls != 1 {
		t.Fatalf("inner create calls = %d, want 1", spy.createCalls)
	}
}

func TestIsNotPrivilegedWrapper(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cl := handlersFakeClient(t, &capsulev1beta2.CapsuleConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "capsule"},
		Spec: capsulev1beta2.CapsuleConfigurationSpec{
			Administrators: rbac.UserListSpec{{Kind: rbac.UserOwner, Name: "admin"}},
		},
	})
	cfg := configuration.NewCapsuleConfiguration(ctx, cl, cl, &rest.Config{}, "capsule")
	spy := &spyHandler{}
	wrapped := handlers.IsNotPrivileged(cfg, spy)

	if resp := wrapped.OnDelete(cl, cl, nil, nil)(ctx, requestFor("admin", nil)); resp != nil {
		t.Fatalf("OnDelete() = %#v, want nil for admin user", resp)
	}
	if spy.deleteCalls != 0 {
		t.Fatalf("inner handler called for admin user")
	}

	resp := wrapped.OnDelete(cl, cl, nil, nil)(ctx, requestFor("alice", nil))
	if resp == nil || resp.Allowed {
		t.Fatalf("OnDelete() = %#v, want inner denial response", resp)
	}
	if spy.deleteCalls != 1 {
		t.Fatalf("inner delete calls = %d, want 1", spy.deleteCalls)
	}
}

func TestTypedTenantHandlerOnCreate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := handlersScheme(t)
	cl := handlersFakeClientWithScheme(t, scheme,
		&capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a", UID: types.UID("tenant-uid")}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-a-ns",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: capsulev1beta2.GroupVersion.String(),
				Kind:       "Tenant",
				Name:       "tenant-a",
				UID:        types.UID("tenant-uid"),
			}},
		}},
	)
	decoder := admission.NewDecoder(scheme)
	spy := &typedTenantSpy{}
	handler := &handlers.TypedTenantHandler[*corev1.ConfigMap]{
		Factory:  func() *corev1.ConfigMap { return &corev1.ConfigMap{} },
		Handlers: []handlers.TypedHandlerWithTenant[*corev1.ConfigMap]{spy},
	}

	resp := handler.OnCreate(cl, cl, decoder, nil)(ctx, admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: "tenant-a-ns",
			Object:    rawExtension(t, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: "tenant-a-ns"}}),
		},
	})
	if resp != nil {
		t.Fatalf("OnCreate() = %#v, want nil", resp)
	}
	if spy.createCalls != 1 || spy.lastObjectName != "settings" || spy.lastTenantName != "tenant-a" {
		t.Fatalf("typed handler spy = %#v", spy)
	}

	resp = handler.OnCreate(cl, cl, decoder, nil)(ctx, admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Object: rawExtension(t, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "settings"}}),
		},
	})
	if resp != nil {
		t.Fatalf("OnCreate() without namespace = %#v, want nil", resp)
	}
	if spy.createCalls != 1 {
		t.Fatalf("typed handler was called for request without namespace")
	}
}

type spyHandler struct {
	createCalls int
	updateCalls int
	deleteCalls int
}

func (s *spyHandler) OnCreate(client.Client, client.Reader, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		s.createCalls++

		return deniedResponse()
	}
}

func (s *spyHandler) OnUpdate(client.Client, client.Reader, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		s.updateCalls++

		return deniedResponse()
	}
}

func (s *spyHandler) OnDelete(client.Client, client.Reader, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		s.deleteCalls++

		return deniedResponse()
	}
}

type typedTenantSpy struct {
	createCalls    int
	lastObjectName string
	lastTenantName string
}

func (s *typedTenantSpy) OnCreate(
	_ client.Client,
	_ client.Reader,
	obj *corev1.ConfigMap,
	_ admission.Decoder,
	_ events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		s.createCalls++
		s.lastObjectName = obj.Name
		s.lastTenantName = tnt.Name

		return nil
	}
}

func (s *typedTenantSpy) OnUpdate(client.Client, client.Reader, *corev1.ConfigMap, *corev1.ConfigMap, admission.Decoder, events.EventRecorder, *capsulev1beta2.Tenant) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { return nil }
}

func (s *typedTenantSpy) OnDelete(client.Client, client.Reader, *corev1.ConfigMap, admission.Decoder, events.EventRecorder, *capsulev1beta2.Tenant) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { return nil }
}

func requestFor(username string, groups []string) admission.Request {
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		UserInfo: authenticationv1.UserInfo{Username: username, Groups: groups},
	}}
}

func deniedResponse() *admission.Response {
	resp := admission.Denied("denied")

	return &resp
}

func rawExtension(t *testing.T, obj client.Object) runtime.RawExtension {
	t.Helper()

	data, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshalling object: %v", err)
	}

	return runtime.RawExtension{
		Raw: data,
		Object: &metav1.PartialObjectMetadata{
			TypeMeta: metav1.TypeMeta{
				APIVersion: schema.GroupVersion{Version: "v1"}.String(),
				Kind:       "ConfigMap",
			},
		},
	}
}

func handlersFakeClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()

	return handlersFakeClientWithScheme(t, handlersScheme(t), objects...)
}

func handlersFakeClientWithScheme(t *testing.T, scheme *runtime.Scheme, objects ...client.Object) client.Client {
	t.Helper()

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()
}

func handlersScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding core scheme: %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("adding capsule scheme: %v", err)
	}

	return scheme
}
