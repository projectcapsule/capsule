// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func BenchmarkIngressClassValidation(b *testing.B) {
	for _, target := range []struct {
		kind          rules.IngressType
		version, name string
	}{
		{rules.IngressTypeIngress, "v1", "Ingress"},
		{rules.IngressTypeGateway, "v1", "Gateway"},
		{rules.IngressTypeGateway, "v1beta1", "Gateway-v1beta1"},
	} {
		kind := target.kind
		for _, count := range []int{1, 32} {
			for _, tenants := range []int{1, 16} {
				for _, state := range []string{"name", "selector", "deny", "skip", "cold-selector"} {
					b.Run(fmt.Sprintf("%s/rules=%d/tenants=%d/%s", target.name, count, tenants, state), func(b *testing.B) {
						_, reader, h := ingressClassFixture(b, kind, "edge")
						profiles := make([][]*rules.NamespaceRuleBodyNamespace, tenants)
						objects := make([]*unstructured.Unstructured, tenants)
						for tenant := range tenants {
							name := fmt.Sprintf("tenant-%d-edge", tenant)
							var class client.Object = &networkingv1.IngressClass{Name: name, Labels: map[string]string{"tenant": fmt.Sprint(tenant)}}
							if kind == rules.IngressTypeGateway {
								class = &gatewayv1.GatewayClass{Name: name, Labels: map[string]string{"tenant": fmt.Sprint(tenant)}}
							}
							require.NoError(b, reader.Reader.(client.Client).Create(b.Context(), class))
							obj := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
							field := "ingressClassName"
							if kind == rules.IngressTypeGateway {
								field = "gatewayClassName"
							}
							require.NoError(b, unstructured.SetNestedField(obj.Object, name, "spec", field))
							objects[tenant] = obj
							for range count {
								body := ingressClassRule(rules.ActionTypeAllow, kind, rules.IngressClassMatch{Exact: []string{name}})
								if state == "selector" || state == "cold-selector" {
									body.Ingress.Classes = []rules.IngressClassMatch{{MatchLabels: map[string]string{"tenant": fmt.Sprint(tenant)}}}
								}
								if state == "deny" {
									body.Action = rules.ActionTypeDeny
								}
								if state == "skip" {
									other := rules.IngressTypeGateway
									if kind == other {
										other = rules.IngressTypeIngress
									}
									body.Ingress.Types = []rules.IngressType{other}
								}
								profiles[tenant] = append(profiles[tenant], &rules.NamespaceRuleBodyNamespace{Enforce: body})
							}
						}
						group := "networking.k8s.io"
						if kind == rules.IngressTypeGateway {
							group = "gateway.networking.k8s.io"
						}
						req := admission.Request{Operation: admissionv1.Create, Kind: metav1.GroupVersionKind{Group: group, Version: target.version, Kind: string(kind)}}
						recorder := events.NewDiscardRecorder()
						tenant := &capsulev1beta2.Tenant{Name: "tenant"}
						b.ReportAllocs()
						for i := 0; b.Loop(); i++ {
							if state == "cold-selector" {
								b.StopTimer()
								h.selectors = cache.NewLabelSelectorCache()
								b.StartTimer()
							}
							response := h.OnCreate(nil, reader, objects[i%tenants], nil, recorder, tenant, profiles[i%tenants])(b.Context(), req)
							if state == "deny" {
								if response == nil || response.Allowed {
									b.Fatal("expected class denial")
								}
							} else if response != nil {
								b.Fatalf("unexpected class response: %+v", response)
							}
						}
						b.ReportMetric(float64(reader.gets)/float64(b.N), "class-reads/op")
					})
				}
			}
		}
	}
}

func BenchmarkIngressClassConcurrent(b *testing.B) {
	obj, reader, h := ingressClassFixture(b, rules.IngressTypeIngress, "edge")
	body := ingressClassRule(rules.ActionTypeAllow, rules.IngressTypeIngress, rules.IngressClassMatch{MatchLabels: map[string]string{"env": "customer"}})
	bodies := []*rules.NamespaceRuleBodyNamespace{{Enforce: body}}
	req := admission.Request{Kind: metav1.GroupVersionKind{Group: "networking.k8s.io", Version: "v1", Kind: "Ingress"}}
	var reads atomic.Int64
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		local := &ingressClassReader{Reader: reader.Reader}
		for pb.Next() {
			if response := h.OnCreate(nil, local, obj, nil, events.NewDiscardRecorder(), nil, bodies)(b.Context(), req); response != nil {
				b.Error("unexpected class denial")
			}
		}
		reads.Add(int64(local.gets))
	})
	b.ReportMetric(float64(reads.Load())/float64(b.N), "class-reads/op")
}
