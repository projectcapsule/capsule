// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package namespace

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

type OwnerReference struct{}

func (o OwnerReference) Object() client.Object {
	return &corev1.Namespace{}
}

func (o OwnerReference) Field() string {
	return ".metadata.ownerReferences[*].capsule"
}

func (o OwnerReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		res := []string{}
		ns, ok := object.(*corev1.Namespace)

		if !ok {
			panic(fmt.Errorf("expected *corev1.Namespace, got %T", ns))
		}

		for _, or := range ns.OwnerReferences {
			if or.APIVersion == capsulev1beta1.GroupVersion.String() {
				res = append(res, or.Name)
			}
		}

		return res
	}
}
