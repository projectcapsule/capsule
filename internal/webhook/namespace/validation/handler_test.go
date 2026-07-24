// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestNamespaceHandlerAllowsUnchangedFinalizeWithoutTenant(t *testing.T) {
	t.Parallel()

	scheme := namespaceValidationScheme(t)
	now := metav1.Now()
	oldNs := namespaceWithTenantReference("workloads", "missing", "missing-uid")
	oldNs.DeletionTimestamp = &now
	oldNs.Status.Phase = corev1.NamespaceTerminating
	newNs := oldNs.DeepCopy()
	newNs.Spec.Finalizers = nil

	response := NamespaceHandler(nil).OnUpdate(
		nil,
		nil,
		admission.NewDecoder(scheme),
		nil,
	)(context.Background(), namespaceUpdateRequest(t, oldNs, newNs, "finalize"))

	if response != nil {
		t.Fatalf("finalize response = %#v, want no interception", response)
	}
}

func TestNamespaceHandlerRejectsTenantChangeDuringFinalize(t *testing.T) {
	t.Parallel()

	scheme := namespaceValidationScheme(t)
	now := metav1.Now()
	oldNs := namespaceWithTenantReference("workloads", "solar", "solar-uid")
	oldNs.DeletionTimestamp = &now
	oldNs.Status.Phase = corev1.NamespaceTerminating
	newNs := namespaceWithTenantReference("workloads", "lunar", "lunar-uid")
	newNs.DeletionTimestamp = &now
	newNs.Status.Phase = corev1.NamespaceTerminating

	response := NamespaceHandler(nil).OnUpdate(
		nil,
		nil,
		admission.NewDecoder(scheme),
		nil,
	)(context.Background(), namespaceUpdateRequest(t, oldNs, newNs, "finalize"))

	if response == nil || response.Allowed {
		t.Fatalf("finalize response = %#v, want tenant assignment denial", response)
	}
}

func TestNamespaceHandlerAllowsDeleteWithMissingTenant(t *testing.T) {
	t.Parallel()

	scheme := namespaceValidationScheme(t)
	reader := fake.NewClientBuilder().WithScheme(scheme).Build()
	oldNs := namespaceWithTenantReference("workloads", "missing", "missing-uid")
	raw, err := json.Marshal(oldNs)
	if err != nil {
		t.Fatal(err)
	}

	response := NamespaceHandler(nil).OnDelete(
		reader,
		reader,
		admission.NewDecoder(scheme),
		nil,
	)(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation: admissionv1.Delete,
		OldObject: runtime.RawExtension{Raw: raw},
	}})

	if response != nil {
		t.Fatalf("delete response = %#v, want missing Tenant to be ignored", response)
	}
}

func namespaceValidationScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	return scheme
}

func namespaceWithTenantReference(name, tenantName, tenantUID string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   name,
		Labels: map[string]string{meta.TenantLabel: tenantName},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: capsulev1beta2.GroupVersion.String(),
			Kind:       "Tenant",
			Name:       tenantName,
			UID:        types.UID(tenantUID),
		}},
	}}
}

func namespaceUpdateRequest(
	t *testing.T,
	oldNs, newNs *corev1.Namespace,
	subresource string,
) admission.Request {
	t.Helper()

	oldRaw, err := json.Marshal(oldNs)
	if err != nil {
		t.Fatal(err)
	}
	newRaw, err := json.Marshal(newNs)
	if err != nil {
		t.Fatal(err)
	}

	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation:   admissionv1.Update,
		SubResource: subresource,
		Object:      runtime.RawExtension{Raw: newRaw},
		OldObject:   runtime.RawExtension{Raw: oldRaw},
	}}
}
