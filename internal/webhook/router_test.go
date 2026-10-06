// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	namespacevalidation "github.com/projectcapsule/capsule/internal/webhook/namespace/validation"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type followingNamespaceGuard struct{ handlers.Handler }

func (followingNamespaceGuard) OnUpdate(client.Client, client.Reader, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		response := admission.Denied("following namespace guard")
		return &response
	}
}

func TestNamespaceLifecycleContinuesAdmissionChain(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	now := metav1.Now()
	ns := &corev1.Namespace{
		Name: "late", UID: "late-uid", DeletionTimestamp: &now,
		Finalizers:      []string{"example.com/hold"},
		Labels:          map[string]string{meta.TenantLabel: "gone"},
		OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: "gone", UID: "gone-uid"}},
	}
	raw, err := json.Marshal(ns)
	if err != nil {
		t.Fatal(err)
	}
	newNs := ns.DeepCopy()
	newNs.Finalizers = nil
	newRaw, err := json.Marshal(newNs)
	if err != nil {
		t.Fatal(err)
	}
	router := &handlerRouter{
		client: cl, reader: cl, decoder: admission.NewDecoder(scheme),
		handlers: []handlers.Handler{namespacevalidation.NamespaceHandler(nil), followingNamespaceGuard{}},
	}
	for _, subresource := range []string{"", "status", "finalize"} {
		response := router.Handle(t.Context(), admission.Request{
			Operation: admissionv1.Update, SubResource: subresource,
			Object: runtime.RawExtension{Raw: newRaw}, OldObject: runtime.RawExtension{Raw: raw},
			UserInfo: authenticationv1.UserInfo{Username: "system:kube-controller-manager"},
		})
		if response.Allowed || response.Result.Message != "following namespace guard" {
			t.Fatalf("%s: lifecycle handler skipped the following guard: %+v", subresource, response)
		}
	}
}
