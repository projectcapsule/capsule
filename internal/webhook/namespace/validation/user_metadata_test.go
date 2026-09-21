// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"testing"

	"github.com/go-logr/logr"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/users"
	"github.com/projectcapsule/capsule/pkg/utils"
)

func TestNodeSelectorAnnotationProtection(t *testing.T) {
	t.Setenv(configuration.EnvironmentServiceaccountName, "capsule-controller")
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")

	tnt := &capsulev1beta2.Tenant{Spec: capsulev1beta2.TenantSpec{NodeSelector: map[string]string{"node-type": "compute"}}}
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
	for _, actor := range []struct {
		name       string
		user       users.AdmissionUser
		controller bool
	}{
		{"controller", users.NewAdmissionUser(users.AdmissionUserAdmin, users.ServiceAccountUserInfo("capsule-system", "capsule-controller")), true},
		{"tenant owner", users.NewAdmissionUser(users.AdmissionUserCapsule, authenticationv1.UserInfo{Username: "owner@example.com"}), false},
		{"tenant service account", users.NewAdmissionUser(users.AdmissionUserCapsule, users.ServiceAccountUserInfo("team", "builder")), false},
		{"same service account name in another namespace", users.NewAdmissionUser(users.AdmissionUserCapsule, users.ServiceAccountUserInfo("team", "capsule-controller")), false},
		{"another service account in controller namespace", users.NewAdmissionUser(users.AdmissionUserUnknown, users.ServiceAccountUserInfo("capsule-system", "other")), false},
		{"administrator", users.NewAdmissionUser(users.AdmissionUserAdmin, authenticationv1.UserInfo{Username: "cluster-admin"}), false},
	} {
		t.Run(actor.name, func(t *testing.T) {
			for _, change := range []struct {
				name     string
				old, new map[string]string
				changed  bool
			}{
				{"replace selector", map[string]string{utils.NodeSelectorAnnotation: "node-type=worker"}, map[string]string{utils.NodeSelectorAnnotation: "node-type=compute"}, true},
				{"add selector", nil, map[string]string{utils.NodeSelectorAnnotation: "node-type=compute"}, true},
				{"remove selector", map[string]string{utils.NodeSelectorAnnotation: "node-type=worker"}, nil, true},
				{"unchanged selector", map[string]string{utils.NodeSelectorAnnotation: "node-type=compute"}, map[string]string{utils.NodeSelectorAnnotation: "node-type=compute", "example.com/unrelated": "changed"}, false},
			} {
				t.Run(change.name, func(t *testing.T) {
					oldNs := &corev1.Namespace{Name: "team-test", Annotations: change.old}
					newNs := &corev1.Namespace{Name: "team-test", Annotations: change.new}
					response := UserMetadataHandler().OnUpdate(nil, nil, actor.user, newNs, oldNs, nil, recorder, tnt)(t.Context(), admission.Request{})
					wantDenied := change.changed && !actor.controller
					if denied := response != nil && !response.Allowed; denied != wantDenied {
						t.Fatalf("response = %#v, want denied=%t", response, wantDenied)
					}
				})
			}
		})
	}
}

func TestNamespaceHandlerAllowsControllerNodeSelectorReconciliation(t *testing.T) {
	t.Setenv(configuration.EnvironmentServiceaccountName, "capsule-controller")
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")

	scheme := namespaceValidationScheme(t)
	tnt := &capsulev1beta2.Tenant{
		Name: "team", UID: "team-uid",
		Spec: capsulev1beta2.TenantSpec{NodeSelector: map[string]string{"node-type": "compute"}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tnt).Build()
	oldNs := namespaceWithTenantReference("team-test", tnt.Name, string(tnt.UID))
	oldNs.Annotations = map[string]string{utils.NodeSelectorAnnotation: "node-type=worker"}
	newNs := oldNs.DeepCopy()
	newNs.Annotations = utils.BuildNodeSelector(tnt, newNs.Annotations)
	req := namespaceUpdateRequest(t, oldNs, newNs, "")
	req.UserInfo = users.ServiceAccountUserInfo("capsule-system", "capsule-controller")
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)

	response := NamespaceHandler(nil, UserMetadataHandler()).OnUpdate(c, c, admission.NewDecoder(scheme), recorder)(t.Context(), req)
	if response != nil {
		t.Fatalf("controller's reconciled nodeSelector was rejected: %#v", response)
	}
}
