// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func TestNamespaceValidationAllowsRemovingStaleTenantLabelWithoutOwnerReference(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(corev1): %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(capsule): %v", err)
	}

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	cfg := configuration.NewCapsuleConfiguration(context.Background(), c, c, nil, "default")
	decoder := admission.NewDecoder(scheme)
	h := NamespaceHandler(cfg)

	oldNs := &corev1.Namespace{}
	oldNs.SetName("stale")
	oldNs.SetUID(types.UID("stale-uid"))
	oldNs.SetLabels(map[string]string{
		meta.TenantLabel: "tenant-a",
	})

	newNs := oldNs.DeepCopy()
	newNs.SetLabels(map[string]string{})

	req := namespaceUpdateRequest(t, oldNs, newNs)
	resp := h.OnUpdate(c, c, decoder, nil)(context.Background(), req)
	if resp != nil && !resp.Allowed {
		t.Fatalf("expected stale tenant label removal to be allowed, got denial: %s", resp.Result.Message)
	}
}

func TestNamespaceValidationDeniesAddingTenantLabelWithoutOwnerReference(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(corev1): %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(capsule): %v", err)
	}

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	cfg := configuration.NewCapsuleConfiguration(context.Background(), c, c, nil, "default")
	decoder := admission.NewDecoder(scheme)
	h := NamespaceHandler(cfg)

	oldNs := &corev1.Namespace{}
	oldNs.SetName("unmanaged")
	oldNs.SetUID(types.UID("unmanaged-uid"))

	newNs := oldNs.DeepCopy()
	newNs.SetLabels(map[string]string{
		meta.TenantLabel: "tenant-a",
	})

	req := namespaceUpdateRequest(t, oldNs, newNs)
	resp := h.OnUpdate(c, c, decoder, nil)(context.Background(), req)
	if resp == nil || resp.Allowed {
		t.Fatal("expected adding tenant label without ownerReference to be denied")
	}
}

func TestNamespaceValidationAllowsDeletingStaleTenantLabelWithoutOwnerReference(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(corev1): %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(capsule): %v", err)
	}

	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	cfg := configuration.NewCapsuleConfiguration(context.Background(), c, c, nil, "default")
	decoder := admission.NewDecoder(scheme)
	h := NamespaceHandler(cfg)

	oldNs := &corev1.Namespace{}
	oldNs.SetName("stale")
	oldNs.SetUID(types.UID("stale-uid"))
	oldNs.SetLabels(map[string]string{
		meta.TenantLabel: "tenant-a",
	})

	req := namespaceDeleteRequest(t, oldNs)
	resp := h.OnDelete(c, c, decoder, nil)(context.Background(), req)
	if resp != nil && !resp.Allowed {
		t.Fatalf("expected stale tenant label namespace deletion to be allowed, got denial: %s", resp.Result.Message)
	}
}

func namespaceUpdateRequest(t *testing.T, oldNs *corev1.Namespace, newNs *corev1.Namespace) admission.Request {
	t.Helper()

	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Version: "v1",
				Kind:    "Namespace",
			},
			UserInfo: authenticationv1.UserInfo{
				Username: "tenant-owner",
			},
			Object: runtime.RawExtension{
				Raw: encodeNamespace(t, newNs),
			},
			OldObject: runtime.RawExtension{
				Raw: encodeNamespace(t, oldNs),
			},
		},
	}
}

func namespaceDeleteRequest(t *testing.T, oldNs *corev1.Namespace) admission.Request {
	t.Helper()

	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Version: "v1",
				Kind:    "Namespace",
			},
			UserInfo: authenticationv1.UserInfo{
				Username: "tenant-owner",
			},
			OldObject: runtime.RawExtension{
				Raw: encodeNamespace(t, oldNs),
			},
		},
	}
}

func encodeNamespace(t *testing.T, ns *corev1.Namespace) []byte {
	t.Helper()

	ns = ns.DeepCopy()
	ns.APIVersion = "v1"
	ns.Kind = "Namespace"

	encoded, err := json.Marshal(ns)
	if err != nil {
		t.Fatalf("encode namespace: %v", err)
	}

	if !json.Valid(encoded) {
		t.Fatal("encoded namespace is not valid JSON")
	}

	return encoded
}
