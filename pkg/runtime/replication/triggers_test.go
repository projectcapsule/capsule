// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package replication_test

import (
	"reflect"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

func validTrigger() capsulev1beta2.TenantResourceTrigger {
	return capsulev1beta2.TenantResourceTrigger{Name: "secrets", Operations: []admissionv1.OperationType{admissionv1.Create, admissionv1.Update, admissionv1.Delete}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"secrets"}}
}

func TestNativeTriggerRegistration(t *testing.T) {
	compiler, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	trigger := validTrigger()
	trigger.ObjectSelector = &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "selected", Operator: metav1.LabelSelectorOpIn, Values: []string{"yes"}}}}
	trigger.MatchConditions = []admissionv1.MatchCondition{{Name: "changed", Expression: "request.operation != 'UPDATE' || object.data != oldObject.data"}, {Name: "authorized", Expression: "authorizer.requestResource.check('update').allowed()"}}
	original := trigger.DeepCopy()
	url := "https://capsule.example.com"
	hook := replication.Webhook(&trigger, "tenant-a", admissionv1.WebhookClientConfig{URL: &url}, testDeliveryKey(t))
	if !reflect.DeepEqual(original, &trigger) {
		t.Fatal("registration mutated API input")
	}
	if len(validation.IsDNS1123Subdomain(hook.Name)) != 0 || *hook.FailurePolicy != admissionv1.Ignore || *hook.SideEffects != admissionv1.SideEffectClassNoneOnDryRun || *hook.Rules[0].Scope != admissionv1.NamespacedScope {
		t.Fatalf("invalid registration: %+v", hook)
	}
	for _, condition := range hook.MatchConditions {
		if err := compiler.ValidateTriggerCondition(condition.Expression); err != nil {
			t.Fatalf("condition %s: %v", condition.Name, err)
		}
	}
	trigger.Audience = []rules.Audience{{Kind: rules.AudienceKindUser, Name: "alice"}}
	if replication.MatchID(&trigger) != replication.MatchID(original) {
		t.Fatal("audience should be evaluated after shared native predicate")
	}
	trigger.ObjectSelector.MatchExpressions[0].Values[0] = "changed"
	if replication.MatchID(&trigger) == replication.MatchID(original) {
		t.Fatal("selector update reused an obsolete group")
	}
	trigger.Operations = []admissionv1.OperationType{admissionv1.OperationAll}
	wildcard := replication.Webhook(&trigger, "", admissionv1.WebhookClientConfig{URL: &url}, testDeliveryKey(t))
	if !reflect.DeepEqual(wildcard.Rules[0].Operations, []admissionv1.OperationType{admissionv1.Create, admissionv1.Update, admissionv1.Delete}) {
		t.Fatal("wildcard should observe only persistent write operations")
	}
}

func TestTriggerValidation(t *testing.T) {
	compiler, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		edit       func(*capsulev1beta2.TenantResourceCommonSpec)
		namespaced bool
		valid      bool
	}{
		{name: "valid", valid: true},
		{name: "empty backwards compatible", edit: func(s *capsulev1beta2.TenantResourceCommonSpec) { s.Triggers = nil }, valid: true},
		{name: "invalid CEL", edit: func(s *capsulev1beta2.TenantResourceCommonSpec) {
			s.Triggers[0].MatchConditions = []admissionv1.MatchCondition{{Name: "bad", Expression: "object["}}
		}},
		{name: "non boolean CEL", edit: func(s *capsulev1beta2.TenantResourceCommonSpec) {
			s.Triggers[0].MatchConditions = []admissionv1.MatchCondition{{Name: "bad", Expression: "42"}}
		}},
		{name: "unsupported audience", edit: func(s *capsulev1beta2.TenantResourceCommonSpec) {
			s.Triggers[0].Audience = []rules.Audience{{Kind: rules.AudienceKindCustom, Name: "Other"}}
		}},
		{name: "local namespace selector", namespaced: true, edit: func(s *capsulev1beta2.TenantResourceCommonSpec) {
			s.Triggers[0].NamespaceSelector = &metav1.LabelSelector{}
		}},
		{name: "global namespace selector", valid: true, edit: func(s *capsulev1beta2.TenantResourceCommonSpec) {
			s.Triggers[0].NamespaceSelector = &metav1.LabelSelector{}
		}},
		{name: "cluster scope", edit: func(s *capsulev1beta2.TenantResourceCommonSpec) {
			s.Triggers[0].Scope = ptr.To(admissionv1.ClusterScope)
		}},
		{name: "missing operations", edit: func(s *capsulev1beta2.TenantResourceCommonSpec) { s.Triggers[0].Operations = nil }},
		{name: "connect", edit: func(s *capsulev1beta2.TenantResourceCommonSpec) {
			s.Triggers[0].Operations = []admissionv1.OperationType{admissionv1.Connect}
		}},
		{name: "overlapping resources", edit: func(s *capsulev1beta2.TenantResourceCommonSpec) {
			s.Triggers[0].Resources = []string{"*", "secrets"}
		}},
		{name: "overlapping subresources", edit: func(s *capsulev1beta2.TenantResourceCommonSpec) {
			s.Triggers[0].Resources = []string{"pods/*", "pods/status"}
		}},
		{name: "overlapping wildcard resources", edit: func(s *capsulev1beta2.TenantResourceCommonSpec) {
			s.Triggers[0].Resources = []string{"*/status", "pods/status"}
		}},
		{name: "wildcard and specific subresource", valid: true, edit: func(s *capsulev1beta2.TenantResourceCommonSpec) {
			s.Triggers[0].Resources = []string{"*", "pods/status"}
		}},
		{name: "nonoverlapping subresources", valid: true, edit: func(s *capsulev1beta2.TenantResourceCommonSpec) {
			s.Triggers[0].Resources = []string{"pods/*", "secrets/status"}
		}},
		{name: "duplicate names", edit: func(s *capsulev1beta2.TenantResourceCommonSpec) {
			s.Triggers = append(s.Triggers, *s.Triggers[0].DeepCopy())
		}},
		{name: "negative interval", edit: func(s *capsulev1beta2.TenantResourceCommonSpec) { s.TriggerMinInterval.Duration = -time.Second }},
		{name: "small interval", edit: func(s *capsulev1beta2.TenantResourceCommonSpec) { s.TriggerMinInterval.Duration = time.Millisecond }},
		{name: "reserved condition", edit: func(s *capsulev1beta2.TenantResourceCommonSpec) {
			s.Triggers[0].MatchConditions = []admissionv1.MatchCondition{{Name: "capsule-trigger-override", Expression: "true"}}
		}},
		{name: "bad selector", edit: func(s *capsulev1beta2.TenantResourceCommonSpec) {
			s.Triggers[0].ObjectSelector = &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "x", Operator: "Invalid"}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{validTrigger()}}
			if tc.edit != nil {
				tc.edit(&s)
			}
			err := replication.ValidateTriggers(&s, tc.namespaced, compiler)
			if (err == nil) != tc.valid {
				t.Fatalf("validation error=%v valid=%v", err, tc.valid)
			}
		})
	}
}

func TestManagedTriggerEligibility(t *testing.T) {
	if replication.Managed(nil) || replication.Managed(&metav1.PartialObjectMetadata{Labels: map[string]string{meta.TenantLabel: "tenant-a", "app.kubernetes.io/managed-by": "helm"}}) {
		t.Fatal("ordinary tenant resources were excluded")
	}
	for _, name := range []string{"controller", "resources", "replications"} {
		if replication.Managed(&metav1.PartialObjectMetadata{Labels: map[string]string{meta.ManagedByCapsuleLabel: name, meta.NewTenantLabel: name}}) {
			t.Fatalf("tenant %s was treated as a management marker", name)
		}
	}

	for _, key := range []string{meta.NewManagedByCapsuleLabel, meta.CreatedByCapsuleLabel, meta.ProtectedByCapsuleLabel} {
		for _, value := range []string{meta.ValueController, meta.ValueControllerResources, meta.ValueControllerReplications} {
			if !replication.Managed(&metav1.PartialObjectMetadata{Labels: map[string]string{key: value}}) {
				t.Fatalf("missing %s=%s", key, value)
			}
		}
	}
	for _, key := range []string{meta.ResourcesLabel, meta.ReplicationProtectionLabel, meta.RuleQuotaLabel, meta.LimitRangeLabel, meta.NetworkPolicyLabel, meta.ResourceQuotaLabel, meta.RolebindingLabel} {
		if !replication.Managed(&metav1.PartialObjectMetadata{Labels: map[string]string{key: ""}}) {
			t.Fatalf("missing legacy marker %s", key)
		}
	}
}

func BenchmarkTriggerPredicate(b *testing.B) {
	trigger := validTrigger()
	trigger.ObjectSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"selected": "true"}}
	b.ReportAllocs()
	for b.Loop() {
		if replication.MatchID(&trigger) == "" {
			b.Fatal("empty predicate")
		}
	}
}

func testDeliveryKey(tb testing.TB) *replication.DeliveryKey {
	tb.Helper()
	key, err := replication.NewDeliveryKey(make([]byte, 32))
	if err != nil {
		tb.Fatal(err)
	}
	return key
}
