// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	capevents "github.com/projectcapsule/capsule/pkg/runtime/events"
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

func TestNamespaceHandlerRejectsTenantOwnerLabelMigrationWithEmptyOwnerReferences(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := testScheme(t)
	owner := rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Name: "alice", Kind: rbac.UserOwner}}
	green := testTenant("green", "green-uid")
	green.Status.Owners = rbac.OwnerStatusListSpec{owner}
	blue := testTenant("blue", "blue-uid")
	configurationObject := &capsulev1beta2.CapsuleConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "capsule"},
		Status: capsulev1beta2.CapsuleConfigurationStatus{
			Users: rbac.UserListSpec{owner.UserSpec},
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(configurationObject, green, blue).
		Build()
	cfg := configuration.NewCapsuleConfiguration(ctx, cl, cl, nil, configurationObject.Name)
	recorder := capevents.NewEventRecorder(nil, logr.Discard(), nil, nil)

	oldNs := testTenantNamespace("workloads", green)
	newNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   oldNs.Name,
		Labels: map[string]string{meta.TenantLabel: blue.Name},
	}}
	oldRaw, err := json.Marshal(oldNs)
	if err != nil {
		t.Fatal(err)
	}
	newRaw, err := json.Marshal(newNs)
	if err != nil {
		t.Fatal(err)
	}

	response := NamespaceHandler(cfg, OwnerReferenceHandler(cfg)).OnUpdate(
		cl,
		cl,
		admission.NewDecoder(scheme),
		recorder,
	)(ctx, admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Object:    runtime.RawExtension{Raw: newRaw},
		OldObject: runtime.RawExtension{Raw: oldRaw},
		UserInfo: authenticationv1.UserInfo{
			Username: owner.Name,
		},
	}})

	if response == nil || response.Allowed {
		t.Fatalf("expected label migration patch to be denied, got %#v", response)
	}
}
