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
	authuser "k8s.io/apiserver/pkg/authentication/user"
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

func TestNamespaceHandlerRestrictsTerminatingSubresourceBypassWithoutTenant(t *testing.T) {
	t.Parallel()

	scheme := namespaceValidationScheme(t)
	now := metav1.Now()
	oldNs := namespaceWithTenantReference("workloads", "missing", "missing-uid")
	oldNs.DeletionTimestamp = &now
	oldNs.Status.Phase = corev1.NamespaceTerminating
	cl, cfg := namespaceValidationFixture(t, nil)

	actors := []struct {
		name        string
		userInfo    authenticationv1.UserInfo
		wantAllowed bool
	}{
		{
			name: "kube-system service account",
			userInfo: authenticationv1.UserInfo{
				Username: "system:serviceaccount:kube-system:namespace-controller",
				Groups: []string{
					"system:serviceaccounts",
					"system:serviceaccounts:kube-system",
					"system:authenticated",
				},
			},
			wantAllowed: true,
		},
		{
			name: "unknown user",
			userInfo: authenticationv1.UserInfo{
				Username: "mallory",
				Groups:   []string{authuser.AllAuthenticated},
			},
		},
	}

	for _, actor := range actors {
		for _, subresource := range []string{"status", "finalize"} {
			t.Run(actor.name+" "+subresource, func(t *testing.T) {
				newNs := oldNs.DeepCopy()
				newNs.Status.Conditions = append(newNs.Status.Conditions, corev1.NamespaceCondition{
					Type:   "ControlPlaneProbe",
					Status: corev1.ConditionTrue,
				})
				newNs.Spec.Finalizers = nil
				request := namespaceUpdateRequest(t, oldNs, newNs, subresource)
				request.UserInfo = actor.userInfo

				response := NamespaceHandler(cfg).OnUpdate(
					cl,
					cl,
					admission.NewDecoder(scheme),
					nil,
				)(context.Background(), request)

				if actor.wantAllowed && response != nil {
					t.Fatalf("%s response = %#v, want trusted actor allowed", subresource, response)
				}
				if !actor.wantAllowed && (response == nil || response.Allowed) {
					t.Fatalf("%s response = %#v, want stale Tenant resolution failure", subresource, response)
				}
			})
		}
	}
}

func TestNamespaceHandlerRequiresUnknownUsersToOwnTerminatingNamespace(t *testing.T) {
	t.Parallel()

	scheme := namespaceValidationScheme(t)
	owner := rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Name: "alice", Kind: rbac.UserOwner}}
	tnt := namespaceValidationTenant("solar", "solar-uid", owner)
	cl, cfg := namespaceValidationFixture(t, nil, tnt)
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
	now := metav1.Now()
	oldNs := namespaceWithTenantReference("workloads", tnt.Name, string(tnt.UID))
	oldNs.DeletionTimestamp = &now
	oldNs.Status.Phase = corev1.NamespaceTerminating
	oldNs.Spec.Finalizers = []corev1.FinalizerName{corev1.FinalizerKubernetes}

	tests := []struct {
		name        string
		username    string
		subresource string
		wantAllowed bool
	}{
		{name: "owner status", username: owner.Name, subresource: "status", wantAllowed: true},
		{name: "owner finalize", username: owner.Name, subresource: "finalize", wantAllowed: true},
		{name: "non-owner status", username: "mallory", subresource: "status"},
		{name: "non-owner finalize", username: "mallory", subresource: "finalize"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newNs := oldNs.DeepCopy()
			newNs.Status.Conditions = append(newNs.Status.Conditions, corev1.NamespaceCondition{
				Type:   "UserProbe",
				Status: corev1.ConditionTrue,
			})
			newNs.Spec.Finalizers = nil
			request := namespaceUpdateRequest(t, oldNs, newNs, tt.subresource)
			request.UserInfo = authenticationv1.UserInfo{
				Username: tt.username,
				Groups:   []string{authuser.AllAuthenticated},
			}

			response := NamespaceHandler(cfg).OnUpdate(
				cl,
				cl,
				admission.NewDecoder(scheme),
				recorder,
			)(context.Background(), request)

			if tt.wantAllowed && response != nil {
				t.Fatalf("%s response = %#v, want owned update allowed", tt.subresource, response)
			}
			if !tt.wantAllowed && (response == nil || response.Allowed) {
				t.Fatalf("%s response = %#v, want non-owner denial", tt.subresource, response)
			}
		})
	}
}

func TestNamespaceHandlerRejectsUnknownUserForUnownedTerminatingNamespace(t *testing.T) {
	t.Parallel()

	scheme := namespaceValidationScheme(t)
	cl, cfg := namespaceValidationFixture(t, nil)
	now := metav1.Now()
	oldNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:              "unowned",
		DeletionTimestamp: &now,
	}}
	newNs := oldNs.DeepCopy()
	newNs.Spec.Finalizers = nil
	request := namespaceUpdateRequest(t, oldNs, newNs, "finalize")
	request.UserInfo = authenticationv1.UserInfo{
		Username: "mallory",
		Groups:   []string{authuser.AllAuthenticated},
	}

	response := NamespaceHandler(cfg).OnUpdate(
		cl,
		cl,
		admission.NewDecoder(scheme),
		nil,
	)(context.Background(), request)
	if response == nil || response.Allowed {
		t.Fatalf("finalize response = %#v, want unowned namespace denial", response)
	}
}

func TestCanBypassTerminatingNamespaceValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		user users.AdmissionUser
		want bool
	}{
		{name: "configured administrator", user: users.AdmissionUser{Type: users.AdmissionUserAdmin}, want: true},
		{name: "system masters", user: users.AdmissionUser{Groups: []string{authuser.SystemPrivilegedGroup}}, want: true},
		{name: "kubeadm administrator", user: users.AdmissionUser{Groups: []string{kubeadmClusterAdministratorsGroup}}, want: true},
		{name: "kube controller manager", user: users.AdmissionUser{Username: authuser.KubeControllerManager}, want: true},
		{name: "api server", user: users.AdmissionUser{Username: authuser.APIServerUser}, want: true},
		{
			name: "kube-system service account",
			user: users.NewAdmissionUser(users.AdmissionUserUnknown, authenticationv1.UserInfo{
				Username: users.ServiceAccountUsername(metav1.NamespaceSystem, "namespace-controller"),
			}),
			want: true,
		},
		{
			name: "tenant service account",
			user: users.NewAdmissionUser(users.AdmissionUserUnknown, authenticationv1.UserInfo{
				Username: users.ServiceAccountUsername("tenant", "controller"),
			}),
		},
		{name: "capsule user", user: users.AdmissionUser{Type: users.AdmissionUserCapsule, Username: "alice"}},
		{name: "unknown user", user: users.AdmissionUser{Username: "mallory"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canBypassTerminatingNamespaceValidation(tt.user); got != tt.want {
				t.Fatalf("canBypassTerminatingNamespaceValidation() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestNamespaceHandlerRejectsMetadataChangeOnFinalizeWithoutTenant(t *testing.T) {
	t.Parallel()

	scheme := namespaceValidationScheme(t)
	now := metav1.Now()
	oldNs := namespaceWithTenantReference("workloads", "missing", "missing-uid")
	oldNs.DeletionTimestamp = &now
	oldNs.Status.Phase = corev1.NamespaceTerminating
	newNs := oldNs.DeepCopy()
	newNs.Labels["security.example.com/probe"] = "injected"
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

	if response == nil || response.Allowed {
		t.Fatalf("finalize response = %#v, want stale Tenant resolution failure", response)
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

func TestNamespaceHandlerValidatesMetadataOnOwnedTerminatingFinalize(t *testing.T) {
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

	now := metav1.Now()
	oldNs := namespaceWithTenantReference("workloads", tnt.Name, string(tnt.UID))
	oldNs.DeletionTimestamp = &now
	oldNs.Status.Phase = corev1.NamespaceTerminating
	newNs := oldNs.DeepCopy()
	newNs.Labels[forbiddenLabel] = "privileged"
	newNs.Spec.Finalizers = nil
	request := namespaceUpdateRequest(t, oldNs, newNs, "finalize")
	request.UserInfo = authenticationv1.UserInfo{Username: owner.Name}

	response := NamespaceHandler(cfg, UserMetadataHandler()).OnUpdate(
		cl,
		cl,
		admission.NewDecoder(scheme),
		recorder,
	)(context.Background(), request)
	if response == nil || response.Allowed {
		t.Fatalf("finalize response = %#v, want forbidden metadata denial", response)
	}
}

func TestNamespaceMetadataChanged(t *testing.T) {
	t.Parallel()

	if namespaceMetadataChanged(
		&corev1.Namespace{},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Labels:          map[string]string{},
			Annotations:     map[string]string{},
			OwnerReferences: []metav1.OwnerReference{},
		}},
	) {
		t.Fatal("namespaceMetadataChanged() = true for semantically empty metadata")
	}

	oldNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Labels:      map[string]string{"label": "old"},
		Annotations: map[string]string{"annotation": "old"},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: "example.com/v1",
			Kind:       "Owner",
			Name:       "old",
		}},
	}}

	tests := []struct {
		name   string
		mutate func(*corev1.Namespace)
		want   bool
	}{
		{name: "unchanged", mutate: func(*corev1.Namespace) {}, want: false},
		{name: "label", mutate: func(ns *corev1.Namespace) { ns.Labels["label"] = "new" }, want: true},
		{name: "annotation", mutate: func(ns *corev1.Namespace) { ns.Annotations["annotation"] = "new" }, want: true},
		{name: "owner reference", mutate: func(ns *corev1.Namespace) { ns.OwnerReferences[0].Name = "new" }, want: true},
		{name: "namespace finalizer", mutate: func(ns *corev1.Namespace) {
			ns.Spec.Finalizers = []corev1.FinalizerName{corev1.FinalizerKubernetes}
		}, want: false},
		{name: "status", mutate: func(ns *corev1.Namespace) {
			ns.Status.Phase = corev1.NamespaceTerminating
		}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newNs := oldNs.DeepCopy()
			tt.mutate(newNs)
			if got := namespaceMetadataChanged(oldNs, newNs); got != tt.want {
				t.Fatalf("namespaceMetadataChanged() = %t, want %t", got, tt.want)
			}
		})
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
