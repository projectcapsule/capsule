// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

const (
	finalizer = "capsule.clastix.io/resources"
)

type Processor struct {
	Configuration                configuration.Configuration
	AllowCrossNamespaceSelection bool
	GatherClient                 client.Reader
}

type ProcessorOptions struct {
	FieldOwnerPrefix string
	Prune            bool
	Adopt            bool
	Force            bool
	Owner            *metav1.OwnerReference
}
