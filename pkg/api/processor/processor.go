// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

type Processor struct {
	Configuration                configuration.Configuration
	AllowCrossNamespaceSelection bool
	GatherClient                 client.Reader
	Mapper                       k8smeta.RESTMapper
}

type ProcessorOptions struct {
	FieldOwnerPrefix string
	Prune            bool
	Adopt            bool
	Force            bool
	Owner            *metav1.OwnerReference
}

// Scope narrows a reconciliation down to the items replicated into a
// single Namespace of a single Tenant.
type Scope struct {
	Tenant    string
	Namespace string
}

// Matches states whether the given resource identity belongs to the scope.
func (s Scope) Matches(id gvk.ResourceID) bool {
	return id.Tenant == s.Tenant && id.Namespace == s.Namespace
}
