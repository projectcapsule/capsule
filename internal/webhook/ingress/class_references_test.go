// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestClassTypeForGVK(t *testing.T) {
	for _, tc := range []struct {
		group, version, kind string
		want                 rules.IngressType
	}{
		{"networking.k8s.io", "v1", "Ingress", rules.IngressTypeIngress},
		{"gateway.networking.k8s.io", "v1", "Gateway", rules.IngressTypeGateway},
		{"gateway.networking.k8s.io", "v1beta1", "Gateway", rules.IngressTypeGateway},
		{"networking.k8s.io", "v1beta1", "Ingress", ""},
		{"extensions", "v1beta1", "Ingress", ""},
		{"gateway.networking.k8s.io", "v1alpha2", "Gateway", ""},
		{"gateway.networking.k8s.io", "v2", "Gateway", ""},
		{"gateway.networking.k8s.io", "", "Gateway", ""},
		{"example.com", "v1beta1", "Gateway", ""},
		{"gateway.networking.k8s.io", "v1beta1", "HTTPRoute", ""},
		{"gateway.networking.k8s.io", "v1beta1", "ListenerSet", ""},
		{"gateway.networking.k8s.io", "v1", "GatewayClass", ""},
	} {
		t.Run(tc.group+"/"+tc.version+"/"+tc.kind, func(t *testing.T) {
			kind, supported := ClassTypeForGVK(schema.GroupVersionKind{Group: tc.group, Version: tc.version, Kind: tc.kind})
			require.Equal(t, tc.want, kind)
			require.Equal(t, tc.want != "", supported)
		})
	}
}
