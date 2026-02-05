// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
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
				Selectors: []selectors.NamespaceSelector{
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

			isSuccessfullyBoundAndUnsedToPool(pool1, claim1)
		})

		By("Create a second claim and verify binding", func() {
			err := k8sClient.Create(context.TODO(), claim2)
			Expect(err).Should(Succeed(), "Failed to create Claim %s", claim2)

			err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim2.Name, Namespace: claim2.Namespace}, claim2)
			Expect(err).Should(Succeed())

			isSuccessfullyBoundAndUnsedToPool(pool1, claim2)
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

			expectedPool := meta.LocalRFC1123ObjectReferenceWithUID{}
			Expect(claim.Status.Pool).To(Equal(expectedPool), "expected pool name to be empty")

			Expect(len(claim.Status.Conditions)).To(Equal(1), "expected single condition")
			Expect(len(claim.OwnerReferences)).To(Equal(0), "expected no ownerreferences")
			assigned := claim.Status.Conditions.GetConditionByType(meta.ReadyCondition)
			Expect(assigned.Status).To(Equal(metav1.ConditionFalse), "failed to verify condition status")
			Expect(assigned.Type).To(Equal(meta.ReadyCondition), "failed to verify condition type")
			Expect(assigned.Reason).To(Equal(meta.FailedReason), "failed to verify condition reason")
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
				Selectors: []selectors.NamespaceSelector{
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

			expectedPool := meta.LocalRFC1123ObjectReferenceWithUID{
				Name: meta.RFC1123Name(pool.Name),
				UID:  pool.GetUID(),
			}

			isBoundAndUnusedCondition(claim)
			Expect(claim.Status.Pool).To(Equal(expectedPool), "expected pool name to match")
		})

		By("Create a pod with resource requests/limits", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "claim-pod",
					Namespace: claim.Namespace,
					Labels: map[string]string{
						"e2e": "claim-pod",
					},
				},
				Spec: corev1.PodSpec{
					// optional: helps schedule quickly, avoid restarts
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "pause",
							Image: "registry.k8s.io/pause:3.9",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("16Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("20m"),
									corev1.ResourceMemory: resource.MustParse("32Mi"),
								},
							},
						},
					},
				},
			}

			Expect(k8sClient.Create(context.TODO(), pod)).To(Succeed())
		})

		By("Verify the claim is used", func() {
			time.Sleep(250 * time.Millisecond)

			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, claim)
			Expect(err).Should(Succeed())

			isBoundAndUsedCondition(claim)

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

		By("Make the claim unused", func() {
			key := client.ObjectKey{Name: "claim-pod", Namespace: claim.Namespace}

			pod := &corev1.Pod{}
			err := k8sClient.Get(context.TODO(), key, pod)
			Expect(err).To(Succeed(), "pod must exist before deleting")

			Expect(k8sClient.Delete(context.TODO(), pod, &client.DeleteOptions{
				GracePeriodSeconds: ptr.To(int64(0)),
			})).To(Succeed())

			Eventually(func() bool {
				p := &corev1.Pod{}
				err := k8sClient.Get(
					context.TODO(),
					key,
					p,
				)
				return apierrors.IsNotFound(err)
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())

		})

		By("Bind a claim", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, claim)
			Expect(err).Should(Succeed())

			expectedPool := meta.LocalRFC1123ObjectReferenceWithUID{
				Name: meta.RFC1123Name(pool.Name),
				UID:  pool.GetUID(),
			}

			isBoundAndUnusedCondition(claim)
			Expect(claim.Status.Pool).To(Equal(expectedPool), "expected pool name to match")
		})

		By("Allow on patching resources for claim (Increase)", func() {
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

		By("Allow on patching resources for claim (Decrease)", func() {
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

		By("Allow on patching pool name", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, claim)
			Expect(err).Should(Succeed())

			claim.Spec.Pool = "some-random-pool"

			err = k8sClient.Update(context.TODO(), claim)
			Expect(err).Should(Succeed(), "Expected error when updating resources in bound state %s", claim)
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
				Selectors: []selectors.NamespaceSelector{
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
				Selectors: []selectors.NamespaceSelector{
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

	assigned := cl.Status.Conditions.GetConditionByType(meta.ReadyCondition)

	Expect(assigned.Status).To(Equal(metav1.ConditionFalse), "failed to verify condition status")
	Expect(assigned.Type).To(Equal(meta.ReadyCondition), "failed to verify condition type")
	Expect(assigned.Reason).To(Equal(meta.FailedReason), "failed to verify condition reason")
}

func isBoundAndUnusedCondition(claim *capsulev1beta2.ResourcePoolClaim) {
	cl := &capsulev1beta2.ResourcePoolClaim{}
	err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, cl)
	Expect(err).Should(Succeed())

	bound := cl.Status.Conditions.GetConditionByType(meta.BoundCondition)

	Expect(bound.Type).To(Equal(meta.BoundCondition), "failed to verify condition type")
	Expect(bound.Reason).To(Equal(meta.UnusedReason), "failed to verify condition reason")
	Expect(bound.Status).To(Equal(metav1.ConditionFalse), "failed to verify condition status")
}

func isBoundAndUsedCondition(claim *capsulev1beta2.ResourcePoolClaim) {
	cl := &capsulev1beta2.ResourcePoolClaim{}
	err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, cl)
	Expect(err).Should(Succeed())

	bound := cl.Status.Conditions.GetConditionByType(meta.BoundCondition)

	Expect(bound.Status).To(Equal(metav1.ConditionTrue), "failed to verify condition status")
	Expect(bound.Type).To(Equal(meta.BoundCondition), "failed to verify condition type")
	Expect(bound.Reason).To(Equal(meta.InUseReason), "failed to verify condition reason")
}
