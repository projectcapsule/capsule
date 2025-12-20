// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func getType(cq capsulev1beta2.CustomQuota) string {
	if cq.Namespace != "" {
		return "CustomQuota"
	}

	return "ClusterCustomQuota"
}

func getUnstructured(rawExt runtime.RawExtension) (unstructured.Unstructured, error) {
	var (
		obj   runtime.Object
		scope conversion.Scope
	)

	err := runtime.Convert_runtime_RawExtension_To_runtime_Object(&rawExt, &obj, scope)
	if err != nil {
		return unstructured.Unstructured{}, err
	}

	innerObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return unstructured.Unstructured{}, err
	}

	u := unstructured.Unstructured{Object: innerObj}

	return u, nil
}
