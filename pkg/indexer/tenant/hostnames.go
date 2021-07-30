// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

type IngressHostnames struct {
}

func (IngressHostnames) Object() client.Object {
	return &capsulev1beta1.Tenant{}
}

func (IngressHostnames) Field() string {
	return ".spec.ingressHostnames"
}

func (IngressHostnames) Func() client.IndexerFunc {
	return func(object client.Object) (out []string) {
		tenant := object.(*capsulev1beta1.Tenant)
		if tenant.Spec.IngressOptions != nil && tenant.Spec.IngressOptions.AllowedHostnames != nil {
			out = append(out, tenant.Spec.IngressOptions.AllowedHostnames.Exact...)
		}
		return
	}
}
