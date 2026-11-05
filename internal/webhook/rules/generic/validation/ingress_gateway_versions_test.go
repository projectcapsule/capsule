// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	webhookutils "github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

func TestGatewayClassAdmissionVersions(t *testing.T) {
	for _, version := range []string{"v1", "v1beta1"} {
		for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
			for _, tc := range []struct {
				name, denial, subresource string
				selector, hostnames       bool
			}{
				{name: "allowed"},
				{name: "denied", denial: "gateway class"},
				{name: "selector allowed", selector: true},
				{name: "selector denied", selector: true, denial: "gateway class"},
				{name: "hostname denied", hostnames: true, denial: "ingress hostname"},
				{name: "status skipped", selector: true, denial: "", subresource: "status"},
			} {
				t.Run(fmt.Sprintf("%s/%s/%s", version, operation, tc.name), func(t *testing.T) {
					scheme := overlapScheme(t)
					require.NoError(t, gatewayv1.Install(scheme))
					body := ingressClassRule(rules.ActionTypeAllow, rules.IngressTypeGateway, rules.IngressClassMatch{Exact: []string{"edge"}})
					if tc.selector {
						body.Ingress.Classes = []rules.IngressClassMatch{{MatchLabels: map[string]string{"env": "customer"}}}
					}
					if tc.denial != "" {
						body.Action = rules.ActionTypeDeny
					}
					if tc.hostnames {
						body.Ingress.Classes = nil
						body.Ingress.Hostnames = []apiruntime.ExpressionMatch{{Exact: []string{"app.example.com"}}}
					}
					tenant := &capsulev1beta2.Tenant{Name: "tenant", UID: "tenant"}
					ns := overlapNamespace(tenant.Name, "selected", true)
					status := &capsulev1beta2.RuleStatus{Name: meta.NameForManagedRuleStatus(), Namespace: ns.Name}
					status.Status.Rules = []*rules.NamespaceRuleBodyNamespace{{Enforce: body}}
					classReads := 0
					c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tenant, ns, status,
						&gatewayv1.GatewayClass{Name: "edge", Labels: map[string]string{"env": "customer"}}).
						WithInterceptorFuncs(interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if _, ok := obj.(*gatewayv1.GatewayClass); ok {
								classReads++
							}
							return c.Get(ctx, key, obj, opts...)
						}}).Build()
					cfg := configuration.NewCapsuleConfiguration(t.Context(), c, c, nil, "capsule")
					obj := objectWithSpec(map[string]any{"gatewayClassName": "edge", "listeners": []any{map[string]any{"hostname": "app.example.com"}}})
					obj.SetAPIVersion("gateway.networking.k8s.io/" + version)
					obj.SetKind("Gateway")
					obj.SetName("app")
					obj.SetNamespace(ns.Name)
					raw, err := json.Marshal(obj)
					require.NoError(t, err)
					req := admission.Request{Operation: operation, Namespace: ns.Name, Name: obj.GetName(), SubResource: tc.subresource,
						Kind:   metav1.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: version, Kind: "Gateway"},
						Object: runtime.RawExtension{Raw: raw}, OldObject: runtime.RawExtension{Raw: raw}}
					reader := webhookutils.NewRequestCachingReader(c)
					var response *admission.Response
					for _, handler := range Register(nil, nil, cfg, nil, nil).GetHandlers() {
						handle := handler.OnCreate(c, reader, admission.NewDecoder(scheme), events.NewDiscardRecorder())
						if operation == admissionv1.Update {
							handle = handler.OnUpdate(c, reader, admission.NewDecoder(scheme), events.NewDiscardRecorder())
						}
						if response = handle(t.Context(), req); response != nil {
							break
						}
					}
					if tc.denial != "" {
						require.NotNil(t, response, "served compatibility versions must reach enforcement")
						require.False(t, response.Allowed)
						require.Contains(t, response.Result.Message, tc.denial)
					} else {
						require.Nil(t, response, "successful checks must continue the chain")
					}
					wantReads := 0
					if tc.selector && tc.subresource == "" {
						wantReads = 1
					}
					require.Equal(t, wantReads, classReads, "class lookups stay on Gateway API v1")
				})
			}
		}
	}
}
