// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestMutateNamespaceRulesSkipsFinalize(t *testing.T) {
	t.Parallel()

	request := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation:   admissionv1.Update,
		SubResource: "finalize",
	}}

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
		ObjectMeta: metav1.ObjectMeta{Name: "solar"},
		Spec: capsulev1beta2.TenantSpec{
			Rules: []*rules.NamespaceRuleBodyTenant{{
				NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
					Enforce: &rules.NamespaceRuleEnforceBody{
						Metadata: []rules.MetadataRule{{
							VersionKinds: apiruntime.VersionKinds{
								APIGroups: []string{"v1"},
								Kinds:     []string{"Namespace"},
							},
							Labels: map[string]rules.MetadataValueRule{
								"rules.example.com/managed": {Managed: ptr.To("true")},
							},
						}},
					},
				},
			}},
		},
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   "solar-production",
		Labels: map[string]string{meta.TenantLabel: tnt.Name},
	}}

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tnt).Build()
	response := mutateNamespaceRules(client, client, nil, ns)(context.Background(), admission.Request{})
	if response != nil {
		t.Fatalf("mutateNamespaceRules() response = %#v", response)
	}

	if got := ns.Labels["rules.example.com/managed"]; got != "true" {
		t.Fatalf("managed namespace label = %q, want true", got)
	}
}
