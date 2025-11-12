package utils

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/projectcapsule/capsule/pkg/api"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func CreateOrPatch(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	fieldOwner string,
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

func CreateOrUpdate(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	labels, annotations map[string]string,
	ignore []api.IgnoreRule,
) error {
	actual := &unstructured.Unstructured{}
	actual.SetGroupVersionKind(obj.GroupVersionKind())
	actual.SetNamespace(obj.GetNamespace())
	actual.SetName(obj.GetName())

	// Fetch current to have a stable mutate func input
	_ = c.Get(ctx, client.ObjectKeyFromObject(actual), actual) // ignore notfound here

	// Respect Ignores
	igPaths := matchIgnorePaths(ignore, obj)
	for _, p := range igPaths {
		_ = jsonPointerDelete(obj.Object, p)
	}

	_, err := controllerutil.CreateOrPatch(ctx, c, actual, func() error {
		// Keep copies
		live := actual.DeepCopy() // current from cluster (may be empty)
		desired := obj.DeepCopy() // what we want

		// Preserve ignored JSON pointers: copy live -> desired at those paths
		if len(igPaths) > 0 {
			preserveIgnoredPaths(desired.Object, live.Object, igPaths)
		}

		// Replace actual content with the prepared desired content
		uid := actual.GetUID()
		rv := actual.GetResourceVersion()

		actual.Object = desired.Object
		actual.SetUID(uid)
		actual.SetResourceVersion(rv)

		return nil
	})
	return err
}

// jsonPointerGet returns (value, true) if JSON pointer p exists.
func jsonPointerGet(obj map[string]any, p string) (any, bool) {
	if p == "" || p == "/" {
		return obj, true
	}
	parts := strings.Split(p, "/")[1:]
	cur := any(obj)
	for _, raw := range parts {
		key := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[key]
			if !ok {
				return nil, false
			}
			cur = next
		case []any:
			idx, err := strconv.Atoi(key)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil, false
			}
			cur = node[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

func jsonPointerSet(obj map[string]any, p string, val any) error {
	if p == "" || p == "/" {
		return fmt.Errorf("cannot set root with pointer")
	}
	parts := strings.Split(p, "/")[1:]
	cur := obj
	for i, raw := range parts {
		key := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		last := i == len(parts)-1
		if last {
			cur[key] = val
			return nil
		}
		nxt, ok := cur[key]
		if !ok {
			n := map[string]any{}
			cur[key] = n
			cur = n
			continue
		}
		switch m := nxt.(type) {
		case map[string]any:
			cur = m
		default:
			n := map[string]any{}
			cur[key] = n
			cur = n
		}
	}
	return nil
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

func preserveIgnoredPaths(desired, live map[string]any, ptrs []string) {
	for _, p := range ptrs {
		if v, ok := jsonPointerGet(live, p); ok {
			_ = jsonPointerSet(desired, p, v)
		} else {
			_ = jsonPointerDelete(desired, p)
		}
	}
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
