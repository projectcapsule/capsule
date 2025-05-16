// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package meta

import (
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ReleaseAnnotation        = "projectcapsule.dev/release"
	ReleaseAnnotationTrigger = "true"
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
