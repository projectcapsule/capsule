// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

const (
	GlobalDependenciesFieldName     = ".spec.dependsOn.global"
	NamespacedDependenciesFieldName = ".spec.dependsOn.namespaced"
)

type GlobalDependencies struct{}

func (GlobalDependencies) Object() client.Object { return &capsulev1beta2.GlobalTenantResource{} }
func (GlobalDependencies) Field() string         { return GlobalDependenciesFieldName }
func (GlobalDependencies) Func() client.IndexerFunc {
	return func(obj client.Object) []string {
		resource, ok := obj.(*capsulev1beta2.GlobalTenantResource)
		if !ok {
			return nil
		}

		dependencies := make([]string, 0, len(resource.Spec.DependsOn))
		for _, dependency := range resource.Spec.DependsOn {
			dependencies = append(dependencies, dependency.Name.String())
		}

		return dependencies
	}
}

type NamespacedDependencies struct{}

func (NamespacedDependencies) Object() client.Object { return &capsulev1beta2.TenantResource{} }
func (NamespacedDependencies) Field() string         { return NamespacedDependenciesFieldName }
func (NamespacedDependencies) Func() client.IndexerFunc {
	return func(obj client.Object) []string {
		resource, ok := obj.(*capsulev1beta2.TenantResource)
		if !ok {
			return nil
		}

		dependencies := make([]string, 0, len(resource.Spec.DependsOn))
		for _, dependency := range resource.Spec.DependsOn {
			dependencies = append(dependencies, dependency.Name.String())
		}

		return dependencies
	}
}
