// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// CreateOrUpdate Implementation with optional IgnoreRules.
func CreateOrUpdate(
	ctx context.Context,
	c client.Client,
	obj *unstructured.Unstructured,
	labels, annotations map[string]string,
	ignore []IgnoreRule,
) error {
	actual := &unstructured.Unstructured{}
	actual.SetGroupVersionKind(obj.GroupVersionKind())
	actual.SetNamespace(obj.GetNamespace())
	actual.SetName(obj.GetName())

	_ = c.Get(ctx, client.ObjectKeyFromObject(actual), actual) // ignore notfound here

	igPaths := MatchIgnorePaths(ignore, obj)
	for _, p := range igPaths {
		_ = JsonPointerDelete(obj.Object, p)
	}

	_, err := controllerutil.CreateOrPatch(ctx, c, actual, func() error {
		live := actual.DeepCopy()
		desired := obj.DeepCopy()

		if len(igPaths) > 0 {
			PreserveIgnoredPaths(desired.Object, live.Object, igPaths)
		}

		uid := actual.GetUID()
		rv := actual.GetResourceVersion()

		actual.Object = desired.Object
		actual.SetUID(uid)
		actual.SetResourceVersion(rv)

		return nil
	})

	return err
}
