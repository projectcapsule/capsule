package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type JSONPatch struct {
	Operation string `json:"op"`
	Path      string `json:"path"`
	Value     any    `json:"value,omitempty"`
}

func ApplyPatches(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	patches []JSONPatch,
	manager string,
) (err error) {
	if len(patches) == 0 {
		return nil
	}

	existingObject := obj.DeepCopy()

	if len(patches) == 0 {
		return nil
	}

	rawPatch, err := json.Marshal(patches)
	if err != nil {
		return err
	}

	return c.Patch(
		ctx,
		existingObject,
		client.RawPatch(types.JSONPatchType, rawPatch),
		client.FieldOwner(manager),
	)
}

func AddLabelsPatch(object *unstructured.Unstructured, keys map[string]string) []JSONPatch {
	var patches []JSONPatch
	labels := object.GetLabels()
	for key, val := range keys {
		operation := "add"

		if v, ok := labels[key]; ok {
			if v == val {
				continue
			}

			operation = "replace"
		}

		patches = append(patches, JSONPatch{
			Operation: operation,
			Path:      fmt.Sprintf("/metadata/labels/%s", strings.ReplaceAll(key, "/", "~1")),
			Value:     val,
		})

	}
	return patches
}

// PatchRemoveLabels returns a JSONPatch array for removing labels with matching keys.
func PatchRemoveLabels(object *unstructured.Unstructured, keys []string) []JSONPatch {
	var patches []JSONPatch
	labels := object.GetLabels()
	for _, key := range keys {
		if _, ok := labels[key]; ok {
			path := fmt.Sprintf("/metadata/labels/%s", strings.ReplaceAll(key, "/", "~1"))
			patches = append(patches, JSONPatch{
				Operation: "remove",
				Path:      path,
			})
		}
	}
	return patches
}
