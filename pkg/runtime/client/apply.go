// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"fmt"

	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CreateOrPatch(
	ctx context.Context,
	c client.Client,
	obj client.Object,
	fieldOwner string,
	overwrite bool,
) error {
	gvks, _, err := c.Scheme().ObjectKinds(obj)
	if err != nil {
		return err
	}

	if len(gvks) == 0 {
		return fmt.Errorf("no GVK found for object %T", obj)
	}

	obj.GetObjectKind().SetGroupVersionKind(gvks[0])

	//nolint:forcetypeassert
	actual := obj.DeepCopyObject().(client.Object)

	key := client.ObjectKeyFromObject(obj)

	err = c.Get(ctx, key, actual)

	notFound := apierr.IsNotFound(err)
	if err != nil && !notFound {
		return err
	}

	if !notFound {
		obj.SetResourceVersion(actual.GetResourceVersion())
	} else {
		obj.SetResourceVersion("")
	}

	patchOpts := []client.PatchOption{
		client.FieldOwner(fieldOwner),
	}

	if overwrite {
		patchOpts = append(patchOpts, client.ForceOwnership)
	}

	//nolint:staticcheck
	return c.Patch(ctx, obj, client.Apply, patchOpts...)
}

// Returns timestamp of last apply for a manager.
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
