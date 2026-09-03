// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"maps"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/utils"
)

const replicationContextKey = "replications"

func newReplicationContext(object metav1.Object) (map[string]any, error) {
	annotations := maps.Clone(object.GetAnnotations())
	delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")

	metadata, err := utils.ToUnstructuredMap(&metav1.ObjectMeta{
		Name:                       object.GetName(),
		GenerateName:               object.GetGenerateName(),
		Namespace:                  object.GetNamespace(),
		SelfLink:                   object.GetSelfLink(),
		UID:                        object.GetUID(),
		ResourceVersion:            object.GetResourceVersion(),
		Generation:                 object.GetGeneration(),
		CreationTimestamp:          object.GetCreationTimestamp(),
		DeletionTimestamp:          object.GetDeletionTimestamp(),
		DeletionGracePeriodSeconds: object.GetDeletionGracePeriodSeconds(),
		Labels:                     maps.Clone(object.GetLabels()),
		Annotations:                annotations,
		OwnerReferences:            slices.Clone(object.GetOwnerReferences()),
		Finalizers:                 slices.Clone(object.GetFinalizers()),
	})
	if err != nil {
		return nil, err
	}

	return map[string]any{"metadata": metadata}, nil
}
