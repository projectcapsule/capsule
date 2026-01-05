package client

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/projectcapsule/capsule/pkg/api/misc"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func PatchMetadataLabels(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	labels map[string]string,
) error {
	patch := map[string]any{
		"metadata": map[string]any{
			"labels": labels,
		},
	}

	b, err := json.Marshal(patch)
	if err != nil {
		return err
	}

	// Patch against the live object by name/namespace/gvk
	target := &unstructured.Unstructured{}
	target.SetGroupVersionKind(obj.GroupVersionKind())
	target.SetNamespace(obj.GetNamespace())
	target.SetName(obj.GetName())

	return c.Patch(ctx, target, client.RawPatch(types.MergePatchType, b))
}