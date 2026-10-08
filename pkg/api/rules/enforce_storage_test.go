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
