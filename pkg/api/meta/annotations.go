// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

import (
	"context"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ReleaseAnnotation        = "projectcapsule.dev/release"
	ReleaseAnnotationTrigger = "true"

	ReconcileAnnotation = "reconcile.projectcapsule.dev/requestedAt"

	AvailableIngressClassesAnnotation       = "capsule.clastix.io/ingress-classes"
	AvailableIngressClassesRegexpAnnotation = "capsule.clastix.io/ingress-classes-regexp"
	AvailableStorageClassesAnnotation       = "capsule.clastix.io/storage-classes"
	AvailableStorageClassesRegexpAnnotation = "capsule.clastix.io/storage-classes-regexp"
	AllowedRegistriesAnnotation             = "capsule.clastix.io/allowed-registries"
	AllowedRegistriesRegexpAnnotation       = "capsule.clastix.io/allowed-registries-regexp"

	ForbiddenNamespaceLabelsAnnotation            = "capsule.clastix.io/forbidden-namespace-labels"
	ForbiddenNamespaceLabelsRegexpAnnotation      = "capsule.clastix.io/forbidden-namespace-labels-regexp"
	ForbiddenNamespaceAnnotationsAnnotation       = "capsule.clastix.io/forbidden-namespace-annotations"
	ForbiddenNamespaceAnnotationsRegexpAnnotation = "capsule.clastix.io/forbidden-namespace-annotations-regexp"
	ProtectedTenantAnnotation                     = "capsule.clastix.io/protected"

	ResourceQuotaAnnotationPrefix = "quota.resources.capsule.clastix.io"
	ResourceUsedAnnotationPrefix  = "used.resources.capsule.clastix.io"
)

func ReleaseAnnotationTriggers(obj client.Object) bool {
	return annotationTriggers(obj, ReleaseAnnotation, ReleaseAnnotationTrigger)
}

func ReleaseAnnotationRemove(obj client.Object) {
	annotationRemove(obj, ReleaseAnnotation)
}

func TriggerRequestReconcileAnnotation(
	ctx context.Context,
	c client.Client,
	gvk schema.GroupVersionKind,
	key types.NamespacedName,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)

		if err := c.Get(ctx, key, obj); err != nil {
			return err
		}

		base := obj.DeepCopy()

		annotations := obj.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}

		annotations[ReconcileAnnotation] = time.Now().UTC().Format(time.RFC3339Nano)

		obj.SetAnnotations(annotations)

		return c.Patch(ctx, obj, client.MergeFrom(base))
	})
}

func annotationRemove(obj client.Object, anno string) {
	annotations := obj.GetAnnotations()

	if _, ok := annotations[anno]; ok {
		delete(annotations, anno)

		obj.SetAnnotations(annotations)
	}
}

func annotationTriggers(obj client.Object, anno string, trigger string) bool {
	annotations := obj.GetAnnotations()

	if val, ok := annotations[anno]; ok {
		if strings.ToLower(val) == trigger {
			return true
		}
	}

	return false
}
