// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package workloads

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ScaleWorkloadGVK maps the resource of a scale subresource request to the
// parent workload whose template the scaled replicas are created from. The
// request's own Kind is autoscaling/Scale; only the native scalable
// controllers are recognized.
func ScaleWorkloadGVK(resource metav1.GroupVersionResource) schema.GroupVersionKind {
	switch {
	case resource.Group == "apps" && resource.Resource == "deployments":
		return schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	case resource.Group == "apps" && resource.Resource == "statefulsets":
		return schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"}
	case resource.Group == "apps" && resource.Resource == "replicasets":
		return schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"}
	case resource.Group == "" && resource.Resource == "replicationcontrollers":
		return schema.GroupVersionKind{Version: "v1", Kind: "ReplicationController"}
	default:
		return schema.GroupVersionKind{}
	}
}
