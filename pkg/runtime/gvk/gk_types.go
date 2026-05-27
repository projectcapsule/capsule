// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package gvk

import "k8s.io/apimachinery/pkg/runtime/schema"

type VersionKind struct {
	// Kind of the referent.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	Kind string `json:"kind" protobuf:"bytes,1,opt,name=kind"`
	// API version of the referent.
	APIVersion string `json:"apiVersion" protobuf:"bytes,5,opt,name=apiVersion"`
}

func (s VersionKind) GroupVersionKind() schema.GroupVersionKind {
	gv, err := schema.ParseGroupVersion(s.APIVersion)
	if err != nil {
		return schema.GroupVersionKind{
			Kind: s.Kind,
		}
	}

	return gv.WithKind(s.Kind)
}
