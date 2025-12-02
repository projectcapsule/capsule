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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/misc"
)

var _ = Describe("ResourcePoolClaim Tests", Label("resourcepool"), func() {
	_ = &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-claims-1",
			Labels: map[string]string{
				"e2e-resourcepoolclaims": "test",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "wind-user",
							Kind: "User",
						},
					},
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

	It("Claim to Pool Assignment", func() {
		pool1 := &capsulev1beta2.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-binding-claims",
				Labels: map[string]string{
					"e2e-resourcepoolclaims": "test",
				},
			},
			Spec: capsulev1beta2.ResourcePoolSpec{
				Selectors: []misc.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"capsule.clastix.io/tenant": "claims-bindings",
							},
						},
					},
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"capsule.clastix.io/tenant": "claims-bindings-2",
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

		claim1 := &capsulev1beta2.ResourcePoolClaim{
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

		claim2 := &capsulev1beta2.ResourcePoolClaim{
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
						"capsule.clastix.io/tenant": "claims-bindings",
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
						"capsule.clastix.io/tenant": "claims-bindings-2",
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

			Expect(pool1.Status.Namespaces).To(Equal(expectedNamespaces))
			Expect(pool1.Status.NamespaceSize).To(Equal(uint(2)))
		})

		By("Create a first claim and verify binding", func() {

			err := k8sClient.Create(context.TODO(), claim1)
			Expect(err).Should(Succeed(), "Failed to create Claim %s", claim1)

			err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim1.Name, Namespace: claim1.Namespace}, claim1)
			Expect(err).Should(Succeed())

			isSuccessfullyBoundToPool(pool1, claim1)

			expectedPool := api.StatusNameUID{
				Name: api.Name(pool1.Name),
				UID:  pool1.GetUID(),
			}
			Expect(claim1.Status.Pool).To(Equal(expectedPool), "expected pool name to match")
			Expect(claim1.Status.Condition.Status).To(Equal(metav1.ConditionTrue), "failed to verify condition status")
			Expect(claim1.Status.Condition.Type).To(Equal(meta.BoundCondition), "failed to verify condition type")
			Expect(claim1.Status.Condition.Reason).To(Equal(meta.SucceededReason), "failed to verify condition reason")
		})

		By("Create a second claim and verify binding", func() {
			err := k8sClient.Create(context.TODO(), claim2)
			Expect(err).Should(Succeed(), "Failed to create Claim %s", claim2)

			err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim2.Name, Namespace: claim2.Namespace}, claim2)
			Expect(err).Should(Succeed())

			isSuccessfullyBoundToPool(pool1, claim2)

			expectedPool := api.StatusNameUID{
				Name: api.Name(pool1.Name),
				UID:  pool1.GetUID(),
			}
			Expect(claim2.Status.Pool).To(Equal(expectedPool), "expected pool name to match")
			Expect(claim2.Status.Condition.Status).To(Equal(metav1.ConditionTrue), "failed to verify condition status")
			Expect(claim2.Status.Condition.Type).To(Equal(meta.BoundCondition), "failed to verify condition type")
			Expect(claim2.Status.Condition.Reason).To(Equal(meta.SucceededReason), "failed to verify condition reason")
		})

		By("Create a third claim and verify error", func() {
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
			Expect(claim.Status.Condition.Type).To(Equal(meta.AssignedCondition), "failed to verify condition type")
			Expect(claim.Status.Condition.Reason).To(Equal(meta.FailedReason), "failed to verify condition reason")
		})
	})

	It("Admission (Validation) - Patch Guard", func() {
		pool := &capsulev1beta2.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-admission-claims",
				Labels: map[string]string{
					"e2e-resourcepoolclaims": "test",
				},
			},
			Spec: capsulev1beta2.ResourcePoolSpec{
				Config: capsulev1beta2.ResourcePoolSpecConfiguration{
					DeleteBoundResources: ptr.To(false),
				},
				Selectors: []misc.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"capsule.clastix.io/tenant": "admission-guards",
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

		claim := &capsulev1beta2.ResourcePoolClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "admission-pool-claim-1",
				Namespace: "ns-1-pool-admission",
			},
			Spec: capsulev1beta2.ResourcePoolClaimSpec{
				Pool: pool.GetName(),
				ResourceClaims: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("1"),
					corev1.ResourceLimitsMemory:   resource.MustParse("1Gi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("1"),
					corev1.ResourceRequestsMemory: resource.MustParse("1Gi"),
				},
			},
		}

		By("Create the Claim", func() {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: claim.Namespace,
					Labels: map[string]string{
						"e2e-resourcepoolclaims":    "test",
						"capsule.clastix.io/tenant": "admission-guards",
					},
				},
			}

			err := k8sClient.Create(context.TODO(), ns)
			Expect(err).Should(Succeed())

			err = k8sClient.Create(context.TODO(), claim)
			Expect(err).Should(Succeed(), "Failed to create Claim %s", claim)
		})

		By("Create the ResourcePool", func() {
			err := k8sClient.Create(context.TODO(), pool)
			Expect(err).Should(Succeed(), "Failed to create ResourcePool %s", pool)
		})

		By("Get Applied revision", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, pool)
			Expect(err).Should(Succeed())
		})

		By("Bind a claim", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, claim)
			Expect(err).Should(Succeed())

			expectedPool := api.StatusNameUID{
				Name: api.Name(pool.Name),
				UID:  pool.GetUID(),
			}

			isBoundCondition(claim)
			Expect(claim.Status.Pool).To(Equal(expectedPool), "expected pool name to match")
		})

		By("Error on deleting bound claim", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, claim)
			Expect(err).Should(Succeed())

			isBoundCondition(claim)

			err = k8sClient.Delete(context.TODO(), claim)
			Expect(err).ShouldNot(Succeed())
		})

		By("Error on patching resources for claim (Increase)", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, claim)
			Expect(err).Should(Succeed())

			claim.Spec.ResourceClaims = corev1.ResourceList{
				corev1.ResourceLimitsCPU:      resource.MustParse("2"),
				corev1.ResourceLimitsMemory:   resource.MustParse("2Gi"),
				corev1.ResourceRequestsCPU:    resource.MustParse("2"),
				corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
			}

			err = k8sClient.Update(context.TODO(), claim)
			Expect(err).ShouldNot(Succeed(), "Expected error when updating resources in bound state %s", claim)
		})

		By("Error on patching resources for claim (Decrease)", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, claim)
			Expect(err).Should(Succeed())

			claim.Spec.ResourceClaims = corev1.ResourceList{
				corev1.ResourceLimitsCPU:      resource.MustParse("0"),
				corev1.ResourceLimitsMemory:   resource.MustParse("0Gi"),
				corev1.ResourceRequestsCPU:    resource.MustParse("0"),
				corev1.ResourceRequestsMemory: resource.MustParse("0Gi"),
			}

			err = k8sClient.Update(context.TODO(), claim)
			Expect(err).ShouldNot(Succeed(), "Expected error when updating resources in bound state %s", claim)
		})

		By("Error on patching pool name", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, claim)
			Expect(err).Should(Succeed())

			claim.Spec.Pool = "some-random-pool"

			err = k8sClient.Update(context.TODO(), claim)
			Expect(err).ShouldNot(Succeed(), "Expected error when updating resources in bound state %s", claim)
		})

		By("Delete Pool", func() {
			err := k8sClient.Delete(context.TODO(), pool)
			Expect(err).Should(Succeed())
		})

		By("Verify claim is no longer bound", func() {
			isUnassignedCondition(claim)
		})

		By("Allow patching resources for claim (Increase)", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, claim)
			Expect(err).Should(Succeed())

			claim.Spec.ResourceClaims = corev1.ResourceList{
				corev1.ResourceLimitsCPU:      resource.MustParse("2"),
				corev1.ResourceLimitsMemory:   resource.MustParse("2Gi"),
				corev1.ResourceRequestsCPU:    resource.MustParse("2"),
				corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
			}

			err = k8sClient.Update(context.TODO(), claim)
			Expect(err).Should(Succeed(), "Expected error when updating resources in bound state %s", claim)
		})

		By("Allow patching resources for claim (Decrease)", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, claim)
			Expect(err).Should(Succeed())

			claim.Spec.ResourceClaims = corev1.ResourceList{
				corev1.ResourceLimitsCPU:      resource.MustParse("0"),
				corev1.ResourceLimitsMemory:   resource.MustParse("0Gi"),
				corev1.ResourceRequestsCPU:    resource.MustParse("0"),
				corev1.ResourceRequestsMemory: resource.MustParse("0Gi"),
			}

			err = k8sClient.Update(context.TODO(), claim)
			Expect(err).Should(Succeed(), "Expected error when updating resources in bound state %s", claim)
		})

		By("Allow patching pool name", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, claim)
			Expect(err).Should(Succeed())

			claim.Spec.Pool = "some-random-pool"

			err = k8sClient.Update(context.TODO(), claim)
			Expect(err).Should(Succeed(), "Expected no error when updating resources in bound state %s", claim)
		})

	})

	It("Admission (Mutation) - Auto Pool Assign", func() {
		pool1 := &capsulev1beta2.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-auto-assign-1",
				Labels: map[string]string{
					"e2e-resourcepoolclaims": "test",
				},
			},
			Spec: capsulev1beta2.ResourcePoolSpec{
				Config: capsulev1beta2.ResourcePoolSpecConfiguration{
					DeleteBoundResources: ptr.To(false),
				},
				Selectors: []misc.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"capsule.clastix.io/tenant": "admission-auto-assign",
							},
						},
					},
				},
				Quota: corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{
						corev1.ResourceLimitsCPU:   resource.MustParse("2"),
						corev1.ResourceRequestsCPU: resource.MustParse("2"),
					},
				},
			},
		}

		pool2 := &capsulev1beta2.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-auto-assign-2",
				Labels: map[string]string{
					"e2e-resourcepoolclaims": "test",
				},
			},
			Spec: capsulev1beta2.ResourcePoolSpec{
				Config: capsulev1beta2.ResourcePoolSpecConfiguration{
					DeleteBoundResources: ptr.To(false),
				},
				Selectors: []misc.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"capsule.clastix.io/tenant": "admission-auto-assign",
							},
						},
					},
				},
				Quota: corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{
						corev1.ResourceLimitsMemory:   resource.MustParse("2"),
						corev1.ResourceRequestsMemory: resource.MustParse("2"),
					},
				},
			},
		}

		By("Create the ResourcePools", func() {
			err := k8sClient.Create(context.TODO(), pool1)
			Expect(err).Should(Succeed(), "Failed to create ResourcePool %s", pool1)

			err = k8sClient.Create(context.TODO(), pool2)
			Expect(err).Should(Succeed(), "Failed to create ResourcePool %s", pool2)
		})

		By("Auto Assign Claim (CPU)", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auto-assign-1",
					Namespace: "ns-1-pool-assign",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceLimitsCPU:   resource.MustParse("1"),
						corev1.ResourceRequestsCPU: resource.MustParse("1"),
					},
				},
			}

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: claim.Namespace,
					Labels: map[string]string{
						"e2e-resourcepoolclaims":    "test",
						"capsule.clastix.io/tenant": "admission-auto-assign",
					},
				},
			}

			err := k8sClient.Create(context.TODO(), ns)
			Expect(err).Should(Succeed())

			err = k8sClient.Create(context.TODO(), claim)
			Expect(err).Should(Succeed(), "Failed to create Claim %s", claim)

			err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, claim)
			Expect(err).Should(Succeed())

			Expect(claim.Spec.Pool).To(Equal(pool1.Name), "expected pool name to match")
		})

		By("Auto Assign Claim (Memory)", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auto-assign-1",
					Namespace: "ns-2-pool-assign",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceLimitsMemory:   resource.MustParse("1"),
						corev1.ResourceRequestsMemory: resource.MustParse("1"),
					},
				},
			}

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: claim.Namespace,
					Labels: map[string]string{
						"e2e-resourcepoolclaims":    "test",
						"capsule.clastix.io/tenant": "admission-auto-assign",
					},
				},
			}

			err := k8sClient.Create(context.TODO(), ns)
			Expect(err).Should(Succeed())

			err = k8sClient.Create(context.TODO(), claim)
			Expect(err).Should(Succeed(), "Failed to create Claim %s", claim)

			err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, claim)
			Expect(err).Should(Succeed())

			Expect(claim.Spec.Pool).To(Equal(pool2.Name), "expected pool name to match")
		})

		By("No Default available (Storage)", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auto-assign-3",
					Namespace: "ns-3-pool-assign",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceRequestsStorage: resource.MustParse("1"),
					},
				},
			}

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: claim.Namespace,
					Labels: map[string]string{
						"e2e-resourcepoolclaims":    "test",
						"capsule.clastix.io/tenant": "admission-auto-assign",
					},
				},
			}

			err := k8sClient.Create(context.TODO(), ns)
			Expect(err).Should(Succeed())

			err = k8sClient.Create(context.TODO(), claim)
			Expect(err).Should(Succeed(), "Failed to create Claim %s", claim)

			err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, claim)
			Expect(err).Should(Succeed())

			Expect(claim.Spec.Pool).To(Equal(""), "expected pool name to match")
		})

	})

})

func isUnassignedCondition(claim *capsulev1beta2.ResourcePoolClaim) {
	cl := &capsulev1beta2.ResourcePoolClaim{}
	err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, cl)
	Expect(err).Should(Succeed())

	Expect(cl.Status.Condition.Status).To(Equal(metav1.ConditionFalse), "failed to verify condition status")
	Expect(cl.Status.Condition.Type).To(Equal(meta.AssignedCondition), "failed to verify condition type")
	Expect(cl.Status.Condition.Reason).To(Equal(meta.FailedReason), "failed to verify condition reason")
}

func isBoundCondition(claim *capsulev1beta2.ResourcePoolClaim) {
	cl := &capsulev1beta2.ResourcePoolClaim{}
	err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, cl)
	Expect(err).Should(Succeed())

	Expect(cl.Status.Condition.Status).To(Equal(metav1.ConditionTrue), "failed to verify condition status")
	Expect(cl.Status.Condition.Type).To(Equal(meta.BoundCondition), "failed to verify condition type")
	Expect(cl.Status.Condition.Reason).To(Equal(meta.SucceededReason), "failed to verify condition reason")
}
