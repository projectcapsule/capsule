package utils

import (
	"context"
	"fmt"
	"strings"

	"github.com/projectcapsule/capsule/pkg/api"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CreateOrPatch(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	fieldOwner string,
	ignore []api.IgnoreRule,
	overwrite bool,
) error {
	actual := &unstructured.Unstructured{}
	actual.SetGroupVersionKind(obj.GroupVersionKind())
	actual.SetNamespace(obj.GetNamespace())
	actual.SetName(obj.GetName())

	// Fetch current to have a stable mutate func input
	err := c.Get(ctx, client.ObjectKeyFromObject(actual), actual)
	notFound := apierr.IsNotFound(err)
	if err != nil && !notFound {
		return err
	}

	// Respect Ignores
	igPaths := matchIgnorePaths(ignore, obj)
	for _, p := range igPaths {
		_ = jsonPointerDelete(obj.Object, p)
	}

	if !notFound {
		obj.SetResourceVersion(actual.GetResourceVersion())
	} else {
		obj.SetResourceVersion("") // avoid accidental conflicts
	}

	patchOpts := []client.PatchOption{
		client.FieldOwner(fieldOwner),
	}

	if overwrite {
		patchOpts = append(patchOpts, client.ForceOwnership)
	}

	return c.Patch(ctx, obj, client.Apply, patchOpts...)
}

func jsonPointerDelete(obj map[string]any, p string) error {
	if p == "" || p == "/" {
		return fmt.Errorf("cannot delete root with pointer")
	}
	parts := strings.Split(p, "/")[1:]
	cur := obj
	for i, raw := range parts {
		key := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		last := i == len(parts)-1
		if last {
			delete(cur, key)
			return nil
		}
		nxt, ok := cur[key]
		if !ok {
			return nil
		}
		m, ok := nxt.(map[string]any)
		if !ok {
			return nil
		}
		cur = m
	}
	return nil
}

func matchIgnorePaths(rules []api.IgnoreRule, obj *unstructured.Unstructured) []string {
	var out []string
	for _, r := range rules {
		if !r.Matches(obj) {
			continue
		}

		out = append(out, r.Paths...)
	}

	return out
}
