// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

var _ = Describe("ResourcePool Tests", func() {
	_ = &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "wind-quota",
			Labels: map[string]string{
				"e2e-resourcepool": "test",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "wind-user",
					Kind: "User",
				},
			},
		},
	}

	JustAfterEach(func() {
		Eventually(func() error {
			poolList := &capsulev1beta2.TenantList{}
			labelSelector := client.MatchingLabels{"e2e-resourcepool": "test"}
			if err := k8sClient.List(context.TODO(), poolList, labelSelector); err != nil {
				return err
			}

			for _, pool := range poolList.Items {
				if err := k8sClient.Delete(context.TODO(), &pool); err != nil {
					return err
				}
			}

			return nil
		}, "30s", "5s").Should(Succeed())

		Eventually(func() error {
			poolList := &capsulev1beta2.ResourcePoolList{}
			labelSelector := client.MatchingLabels{"e2e-resourcepool": "test"}
			if err := k8sClient.List(context.TODO(), poolList, labelSelector); err != nil {
				return err
			}

			for _, pool := range poolList.Items {
				if err := k8sClient.Delete(context.TODO(), &pool); err != nil {
					return err
				}
			}

			return nil
		}, "30s", "5s").Should(Succeed())

		Eventually(func() error {
			poolList := &corev1.NamespaceList{}
			labelSelector := client.MatchingLabels{"e2e-resourcepool": "test"}
			if err := k8sClient.List(context.TODO(), poolList, labelSelector); err != nil {
				return err
			}

			for _, pool := range poolList.Items {
				if err := k8sClient.Delete(context.TODO(), &pool); err != nil {
					return err
				}
			}

			return nil
		}, "30s", "5s").Should(Succeed())

	})

	It("handles defaults correctly correctly", func() {
		pool := &capsulev1beta2.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default-pool",
				Labels: map[string]string{
					"e2e-resourcepool": "test",
				},
			},
			Spec: capsulev1beta2.ResourcePoolSpec{
				Selectors: []api.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"capsule.clastix.io/tenant": "solar-quota",
							},
						},
					},
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"capsule.clastix.io/tenant": "wind-quota",
							},
						},
					},
				},
				Quota: corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{
						corev1.ResourceLimitsCPU:      resource.MustParse("2"),
						corev1.ResourceLimitsMemory:   resource.MustParse("2Gi"),
						corev1.ResourceRequestsCPU:    resource.MustParse("2"),
						corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
					},
				},
			},
		}

		By("creating a simple pool", func() {
			err := k8sClient.Create(context.TODO(), pool)
			Expect(err).Should(Succeed(), "Failed to create Deployment in namespace %s", pool)
		})

		By("Get Applied revision", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, pool)
			Expect(err).Should(Succeed())
		})

		By("Verify Defaults were enabled and set to 0", func() {
			expected := &capsulev1beta2.ResourcePoolDefaults{
				Enabled: true,
				Resources: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("0"),
					corev1.ResourceLimitsMemory:   resource.MustParse("0"),
					corev1.ResourceRequestsCPU:    resource.MustParse("0"),
					corev1.ResourceRequestsMemory: resource.MustParse("0"),
				},
			}

			ok, msg := DeepCompare(expected, pool.Spec.Defaults)
			Expect(ok).To(BeTrue(), "Mismatch for expected defaults: %s", msg)
		})

		By("Verify Status was correctly initialized", func() {
			expected := &capsulev1beta2.ResourcePoolQuotaStatus{
				Hard: pool.Spec.Quota.Hard,
				Claimed: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("0"),
					corev1.ResourceLimitsMemory:   resource.MustParse("0"),
					corev1.ResourceRequestsCPU:    resource.MustParse("0"),
					corev1.ResourceRequestsMemory: resource.MustParse("0"),
				},
			}

			ok, msg := DeepCompare(*expected, pool.Status.Allocation)
			Expect(ok).To(BeTrue(), "Mismatch for expected status allocation: %s", msg)
		})

		By("Create Namespaces, which are selected by the pool", func() {
			ns1 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-1-default-pool",
					Labels: map[string]string{
						"capsule.clastix.io/tenant": "solar-quota",
					},
				},
			}

			err := k8sClient.Create(context.TODO(), ns1)
			Expect(err).Should(Succeed())

			ns2 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-2-default-pool",
					Labels: map[string]string{
						"capsule.clastix.io/tenant": "wind-quota",
					},
				},
			}

			err = k8sClient.Create(context.TODO(), ns2)
			Expect(err).Should(Succeed())
		})

		By("Verify Namespaces are shown as eligble targets", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, pool)
			Expect(err).Should(Succeed())

			expectedNamespaces := []string{"ns-1-default-pool", "ns-2-default-pool"}

			ok, msg := DeepCompare(expectedNamespaces, pool.Status.Namespaces)
			Expect(ok).To(BeTrue(), "Mismatch for expected namespaces: %s", msg)
		})

	})
})
