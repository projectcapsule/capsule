// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

import (
	"hash/fnv"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	FieldManagerCapsulePrefix     = "projectcapsule.dev"
	FieldManagerCapsuleController = "projectcapsule.dev/controller"
)

func ControllerFieldOwnerPrefix(fieldowner string) string {
	return FieldManagerCapsulePrefix + "/" + fieldowner
}

func ControllerFieldOwner() string {
	return ControllerFieldOwnerPrefix("controller")
}

func ResourceControllerFieldOwnerPrefix() string {
	return ResourceFieldOwner("controller")
}

// ResourceFieldOwner returns a Capsule field manager for an applied resource.
func ResourceFieldOwner(fieldowner string) string {
	return FieldManagerCapsulePrefix + "/resource/" + fieldowner
}

// BreakRequestFieldOwner returns a stable field manager for a BreakRequest.
func BreakRequestFieldOwner(obj metav1.Object) string {
	identity := string(obj.GetUID())
	if identity == "" {
		hash := fnv.New64a()
		_, _ = hash.Write([]byte(obj.GetNamespace()))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(obj.GetName()))
		identity = strconv.FormatUint(hash.Sum64(), 36)
	}

	return ResourceFieldOwner("breakrequest/" + identity)
}

// CapsuleFieldOwners returns the set of managers that start with the Capsule prefix.
func CapsuleFieldOwners(obj *unstructured.Unstructured, prefix string) map[string]struct{} {
	out := map[string]struct{}{}
	if obj == nil {
		return out
	}

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

func HasExactlyCapsuleOwners(obj *unstructured.Unstructured, prefix string, allowed []string) bool {
	owners := CapsuleFieldOwners(obj, prefix)

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
