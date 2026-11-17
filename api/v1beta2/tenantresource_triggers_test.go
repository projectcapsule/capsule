// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
)

func TestReplicationTriggerDefaultsPreserveOldRepresentations(t *testing.T) {
	for _, obj := range []any{&TenantResource{}, &GlobalTenantResource{}} {
		encoded, err := json.Marshal(obj)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "triggerMinInterval") {
			t.Fatal("zero-value typed objects must omit interval so API defaulting applies")
		}
	}
}

func TestReplicationTriggerPreservesAdmissionShapedJSON(t *testing.T) {
	// Existing trigger manifests use these admission-shaped fields. Changing their
	// Go documentation types must preserve stored values, including omitted scope.
	for _, scope := range []string{"", `,"scope":"Namespaced"`} {
		raw := `{"name":"changes","operations":["*"],"apiGroups":[""],"apiVersions":["v1"],"resources":["configmaps"],"objectSelector":{"matchLabels":{"source":"yes"}},"matchConditions":[{"name":"example.com/changed","expression":"operation != 'UPDATE' || object.data != oldObject.data"}]` + scope + `}`
		var trigger TenantResourceTrigger
		if err := json.Unmarshal([]byte(raw), &trigger); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(trigger)
		if err != nil {
			t.Fatal(err)
		}
		var want, got any
		if err := json.Unmarshal([]byte(raw), &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(encoded, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("trigger fields changed on round trip: %s", encoded)
		}
		copy := trigger.DeepCopy()
		copy.Operations[0] = admissionv1.Delete
		copy.APIGroups[0], copy.APIVersions[0], copy.Resources[0] = "apps", "v2", "deployments"
		copy.MatchConditions[0].Expression = "false"
		copy.ObjectSelector.MatchLabels["source"] = "no"
		if copy.Scope != nil {
			*copy.Scope = admissionv1.ClusterScope
		}
		if trigger.Operations[0] != admissionv1.OperationAll || trigger.APIGroups[0] != "" || trigger.APIVersions[0] != "v1" || trigger.Resources[0] != "configmaps" || trigger.MatchConditions[0].Expression == "false" || trigger.ObjectSelector.MatchLabels["source"] != "yes" || (trigger.Scope != nil && *trigger.Scope != admissionv1.NamespacedScope) {
			t.Fatal("deep copy changed the source trigger")
		}
	}
}
