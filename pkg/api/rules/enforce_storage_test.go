// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestStorageRuleRoundTripAndIsolation(t *testing.T) {
	original := &NamespaceRuleBodyNamespace{Enforce: &NamespaceRuleEnforceBody{Action: ActionTypeAllow, Conditions: []AdmissionCondition{{Expression: "volume != null"}}, Storage: NamespaceRuleEnforceStorageBody{Volumes: []PersistentVolumeMatch{{Name: "restore", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "restore"}, MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "zone", Operator: metav1.LabelSelectorOpIn, Values: []string{"a"}}}}}}}}}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded NamespaceRuleBodyNamespace
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Enforce.Storage.Volumes[0].Selector.MatchLabels["pool"] != "restore" {
		t.Fatal("storage policy lost during round trip")
	}
	cloned := original.DeepCopy()
	cloned.Enforce.Storage.Volumes[0].Selector.MatchLabels["pool"] = "other"
	cloned.Enforce.Storage.Volumes[0].Selector.MatchExpressions[0].Values[0] = "b"
	if original.Enforce.Storage.Volumes[0].Selector.MatchLabels["pool"] != "restore" || original.Enforce.Storage.Volumes[0].Selector.MatchExpressions[0].Values[0] != "a" {
		t.Fatal("deep copy shares selector state")
	}
	var legacy NamespaceRuleBodyNamespace
	if err := json.Unmarshal([]byte(`{"enforce":{"action":"deny","services":{"types":["NodePort"]}}}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if len(legacy.Enforce.Storage.Volumes) != 0 {
		t.Fatal("old rules gained volume permissions")
	}
}

func TestStorageClassTargets(t *testing.T) {
	classes := []StorageClassMatch{{Exact: []string{"gold"}}}

	for _, tc := range []struct {
		name    string
		storage NamespaceRuleEnforceStorageBody
		want    bool
	}{
		{name: "no classes", storage: NamespaceRuleEnforceStorageBody{Targets: []StorageValidationTarget{StorageTargetVolumeSnapshot}}},
		{name: "omitted targets select every kind", storage: NamespaceRuleEnforceStorageBody{Classes: classes}, want: true},
		{name: "listed target", storage: NamespaceRuleEnforceStorageBody{Targets: []StorageValidationTarget{StorageTargetVolumeSnapshot}, Classes: classes}, want: true},
		{name: "other target", storage: NamespaceRuleEnforceStorageBody{Targets: []StorageValidationTarget{"persistentvolumeclaim/storageclass"}, Classes: classes}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (NamespaceRuleEnforceBody{Storage: tc.storage}).GetStorageClassTargets(StorageTargetVolumeSnapshot); got != tc.want {
				t.Fatalf("GetStorageClassTargets()=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestStorageClassMatchHelpers(t *testing.T) {
	match := StorageClassMatch{Exact: []string{"gold", "silver"}, Expression: "^csi-", MatchLabels: map[string]string{"env": "customer"}}
	if !match.HasNameMatch() || !match.HasSelector() {
		t.Fatal("match lost a matcher")
	}
	if got, want := match.Describe(), "exact: gold, silver; exp: ^csi-; selector: env=customer"; got != want {
		t.Fatalf("Describe()=%q, want %q", got, want)
	}
	if name := match.NameMatch(); name.Expression != "^csi-" || name.Negate || len(name.Exact) != 2 {
		t.Fatalf("NameMatch()=%+v", name)
	}
	if (StorageClassMatch{MatchLabels: map[string]string{}}).HasSelector() {
		t.Fatal("an empty selector must not read class labels")
	}

	cloned := match.DeepCopy()
	cloned.Exact[0] = "bronze"
	cloned.MatchLabels["env"] = "internal"
	if match.Exact[0] != "gold" || match.MatchLabels["env"] != "customer" {
		t.Fatal("deep copy shares class match state")
	}

	var decoded StorageClassMatch
	if err := json.Unmarshal([]byte(`{"exact":["internal.example.com"],"exp":"^[a-z0-9-]+\\.example\\.com$","matchLabels":{"env":"customer"}}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Exact[0] != "internal.example.com" || decoded.Expression != `^[a-z0-9-]+\.example\.com$` || decoded.MatchLabels["env"] != "customer" {
		t.Fatalf("issue notation decoded as %+v", decoded)
	}
}

func TestStorageAttributeClasses(t *testing.T) {
	var storage NamespaceRuleEnforceStorageBody
	if err := json.Unmarshal([]byte(`{"attributeClasses":[{"exact":["internal.example.com"],"exp":"^[a-z0-9-]+\\.example\\.com$","matchLabels":{"env":"customer"}}]}`), &storage); err != nil {
		t.Fatal(err)
	}
	if len(storage.AttributeClasses) != 1 || len(storage.Classes) != 0 {
		t.Fatalf("issue notation decoded as %+v", storage)
	}
	if (NamespaceRuleEnforceBody{Storage: storage}).GetStorageClassTargets(StorageTargetVolumeSnapshot) {
		t.Fatal("attribute classes must not select snapshot classes")
	}

	cloned := storage.DeepCopy()
	cloned.AttributeClasses[0].Exact[0] = "external.example.com"
	cloned.AttributeClasses[0].MatchLabels["env"] = "internal"
	if storage.AttributeClasses[0].Exact[0] != "internal.example.com" || storage.AttributeClasses[0].MatchLabels["env"] != "customer" {
		t.Fatal("deep copy shares attribute class state")
	}

	data, err := json.Marshal(NamespaceRuleEnforceStorageBody{Classes: []StorageClassMatch{{Exact: []string{"gold"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"classes":[{"exact":["gold"]}]}` {
		t.Fatalf("rules without attribute classes encode as %s", data)
	}
}

func TestStorageClassesToMatch(t *testing.T) {
	silver := StorageClassMatch{Exact: []string{"silver"}}
	csi := StorageClassMatch{Expression: "^csi-"}
	customer := StorageClassMatch{MatchLabels: map[string]string{"env": "customer"}}
	invalid := StorageClassMatch{Expression: "("}

	for _, tc := range []struct {
		name    string
		action  ActionType
		classes []StorageClassMatch
		class   string
		want    []StorageClassMatch
	}{
		{name: "exact match decides an allow rule", action: ActionTypeAllow, classes: []StorageClassMatch{customer, silver, csi}, class: "silver", want: []StorageClassMatch{silver}},
		{name: "expression match decides a deny rule", action: ActionTypeDeny, classes: []StorageClassMatch{customer, csi}, class: "csi-fast", want: []StorageClassMatch{csi}},
		// An omitted action defaults to deny, so it stops at the name match like
		// allow and deny do, instead of keeping every entry like audit.
		{name: "omitted action is not audit", classes: []StorageClassMatch{customer, silver}, class: "silver", want: []StorageClassMatch{silver}},
		{name: "no name match keeps every entry", action: ActionTypeAllow, classes: []StorageClassMatch{silver, customer}, class: "gold", want: []StorageClassMatch{silver, customer}},
		{name: "audit keeps every entry", action: ActionTypeAudit, classes: []StorageClassMatch{customer, silver}, class: "silver", want: []StorageClassMatch{customer, silver}},
		{name: "invalid expression keeps every entry", action: ActionTypeAllow, classes: []StorageClassMatch{invalid, silver}, class: "silver", want: []StorageClassMatch{invalid, silver}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := StorageClassesToMatch(tc.action, tc.classes, nil, tc.class)
			if len(got) != len(tc.want) {
				t.Fatalf("StorageClassesToMatch()=%v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i].Describe() != tc.want[i].Describe() {
					t.Fatalf("StorageClassesToMatch()=%v, want %v", got, tc.want)
				}
			}
		})
	}
}
