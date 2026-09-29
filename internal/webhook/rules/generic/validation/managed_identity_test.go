// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/users"
)

func managedIdentityRules(count int) []*rules.NamespaceRuleBodyNamespace {
	bodies := make([]*rules.NamespaceRuleBodyNamespace, count)
	for i := range bodies {
		bodies[i] = &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
			Action: rules.ActionTypeDeny,
			Metadata: []rules.MetadataRule{{Kinds: []string{"Namespace", "Pod"}, Labels: map[string]rules.MetadataValueRule{
				fmt.Sprintf("example.org/denied-%d", i): {Values: []runtime.ExpressionMatch{{Exact: []string{"true"}}}},
			}}},
		}}
	}
	return bodies
}

func TestManagedMetadataExemptionRequiresControllerIdentity(t *testing.T) {
	t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")
	t.Setenv(configuration.EnvironmentServiceaccountName, "capsule-controller")

	for _, kind := range []string{"Namespace", "Pod"} {
		for _, op := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
			for _, label := range []string{"", meta.ValueController, meta.ValueControllerResources, meta.ValueControllerReplications, meta.ValueControllerResourcePermit} {
				for _, actor := range []struct {
					name       string
					info       authenticationv1.UserInfo
					controller bool
				}{
					{name: "owner", info: authenticationv1.UserInfo{Username: "alice", Groups: []string{"capsule-users"}}},
					{name: "admin", info: authenticationv1.UserInfo{Username: "admin", Groups: []string{"system:masters"}}},
					{name: "unknown"},
					{name: "same SA in another namespace", info: users.ServiceAccountUserInfo("tenant-a", "capsule-controller")},
					{name: "different SA", info: users.ServiceAccountUserInfo("capsule-system", "other")},
					{name: "template SA", info: users.ServiceAccountUserInfo("tenant-a", "runner")},
					{name: "controller", info: users.ServiceAccountUserInfo("capsule-system", "capsule-controller"), controller: true},
				} {
					t.Run(fmt.Sprintf("%s/%s/label=%s/%s", kind, op, label, actor.name), func(t *testing.T) {
						obj := genericMetadataObject(map[string]string{"example.org/denied-0": "true"}, nil)
						if label != "" {
							obj.Labels[meta.NewManagedByCapsuleLabel] = label
						}
						req := admissionRequest("v1", kind)
						req.Operation, req.UserInfo = op, actor.info
						h := GenericRules(nil)
						// Nil clients ensure the identity check adds no API lookups.
						var response *admission.Response
						if op == admissionv1.Create {
							response = h.OnCreate(nil, nil, obj, nil, testEventRecorder{}, testTenant(), managedIdentityRules(1))(t.Context(), req)
						} else {
							old := genericMetadataObject(nil, nil)
							response = h.OnUpdate(nil, nil, old, obj, nil, testEventRecorder{}, testTenant(), managedIdentityRules(1))(t.Context(), req)
						}
						if actor.controller && (label == meta.ValueController || label == meta.ValueControllerResources) {
							require.Nil(t, response, "skip must continue the handler chain")
						} else {
							require.NotNil(t, response)
							require.False(t, response.Allowed)
							require.Contains(t, response.Result.Message, "denied by namespace rule")
						}
					})
				}
			}
		}
	}
}

func TestManagedMetadataExemptionRequiresConfiguredIdentity(t *testing.T) {
	for _, missing := range []string{configuration.EnvironmentControllerNamespace, configuration.EnvironmentServiceaccountName} {
		t.Run(missing, func(t *testing.T) {
			t.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")
			t.Setenv(configuration.EnvironmentServiceaccountName, "capsule-controller")
			t.Setenv(missing, "")
			obj := genericMetadataObject(map[string]string{meta.NewManagedByCapsuleLabel: meta.ValueController, "example.org/denied-0": "true"}, nil)
			req := admissionRequest("v1", "Pod")
			req.UserInfo = users.ServiceAccountUserInfo("capsule-system", "capsule-controller")
			response := GenericRules(nil).OnCreate(nil, nil, obj, nil, testEventRecorder{}, testTenant(), managedIdentityRules(1))(t.Context(), req)
			require.NotNil(t, response)
			require.False(t, response.Allowed)
			require.Contains(t, response.Result.Message, "denied by namespace rule")
		})
	}
}

func BenchmarkGenericMetadataAdmission(b *testing.B) {
	b.Setenv(configuration.EnvironmentControllerNamespace, "capsule-system")
	b.Setenv(configuration.EnvironmentServiceaccountName, "capsule-controller")
	for _, count := range []int{1, 32} {
		for _, tc := range []struct {
			name, label, username string
			denied                bool
		}{
			{name: "allow", username: "alice"},
			{name: "deny", username: "alice", denied: true},
			{name: "forged-controller", label: meta.ValueController, username: "alice", denied: true},
			{name: "forged-resources", label: meta.ValueControllerResources, username: "alice", denied: true},
			{name: "controller", label: meta.ValueController, username: users.ServiceAccountUsername("capsule-system", "capsule-controller")},
		} {
			b.Run(fmt.Sprintf("rules=%d/%s", count, tc.name), func(b *testing.B) {
				h := GenericRules(nil)
				bodies := managedIdentityRules(count)
				obj := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "pod", Labels: map[string]string{"example.org/allowed": "true"}}}
				if tc.denied || tc.label != "" {
					obj.Labels["example.org/denied-0"] = "true"
				}
				if tc.label != "" {
					obj.Labels[meta.NewManagedByCapsuleLabel] = tc.label
				}
				req := admissionRequest("v1", "Pod")
				req.UserInfo.Username = tc.username
				handle := h.OnCreate(nil, nil, obj, nil, testEventRecorder{}, testTenant(), bodies)
				check := func() {
					response := handle(b.Context(), req)
					if (response != nil && !response.Allowed) != tc.denied {
						b.Fatal("unexpected admission result")
					}
				}
				check()
				b.ReportAllocs()
				for b.Loop() {
					check()
				}
				b.ReportMetric(0, "API-calls/op")
			})
		}
	}
}
