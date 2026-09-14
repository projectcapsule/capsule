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
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/users"
)

func TestRulesMetadataHandlerAllowsSubResourceMetadataModifications(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core API to scheme: %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule API to scheme: %v", err)
	}

	tnt := &capsulev1beta2.Tenant{Name: "solar"}

	tests := []struct {
		name   string
		modify func(ns *corev1.Namespace)
	}{
		{
			name: "finalizers modified",
			modify: func(ns *corev1.Namespace) {
				ns.Finalizers = []string{}
			},
		},
		{
			name: "resourceVersion modified",
			modify: func(ns *corev1.Namespace) {
				ns.ResourceVersion = "2"
			},
		},
		{
			name: "generation modified",
			modify: func(ns *corev1.Namespace) {
				ns.Generation = 2
			},
		},
		{
			name: "managedFields modified",
			modify: func(ns *corev1.Namespace) {
				ns.ManagedFields = []metav1.ManagedFieldsEntry{
					{
						Manager:    "kube-controller-manager",
						Operation:  metav1.ManagedFieldsOperationUpdate,
						APIVersion: "v1",
					},
				}
			},
		},
		{
			name: "all allowed modifications combined",
			modify: func(ns *corev1.Namespace) {
				ns.Finalizers = []string{}
				ns.ResourceVersion = "2"
				ns.Generation = 2
				ns.ManagedFields = []metav1.ManagedFieldsEntry{
					{
						Manager:    "kube-controller-manager",
						Operation:  metav1.ManagedFieldsOperationUpdate,
						APIVersion: "v1",
					},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			oldNs := &corev1.Namespace{
				Name:            "solar-system",
				Labels:          map[string]string{"env": "prod"},
				Finalizers:      []string{"capsule.clastix.io/finalizer"},
				ResourceVersion: "1",
				Generation:      1,
				ManagedFields: []metav1.ManagedFieldsEntry{
					{
						Manager:    "kubectl",
						Operation:  metav1.ManagedFieldsOperationUpdate,
						APIVersion: "v1",
					},
				},
			}
			newNs := oldNs.DeepCopy()
			tt.modify(newNs)

			client := fake.NewClientBuilder().WithScheme(scheme).Build()
			recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
			handler := RulesMetadataHandler(cache.NewRegexCache(), nil)
			request := admission.Request{
				Kind:        metav1.GroupVersionKind{Version: "v1", Kind: "Namespace"},
				Operation:   admissionv1.Update,
				SubResource: "finalize",
			}

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
			if response != nil && !response.Allowed {
				t.Fatalf("OnUpdate() response = %#v, want allowed", response)
			}
		})
	}
}

func TestRulesMetadataHandlerRejectsSubresourceMetadataModifications(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core API to scheme: %v", err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatalf("add Capsule API to scheme: %v", err)
	}

	tnt := &capsulev1beta2.Tenant{Name: "solar"}

	tests := []struct {
		name   string
		modify func(ns *corev1.Namespace)
	}{
		{
			name: "label modified",
			modify: func(ns *corev1.Namespace) {
				ns.Labels["new-label"] = "injected"
			},
		},
		{
			name: "annotation modified",
			modify: func(ns *corev1.Namespace) {
				if ns.Annotations == nil {
					ns.Annotations = map[string]string{}
				}
				ns.Annotations["new-annotation"] = "injected"
			},
		},
		{
			name: "ownerReference modified",
			modify: func(ns *corev1.Namespace) {
				ns.OwnerReferences = append(ns.OwnerReferences, metav1.OwnerReference{
					APIVersion: "capsule.clastix.io/v1beta2",
					Kind:       "Tenant",
					Name:       "attacker",
					UID:        "12345",
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			oldNs := &corev1.Namespace{
				Name:       "solar-system",
				Labels:     map[string]string{"env": "prod"},
				Finalizers: []string{"capsule.clastix.io/finalizer"},
			}
			newNs := oldNs.DeepCopy()
			newNs.Finalizers = []string{}
			tt.modify(newNs)

			client := fake.NewClientBuilder().WithScheme(scheme).Build()
			recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
			handler := RulesMetadataHandler(cache.NewRegexCache(), nil)
			request := admission.Request{
				Kind:        metav1.GroupVersionKind{Version: "v1", Kind: "Namespace"},
				Operation:   admissionv1.Update,
				SubResource: "finalize",
			}

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
				t.Fatalf("OnUpdate() response = %#v, want denied", response)
			}
		})
	}
}

func BenchmarkNamespaceMetadataChanged(b *testing.B) {
	oldNs := &corev1.Namespace{
		Name: "solar-system",
		Labels: map[string]string{
			"env":  "prod",
			"tier": "frontend",
		},
		Annotations: map[string]string{
			"capsule.clastix.io/ingress": "true",
		},
		Finalizers: []string{"capsule.clastix.io/finalizer"},
	}
	newNs := oldNs.DeepCopy()
	newNs.Finalizers = []string{}

	b.ResetTimer()
	for b.Loop() {
		_ = namespaceMetadataChanged(oldNs, newNs)
	}
}
