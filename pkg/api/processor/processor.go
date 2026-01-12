// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/configuration"
)

const (
	finalizer = "capsule.clastix.io/resources"
)

type Processor struct {
	Configuration                configuration.Configuration
	AllowCrossNamespaceSelection bool
}

type ProcessorOptions struct {
	FieldOwnerPrefix string
	Prune            bool
	Adopt            bool
	Force            bool
	Owner            *metav1.OwnerReference
}
