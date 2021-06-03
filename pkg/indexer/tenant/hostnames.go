// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule/api/v1alpha1"
)

type IngressHostnames struct {
}

func (IngressHostnames) Object() client.Object {
	return &v1alpha1.Tenant{}
}

func (IngressHostnames) Field() string {
	return ".spec.ingressHostnames"
}

func (IngressHostnames) Func() client.IndexerFunc {
	return func(object client.Object) (out []string) {
		tenant := object.(*v1alpha1.Tenant)
		if tenant.Spec.IngressHostnames != nil {
			out = append(out, tenant.Spec.IngressHostnames.Exact...)
		}
		return
	}
}
