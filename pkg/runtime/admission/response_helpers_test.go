// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"errors"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAdmissionResponseHelpers(t *testing.T) {
	t.Parallel()

	denied := Deny("blocked")
	if denied == nil || denied.Allowed {
		t.Fatalf("Deny() = %#v, want denied response", denied)
	}
	if denied.Result == nil || denied.Result.Message != "blocked" {
		t.Fatalf("Deny() message = %#v", denied.Result)
	}

	denied = Denyf("blocked %s", "tenant")
	if denied == nil || denied.Allowed || denied.Result == nil || denied.Result.Message != "blocked tenant" {
		t.Fatalf("Denyf() = %#v", denied)
	}

	allowed := Allow("ok")
	if allowed == nil || !allowed.Allowed {
		t.Fatalf("Allow() = %#v, want allowed response", allowed)
	}
	if allowed.Result == nil || allowed.Result.Message != "ok" {
		t.Fatalf("Allow() message = %#v", allowed.Result)
	}

	allowed = Allowf("ok %s", "tenant")
	if allowed == nil || !allowed.Allowed || allowed.Result == nil || allowed.Result.Message != "ok tenant" {
		t.Fatalf("Allowf() = %#v", allowed)
	}

	errResponse := ErroredResponse(errors.New("boom"))
	if errResponse == nil || errResponse.Allowed || errResponse.Result == nil {
		t.Fatalf("ErroredResponse() = %#v", errResponse)
	}
	if errResponse.Result.Code != 500 || !strings.Contains(errResponse.Result.Message, "boom") {
		t.Fatalf("ErroredResponse() result = %#v", errResponse.Result)
	}
}

func TestPreserveNamespaceSelectors(t *testing.T) {
	annotations := map[string]string{}
	external := metav1.LabelSelectorRequirement{Key: "external", Operator: metav1.LabelSelectorOpExists}
	desired := []*metav1.LabelSelector{{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "owned", Operator: metav1.LabelSelectorOpExists}}}}
	selector := func(s **metav1.LabelSelector) (string, **metav1.LabelSelector) { return "test", s }
	if err := PreserveNamespaceSelectors(annotations, nil, desired, selector); err != nil {
		t.Fatal(err)
	}
	desired[0].MatchExpressions = append(desired[0].MatchExpressions, external)
	updated := []*metav1.LabelSelector{nil}
	if err := PreserveNamespaceSelectors(annotations, desired, updated, selector); err != nil || updated[0] == nil || len(updated[0].MatchExpressions) != 1 || updated[0].MatchExpressions[0].Key != external.Key {
		t.Fatalf("unexpected selector %v: %v", updated[0], err)
	}
}
