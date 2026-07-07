// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission_test

import (
	"reflect"
	"testing"

	capsuleadmission "github.com/projectcapsule/capsule/pkg/runtime/admission"
	jsonpatch "gomodules.xyz/jsonpatch/v2"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestAccumulateAdmissionResponse(t *testing.T) {
	t.Parallel()

	seed := []jsonpatch.JsonPatchOperation{
		jsonpatch.NewOperation("add", "/metadata/labels/existing", "true"),
	}

	got, response := capsuleadmission.AccumulateAdmissionResponse(seed, nil)
	if response != nil {
		t.Fatalf("nil response returned admission response %#v", response)
	}
	if !reflect.DeepEqual(got, seed) {
		t.Fatalf("nil response patches = %#v, want %#v", got, seed)
	}

	denied := admission.Denied("stop")
	got, response = capsuleadmission.AccumulateAdmissionResponse(seed, &denied)
	if response != &denied {
		t.Fatalf("denied response = %#v, want original response", response)
	}
	if !reflect.DeepEqual(got, seed) {
		t.Fatalf("denied patches = %#v, want %#v", got, seed)
	}

	patches := []jsonpatch.JsonPatchOperation{
		jsonpatch.NewOperation("add", "/metadata/labels/new", "value"),
		jsonpatch.NewOperation("replace", "/metadata/name", "renamed"),
	}
	allowed := admission.Patched("mutated", patches...)
	got, response = capsuleadmission.AccumulateAdmissionResponse(seed, &allowed)
	if response != nil {
		t.Fatalf("allowed response returned admission response %#v", response)
	}

	want := append(append([]jsonpatch.JsonPatchOperation{}, seed...), patches...)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed patches = %#v, want %#v", got, want)
	}
}
