// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	genericvalidation "github.com/projectcapsule/capsule/internal/webhook/rules/generic/validation"
	networkvalidation "github.com/projectcapsule/capsule/internal/webhook/rules/networkpolicies/validation"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

type routerCountingClient struct {
	client.Client
	gets, lists atomic.Int64
}

func (c *routerCountingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c.gets.Add(1)
	return c.Client.Get(ctx, key, obj, opts...)
}
func (c *routerCountingClient) List(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
	c.lists.Add(1)
	return c.Client.List(ctx, obj, opts...)
}

func rulesRouterFixture(tb testing.TB, tenants, ruleCount int, metadataAction, networkAction rules.ActionType) (*handlerRouter, *routerCountingClient, []admission.Request) {
	tb.Helper()
	scheme := runtime.NewScheme()
	require.NoError(tb, corev1.AddToScheme(scheme))
	require.NoError(tb, networkingv1.AddToScheme(scheme))
	require.NoError(tb, capsulev1beta2.AddToScheme(scheme))
	var objects []client.Object
	var requests []admission.Request
	for i := range tenants {
		name := fmt.Sprintf("tenant-%d", i)
		tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name)}}
		objects = append(objects, tnt)
		// Two different namespace profiles in every tenant, including an unaffected profile.
		for _, selected := range []bool{true, false} {
			namespace := fmt.Sprintf("%s-selected-%v", name, selected)
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace, OwnerReferences: []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: name, UID: tnt.UID}}}}
			rs := &capsulev1beta2.RuleStatus{ObjectMeta: metav1.ObjectMeta{Name: meta.NameForManagedRuleStatus(), Namespace: namespace}}
			if selected {
				for n := range ruleCount {
					key := fmt.Sprintf("unmatched-%d", n)
					if n == ruleCount-1 {
						key = "review"
					}
					rs.Status.Rules = append(rs.Status.Rules, &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
						Action:   metadataAction,
						Metadata: []rules.MetadataRule{{VersionKinds: apiruntime.VersionKinds{APIGroups: []string{"networking.k8s.io/v1"}, Kinds: []string{"NetworkPolicy"}}, Labels: map[string]rules.MetadataValueRule{key: {}}}},
					}})
				}
				rs.Status.Rules = append(rs.Status.Rules, &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
					Action: networkAction, Network: rules.NamespaceRuleEnforceNetworkBody{Policies: rules.NamespaceRuleEnforceNetworkPoliciesBody{Egress: &rules.NetworkPolicyCIDRRule{CIDRs: []string{"10.0.0.0/8"}}}},
				}})
			}
			objects = append(objects, ns, rs)
			policy := &networkingv1.NetworkPolicy{TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"}, ObjectMeta: metav1.ObjectMeta{Name: "policy", Namespace: namespace}, Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}, Egress: []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "10.20.0.0/16"}}}}},
			}}
			old, err := json.Marshal(policy)
			require.NoError(tb, err)
			policy.Labels = map[string]string{"review": "yes"}
			raw, err := json.Marshal(policy)
			require.NoError(tb, err)
			requests = append(requests, admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Kind: metav1.GroupVersionKind{Group: "networking.k8s.io", Version: "v1", Kind: "NetworkPolicy"}, Namespace: namespace, Name: policy.Name,
				UID: types.UID(namespace), Operation: admissionv1.Create, Object: runtime.RawExtension{Raw: raw}, OldObject: runtime.RawExtension{Raw: old}, UserInfo: authenticationv1.UserInfo{Username: "alice"},
			}})
		}
	}
	cl := &routerCountingClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithStatusSubresource(&capsulev1beta2.RuleStatus{}).Build()}
	chain := genericvalidation.Register(cache.NewRegexCache(), nil, nil, nil, nil, networkvalidation.Handler(nil, nil)).GetHandlers()
	return &handlerRouter{client: cl, reader: cl, decoder: admission.NewDecoder(scheme), handlers: chain, recorder: events.NewDiscardRecorder()}, cl, requests
}

func TestRouterComposedRulesDryRun(t *testing.T) {
	for _, tc := range []struct {
		metadata, network rules.ActionType
		reason, denial    string
		eventCount        int
	}{
		{rules.ActionTypeAudit, rules.ActionTypeAudit, events.ReasonNamespaceRuleAudit, "", 2},
		{rules.ActionTypeDeny, rules.ActionTypeAudit, events.ReasonForbiddenMetadata, "metadata label", 1},
		{rules.ActionTypeAudit, rules.ActionTypeDeny, events.ReasonForbiddenNetworkPolicyEgressCIDR, "networkPolicy egress CIDR", 2},
		{rules.ActionTypeAllow, rules.ActionTypeDeny, events.ReasonForbiddenNetworkPolicyEgressCIDR, "networkPolicy egress CIDR", 1},
	} {
		for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
			t.Run(fmt.Sprintf("metadata=%s/network=%s/%s", tc.metadata, tc.network, operation), func(t *testing.T) {
				router, cl, requests := rulesRouterFixture(t, 2, 1, tc.metadata, tc.network)
				recorder, writes, _ := testRecorder()
				router.recorder = recorder
				// Interleave dry and live admissions on the same router and tenants.
				for _, dryRun := range []*bool{new(true), nil, new(false), new(true)} {
					for i, req := range requests {
						req.Operation, req.DryRun = operation, dryRun
						cl.gets.Store(0)
						cl.lists.Store(0)
						response := router.Handle(t.Context(), req)
						selected := i%2 == 0
						require.Equal(t, !selected || tc.denial == "", response.Allowed)
						if selected && tc.denial != "" {
							require.Contains(t, response.Result.Message, tc.denial)
						}
						require.EqualValues(t, 3, cl.gets.Load())
						require.Zero(t, cl.lists.Load())
						emitted := drainEvents(t, recorder, writes)
						if !selected || (dryRun != nil && *dryRun) {
							require.Empty(t, emitted)
							continue
						}
						require.Len(t, emitted, tc.eventCount)
						require.Equal(t, tc.reason, emitted[len(emitted)-1].Reason)
						for _, event := range emitted {
							require.Equal(t, req.Namespace, event.Namespace)
							require.Equal(t, fmt.Sprintf("tenant-%d", i/2), event.Labels[meta.NewTenantLabel])
							require.Equal(t, string(req.UID), event.Annotations[meta.AuditRequestUID])
						}
					}
				}
			})
		}
	}
}
