// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsuleapi "github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/users"
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
	cl, cfg := namespaceValidationFixture(t, nil)
	request := namespaceUpdateRequest(t, oldNs, newNs, "finalize")
	request.UserInfo = authenticationv1.UserInfo{
		Username: "system:serviceaccount:kube-system:namespace-controller",
		Groups: []string{
			"system:serviceaccounts",
			"system:serviceaccounts:kube-system",
			"system:authenticated",
		},
	}

	response := NamespaceHandler(cfg).OnUpdate(
		cl,
		cl,
		admission.NewDecoder(scheme),
		nil,
	)(context.Background(), request)

	if response != nil {
		t.Fatalf("finalize response = %#v, want no interception", response)
	}
}

func TestNamespaceHandlerValidatesMetadataOnActiveSubresources(t *testing.T) {
	t.Parallel()

	const forbiddenLabel = "pod-security.kubernetes.io/enforce"

	scheme := namespaceValidationScheme(t)
	owner := rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Name: "alice", Kind: rbac.UserOwner}}
	tnt := namespaceValidationTenant("solar", "solar-uid", owner)
	tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
		ForbiddenLabels: capsuleapi.ForbiddenListSpec{Exact: []string{forbiddenLabel}},
	}
	cl, cfg := namespaceValidationFixture(t, []rbac.UserSpec{owner.UserSpec}, tnt)
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)

	tests := []struct {
		name           string
		subresource    string
		oldPhase       corev1.NamespacePhase
		requestedPhase corev1.NamespacePhase
	}{
		{name: "status requested terminating phase", subresource: "status", requestedPhase: corev1.NamespaceTerminating},
		{name: "status previously spoofed terminating phase", subresource: "status", oldPhase: corev1.NamespaceTerminating, requestedPhase: corev1.NamespaceTerminating},
		{name: "finalize", subresource: "finalize"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldNs := namespaceWithTenantReference("workloads", tnt.Name, string(tnt.UID))
			oldNs.Status.Phase = tt.oldPhase
			newNs := oldNs.DeepCopy()
			newNs.Labels[forbiddenLabel] = "privileged"
			newNs.Status.Phase = tt.requestedPhase

			request := namespaceUpdateRequest(t, oldNs, newNs, tt.subresource)
			request.UserInfo = authenticationv1.UserInfo{Username: owner.Name}
			response := NamespaceHandler(cfg, UserMetadataHandler()).OnUpdate(
				cl,
				cl,
				admission.NewDecoder(scheme),
				recorder,
			)(context.Background(), request)
			if response == nil || response.Allowed {
				t.Fatalf("%s response = %#v, want forbidden metadata denial", tt.subresource, response)
			}
		})
	}
}

func TestNamespaceHandlerRejectsCrossTenantFinalize(t *testing.T) {
	t.Parallel()

	scheme := namespaceValidationScheme(t)
	attacker := rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Name: "alice", Kind: rbac.UserOwner}}
	owner := rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Name: "bob", Kind: rbac.UserOwner}}
	tnt := namespaceValidationTenant("solar", "solar-uid", owner)
	cl, cfg := namespaceValidationFixture(t, []rbac.UserSpec{attacker.UserSpec, owner.UserSpec}, tnt)
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)

	for _, terminating := range []bool{false, true} {
		t.Run(fmt.Sprintf("terminating=%t", terminating), func(t *testing.T) {
			oldNs := namespaceWithTenantReference("workloads", tnt.Name, string(tnt.UID))
			if terminating {
				now := metav1.Now()
				oldNs.DeletionTimestamp = &now
			}
			newNs := oldNs.DeepCopy()
			newNs.Labels["security.example.com/probe"] = "injected"
			request := namespaceUpdateRequest(t, oldNs, newNs, "finalize")
			request.UserInfo = authenticationv1.UserInfo{Username: attacker.Name}

			response := NamespaceHandler(cfg).OnUpdate(
				cl,
				cl,
				admission.NewDecoder(scheme),
				recorder,
			)(context.Background(), request)
			if response == nil || response.Allowed {
				t.Fatalf("finalize response = %#v, want cross-tenant denial", response)
			}
		})
	}
}

func TestNamespaceHandlerAllowsOwnedTerminatingFinalize(t *testing.T) {
	t.Parallel()

	scheme := namespaceValidationScheme(t)
	owner := rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Name: "alice", Kind: rbac.UserOwner}}
	tnt := namespaceValidationTenant("solar", "solar-uid", owner)
	cl, cfg := namespaceValidationFixture(t, []rbac.UserSpec{owner.UserSpec}, tnt)

	now := metav1.Now()
	oldNs := namespaceWithTenantReference("workloads", tnt.Name, string(tnt.UID))
	oldNs.DeletionTimestamp = &now
	oldNs.Status.Phase = corev1.NamespaceTerminating
	oldNs.Spec.Finalizers = []corev1.FinalizerName{corev1.FinalizerKubernetes}
	newNs := oldNs.DeepCopy()
	newNs.Spec.Finalizers = nil
	request := namespaceUpdateRequest(t, oldNs, newNs, "finalize")
	request.UserInfo = authenticationv1.UserInfo{Username: owner.Name}

	response := NamespaceHandler(cfg).OnUpdate(
		cl,
		cl,
		admission.NewDecoder(scheme),
		nil,
	)(context.Background(), request)
	if response != nil {
		t.Fatalf("finalize response = %#v, want owned terminating namespace allowed", response)
	}
}

func TestValidateNamespaceTenantReferenceTransitionRejectsCapsuleUserForUnownedNamespace(t *testing.T) {
	t.Parallel()

	oldNs := &corev1.Namespace{}
	newNs := &corev1.Namespace{}
	response, stop := validateNamespaceTenantReferenceTransition(
		users.AdmissionUser{Type: users.AdmissionUserCapsule, Username: "alice"},
		oldNs,
		newNs,
	)
	if !stop || response == nil || response.Allowed {
		t.Fatalf("transition response = %#v, stop = %t, want unowned namespace denial", response, stop)
	}

	response, stop = validateNamespaceTenantReferenceTransition(users.AdmissionUser{}, oldNs, newNs)
	if !stop || response != nil {
		t.Fatalf("system transition response = %#v, stop = %t, want unchanged pass-through", response, stop)
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

func namespaceValidationFixture(
	t *testing.T,
	capsuleUsers rbac.UserListSpec,
	objects ...client.Object,
) (client.Client, configuration.Configuration) {
	t.Helper()

	const configurationName = "capsule"

	scheme := namespaceValidationScheme(t)
	configurationObject := &capsulev1beta2.CapsuleConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: configurationName},
		Status: capsulev1beta2.CapsuleConfigurationStatus{
			Users: capsuleUsers,
		},
	}
	objects = append(objects, configurationObject)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	cfg := configuration.NewCapsuleConfiguration(context.Background(), cl, cl, nil, configurationName)

	return cl, cfg
}

func namespaceValidationTenant(
	name string,
	uid types.UID,
	owners ...rbac.CoreOwnerSpec,
) *capsulev1beta2.Tenant {
	ownerSpecs := make(rbac.OwnerListSpec, 0, len(owners))
	for _, owner := range owners {
		ownerSpecs = append(ownerSpecs, rbac.OwnerSpec{CoreOwnerSpec: owner})
	}

	return &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: name, UID: uid},
		Spec: capsulev1beta2.TenantSpec{
			Owners: ownerSpecs,
		},
		Status: capsulev1beta2.TenantStatus{
			Owners: rbac.OwnerStatusListSpec(owners),
		},
	}
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
