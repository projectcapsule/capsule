// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

type JSONPatch struct {
	Operation JSONPatchOperation `json:"op"`
	Path      string             `json:"path"`
	Value     any                `json:"value,omitempty"`
}

type JSONPatchOperation string

const (
	JSONPatchAdd     JSONPatchOperation = "add"
	JSONPatchReplace JSONPatchOperation = "replace"
	JSONPatchRemove  JSONPatchOperation = "remove"
)

func (j JSONPatchOperation) String() string {
	return string(j)
}

func JSONPatchesToRawPatch(patches []JSONPatch) (patch []byte, err error) {
	return json.Marshal(patches)
}

func ApplyPatches(
	ctx context.Context,
	c client.Client,
	obj client.Object,
	patches []JSONPatch,
	manager string,
) (err error) {
	if len(patches) == 0 {
		return nil
	}

	rawPatch, err := JSONPatchesToRawPatch(patches)
	if err != nil {
		return err
	}

	return c.Patch(
		ctx,
		obj,
		client.RawPatch(types.JSONPatchType, rawPatch),
		client.FieldOwner(manager),
	)
}

func AddLabelsPatch(labels map[string]string, keys map[string]string) []JSONPatch {
	if len(keys) == 0 {
		return nil
	}

	patches := make([]JSONPatch, 0, len(keys)+1)

	// If labels is nil, /metadata/labels likely doesn't exist.
	// JSONPatch add/replace to /metadata/labels/<k> requires /metadata/labels to exist.
	if labels == nil {
		patches = append(patches, JSONPatch{
			Operation: JSONPatchAdd,
			Path:      "/metadata/labels",
			Value:     map[string]string{},
		})

		labels = map[string]string{} // local view for replace/add decision
	}

	for key, val := range keys {
		op := JSONPatchAdd

		if existing, ok := labels[key]; ok {
			if existing == val {
				continue
			}

			op = JSONPatchReplace
		}

		patches = append(patches, JSONPatch{
			Operation: op,
			Path:      fmt.Sprintf("/metadata/labels/%s", strings.ReplaceAll(key, "/", "~1")),
			Value:     val,
		})
	}

	return patches
}

func AddAnnotationsPatch(annotations map[string]string, keys map[string]string) []JSONPatch {
	if len(keys) == 0 {
		return nil
	}

	patches := make([]JSONPatch, 0, len(keys)+1)

	// If annotations is nil, /metadata/annotations likely doesn't exist.
	// JSONPatch add/replace to /metadata/annotations/<k> requires /metadata/annotations to exist.
	if annotations == nil {
		patches = append(patches, JSONPatch{
			Operation: JSONPatchAdd,
			Path:      "/metadata/annotations",
			Value:     map[string]string{},
		})
		annotations = map[string]string{}
	}

	for key, val := range keys {
		op := JSONPatchAdd

		if existing, ok := annotations[key]; ok {
			if existing == val {
				continue
			}

			op = JSONPatchReplace
		}

		patches = append(patches, JSONPatch{
			Operation: op,
			Path:      fmt.Sprintf("/metadata/annotations/%s", strings.ReplaceAll(key, "/", "~1")),
			Value:     val,
		})
	}

	return patches
}

// PatchRemoveLabels returns a JSONPatch array for removing labels with matching keys.
func PatchRemoveLabels(labels map[string]string, keys []string) []JSONPatch {
	var patches []JSONPatch

	if labels == nil {
		return patches
	}

	for _, key := range keys {
		if _, ok := labels[key]; ok {
			path := fmt.Sprintf("/metadata/labels/%s", strings.ReplaceAll(key, "/", "~1"))
			patches = append(patches, JSONPatch{
				Operation: JSONPatchRemove,
				Path:      path,
			})
		}
	}

	return patches
}

// PatchRemoveAnnotations returns a JSONPatch array for removing annotations with matching keys.
func PatchRemoveAnnotations(annotations map[string]string, keys []string) []JSONPatch {
	var patches []JSONPatch

	if annotations == nil {
		return patches
	}

	for _, key := range keys {
		if _, ok := annotations[key]; ok {
			path := fmt.Sprintf("/metadata/annotations/%s", strings.ReplaceAll(key, "/", "~1"))
			patches = append(patches, JSONPatch{
				Operation: JSONPatchRemove,
				Path:      path,
			})
		}
	}

	return patches
}

func AddOwnerReferencePatch(
	ownerrefs []metav1.OwnerReference,
	ownerreference *metav1.OwnerReference,
) []JSONPatch {
	if ownerreference == nil {
		return nil
	}

	patches := make([]JSONPatch, 0, 2)

	// Ensure parent exists if missing (nil slice usually means field absent)
	if ownerrefs == nil {
		patches = append(patches, JSONPatch{
			Operation: JSONPatchAdd,
			Path:      "/metadata/ownerReferences",
			Value:     []metav1.OwnerReference{},
		})

		patches = append(patches, JSONPatch{
			Operation: JSONPatchAdd,
			Path:      "/metadata/ownerReferences/-",
			Value:     ownerreference,
		})

		return patches
	}

	for i := range ownerrefs {
		if ownerrefs[i].UID != ownerreference.UID {
			continue
		}

		existing := ownerrefs[i]

		if meta.LooseOwnerReferenceEqual(existing, *ownerreference) {
			return nil
		}

		patches = append(patches, JSONPatch{
			Operation: JSONPatchReplace,
			Path:      fmt.Sprintf("/metadata/ownerReferences/%d", i),
			Value:     ownerreference,
		})

		return patches
	}

	// Otherwise append
	patches = append(patches, JSONPatch{
		Operation: JSONPatchAdd,
		Path:      "/metadata/ownerReferences/-",
		Value:     ownerreference,
	})

	return patches
}

func RemoveOwnerReferencePatch(
	ownerRefs []metav1.OwnerReference,
	toRemove *metav1.OwnerReference,
) []JSONPatch {
	if toRemove == nil {
		return nil
	}

	if len(ownerRefs) == 0 {
		return nil
	}

	idx := -1

	for i := range ownerRefs {
		if meta.LooseOwnerReferenceEqual(ownerRefs[i], *toRemove) {
			idx = i

			break
		}
	}

	if idx == -1 {
		return nil
	}

	patches := []JSONPatch{
		{
			Operation: JSONPatchRemove,
			Path:      fmt.Sprintf("/metadata/ownerReferences/%d", idx),
		},
	}

	if len(ownerRefs) == 1 {
		patches = append(patches, JSONPatch{
			Operation: JSONPatchRemove,
			Path:      "/metadata/ownerReferences",
		})
	}

	return patches
}
