// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type contextObjectKey struct {
	GVK schema.GroupVersionKind
	Key client.ObjectKey
}

// Records only resource versions, before context sanitization. A conditional
// generator reading its own target must not overwrite a newer target snapshot.
// The embedded client retains the existing impersonation and namespace checks.
type contextSnapshotClient struct {
	client.Client

	versions map[contextObjectKey]string
}

func (c *contextSnapshotClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	err := c.Client.Get(ctx, key, obj, opts...)

	if _, ok := obj.(*unstructured.Unstructured); ok {
		switch {
		case apierrors.IsNotFound(err):
			c.versions[contextObjectKey{GVK: obj.GetObjectKind().GroupVersionKind(), Key: key}] = ""
		case err == nil:
			c.versions[contextObjectKey{GVK: obj.GetObjectKind().GroupVersionKind(), Key: key}] = obj.GetResourceVersion()
		}
	}

	return err
}

func (c *contextSnapshotClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if err := c.Client.List(ctx, list, opts...); err != nil {
		return err
	}

	if objects, ok := list.(*unstructured.UnstructuredList); ok {
		for _, obj := range objects.Items {
			c.versions[contextObjectKey{GVK: obj.GroupVersionKind(), Key: client.ObjectKeyFromObject(&obj)}] = obj.GetResourceVersion()
		}
	}

	return nil
}
