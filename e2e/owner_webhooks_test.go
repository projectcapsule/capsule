//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/api"
)

var _ = Describe("when Tenant owner interacts with the webhooks", func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-owner",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "ruby",
					Kind: "User",
				},
			},
			StorageClasses: &api.DefaultAllowedListSpec{
				SelectorAllowedListSpec: api.SelectorAllowedListSpec{
					AllowedListSpec: api.AllowedListSpec{
						Exact: []string{
							"cephfs",
							"glusterfs",
						},
					},
				},
			},
			LimitRanges: api.LimitRangesSpec{Items: []corev1.LimitRangeSpec{
				{
					Limits: []corev1.LimitRangeItem{
						{
							Type: corev1.LimitTypePod,
							Min: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    resource.MustParse("50m"),
								corev1.ResourceMemory: resource.MustParse("5Mi"),
							},
							Max: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
			},
			NetworkPolicies: api.NetworkPolicySpec{Items: []networkingv1.NetworkPolicySpec{
				{
					Egress: []networkingv1.NetworkPolicyEgressRule{
						{
							To: []networkingv1.NetworkPolicyPeer{
								{
									IPBlock: &networkingv1.IPBlock{
										CIDR: "0.0.0.0/0",
									},
								},
							},
						},
					},
					PodSelector: metav1.LabelSelector{},
					PolicyTypes: []networkingv1.PolicyType{
						networkingv1.PolicyTypeIngress,
						networkingv1.PolicyTypeEgress,
					},
				},
			},
			},
			ResourceQuota: api.ResourceQuotaSpec{Items: []corev1.ResourceQuotaSpec{
				{
					Hard: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourcePods: resource.MustParse("10"),
					},
				},
			},
			},
		},
	}
	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})

	It("should disallow deletions", func() {
		By("blocking Capsule Limit ranges", func() {
			ns := NewNamespace("")
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			lr := &corev1.LimitRange{}
			Eventually(func() error {
				n := fmt.Sprintf("capsule-%s-0", tnt.GetName())
				return k8sClient.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: ns.GetName()}, lr)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			cs := ownerClient(tnt.Spec.Owners[0])
			Expect(cs.CoreV1().LimitRanges(ns.GetName()).Delete(context.TODO(), lr.Name, metav1.DeleteOptions{})).ShouldNot(Succeed())
		})
		By("blocking Capsule Network Policy", func() {
			ns := NewNamespace("")
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			np := &networkingv1.NetworkPolicy{}
			Eventually(func() error {
				n := fmt.Sprintf("capsule-%s-0", tnt.GetName())
				return k8sClient.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: ns.GetName()}, np)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			cs := ownerClient(tnt.Spec.Owners[0])
			Expect(cs.NetworkingV1().NetworkPolicies(ns.GetName()).Delete(context.TODO(), np.Name, metav1.DeleteOptions{})).ShouldNot(Succeed())
		})
		By("blocking Capsule Resource Quota", func() {
			ns := NewNamespace("")
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			rq := &corev1.ResourceQuota{}
			Eventually(func() error {
				n := fmt.Sprintf("capsule-%s-0", tnt.GetName())
				return k8sClient.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: ns.GetName()}, rq)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			cs := ownerClient(tnt.Spec.Owners[0])
			Expect(cs.NetworkingV1().NetworkPolicies(ns.GetName()).Delete(context.TODO(), rq.Name, metav1.DeleteOptions{})).ShouldNot(Succeed())
		})
	})

	It("should allow", func() {
		By("listing Limit Range", func() {
			ns := NewNamespace("")
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			Eventually(func() (err error) {
				cs := ownerClient(tnt.Spec.Owners[0])
				_, err = cs.CoreV1().LimitRanges(ns.GetName()).List(context.TODO(), metav1.ListOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
		By("listing Network Policy", func() {
			ns := NewNamespace("")
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			Eventually(func() (err error) {
				cs := ownerClient(tnt.Spec.Owners[0])
				_, err = cs.NetworkingV1().NetworkPolicies(ns.GetName()).List(context.TODO(), metav1.ListOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
		By("listing Resource Quota", func() {
			ns := NewNamespace("")
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			Eventually(func() (err error) {
				cs := ownerClient(tnt.Spec.Owners[0])
				_, err = cs.NetworkingV1().NetworkPolicies(ns.GetName()).List(context.TODO(), metav1.ListOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
	})

	It("should allow all actions to Tenant owner Network Policy", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		cs := ownerClient(tnt.Spec.Owners[0])
		np := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: "custom-network-policy",
			},
			Spec: tnt.Spec.NetworkPolicies.Items[0],
		}
		By("creating", func() {
			Eventually(func() (err error) {
				_, err = cs.NetworkingV1().NetworkPolicies(ns.GetName()).Create(context.TODO(), np, metav1.CreateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
		By("updating", func() {
			Eventually(func() (err error) {
				np.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{}
				_, err = cs.NetworkingV1().NetworkPolicies(ns.GetName()).Update(context.TODO(), np, metav1.UpdateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
		By("deleting", func() {
			Eventually(func() (err error) {
				return cs.NetworkingV1().NetworkPolicies(ns.GetName()).Delete(context.TODO(), np.Name, metav1.DeleteOptions{})
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
	})
})
