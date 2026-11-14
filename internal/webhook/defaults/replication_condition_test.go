// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package defaults

import (
	"fmt"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	registrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestReplicationConditionsAdmission(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	decoder := admission.NewDecoder(scheme)
	conditions, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(nil, nil, conditions)
	for _, kind := range []string{"TenantResource", "GlobalTenantResource"} {
		for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
			for _, tc := range []struct {
				expression string
				allow      bool
			}{
				{"", true},
				{"false", true},
				{`object == null || now > timestamp(object.metadata.creationTimestamp) + duration('5m')`, true},
				{"object..broken", false},
				{"42", false},
				{" ", false},
				{"undeclared == true", false},
			} {
				spec := capsulev1beta2.TenantResourceCommonSpec{Resources: []capsulev1beta2.ResourceSpec{{Policy: &apiruntime.ResourceReplicationPolicy{Condition: tc.expression}}}}
				req := replicationPolicyRequest(t, kind, spec)
				req.Operation = operation
				run := h.OnCreate(nil, nil, decoder, nil)
				if operation == admissionv1.Update {
					run = h.OnUpdate(nil, nil, decoder, nil)
				}
				response := run(t.Context(), req)
				if response == nil || response.Allowed != tc.allow {
					t.Fatalf("%s %s %q: %#v", kind, operation, tc.expression, response)
				}
				if len(response.Patches) > 0 {
					t.Fatal("condition validation altered explicit policy")
				}
			}
		}
	}
	if conditions.Stats() != 2 {
		t.Fatalf("condition cache has %d entries, want two shared programs", conditions.Stats())
	}
}

func BenchmarkReplicationWatchConfigurationConversion(b *testing.B) {
	for _, count := range []int{1, 32} {
		b.Run(fmt.Sprintf("triggers=%d", count), func(b *testing.B) {
			scheme := runtime.NewScheme()
			if err := capsulev1beta2.AddToScheme(scheme); err != nil {
				b.Fatal(err)
			}
			compiler, _ := cache.NewCELCache()
			decoder := admission.NewDecoder(scheme)
			spec := capsulev1beta2.TenantResourceCommonSpec{Resources: []capsulev1beta2.ResourceSpec{{Policy: &apiruntime.ResourceReplicationPolicy{}}}}
			for i := range count {
				spec.Triggers = append(spec.Triggers, capsulev1beta2.TenantResourceTrigger{Name: fmt.Sprintf("source-%d", i), Operations: []registrationv1.OperationType{registrationv1.Update}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}, MatchConditions: []registrationv1.MatchCondition{{Name: "state", Expression: "object != null"}}})
			}
			req := replicationPolicyRequest(b, "TenantResource", spec)
			run := func() {
				if response := mutateReplicationPolicy(req, decoder, compiler); response != nil && !response.Allowed {
					b.Fatal(response)
				}
			}
			run()
			b.ReportAllocs()
			for b.Loop() {
				run()
			}
		})
	}
}

func TestReplicationPolicyConversionDoesNotValidateWatchTriggers(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	decoder := admission.NewDecoder(scheme)
	for _, kind := range []string{"TenantResource", "GlobalTenantResource"} {
		spec := capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{{Name: "events", Operations: []registrationv1.OperationType{registrationv1.Update}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}, MatchConditions: []registrationv1.MatchCondition{{Name: "invalid", Expression: "request.operation == 'UPDATE'"}}}}, Resources: []capsulev1beta2.ResourceSpec{{}}}
		response := mutateReplicationPolicy(replicationPolicyRequest(t, kind, spec), decoder, nil)
		if response == nil || !response.Allowed || len(response.Patches) != 1 {
			t.Fatalf("%s: trigger configuration affected policy conversion: %#v", kind, response)
		}
	}
}

func BenchmarkReplicationConditionAdmission(b *testing.B) {
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		b.Fatal(err)
	}
	decoder := admission.NewDecoder(scheme)
	conditions, err := cache.NewCELCache()
	if err != nil {
		b.Fatal(err)
	}
	h := Handler(nil, nil, conditions).OnCreate(nil, nil, decoder, nil)
	spec := capsulev1beta2.TenantResourceCommonSpec{Resources: []capsulev1beta2.ResourceSpec{{Policy: &apiruntime.ResourceReplicationPolicy{Condition: `object == null || now > timestamp(object.metadata.creationTimestamp) + duration('5m')`}}}}
	req := replicationPolicyRequest(b, "GlobalTenantResource", spec)
	if response := h(b.Context(), req); response == nil || !response.Allowed {
		b.Fatal("condition rejected")
	}
	b.ReportAllocs()
	for b.Loop() {
		if response := h(b.Context(), req); response == nil || !response.Allowed {
			b.Fatal("condition rejected")
		}
	}
}
