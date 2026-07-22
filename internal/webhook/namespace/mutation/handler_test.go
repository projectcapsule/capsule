// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func TestNamespaceHandlerDoesNotInterceptUnlabelledAdministratorCreate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	const (
		configurationName = "capsule"
		administratorName = "configured-administrator"
	)

	configurationObject := &capsulev1beta2.CapsuleConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: configurationName},
		Spec: capsulev1beta2.CapsuleConfigurationSpec{
			Administrators: rbac.UserListSpec{{
				Name: administratorName,
				Kind: rbac.UserOwner,
			}},
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(configurationObject).
		Build()
	cfg := configuration.NewCapsuleConfiguration(ctx, cl, cl, nil, configurationName)

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   "unassigned",
		Labels: map[string]string{"example.com/label": "value"},
	}}
	raw, err := json.Marshal(ns)
	if err != nil {
		t.Fatal(err)
	}

	response := NamespaceHandler(cfg).OnCreate(
		cl,
		cl,
		admission.NewDecoder(scheme),
		nil,
	)(ctx, admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Object: runtime.RawExtension{Raw: raw},
		UserInfo: authenticationv1.UserInfo{
			Username: administratorName,
		},
	}})

	if response != nil {
		t.Fatalf("expected unlabelled administrator create not to be intercepted, got %#v", response)
	}
}
