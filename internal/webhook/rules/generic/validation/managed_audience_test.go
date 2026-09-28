// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	namespacevalidation "github.com/projectcapsule/capsule/internal/webhook/namespace/validation"
	genericvalidation "github.com/projectcapsule/capsule/internal/webhook/rules/generic/validation"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/users"
)

func TestManagedMetadataAdmissionRespectsAudience(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	managedRule := func(audience rules.Audience, policies map[string]rules.MetadataValueRule) *rules.NamespaceRuleBodyNamespace {
		return &rules.NamespaceRuleBodyNamespace{
			Audience: []rules.Audience{audience},
			Enforce: &rules.NamespaceRuleEnforceBody{
				Action: rules.ActionTypeAllow,
				Metadata: []rules.MetadataRule{{
					Kinds:  []string{"Namespace", "ConfigMap"},
					Labels: policies, Annotations: policies,
				}},
			},
		}
	}
	bodies := []*rules.NamespaceRuleBodyNamespace{
		managedRule(rules.Audience{Kind: rules.AudienceKindUser, Name: "alice"}, map[string]rules.MetadataValueRule{
			"example.corp/alice": {Managed: new("alice")}, "example.corp/shared": {Managed: new("alice")},
		}),
		managedRule(rules.Audience{Kind: rules.AudienceKindUser, Name: "bob"}, map[string]rules.MetadataValueRule{
			"example.corp/shared": {Managed: new("bob")},
		}),
		managedRule(rules.Audience{Kind: rules.AudienceKindCustom, Name: string(rules.CustomAudienceCapsuleUser)}, map[string]rules.MetadataValueRule{
			"example.corp/common": {Managed: new("capsule")},
		}),
		{
			Audience: []rules.Audience{{Kind: rules.AudienceKindCustom, Name: string(rules.CustomAudienceCapsuleUser)}},
			Enforce: &rules.NamespaceRuleEnforceBody{
				Action: rules.ActionTypeDeny,
				Metadata: []rules.MetadataRule{{
					Kinds:       []string{"Namespace", "ConfigMap"},
					Labels:      map[string]rules.MetadataValueRule{"example.corp/.*": {}},
					Annotations: map[string]rules.MetadataValueRule{"example.corp/.*": {}},
				}},
			},
		},
	}
	tnt := &capsulev1beta2.Tenant{
		Name: "tenant", UID: "tenant-uid",
		Status: capsulev1beta2.TenantStatus{
			Namespaces: []string{"tenant-ns"},
			Owners: rbac.OwnerStatusListSpec{{
				Kind: rbac.ServiceAccountOwner, Name: users.ServiceAccountUsername("tenant-ns", "promoted")}},
		},
	}
	for _, body := range bodies {
		tnt.Spec.Rules = append(tnt.Spec.Rules, &rules.NamespaceRuleBodyTenant{NamespaceRuleBodyNamespace: body})
	}
	ns := &corev1.Namespace{
		Name: "tenant-ns", Labels: map[string]string{meta.TenantLabel: tnt.Name},
		OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: tnt.Name, UID: tnt.UID}}}
	rs := &capsulev1beta2.RuleStatus{Name: meta.NameForManagedRuleStatus(), Namespace: ns.Name}
	rs.Status.Rules = bodies
	config := &capsulev1beta2.CapsuleConfiguration{Name: "capsule"}
	config.Status.Users = rbac.UserListSpec{{Kind: rbac.GroupOwner, Name: "capsule-users"}}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tnt, ns, rs, config).
		WithIndex(&capsulev1beta2.Tenant{}, ".status.namespaces", func(obj client.Object) []string {
			return obj.(*capsulev1beta2.Tenant).Status.Namespaces
		}).Build()
	cfg := configuration.NewCapsuleConfiguration(t.Context(), cl, cl, nil, config.Name)
	decoder := admission.NewDecoder(scheme)
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
	alice := authenticationv1.UserInfo{Username: "alice", Groups: []string{"capsule-users"}}
	bob := authenticationv1.UserInfo{Username: "bob", Groups: []string{"capsule-users"}}

	for _, kind := range []string{"Namespace", "ConfigMap"} {
		for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
			for _, field := range []string{"labels", "annotations"} {
				for _, tt := range []struct {
					name         string
					user         authenticationv1.UserInfo
					key, value   string
					wantBlocking bool
				}{
					{name: "matching user", user: alice, key: "example.corp/alice", value: "alice"},
					{name: "other user cannot inherit exemption", user: bob, key: "example.corp/alice", value: "alice", wantBlocking: true},
					{name: "other user empty value", user: bob, key: "example.corp/alice", wantBlocking: true},
					{name: "first audience shared value", user: alice, key: "example.corp/shared", value: "alice"},
					{name: "last audience shared value", user: bob, key: "example.corp/shared", value: "bob"},
					{name: "wrong audience shared value", user: alice, key: "example.corp/shared", value: "bob", wantBlocking: true},
					{name: "CapsuleUser audience", user: alice, key: "example.corp/common", value: "capsule"},
					{name: "unpromoted CapsuleUser", user: users.ServiceAccountUserInfo(ns.Name, "unpromoted"), key: "example.corp/common", value: "capsule"},
					{name: "promoted CapsuleUser", user: users.ServiceAccountUserInfo(ns.Name, "promoted"), key: "example.corp/common", value: "capsule"},
					{name: "service account cannot inherit user exemption", user: users.ServiceAccountUserInfo(ns.Name, "promoted"), key: "example.corp/alice", value: "alice", wantBlocking: true},
					{name: "outside deny audience", user: authenticationv1.UserInfo{Username: "admin"}, key: "example.corp/alice", value: "arbitrary"},
				} {
					t.Run(strings.Join([]string{kind, string(operation), field, tt.name}, "/"), func(t *testing.T) {
						old := ns.DeepCopy()
						obj := old.DeepCopy()
						if field == "labels" {
							obj.Labels[tt.key] = tt.value
						} else {
							obj.Annotations = map[string]string{tt.key: tt.value}
						}
						req := admission.Request{
							Kind: metav1.GroupVersionKind{Version: "v1", Kind: kind}, Operation: operation, UserInfo: tt.user}
						var response *admission.Response
						if kind == "Namespace" {
							handler := namespacevalidation.RulesMetadataHandler(cache.NewRegexCache(), cfg)
							if operation == admissionv1.Create {
								response = handler.OnCreate(cl, cl, users.AdmissionUser{}, obj, decoder, recorder, tnt)(t.Context(), req)
							} else {
								response = handler.OnUpdate(cl, cl, users.AdmissionUser{}, obj, old, decoder, recorder, tnt)(t.Context(), req)
							}
						} else {
							req.Namespace = ns.Name
							raw, err := json.Marshal(&corev1.ConfigMap{ObjectMeta: obj.ObjectMeta})
							if err != nil {
								t.Fatal(err)
							}
							req.Object.Raw = raw
							raw, err = json.Marshal(&corev1.ConfigMap{ObjectMeta: old.ObjectMeta})
							if err != nil {
								t.Fatal(err)
							}
							req.OldObject.Raw = raw
							handler := genericvalidation.Register(cache.NewRegexCache(), cfg).GetHandlers()[0]
							if operation == admissionv1.Create {
								response = handler.OnCreate(cl, cl, decoder, recorder)(t.Context(), req)
							} else {
								response = handler.OnUpdate(cl, cl, decoder, recorder)(t.Context(), req)
							}
						}
						if blocked := response != nil && !response.Allowed; blocked != tt.wantBlocking {
							t.Fatalf("blocked = %v, want %v; response = %#v", blocked, tt.wantBlocking, response)
						}
						if tt.wantBlocking && !strings.Contains(response.Result.Message, "denied by namespace rule") {
							t.Fatalf("unexpected denial: %s", response.Result.Message)
						}
					})
				}
			}
		}
	}
}
