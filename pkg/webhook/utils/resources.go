// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"

	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	schedulev1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/version"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const TRUE string = "true"

// Get PriorityClass by name (Does not return error if not found).
func GetPriorityClassByName(ctx context.Context, c client.Client, name string) (*schedulev1.PriorityClass, error) {
	class := &schedulev1.PriorityClass{}
	if err := c.Get(ctx, types.NamespacedName{Name: name}, class); err != nil {
		return nil, err
	}

	return class, nil
}

// Get StorageClass by name (Does not return error if not found).
func GetStorageClassByName(ctx context.Context, c client.Client, name string) (*storagev1.StorageClass, error) {
	class := &storagev1.StorageClass{}
	if err := c.Get(ctx, types.NamespacedName{Name: name}, class); err != nil {
		return nil, err
	}

	return class, nil
}

// Get IngressClass by name (Does not return error if not found).
func GetIngressClassByName(ctx context.Context, version *version.Version, c client.Client, ingressClassName *string) (client.Object, error) {
	if ingressClassName == nil {
		return nil, nil
	}

	var obj client.Object

	switch {
	case version == nil:
		obj = &networkingv1.IngressClass{}
	case version.Minor() < 18:
		return nil, nil
	case version.Minor() < 19:
		obj = &networkingv1beta1.IngressClass{}
	default:
		obj = &networkingv1.IngressClass{}
	}

	if err := c.Get(ctx, types.NamespacedName{Name: *ingressClassName}, obj); err != nil {
		return nil, err
	}

	return obj, nil
}

// IsDefaultPriorityClass checks if the given PriorityClass is cluster default.
func IsDefaultPriorityClass(class *schedulev1.PriorityClass) bool {
	if class != nil {
		return class.GlobalDefault
	}

	return false
}

func IsDefaultIngressClass(class client.Object) bool {
	annotation := "ingressclass.kubernetes.io/is-default-class"

	if class != nil {
		annotations := class.GetAnnotations()
		if v, ok := annotations[annotation]; ok && v == TRUE {
			return true
		}
	}

	return false
}

// IsDefaultStorageClass checks if the given StorageClass is cluster default.
func IsDefaultStorageClass(class client.Object) bool {
	annotation := "storageclass.kubernetes.io/is-default-class"

	if class != nil {
		annotations := class.GetAnnotations()
		if v, ok := annotations[annotation]; ok && v == TRUE {
			return true
		}
	}

	return false
}
