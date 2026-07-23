// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/users"
)

func TestRulesMetadataHandlerSkipsFinalize(t *testing.T) {
	t.Parallel()

	handler := RulesMetadataHandler(nil, nil)
	request := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation:   admissionv1.Update,
		SubResource: "finalize",
	}}

	response := handler.OnUpdate(
		nil,
		nil,
		users.AdmissionUser{},
		nil,
		nil,
		nil,
		nil,
		nil,
	)(context.Background(), request)
	if response != nil {
		t.Fatalf("OnUpdate() response = %#v, want nil", response)
	}
}

func TestRulesMetadataHandlerValidatesStatusMetadata(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core API to scheme: %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule API to scheme: %v", err)
	}

	tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "solar"}}
	tnt.Spec.Rules = []*rules.NamespaceRuleBodyTenant{{
		NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
			Enforce: &rules.NamespaceRuleEnforceBody{
				Action: rules.ActionTypeAllow,
				Metadata: []rules.MetadataRule{{
					VersionKinds: apiruntime.VersionKinds{APIGroups: []string{"v1"}, Kinds: []string{"Namespace"}},
					Labels: map[string]rules.MetadataValueRule{
						"pod-security.kubernetes.io/enforce": {
							Required: true,
							Values:   []apiruntime.ExpressionMatch{{Exact: []string{"restricted", "baseline"}}},
						},
					},
				}},
			},
		},
	}}

	oldNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   "solar-system",
		Labels: map[string]string{"pod-security.kubernetes.io/enforce": "baseline"},
	}}
	newNs := oldNs.DeepCopy()
	newNs.Labels["pod-security.kubernetes.io/enforce"] = "privileged"

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
	handler := RulesMetadataHandler(cache.NewRegexCache(), nil)
	request := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Kind:        metav1.GroupVersionKind{Version: "v1", Kind: "Namespace"},
		Operation:   admissionv1.Update,
		SubResource: "status",
	}}

	response := handler.OnUpdate(
		client,
		client,
		users.AdmissionUser{},
		newNs,
		oldNs,
		nil,
		recorder,
		tnt,
	)(context.Background(), request)
	if response == nil || response.Allowed {
		t.Fatalf("OnUpdate() response = %#v, want metadata injection denied", response)
	}
}
