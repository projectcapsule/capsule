// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package workloads

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestScaleWorkloadGVK(t *testing.T) {
	for _, tc := range []struct {
		name     string
		resource metav1.GroupVersionResource
		want     schema.GroupVersionKind
	}{
		{name: "deployments", resource: metav1.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, want: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}},
		{name: "statefulsets", resource: metav1.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, want: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"}},
		{name: "replicasets", resource: metav1.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}, want: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"}},
		{name: "replicationcontrollers", resource: metav1.GroupVersionResource{Version: "v1", Resource: "replicationcontrollers"}, want: schema.GroupVersionKind{Version: "v1", Kind: "ReplicationController"}},
		{name: "daemonsets have no scale", resource: metav1.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}},
		{name: "custom group is not a workload", resource: metav1.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "deployments"}},
		{name: "empty resource", resource: metav1.GroupVersionResource{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ScaleWorkloadGVK(tc.resource))

			if !tc.want.Empty() {
				require.NotEmpty(t, PodTemplatePath(tc.want), "every scalable parent must expose its template path")
			}
		})
	}
}
