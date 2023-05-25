//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"strings"

	"github.com/clastix/capsule/pkg/api"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

var _ = Describe("creating namespaces within a Tenant with resources", func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-resources",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "john",
					Kind: "User",
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
						{
							Type: corev1.LimitTypeContainer,
							Default: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    resource.MustParse("200m"),
								corev1.ResourceMemory: resource.MustParse("100Mi"),
							},
							DefaultRequest: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("10Mi"),
							},
							Min: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    resource.MustParse("50m"),
								corev1.ResourceMemory: resource.MustParse("5Mi"),
							},
							Max: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
						{
							Type: corev1.LimitTypePersistentVolumeClaim,
							Min: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
							Max: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("10Gi"),
							},
						},
					},
				},
			},
			},
			NetworkPolicies: api.NetworkPolicySpec{Items: []networkingv1.NetworkPolicySpec{
				{
					Ingress: []networkingv1.NetworkPolicyIngressRule{
						{
							From: []networkingv1.NetworkPolicyPeer{
								{
									NamespaceSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"capsule.clastix.io/tenant": "tenant-resources",
										},
									},
								},
								{
									PodSelector: &metav1.LabelSelector{},
								},
								{
									IPBlock: &networkingv1.IPBlock{
										CIDR: "192.168.0.0/12",
									},
								},
							},
						},
					},
					Egress: []networkingv1.NetworkPolicyEgressRule{
						{
							To: []networkingv1.NetworkPolicyPeer{
								{
									IPBlock: &networkingv1.IPBlock{
										CIDR: "0.0.0.0/0",
										Except: []string{
											"192.168.0.0/12",
										},
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
			NodeSelector: map[string]string{
				"kubernetes.io/os": "linux",
			},
			ResourceQuota: api.ResourceQuotaSpec{Items: []corev1.ResourceQuotaSpec{
				{
					Hard: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceLimitsCPU:      resource.MustParse("8"),
						corev1.ResourceLimitsMemory:   resource.MustParse("16Gi"),
						corev1.ResourceRequestsCPU:    resource.MustParse("8"),
						corev1.ResourceRequestsMemory: resource.MustParse("16Gi"),
					},
					Scopes: []corev1.ResourceQuotaScope{
						corev1.ResourceQuotaScopeNotTerminating,
					},
				},
				{
					Hard: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourcePods: resource.MustParse("10"),
					},
				},
				{
					Hard: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceRequestsStorage: resource.MustParse("100Gi"),
					},
				},
			},
			},
		},
	}
	nsl := []string{"bim", "bum", "bam"}
	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
		By("creating the Namespaces", func() {
			for _, i := range nsl {
				ns := NewNamespace(i)
				NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
				TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
			}
		})
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})
	It("should contains all replicated resources", func() {
		for _, name := range nsl {
			By("checking Limit Range", func() {
				for i, s := range tnt.Spec.LimitRanges.Items {
					n := fmt.Sprintf("capsule-%s-%d", tnt.GetName(), i)
					lr := &corev1.LimitRange{}
					Eventually(func() error {
						return k8sClient.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: name}, lr)
					}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
					Expect(lr.Spec).Should(Equal(s))
				}
			})
			By("checking Network Policy", func() {
				for i, s := range tnt.Spec.NetworkPolicies.Items {
					n := fmt.Sprintf("capsule-%s-%d", tnt.GetName(), i)
					np := &networkingv1.NetworkPolicy{}
					Eventually(func() error {
						return k8sClient.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: name}, np)
					}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
					Expect(np.Spec).Should(Equal(s))
				}
			})
			By("checking the Namespace scheduler annotation", func() {
				var selector []string
				for k, v := range tnt.Spec.NodeSelector {
					selector = append(selector, fmt.Sprintf("%s=%s", k, v))
				}
				Eventually(func() string {
					ns := &corev1.Namespace{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: name}, ns)).Should(Succeed())
					return ns.GetAnnotations()["scheduler.alpha.kubernetes.io/node-selector"]
				}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(strings.Join(selector, ",")))
			})
			By("checking the Resource Quota", func() {
				for i, s := range tnt.Spec.ResourceQuota.Items {
					Eventually(func() corev1.ResourceQuotaSpec {
						n := fmt.Sprintf("capsule-%s-%d", tnt.GetName(), i)

						rq := &corev1.ResourceQuota{}
						if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: name}, rq); err != nil {
							return corev1.ResourceQuotaSpec{}
						}

						return rq.Spec
					}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(s))
				}
			})
		}
	})
})
