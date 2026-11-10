// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	admissionapi "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenantresource"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

type admissionConfigurationFixture struct {
	client      client.WithWatch
	reconciler  reconcile.Reconciler
	configReads int
	directReads int
}

func newAdmissionConfigurationFixture(t *testing.T, kind string, count int, fallback bool) *admissionConfigurationFixture {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{admissionv1.AddToScheme, capsulev1beta2.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	url := "https://capsule.example.com"
	sideEffects := admissionv1.SideEffectClassNone
	dynamic := admissionapi.DynamicAdmissionConfig{Name: meta.RFC1123Name(kind), Client: &admissionv1.WebhookClientConfig{URL: &url}}
	object := &capsulev1beta2.CapsuleConfiguration{
		Name: "capsule", UID: "config-uid",
		Spec: capsulev1beta2.CapsuleConfigurationSpec{
			Users:          rbac.UserListSpec{{Kind: rbac.UserOwner, Name: "alice"}},
			Administrators: rbac.UserListSpec{{Kind: rbac.GroupOwner, Name: "admins"}},
			Admission: capsulev1beta2.DynamicAdmission{
				Mutating:   &capsulev1beta2.DynamicMutatingAdmissionConfig{DynamicAdmissionConfig: dynamic},
				Validating: &capsulev1beta2.DynamicValidatingAdmissionConfig{DynamicAdmissionConfig: dynamic},
			},
		},
	}
	options := admissionapi.WebhookOptions{CapsuleUsers: true, Administrators: true}
	for i := range count {
		name := fmt.Sprintf("hook-%02d.capsule.example.com", i)
		object.Spec.Admission.Mutating.Webhooks = append(object.Spec.Admission.Mutating.Webhooks,
			&admissionapi.MutatingWebhook{Name: name, Options: options, SideEffects: &sideEffects, AdmissionReviewVersions: []string{"v1"}})
		object.Spec.Admission.Validating.Webhooks = append(object.Spec.Admission.Validating.Webhooks,
			&admissionapi.ValidatingWebhook{Name: name, Options: options, SideEffects: &sideEffects, AdmissionReviewVersions: []string{"v1"}})
	}
	f := &admissionConfigurationFixture{client: fake.NewClientBuilder().WithScheme(scheme).WithIndex(&capsulev1beta2.GlobalTenantResource{}, replication.TriggerIndexerField, (tenantresource.Triggers{}).Func()).WithIndex(&capsulev1beta2.TenantResource{}, replication.TriggerIndexerField, (tenantresource.Triggers{}).Func()).WithObjects(object).Build()}
	cached := interceptor.NewClient(f.client, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*capsulev1beta2.CapsuleConfiguration); ok {
				f.configReads++
				if fallback {
					return context.DeadlineExceeded
				}
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})
	direct := interceptor.NewClient(f.client, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			f.directReads++
			return c.Get(ctx, key, obj, opts...)
		},
	})
	cfg := configuration.NewCapsuleConfiguration(t.Context(), cached, direct, nil, object.Name)
	f.reconciler = &mutatingReconciler{client: cached, configuration: cfg, log: logr.Discard()}
	if kind == "validating" {
		f.reconciler = &validatingReconciler{client: cached, configuration: cfg, log: logr.Discard()}
	}
	return f
}

func (f *admissionConfigurationFixture) reconcile(t *testing.T, kind string, count int, users, admins rbac.UserListSpec) {
	t.Helper()
	if _, err := f.reconciler.Reconcile(t.Context(), reconcile.Request{Name: kind}); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		return
	}
	var conditions [][]admissionv1.MatchCondition
	if kind == "mutating" {
		obj := &admissionv1.MutatingWebhookConfiguration{}
		if err := f.client.Get(t.Context(), client.ObjectKey{Name: kind}, obj); err != nil {
			t.Fatal(err)
		}
		for _, hook := range obj.Webhooks {
			conditions = append(conditions, hook.MatchConditions)
		}
	} else {
		obj := &admissionv1.ValidatingWebhookConfiguration{}
		if err := f.client.Get(t.Context(), client.ObjectKey{Name: kind}, obj); err != nil {
			t.Fatal(err)
		}
		for _, hook := range obj.Webhooks {
			conditions = append(conditions, hook.MatchConditions)
		}
	}
	if len(conditions) != count {
		t.Fatalf("webhooks = %d, want %d", len(conditions), count)
	}
	expected := admissionapi.BuildGatingUserCondition(admissionapi.WebhookOptions{CapsuleUsers: true, Administrators: true}, users, admins)
	for i, got := range conditions {
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("hook %d identity conditions = %#v, want %#v", i, got, expected)
		}
	}
}

func TestAdmissionConfigurationBoundsIdentityReads(t *testing.T) {
	users := rbac.UserListSpec{{Kind: rbac.UserOwner, Name: "alice"}}
	admins := rbac.UserListSpec{{Kind: rbac.GroupOwner, Name: "admins"}}
	for _, kind := range []string{"mutating", "validating"} {
		for _, fallback := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/fallback=%t", kind, fallback), func(t *testing.T) {
				baseline := newAdmissionConfigurationFixture(t, kind, 1, fallback)
				baseline.reconcile(t, kind, 1, users, admins)
				for _, count := range []int{0, 1, 32} {
					t.Run(fmt.Sprintf("hooks=%d", count), func(t *testing.T) {
						f := newAdmissionConfigurationFixture(t, kind, count, fallback)
						f.reconcile(t, kind, count, users, admins)
						wantReads := baseline.configReads
						if count == 0 {
							wantReads = 1
						}
						if f.configReads != wantReads {
							t.Fatalf("configuration reads = %d, want %d independent of webhook count", f.configReads, wantReads)
						}
						wantDirect := 0
						if fallback {
							wantDirect = wantReads
						}
						if f.directReads != wantDirect {
							t.Fatalf("direct reads = %d, want %d", f.directReads, wantDirect)
						}
					})
				}
			})
		}
	}
}

func TestAdmissionConfigurationRefreshesIdentities(t *testing.T) {
	for _, kind := range []string{"mutating", "validating"} {
		t.Run(kind, func(t *testing.T) {
			f := newAdmissionConfigurationFixture(t, kind, 3, false)
			f.reconcile(t, kind, 3, rbac.UserListSpec{{Kind: rbac.UserOwner, Name: "alice"}}, rbac.UserListSpec{{Kind: rbac.GroupOwner, Name: "admins"}})
			current := &capsulev1beta2.CapsuleConfiguration{}
			if err := f.client.Get(t.Context(), client.ObjectKey{Name: "capsule"}, current); err != nil {
				t.Fatal(err)
			}
			current.Spec.Users = rbac.UserListSpec{{Kind: rbac.GroupOwner, Name: "replacement-users"}}
			current.Spec.Administrators = rbac.UserListSpec{{Kind: rbac.UserOwner, Name: "replacement-admin"}}
			if err := f.client.Update(t.Context(), current); err != nil {
				t.Fatal(err)
			}
			f.reconcile(t, kind, 3, current.Spec.Users, current.Spec.Administrators)
		})
	}
}
