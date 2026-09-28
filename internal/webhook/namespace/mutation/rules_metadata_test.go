// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/users"
)

func TestNamespaceMutationPreservesTemplatedAudience(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	tnt := &capsulev1beta2.Tenant{
		Name: "team",
		Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{{
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Audience: []rules.Audience{{Kind: rules.AudienceKindUser, Name: `{{ index .namespace.metadata.labels "example.com/owner" }}`}},
				Enforce: &rules.NamespaceRuleEnforceBody{Metadata: []rules.MetadataRule{{
					Kinds:  []string{"Namespace"},
					Labels: map[string]rules.MetadataValueRule{"example.com/managed": {Managed: new("{{ .tenant.metadata.name }}")}},
				}}},
			},
		}}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tnt).Build()
	for _, owner := range []string{"alice", "bob", "alice"} {
		ns := &corev1.Namespace{Name: "team-test", Labels: map[string]string{meta.TenantLabel: tnt.Name, "example.com/owner": owner}}
		req := admission.Request{Operation: admissionv1.Create, UserInfo: authenticationv1.UserInfo{Username: "alice"}}
		if response := mutateNamespaceRules(c, c, nil, ns)(t.Context(), req); response != nil {
			t.Fatalf("mutation failed: %#v", response)
		}
		value, present := ns.Labels["example.com/managed"]
		if present != (owner == "alice") || (present && value != tnt.Name) {
			t.Fatalf("owner=%q, managed=%q, present=%t", owner, value, present)
		}
	}
}

func TestNamespaceDenyOnlyMutationSkipsAudienceLookup(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	tnt := &capsulev1beta2.Tenant{
		Name: "team",
		Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{{
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Audience: []rules.Audience{{Kind: rules.AudienceKindCustom, Name: "CapsuleUser"}},
				Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny, Metadata: []rules.MetadataRule{{
					Kinds:  []string{"Namespace"},
					Labels: map[string]rules.MetadataValueRule{"example.com/restricted": {}},
				}}},
			},
		}}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tnt).Build()
	ns := &corev1.Namespace{Name: "team-test", Labels: map[string]string{meta.TenantLabel: tnt.Name}}
	// A custom audience requires configuration when evaluated. Mutation has no
	// work here; validation still evaluates the audience and the denial policy.
	if response := mutateNamespaceRules(c, c, nil, ns)(t.Context(), admission.Request{}); response != nil {
		t.Fatalf("deny-only mutation evaluated audience: %#v", response)
	}
}

func TestMutateNamespaceRulesSkipsFinalize(t *testing.T) {
	t.Parallel()

	request := admission.Request{
		Operation:   admissionv1.Update,
		SubResource: "finalize"}

	if response := mutateNamespaceRules(nil, nil, nil, nil)(context.Background(), request); response != nil {
		t.Fatalf("mutateNamespaceRules() response = %#v, want nil", response)
	}
}

func TestMutateNamespaceRules(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core API to scheme: %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule API to scheme: %v", err)
	}

	tnt := &capsulev1beta2.Tenant{
		Name: "solar",
		Spec: capsulev1beta2.TenantSpec{
			Rules: []*rules.NamespaceRuleBodyTenant{{
				NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Metadata: []rules.MetadataRule{{
							APIGroups: []string{"v1"},
							Kinds:     []string{"Namespace"},
							Labels: map[string]rules.MetadataValueRule{
								"rules.example.com/managed": {Managed: new("true")},
							},
						}},
					},
				},
			}},
		},
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tnt).Build()
	for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
		t.Run(string(operation), func(t *testing.T) {
			ns := &corev1.Namespace{
				Name:   "solar-production",
				Labels: map[string]string{meta.TenantLabel: tnt.Name}}
			old := ns.DeepCopy()
			handler := RulesMetadataHandler(nil)
			req := admission.Request{Operation: operation}
			var response *admission.Response
			if operation == admissionv1.Create {
				response = handler.OnCreate(client, client, users.AdmissionUser{}, ns, nil, nil)(t.Context(), req)
			} else {
				response = handler.OnUpdate(client, client, users.AdmissionUser{}, ns, old, nil, nil)(t.Context(), req)
			}
			if response != nil {
				t.Fatalf("metadata mutation response = %#v", response)
			}

			if got := ns.Labels["rules.example.com/managed"]; got != "true" {
				t.Fatalf("managed namespace label = %q, want true", got)
			}
			if _, ok := old.Labels["rules.example.com/managed"]; ok {
				t.Fatal("metadata mutation modified the old namespace")
			}
		})
	}
}
