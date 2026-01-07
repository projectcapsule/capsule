// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	CapsuleFieldOwnerPrefix = "projectcapsule.dev"
)

func ControllerFieldOwner() string {
	return ControllerFieldOwnerPrefix("controller")
}

func ControllerFieldOwnerPrefix(fieldowner string) string {
	return CapsuleFieldOwnerPrefix + "/" + fieldowner
}

// CapsuleFieldOwners returns the set of managers that start with the Capsule prefix.
func CapsuleFieldOwners(obj *unstructured.Unstructured) map[string]struct{} {
	out := map[string]struct{}{}
	if obj == nil {
		return out
	}

	prefix := CapsuleFieldOwnerPrefix + "/"
	for _, mf := range obj.GetManagedFields() {
		mgr := mf.Manager
		if mgr == "" {
			continue
		}
		if strings.HasPrefix(mgr, prefix) {
			out[mgr] = struct{}{}
		}
	}
	return out
}

func HasExactlyCapsuleOwners(obj *unstructured.Unstructured, allowed []string) bool {
	owners := CapsuleFieldOwners(obj)

	if len(owners) != len(allowed) {
		return false
	}

	for _, a := range allowed {
		if _, ok := owners[a]; !ok {
			return false
		}
	}

	return true
}
