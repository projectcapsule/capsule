// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func PatchApply(
	ctx context.Context,
	c client.Client,
	obj client.Object,
	fieldOwner string,
	overwrite bool,
) error {
	opts := []client.PatchOption{client.FieldOwner(fieldOwner)}
	if overwrite {
		opts = append(opts, client.ForceOwnership)
	}

	return c.Patch(ctx, obj, client.Apply, opts...)
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
