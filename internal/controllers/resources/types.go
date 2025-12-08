// Copyright 2020-2025 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// Keeps track of generated items
type Accumulator = map[capsulev1beta2.ResourceIDWithOptions]*unstructured.Unstructured
