// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

// Model fields added by Kubernetes and Capsule admission before persistence.
// A plain fake client does not exercise the real cluster's update churn.
func provisioningDefaultingClient(base client.WithWatch) client.WithWatch {
	apply := func(obj client.Object) {
		switch typed := obj.(type) {
		case *rbacv1.RoleBinding:
			for i := range typed.Subjects {
				if typed.Subjects[i].APIGroup == "" && (typed.Subjects[i].Kind == rbacv1.UserKind || typed.Subjects[i].Kind == rbacv1.GroupKind) {
					typed.Subjects[i].APIGroup = rbacv1.GroupName
				}
			}
			if name := typed.Labels[meta.NewTenantLabel]; name != "" {
				typed.Labels[meta.ManagedByCapsuleLabel] = name
			}
		case *networkingv1.NetworkPolicy:
			if len(typed.Spec.PolicyTypes) == 0 {
				typed.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
				if len(typed.Spec.Egress) > 0 {
					typed.Spec.PolicyTypes = append(typed.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)
				}
			}
		}
	}
	return interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			apply(obj)
			return c.Create(ctx, obj, opts...)
		},
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			apply(obj)
			return c.Update(ctx, obj, opts...)
		},
	})
}

func TestProvisioningDoesNotRewriteAdmissionAndAPIDefaults(t *testing.T) {
	for _, policy := range []struct {
		name string
		spec networkingv1.NetworkPolicySpec
		want []networkingv1.PolicyType
	}{
		{"default ingress", networkingv1.NetworkPolicySpec{}, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}},
		{"default ingress and egress", networkingv1.NetworkPolicySpec{Egress: []networkingv1.NetworkPolicyEgressRule{{}}}, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}},
		{"explicit egress only", networkingv1.NetworkPolicySpec{PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}}, []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}},
	} {
		t.Run(policy.name, func(t *testing.T) {
			tnt := provisioningTenant()
			tnt.Spec.AdditionalRoleBindings = []rbac.AdditionalRoleBindingsSpec{{ClusterRoleName: "view", Subjects: []rbacv1.Subject{{Kind: rbacv1.UserKind, Name: "reader"}, {Kind: rbacv1.GroupKind, Name: "readers"}}}}
			tnt.Spec.NetworkPolicies = api.NetworkPolicySpec{Items: []networkingv1.NetworkPolicySpec{policy.spec}}
			ns := cleanupOwnedNamespace(tnt, "selected", false)
			other := &capsulev1beta2.Tenant{Name: "other", UID: "other-uid"}
			foreign := cleanupOwnedNamespace(other, "foreign", false)
			r, _ := namespaceCleanupFixture(t, tnt, ns, other, foreign)
			defaults := provisioningDefaultingClient(r.Client.(client.WithWatch))
			updates := 0
			r.Client = interceptor.NewClient(defaults, interceptor.Funcs{Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				switch obj.(type) {
				case *rbacv1.RoleBinding, *networkingv1.NetworkPolicy:
					updates++
				}
				return c.Update(ctx, obj, opts...)
			}})
			if err := r.reconcile(t.Context(), logr.Discard(), tnt); err != nil {
				t.Fatal(err)
			}
			before := tnt.Spec.DeepCopy()
			updates = 0
			if err := r.reconcile(t.Context(), logr.Discard(), tnt); err != nil {
				t.Fatal(err)
			}
			if updates != 0 {
				t.Fatalf("unchanged provisioning issued %d RBAC/policy updates", updates)
			}
			if tnt.Spec.AdditionalRoleBindings[0].Subjects[0].APIGroup != "" || tnt.Spec.AdditionalRoleBindings[0].Subjects[1].APIGroup != "" {
				t.Fatal("subject defaulting mutated Tenant input")
			}
			if len(tnt.Spec.NetworkPolicies.Items[0].PolicyTypes) != len(before.NetworkPolicies.Items[0].PolicyTypes) {
				t.Fatal("API defaulting mutated Tenant policy input")
			}
			policies := &networkingv1.NetworkPolicyList{}
			if err := r.List(t.Context(), policies, client.InNamespace(ns.Name)); err != nil {
				t.Fatal(err)
			}
			if len(policies.Items) != 1 || !reflect.DeepEqual(policies.Items[0].Spec.PolicyTypes, policy.want) {
				t.Fatalf("persisted policy direction changed: %+v", policies.Items)
			}
			assertBindingSubject(t, t.Context(), r.Client, ns.Name, "alice", true)
			assertBindingSubject(t, t.Context(), r.Client, foreign.Name, "alice", false)
		})
	}
}

func TestNetworkPolicyClientResponsesCannotMutateTenantProfiles(t *testing.T) {
	tnt := provisioningTenant()
	tnt.Spec.NetworkPolicies.Items = []networkingv1.NetworkPolicySpec{{PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "original"}}}}
	r, _ := namespaceCleanupFixture(t, tnt)
	r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
		if err := c.Create(ctx, obj, opts...); err != nil {
			return err
		}
		if policy, ok := obj.(*networkingv1.NetworkPolicy); ok {
			policy.Spec.PodSelector.MatchLabels["app"] = "API-response-label"
		}
		return nil
	}})
	for _, namespace := range []string{"namespace-a", "namespace-b"} {
		if err := r.syncNetworkPolicy(t.Context(), logr.Discard(), tnt, namespace, []string{"0"}); err != nil {
			t.Fatal(err)
		}
		if tnt.Spec.NetworkPolicies.Items[0].PodSelector.MatchLabels["app"] != "original" {
			t.Fatal("client response mutated another namespace's policy input")
		}
	}
}

func TestLimitRangeClientResponsesCannotMutateTenantProfiles(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		t.Run(operation, func(t *testing.T) {
			tnt := provisioningTenant()
			tnt.Spec.LimitRanges.Items = []corev1.LimitRangeSpec{{Limits: []corev1.LimitRangeItem{{
				Type:    corev1.LimitTypeContainer,
				Default: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
			}}}}
			r, _ := namespaceCleanupFixture(t, tnt)
			before := tnt.DeepCopy()
			mutateResponse := func(obj client.Object) {
				if limit, ok := obj.(*corev1.LimitRange); ok {
					limit.Spec.Limits[0].Default[corev1.ResourceCPU] = resource.MustParse("2")
					limit.Spec.Limits[0].Max = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")}
				}
			}
			base := r.Client
			if operation == "update" {
				for _, ns := range []string{"namespace-a", "namespace-b"} {
					if err := base.Create(t.Context(), &corev1.LimitRange{Name: "capsule-tenant-a-0", Namespace: ns}); err != nil {
						t.Fatal(err)
					}
				}
			}
			r.Client = interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if err := c.Create(ctx, obj, opts...); err != nil {
						return err
					}
					mutateResponse(obj)
					return nil
				},
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					if err := c.Update(ctx, obj, opts...); err != nil {
						return err
					}
					mutateResponse(obj)
					return nil
				},
			})
			if err := runForNamespaces(t.Context(), []string{"namespace-a", "namespace-b"}, func(ctx context.Context, ns string) error {
				// Quota reconciliation also copies this shared Tenant concurrently.
				_ = tnt.DeepCopy()
				return r.syncLimitRange(ctx, logr.Discard(), tnt, ns, []string{"0"})
			}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(tnt, before) {
				t.Fatal("LimitRange client responses mutated the shared Tenant")
			}
			for _, ns := range []string{"namespace-a", "namespace-b"} {
				stored := &corev1.LimitRange{}
				if err := base.Get(t.Context(), client.ObjectKey{Name: "capsule-tenant-a-0", Namespace: ns}, stored); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(stored.Spec, before.Spec.LimitRanges.Items[0]) {
					t.Fatalf("another namespace's response changed persisted defaults: %+v", stored.Spec)
				}
			}
		})
	}
}

func provisioningTenant() *capsulev1beta2.Tenant {
	return &capsulev1beta2.Tenant{
		Name: "tenant-a", UID: "tenant-a-uid", Generation: 1,
		Spec:   capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: "alice", ClusterRoles: []string{"admin"}}}},
		Status: capsulev1beta2.TenantStatus{ObservedGeneration: 1, Owners: rbac.OwnerStatusListSpec{{Kind: rbac.UserOwner, Name: "alice", ClusterRoles: []string{"admin"}}}},
	}
}
