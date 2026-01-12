// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"

	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// Returns timestamp of last apply for a manager
func LastApplyTimeForManager(obj *unstructured.Unstructured, manager string) *metav1.Time {
	var latest *metav1.Time

	for i := range obj.GetManagedFields() {
		mf := obj.GetManagedFields()[i]

		if mf.Manager != manager {
			continue
		}
		if mf.Operation != metav1.ManagedFieldsOperationApply {
			continue
		}
		if mf.Time == nil {
			continue
		}

		if latest == nil || mf.Time.After(latest.Time) {
			t := *mf.Time
			latest = &t
		}
	}

	return latest
}

// CreateOrUpdate CreateOrUpdate Implementation with optional IgnoreRules
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

	// Fetch current to have a stable mutate func input
	_ = c.Get(ctx, client.ObjectKeyFromObject(actual), actual) // ignore notfound here

	// Respect Ignores
	igPaths := MatchIgnorePaths(ignore, obj)
	for _, p := range igPaths {
		_ = JsonPointerDelete(obj.Object, p)
	}

	_, err := controllerutil.CreateOrPatch(ctx, c, actual, func() error {
		// Keep copies
		live := actual.DeepCopy() // current from cluster (may be empty)
		desired := obj.DeepCopy() // what we want

		// Preserve ignored JSON pointers: copy live -> desired at those paths
		if len(igPaths) > 0 {
			PreserveIgnoredPaths(desired.Object, live.Object, igPaths)
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
