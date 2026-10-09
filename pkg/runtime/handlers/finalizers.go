// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"bytes"
	"reflect"
	"slices"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/users"
)

// Completing Capsule cleanup must not require the former Tenant to still exist.
// Only the configured controller may remove existing non-Capsule finalizers from
// an already deleting object. All resource data and other metadata stay unchanged,
// including ownership and resourceVersion. Returning nil continues later guards.
func isControllerFinalizerRemoval(req admission.Request) bool {
	if req.Operation != admissionv1.Update || req.SubResource != "" ||
		!strings.HasPrefix(req.UserInfo.Username, serviceaccount.ServiceAccountUsernamePrefix) ||
		!users.IsControllerServiceAccount(req.UserInfo.Username) ||
		(req.Kind.Group == "" && req.Kind.Kind == "Pod") ||
		!bytes.Contains(req.OldObject.Raw, []byte(`"deletionTimestamp"`)) {
		return false
	}

	// API admission snapshots encode metadata keys literally. The inexpensive
	// prefilter avoids decoding ordinary controller writes; the full comparison
	// below remains authoritative if that key occurs elsewhere in the payload.

	old, obj := &unstructured.Unstructured{}, &unstructured.Unstructured{}
	if old.UnmarshalJSON(req.OldObject.Raw) != nil || obj.UnmarshalJSON(req.Object.Raw) != nil {
		return false
	}

	if old.GetUID() == "" || old.GetUID() != obj.GetUID() || old.GetDeletionTimestamp() == nil ||
		old.GetNamespace() != req.Namespace || old.GetName() != req.Name {
		return false
	}

	previous, _, err := unstructured.NestedStringSlice(old.Object, "metadata", "finalizers")
	if err != nil {
		return false
	}

	remaining, _, err := unstructured.NestedStringSlice(obj.Object, "metadata", "finalizers")
	if err != nil || len(remaining) >= len(previous) {
		return false
	}

	// Require an ordered subset, retaining Capsule's own lifecycle protection.
	for _, protected := range []string{meta.ControllerFinalizer, meta.LegacyResourceFinalizer} {
		if slices.Contains(previous, protected) && !slices.Contains(remaining, protected) {
			return false
		}
	}

	next := 0

	for _, finalizer := range previous {
		if next < len(remaining) && finalizer == remaining[next] {
			next++
		}
	}

	if next != len(remaining) {
		return false
	}

	oldMetadata, oldOK := old.Object["metadata"].(map[string]any)
	newMetadata, newOK := obj.Object["metadata"].(map[string]any)

	if !oldOK || !newOK {
		return false
	}

	delete(oldMetadata, "finalizers")
	delete(newMetadata, "finalizers")
	// The API server maintains field ownership while applying the cleanup patch.
	delete(oldMetadata, "managedFields")
	delete(newMetadata, "managedFields")

	return reflect.DeepEqual(old.Object, obj.Object)
}
