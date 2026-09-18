// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"testing"

	"k8s.io/utils/ptr"
)

func TestResourceTemplatePolicyDefaults(t *testing.T) {
	t.Parallel()

	policy := ResourceTemplatePolicy{}
	if policy.AllowsAdoption() {
		t.Fatal("zero-value policy allows adoption, want Owner semantics")
	}
	if !policy.IsProtected() {
		t.Fatal("zero-value policy is not protected, want protection enabled")
	}
	if policy.ShouldOrphan() {
		t.Fatal("zero-value policy orphans resources, want Remove semantics")
	}

	policy.Creation = ResourceCreationPolicyMerge
	policy.Protect = ptr.To(false)
	if !policy.AllowsAdoption() {
		t.Fatal("Merge policy does not allow adoption")
	}
	if policy.IsProtected() {
		t.Fatal("protect=false policy is protected")
	}

	policy.Deletion = ResourceDeletionPolicyOrphan
	if !policy.ShouldOrphan() {
		t.Fatal("Orphan policy does not retain resources")
	}
}
