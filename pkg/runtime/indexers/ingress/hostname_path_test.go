// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ingress_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/projectcapsule/capsule/pkg/runtime/indexers/ingress"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestHostnamePathIndexers(t *testing.T) {
	t.Parallel()

	want := []string{"example.com;/", "example.com;/api"}

	tests := []struct {
		name string
		idx  ingress.HostnamePath
		obj  client.Object
	}{
		{
			name: "networking v1",
			idx:  ingress.HostnamePath{Obj: &networkingv1.Ingress{}},
			obj: &networkingv1.Ingress{Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{
				Host: "example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{
					{Path: "/"}, {Path: "/api"},
				}}},
			}}}},
		},
		{
			name: "networking v1beta1",
			idx:  ingress.HostnamePath{Obj: &networkingv1beta1.Ingress{}},
			obj: &networkingv1beta1.Ingress{Spec: networkingv1beta1.IngressSpec{Rules: []networkingv1beta1.IngressRule{{
				Host: "example.com",
				IngressRuleValue: networkingv1beta1.IngressRuleValue{HTTP: &networkingv1beta1.HTTPIngressRuleValue{Paths: []networkingv1beta1.HTTPIngressPath{
					{Path: "/"}, {Path: "/api"},
				}}},
			}}}},
		},
		{
			name: "extensions v1beta1",
			idx:  ingress.HostnamePath{Obj: &extensionsv1beta1.Ingress{}},
			obj: &extensionsv1beta1.Ingress{Spec: extensionsv1beta1.IngressSpec{Rules: []extensionsv1beta1.IngressRule{{
				Host: "example.com",
				IngressRuleValue: extensionsv1beta1.IngressRuleValue{HTTP: &extensionsv1beta1.HTTPIngressRuleValue{Paths: []extensionsv1beta1.HTTPIngressPath{
					{Path: "/"}, {Path: "/api"},
				}}},
			}}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.idx.Object() == nil || tt.idx.Field() != ingress.HostPathPair {
				t.Fatalf("unexpected object/field")
			}
			got := tt.idx.Func()(tt.obj)
			sort.Strings(got)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Func() = %#v, want %#v", got, want)
			}
		})
	}
}
