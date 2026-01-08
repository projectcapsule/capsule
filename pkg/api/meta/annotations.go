// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

import (
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ReleaseAnnotation        = "projectcapsule.dev/release"
	ReleaseAnnotationTrigger = "true"

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
)

func ReleaseAnnotationTriggers(obj client.Object) bool {
	return annotationTriggers(obj, ReleaseAnnotation, ReleaseAnnotationTrigger)
}

func ReleaseAnnotationRemove(obj client.Object) {
	annotationRemove(obj, ReleaseAnnotation)
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
