// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"errors"
	"strings"
	"testing"
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
