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
	"github.com/projectcapsule/capsule/pkg/meta"
)

var _ = Describe("ResourcePool Tests", func() {
	_ = &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-claims-1",
			Labels: map[string]string{
				"e2e-resourcepoolclaims": "test",
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
			labelSelector := client.MatchingLabels{"e2e-resourcepoolclaims": "test"}
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
			labelSelector := client.MatchingLabels{"e2e-resourcepoolclaims": "test"}
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
			labelSelector := client.MatchingLabels{"e2e-resourcepoolclaims": "test"}
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

	It("Pool Assignment", func() {
		pool1 := &capsulev1beta2.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-binding-claims",
				Labels: map[string]string{
					"e2e-resourcepoolclaims": "test",
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

		By("Create the ResourcePool", func() {
			err := k8sClient.Create(context.TODO(), pool1)
			Expect(err).Should(Succeed(), "Failed to create ResourcePool %s", pool1)
		})

		By("Get Applied revision", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool1.Name}, pool1)
			Expect(err).Should(Succeed())
		})

		By("Create Namespaces, which are selected by the pool", func() {
			ns1 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-1-pool-assign",
					Labels: map[string]string{
						"e2e-resourcepoolclaims":    "test",
						"capsule.clastix.io/tenant": "solar-quota",
					},
				},
			}

			err := k8sClient.Create(context.TODO(), ns1)
			Expect(err).Should(Succeed())

			ns2 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-2-pool-assign",
					Labels: map[string]string{
						"e2e-resourcepoolclaims":    "test",
						"capsule.clastix.io/tenant": "wind-quota",
					},
				},
			}

			err = k8sClient.Create(context.TODO(), ns2)
			Expect(err).Should(Succeed())

			ns3 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-3-pool-assign",
					Labels: map[string]string{
						"e2e-resourcepoolclaims":    "test",
						"capsule.clastix.io/tenant": "something-else",
					},
				},
			}

			err = k8sClient.Create(context.TODO(), ns3)
			Expect(err).Should(Succeed())
		})

		By("Verify Namespaces are shown as allowed targets", func() {
			expectedNamespaces := []string{"ns-1-pool-assign", "ns-2-pool-assign"}

			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool1.Name}, pool1)
			Expect(err).Should(Succeed())

			ok, msg := DeepCompare(expectedNamespaces, pool1.Status.Namespaces)
			Expect(ok).To(BeTrue(), "Mismatch for expected namespaces: %s", msg)

			Expect(pool1.Status.NamespaceSize).To(Equal(uint(2)))
		})

		By("Create a first claim and verify binding", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "assign-pool-claim-1",
					Namespace: "ns-1-pool-assign",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					Pool: "test-binding-claims",
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceLimitsCPU:      resource.MustParse("0"),
						corev1.ResourceLimitsMemory:   resource.MustParse("0"),
						corev1.ResourceRequestsCPU:    resource.MustParse("0"),
						corev1.ResourceRequestsMemory: resource.MustParse("0"),
					},
				},
			}

			err := k8sClient.Create(context.TODO(), claim)
			Expect(err).Should(Succeed(), "Failed to create Claim %s", claim)

			err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, claim)
			Expect(err).Should(Succeed())

			expectedPool := api.StatusNameUID{
				Name: api.Name(pool1.Name),
				UID:  pool1.GetUID(),
			}
			Expect(claim.Status.Pool).To(Equal(expectedPool), "expected pool name to match")

			Expect(claim.Status.Condition.Status).To(Equal(metav1.ConditionTrue), "failed to verify condition status")
			Expect(claim.Status.Condition.Type).To(Equal(meta.ReadyCondition), "failed to verify condition type")
			Expect(claim.Status.Condition.Reason).To(Equal(meta.BoundReason), "failed to verify condition reason")
		})

		By("Create a second claim and verify binding", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "assign-pool-claim-2",
					Namespace: "ns-2-pool-assign",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					Pool: "test-binding-claims",
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceLimitsCPU:      resource.MustParse("0"),
						corev1.ResourceLimitsMemory:   resource.MustParse("0"),
						corev1.ResourceRequestsCPU:    resource.MustParse("0"),
						corev1.ResourceRequestsMemory: resource.MustParse("0"),
					},
				},
			}

			err := k8sClient.Create(context.TODO(), claim)
			Expect(err).Should(Succeed(), "Failed to create Claim %s", claim)

			err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, claim)
			Expect(err).Should(Succeed())

			expectedPool := api.StatusNameUID{
				Name: api.Name(pool1.Name),
				UID:  pool1.GetUID(),
			}
			Expect(claim.Status.Pool).To(Equal(expectedPool), "expected pool name to match")

			Expect(claim.Status.Condition.Status).To(Equal(metav1.ConditionTrue), "failed to verify condition status")
			Expect(claim.Status.Condition.Type).To(Equal(meta.ReadyCondition), "failed to verify condition type")
			Expect(claim.Status.Condition.Reason).To(Equal(meta.BoundReason), "failed to verify condition reason")
		})

		By("Create a second claim and verify error", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "assign-pool-claim-3",
					Namespace: "ns-3-pool-assign",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					Pool: "test-binding-claims",
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceLimitsCPU:      resource.MustParse("0"),
						corev1.ResourceLimitsMemory:   resource.MustParse("0"),
						corev1.ResourceRequestsCPU:    resource.MustParse("0"),
						corev1.ResourceRequestsMemory: resource.MustParse("0"),
					},
				},
			}

			err := k8sClient.Create(context.TODO(), claim)
			Expect(err).Should(Succeed(), "Failed to create Claim %s", claim)

			err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, claim)
			Expect(err).Should(Succeed())

			expectedPool := api.StatusNameUID{}
			Expect(claim.Status.Pool).To(Equal(expectedPool), "expected pool name to be empty")

			Expect(claim.Status.Condition.Status).To(Equal(metav1.ConditionFalse), "failed to verify condition status")
			Expect(claim.Status.Condition.Type).To(Equal(meta.NotReadyCondition), "failed to verify condition type")
			Expect(claim.Status.Condition.Reason).To(Equal(meta.AssignedReason), "failed to verify condition reason")
		})
	})
})
