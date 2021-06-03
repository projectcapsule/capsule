// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package namespace

import (
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule/api/v1alpha1"
)

type OwnerReference struct {
}

func (o OwnerReference) Object() client.Object {
	return &v1.Namespace{}
}

func (o OwnerReference) Field() string {
	return ".metadata.ownerReferences[*].capsule"
}

func (o OwnerReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		var res []string
		ns := object.(*v1.Namespace)
		for _, or := range ns.OwnerReferences {
			if or.APIVersion == v1alpha1.GroupVersion.String() {
				res = append(res, or.Name)
			}
		}
		return res
	}
}
