// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
	"github.com/projectcapsule/capsule/pkg/utils"
)

var _ = Describe("ResourcePool Tests", Ordered, Label("resourcepool", "pool"), func() {
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

	It("Assign Defaults correctly", func() {
		pool := &capsulev1beta2.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: "defaults-pool",
				Labels: map[string]string{
					"e2e-resourcepool": "test",
				},
			},
			Spec: capsulev1beta2.ResourcePoolSpec{
				Selectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"e2e.capsule.dev/test-suite": "defaults-pool",
							},
						},
					},
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"e2e.capsule.dev/test-suite": "defaults-pool",
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

		namespaces := []string{"ns-1-default-pool", "ns-2-default-pool", "ns-3-default-pool"}

		By("Create the ResourcePool", func() {
			err := k8sClient.Create(context.TODO(), pool)
			Expect(err).Should(Succeed(), "Failed to create ResourcePool %s", pool)
		})

		By("Get Applied revision", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, pool)
			Expect(err).Should(Succeed())
		})

		By("Has no Finalizer", func() {
			Expect(controllerutil.ContainsFinalizer(pool, meta.ControllerFinalizer)).To(BeFalse())
		})

		By("Verify Defaults were set", func() {
			Expect(pool.Spec.Defaults).To(BeNil())
		})

		By("Verify Status was correctly initialized", func() {
			Eventually(func(g Gomega) {
				expected := &capsulev1beta2.ResourcePoolQuotaStatus{
					Hard: pool.Spec.Quota.Hard,
					Claimed: corev1.ResourceList{
						corev1.ResourceLimitsCPU:      resource.MustParse("0"),
						corev1.ResourceLimitsMemory:   resource.MustParse("0"),
						corev1.ResourceRequestsCPU:    resource.MustParse("0"),
						corev1.ResourceRequestsMemory: resource.MustParse("0"),
					},
					Available: pool.Spec.Quota.Hard,
				}

				ok, msg := DeepCompare(*expected, pool.Status.Allocation)
				Expect(ok).To(BeTrue(), "Mismatch for expected status allocation: %s", msg)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("Create Namespaces, which are selected by the pool", func() {
			ns1 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-1-default-pool",
					Labels: map[string]string{
						"e2e-resourcepool":           "test",
						"e2e.capsule.dev/test-suite": "defaults-pool",
					},
				},
			}

			err := k8sClient.Create(context.TODO(), ns1)
			Expect(err).Should(Succeed())

			ns2 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-2-default-pool",
					Labels: map[string]string{
						"e2e-resourcepool":           "test",
						"e2e.capsule.dev/test-suite": "defaults-pool",
					},
				},
			}

			err = k8sClient.Create(context.TODO(), ns2)
			Expect(err).Should(Succeed())

			ns3 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-3-default-pool",
					Labels: map[string]string{
						"e2e-resourcepool":           "test",
						"e2e.capsule.dev/test-suite": "defaults-pool",
					},
				},
			}

			err = k8sClient.Create(context.TODO(), ns3)
			Expect(err).Should(Succeed())
		})

		By("Verify Namespaces are shown as allowed targets", func() {
			Eventually(func(g Gomega) {
				stat := &capsulev1beta2.ResourcePool{}
				err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, stat)
				g.Expect(err).Should(Succeed())

				g.Expect(stat.Status.Namespaces).To(ConsistOf(namespaces))
				g.Expect(stat.Status.NamespaceSize).To(Equal(uint(len(namespaces))))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("Verify ResourceQuotas for namespaces", func() {
			quotaLabel, err := utils.GetTypeLabel(&capsulev1beta2.ResourcePool{})
			Expect(err).Should(Succeed())

			for _, ns := range namespaces {
				rq := &corev1.ResourceQuota{}

				err := k8sClient.Get(context.TODO(), client.ObjectKey{
					Name:      pool.GetQuotaName(),
					Namespace: ns},
					rq)
				Expect(err).Should(Succeed())

				Expect(rq.ObjectMeta.Labels[quotaLabel]).To(Equal(pool.Name), "Expected "+quotaLabel+" to be set to "+pool.Name)

				Expect(rq.Spec.Hard).To(BeNil())

				found := false
				for _, ref := range rq.OwnerReferences {
					if ref.Kind == "ResourcePool" && ref.UID == pool.UID {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue(), "Expected ResourcePool to be owner of ResourceQuota in namespace %s", ns)
			}
		})
		By("Add Claims for namespaces", func() {
			claim1 := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-1",
					Namespace: "ns-1-default-pool",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceLimitsMemory: resource.MustParse("128Mi"),
					},
				},
			}

			Eventually(func() error {
				claim1.ResourceVersion = ""

				return k8sClient.Create(context.TODO(), claim1)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed(), "Failed to create Claim %s", claim1)

			isSuccessfullyBoundAndUnsedToPool(pool, claim1)

			claim2 := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-2",
					Namespace: "ns-2-default-pool",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceLimitsMemory: resource.MustParse("512Mi"),
					},
				},
			}

			Eventually(func() error {
				claim2.ResourceVersion = ""

				return k8sClient.Create(context.TODO(), claim2)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed(), "Failed to create Claim %s", claim2)

			isSuccessfullyBoundAndUnsedToPool(pool, claim2)

			claim3 := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-3",
					Namespace: "ns-3-default-pool",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceLimitsMemory: resource.MustParse("10Gi"),
					},
				},
			}

			Eventually(func() error {
				claim3.ResourceVersion = ""

				return k8sClient.Create(context.TODO(), claim3)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed(), "Failed to create Claim %s", claim3)

			Eventually(func(g Gomega) {
				fetchedPool := &capsulev1beta2.ResourcePool{}
				g.Expect(k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, fetchedPool)).To(Succeed())

				fetchedClaim := &capsulev1beta2.ResourcePoolClaim{}
				g.Expect(k8sClient.Get(context.TODO(), client.ObjectKey{
					Name:      claim3.Name,
					Namespace: claim3.Namespace,
				}, fetchedClaim)).To(Succeed())

				g.Expect(isNotBoundToPool(fetchedPool, fetchedClaim)).To(BeTrue())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("Verify Status was correctly initialized", func() {
			Eventually(func(g Gomega) {
				err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, pool)
				Expect(err).Should(Succeed())

				expected := &capsulev1beta2.ResourcePoolQuotaStatus{
					Hard: pool.Spec.Quota.Hard,
					Claimed: corev1.ResourceList{
						corev1.ResourceLimitsCPU:      resource.MustParse("0"),
						corev1.ResourceRequestsMemory: resource.MustParse("0"),
						corev1.ResourceRequestsCPU:    resource.MustParse("0"),
						corev1.ResourceLimitsMemory:   resource.MustParse("640Mi"),
					},
					Available: corev1.ResourceList{
						corev1.ResourceLimitsCPU:      resource.MustParse("2"),
						corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
						corev1.ResourceRequestsCPU:    resource.MustParse("2"),
						corev1.ResourceLimitsMemory:   resource.MustParse("1408Mi"),
					},
				}

				ok, msg := DeepCompare(*expected, pool.Status.Allocation)
				Expect(ok).To(BeTrue(), "Mismatch for expected status allocation: %s", msg)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		})

		By("Pool Has Finalizer", func() {
			Expect(controllerutil.ContainsFinalizer(pool, meta.ControllerFinalizer)).To(BeTrue())
		})

		By("Verify ResourceQuotas for namespaces", func() {
			status := map[string]corev1.ResourceList{
				"ns-1-default-pool": corev1.ResourceList{
					corev1.ResourceLimitsMemory: resource.MustParse("128Mi"),
				},
				"ns-2-default-pool": corev1.ResourceList{
					corev1.ResourceLimitsMemory: resource.MustParse("512Mi"),
				},
				"ns-3-default-pool": nil,
			}

			quotaLabel, err := utils.GetTypeLabel(&capsulev1beta2.ResourcePool{})
			Expect(err).Should(Succeed())

			for _, ns := range namespaces {
				rq := &corev1.ResourceQuota{}

				err := k8sClient.Get(context.TODO(), client.ObjectKey{
					Name:      pool.GetQuotaName(),
					Namespace: ns},
					rq)
				Expect(err).Should(Succeed())

				Expect(rq.ObjectMeta.Labels[quotaLabel]).To(Equal(pool.Name), "Expected "+quotaLabel+" to be set to "+pool.Name)

				ok, msg := DeepCompare(status[ns], rq.Spec.Hard)
				Expect(ok).To(BeTrue(), "Mismatch for resources for resourcequota: %s", msg)

				found := false
				for _, ref := range rq.OwnerReferences {
					if ref.Kind == "ResourcePool" && ref.UID == pool.UID {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue(), "Expected ResourcePool to be owner of ResourceQuota in namespace %s", ns)
			}
		})

		By("Update the ResourcePool", func() {
			pool.Spec.Defaults = corev1.ResourceList{
				corev1.ResourceLimitsCPU:       resource.MustParse("1"),
				corev1.ResourceLimitsMemory:    resource.MustParse("1Gi"),
				corev1.ResourceRequestsCPU:     resource.MustParse("1"),
				corev1.ResourceRequestsMemory:  resource.MustParse("1Gi"),
				corev1.ResourceRequestsStorage: resource.MustParse("5Gi"),
			}

			err := k8sClient.Update(context.TODO(), pool)
			Expect(err).Should(Succeed(), "Failed to update ResourcePool %s", pool)
		})

		By("Verify ResourceQuotas for namespaces", func() {
			status := map[string]corev1.ResourceList{

				"ns-1-default-pool": corev1.ResourceList{
					corev1.ResourceLimitsCPU:       resource.MustParse("1"),
					corev1.ResourceLimitsMemory:    resource.MustParse("1152Mi"),
					corev1.ResourceRequestsCPU:     resource.MustParse("1"),
					corev1.ResourceRequestsMemory:  resource.MustParse("1Gi"),
					corev1.ResourceRequestsStorage: resource.MustParse("5Gi"),
				},
				"ns-2-default-pool": corev1.ResourceList{
					corev1.ResourceLimitsCPU:       resource.MustParse("1"),
					corev1.ResourceLimitsMemory:    resource.MustParse("1536Mi"),
					corev1.ResourceRequestsCPU:     resource.MustParse("1"),
					corev1.ResourceRequestsMemory:  resource.MustParse("1Gi"),
					corev1.ResourceRequestsStorage: resource.MustParse("5Gi"),
				},
				"ns-3-default-pool": corev1.ResourceList{
					corev1.ResourceLimitsCPU:       resource.MustParse("1"),
					corev1.ResourceLimitsMemory:    resource.MustParse("1Gi"),
					corev1.ResourceRequestsCPU:     resource.MustParse("1"),
					corev1.ResourceRequestsMemory:  resource.MustParse("1Gi"),
					corev1.ResourceRequestsStorage: resource.MustParse("5Gi"),
				},
			}

			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, pool)
			Expect(err).Should(Succeed())

			for _, ns := range namespaces {
				rq := &corev1.ResourceQuota{}

				err := k8sClient.Get(context.TODO(), client.ObjectKey{
					Name:      pool.GetQuotaName(),
					Namespace: ns},
					rq)
				Expect(err).Should(Succeed())

				ok, msg := DeepCompare(status[ns], rq.Spec.Hard)
				Expect(ok).To(BeTrue(), "Mismatch for resources for resourcequota: %s", msg)
			}
		})

		By("Remove namespace from being selected (Patch Labels)", func() {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-2-default-pool",
				},
			}

			Eventually(func(g Gomega) {
				stat := &corev1.Namespace{}
				err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.Name}, stat)
				Expect(err).Should(Succeed())

				stat.ObjectMeta.Labels = map[string]string{
					"e2e-resourcepool":           "test",
					"e2e.capsule.dev/test-suite": "do-not-select",
				}

				err = k8sClient.Update(context.TODO(), stat)
				Expect(err).Should(Succeed())
			}).Should(Succeed())
		})

		By("Verify Namespaces were removed as allowed targets", func() {
			expected := []string{"ns-1-default-pool", "ns-3-default-pool"}

			Eventually(func(g Gomega) {
				stat := &capsulev1beta2.ResourcePool{}
				err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, stat)
				g.Expect(err).Should(Succeed())

				g.Expect(stat.Status.Namespaces).To(ConsistOf(expected))
				g.Expect(stat.Status.NamespaceSize).To(Equal(uint(len(expected))))
				g.Expect(stat.Status.ClaimSize).To(Equal(uint(1)))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("Verify ResourceQuota was cleaned up", func() {
			rq := &corev1.ResourceQuota{}
			Eventually(func() error {
				return k8sClient.Get(context.TODO(), client.ObjectKey{
					Name:      pool.GetQuotaName(),
					Namespace: "ns-2-default-pool",
				}, rq)
			}, "30s", "1s").ShouldNot(Succeed(), "Expected ResourceQuota to be deleted from namespace %s", "ns-2-default-pool")
		})

		By("Verify Status was correctly initialized", func() {

			expected := &capsulev1beta2.ResourcePoolQuotaStatus{
				Hard: pool.Spec.Quota.Hard,
				Claimed: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("0"),
					corev1.ResourceRequestsMemory: resource.MustParse("0"),
					corev1.ResourceRequestsCPU:    resource.MustParse("0"),
					corev1.ResourceLimitsMemory:   resource.MustParse("128Mi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("2"),
					corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("2"),
					corev1.ResourceLimitsMemory:   resource.MustParse("1920Mi"),
				},
			}

			Eventually(func(g Gomega) {
				stat := &capsulev1beta2.ResourcePool{}

				err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, stat)
				Expect(err).Should(Succeed())

				err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, stat)
				Expect(err).Should(Succeed())

				ok, msg := DeepCompare(*expected, stat.Status.Allocation)
				Expect(ok).To(BeTrue(), "Mismatch for expected status allocation: %s", msg)
			}).Should(Succeed())
		})

		By("Remove namespace from being selected (Delete Namespace)", func() {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-3-default-pool",
				},
			}

			err := k8sClient.Delete(context.TODO(), ns)
			Expect(err).Should(Succeed())
		})

		By("Get Applied revision", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, pool)
			Expect(err).Should(Succeed())
		})

		By("Verify Namespaces was removed as allowed targets", func() {
			expected := []string{"ns-1-default-pool"}

			Eventually(func(g Gomega) {
				current := &capsulev1beta2.ResourcePool{}

				err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, current)
				g.Expect(err).Should(Succeed())

				g.Expect(current.Status.Namespaces).To(ConsistOf(expected))
				g.Expect(current.Status.NamespaceSize).To(Equal(uint(len(expected))))
				g.Expect(current.Status.ClaimSize).To(Equal(uint(1)))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("Delete Resourcepool", func() {
			err := k8sClient.Delete(context.TODO(), pool)
			Expect(err).Should(Succeed())
		})

		By("Ensure ResourceQuotas are cleaned up", func() {
			for _, ns := range namespaces {
				rq := &corev1.ResourceQuota{}
				Eventually(func() error {
					return k8sClient.Get(context.TODO(), client.ObjectKey{
						Name:      pool.GetQuotaName(),
						Namespace: ns,
					}, rq)
				}, "30s", "1s").ShouldNot(Succeed(), "Expected ResourceQuota to be deleted from namespace %s", ns)
			}
		})
	})

	It("Assigns Defaults correctly (DefaultsZero)", func() {
		pool := &capsulev1beta2.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: "no-defaults-pool",
				Labels: map[string]string{
					"e2e-resourcepool": "test",
				},
			},
			Spec: capsulev1beta2.ResourcePoolSpec{
				Selectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"e2e.capsule.dev/test-suite": "no-defaults",
							},
						},
					},
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"e2e.capsule.dev/test-suite": "no-defaults",
							},
						},
					},
				},
				Config: capsulev1beta2.ResourcePoolSpecConfiguration{
					DefaultsAssignZero: ptr.To(true),
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

		namespaces := []string{"ns-1-zero-pool", "ns-2-zero-pool"}

		By("Create the ResourcePool", func() {
			err := k8sClient.Create(context.TODO(), pool)
			Expect(err).Should(Succeed(), "Failed to create ResourcePool %s", pool)
		})

		By("Get Applied revision", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, pool)
			Expect(err).Should(Succeed())
		})

		By("Verify Defaults are empty", func() {
			expected := corev1.ResourceList{
				corev1.ResourceLimitsCPU:      resource.MustParse("0"),
				corev1.ResourceLimitsMemory:   resource.MustParse("0"),
				corev1.ResourceRequestsCPU:    resource.MustParse("0"),
				corev1.ResourceRequestsMemory: resource.MustParse("0"),
			}

			Expect(pool.Spec.Defaults).To(Equal(expected))
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
				Available: pool.Spec.Quota.Hard,
			}

			ok, msg := DeepCompare(*expected, pool.Status.Allocation)
			Expect(ok).To(BeTrue(), "Mismatch for expected status allocation: %s", msg)
		})

		By("Create Namespaces, which are selected by the pool", func() {
			ns1 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-1-zero-pool",
					Labels: map[string]string{
						"e2e-resourcepool":           "test",
						"e2e.capsule.dev/test-suite": "no-defaults",
					},
				},
			}

			err := k8sClient.Create(context.TODO(), ns1)
			Expect(err).Should(Succeed())

			ns2 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-2-zero-pool",
					Labels: map[string]string{
						"e2e-resourcepool":           "test",
						"e2e.capsule.dev/test-suite": "no-defaults",
					},
				},
			}

			err = k8sClient.Create(context.TODO(), ns2)
			Expect(err).Should(Succeed())
		})

		By("Verify Namespaces are shown as allowed targets", func() {
			Eventually(func(g Gomega) {
				stat := &capsulev1beta2.ResourcePool{}
				err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, stat)
				g.Expect(err).Should(Succeed())

				g.Expect(stat.Status.Namespaces).To(ConsistOf(namespaces))
				g.Expect(stat.Status.NamespaceSize).To(Equal(uint(2)))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("Verify ResourceQuotas for namespaces", func() {
			resources := corev1.ResourceList{
				corev1.ResourceLimitsCPU:      resource.MustParse("0"),
				corev1.ResourceLimitsMemory:   resource.MustParse("0"),
				corev1.ResourceRequestsCPU:    resource.MustParse("0"),
				corev1.ResourceRequestsMemory: resource.MustParse("0"),
			}

			quotaLabel, err := utils.GetTypeLabel(&capsulev1beta2.ResourcePool{})
			Expect(err).Should(Succeed())

			for _, ns := range namespaces {
				rq := &corev1.ResourceQuota{}

				err := k8sClient.Get(context.TODO(), client.ObjectKey{
					Name:      pool.GetQuotaName(),
					Namespace: ns},
					rq)
				Expect(err).Should(Succeed())

				Expect(rq.ObjectMeta.Labels[quotaLabel]).To(Equal(pool.Name), "Expected "+quotaLabel+" to be set to "+pool.Name)

				ok, msg := DeepCompare(resources, rq.Spec.Hard)
				Expect(ok).To(BeTrue(), "Mismatch for resources for resourcequota: %s", msg)

				found := false
				for _, ref := range rq.OwnerReferences {
					if ref.Kind == "ResourcePool" && ref.UID == pool.UID {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue(), "Expected ResourcePool to be owner of ResourceQuota in namespace %s", ns)

			}
		})

		By("Delete Resourcepool", func() {
			err := k8sClient.Delete(context.TODO(), pool)
			Expect(err).Should(Succeed())
		})

		By("Ensure ResourceQuotas are cleaned up", func() {
			for _, ns := range namespaces {
				rq := &corev1.ResourceQuota{}
				Eventually(func() error {
					return k8sClient.Get(context.TODO(), client.ObjectKey{
						Name:      pool.GetQuotaName(),
						Namespace: ns,
					}, rq)
				}, "30s", "1s").ShouldNot(Succeed(), "Expected ResourceQuota to be deleted from namespace %s", ns)
			}
		})

	})

	It("ResourcePool Scheduling - Unordered", func() {
		pool := &capsulev1beta2.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: "unordered-scheduling",
				Labels: map[string]string{
					"e2e-resourcepool": "test",
				},
			},
			Spec: capsulev1beta2.ResourcePoolSpec{
				Selectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"e2e.capsule.dev/test-suite": "unordered-scheduling",
							},
						},
					},
				},
				Config: capsulev1beta2.ResourcePoolSpecConfiguration{
					OrderedQueue: ptr.To(false),
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
			err := k8sClient.Create(context.TODO(), pool)
			Expect(err).Should(Succeed(), "Failed to create ResourcePool %s", pool)
		})

		By("Create source namespaces", func() {
			ns1 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-1-pool-unordered",
					Labels: map[string]string{
						"e2e-resourcepool":           "test",
						"e2e.capsule.dev/test-suite": "unordered-scheduling",
					},
				},
			}

			err := k8sClient.Create(context.TODO(), ns1)
			Expect(err).Should(Succeed(), "Failed to create Namespace %s", ns1)

			ns2 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-2-pool-unordered",
					Labels: map[string]string{
						"e2e-resourcepool":           "test",
						"e2e.capsule.dev/test-suite": "unordered-scheduling",
					},
				},
			}

			err = k8sClient.Create(context.TODO(), ns2)
			Expect(err).Should(Succeed(), "Failed to create Namespace %s", ns2)

		})

		By("Create claim for limits.memory", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-1",
					Namespace: "ns-1-pool-unordered",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceLimitsMemory: resource.MustParse("128Mi"),
					},
				},
			}

			err := k8sClient.Create(context.TODO(), claim)
			Expect(err).Should(Succeed(), "Failed to create Claim %s", claim)

			isSuccessfullyBoundAndUnsedToPool(pool, claim)
		})

		By("Verify Status was correctly initialized", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, pool)
			Expect(err).Should(Succeed())

			expected := &capsulev1beta2.ResourcePoolQuotaStatus{
				Hard: pool.Spec.Quota.Hard,
				Claimed: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("0"),
					corev1.ResourceRequestsMemory: resource.MustParse("0"),
					corev1.ResourceRequestsCPU:    resource.MustParse("0"),
					corev1.ResourceLimitsMemory:   resource.MustParse("128Mi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("2"),
					corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("2"),
					corev1.ResourceLimitsMemory:   resource.MustParse("1920Mi"),
				},
			}

			ok, msg := DeepCompare(*expected, pool.Status.Allocation)
			Expect(ok).To(BeTrue(), "Mismatch for expected status allocation: %s", msg)
		})

		By("Verify ResourceQuota", func() {
			rqHardResources := corev1.ResourceList{
				corev1.ResourceLimitsMemory: resource.MustParse("128Mi"),
			}

			rq := &corev1.ResourceQuota{}

			err := k8sClient.Get(context.TODO(), client.ObjectKey{
				Name:      pool.GetQuotaName(),
				Namespace: "ns-1-pool-unordered"},
				rq)
			Expect(err).Should(Succeed())

			ok, msg := DeepCompare(rqHardResources, rq.Spec.Hard)
			Expect(ok).To(BeTrue(), "Mismatch for resources for resourcequota: %s", msg)
		})

		By("Create claim exhausting requests.cpu", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-2",
					Namespace: "ns-1-pool-unordered",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceRequestsCPU: resource.MustParse("4"),
					},
				},
			}

			err := k8sClient.Create(context.TODO(), claim)
			Expect(err).Should(Succeed(), "Failed to create Claim %s", claim)

			Expect(isNotBoundToPool(pool, claim)).To(BeTrue())

			err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, claim)
			Expect(err).Should(Succeed())

			expected := []string{
				"requested.requests.cpu=4",
				"available.requests.cpu=2",
			}

			exhausted := claim.Status.Conditions.GetConditionByType(meta.ExhaustedCondition)
			Expect(containsAll(extractResourcePoolMessage(exhausted.Message), expected)).To(BeTrue(), "Actual message"+exhausted.Message)
			Expect(exhausted.Reason).To(Equal(meta.PoolExhaustedReason))
			Expect(exhausted.Status).To(Equal(metav1.ConditionTrue))
			Expect(exhausted.Type).To(Equal(meta.ExhaustedCondition))
		})

		By("Create claim for request.memory", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-3",
					Namespace: "ns-2-pool-unordered",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceRequestsMemory: resource.MustParse("1Gi"),
					},
				},
			}

			err := k8sClient.Create(context.TODO(), claim)
			Expect(err).Should(Succeed(), "Failed to create Claim %s", claim)

			isSuccessfullyBoundAndUnsedToPool(pool, claim)
		})

		By("Verify Status was correctly initialized", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, pool)
			Expect(err).Should(Succeed())

			expected := &capsulev1beta2.ResourcePoolQuotaStatus{
				Hard: pool.Spec.Quota.Hard,
				Claimed: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("0"),
					corev1.ResourceLimitsMemory:   resource.MustParse("128Mi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("0"),
					corev1.ResourceRequestsMemory: resource.MustParse("1Gi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("2"),
					corev1.ResourceLimitsMemory:   resource.MustParse("1920Mi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("2"),
					corev1.ResourceRequestsMemory: resource.MustParse("1Gi"),
				},
			}

			ok, msg := DeepCompare(*expected, pool.Status.Allocation)
			Expect(ok).To(BeTrue(), "Mismatch for expected status allocation: %s", msg)
		})

		By("Create claim for requests.cpu (skip exhausting one)", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-4",
					Namespace: "ns-2-pool-unordered",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceRequestsCPU: resource.MustParse("2"),
					},
				},
			}

			err := k8sClient.Create(context.TODO(), claim)
			Expect(err).Should(Succeed(), "Failed to create Claim %s", claim)

			isSuccessfullyBoundAndUnsedToPool(pool, claim)
		})

		By("Verify Status was correctly initialized", func() {
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, pool)
			Expect(err).Should(Succeed())

			expected := &capsulev1beta2.ResourcePoolQuotaStatus{
				Hard: pool.Spec.Quota.Hard,
				Claimed: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("0"),
					corev1.ResourceLimitsMemory:   resource.MustParse("128Mi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("2"),
					corev1.ResourceRequestsMemory: resource.MustParse("1Gi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("2"),
					corev1.ResourceLimitsMemory:   resource.MustParse("1920Mi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("0"),
					corev1.ResourceRequestsMemory: resource.MustParse("1Gi"),
				},
			}

			ok, msg := DeepCompare(*expected, pool.Status.Allocation)
			Expect(ok).To(BeTrue(), "Mismatch for expected status allocation: %s", msg)
		})

		By("Reverify claim exhausting requests.cpu", func() {
			expected := []string{
				"requested.requests.cpu=4",
				"available.requests.cpu=0",
			}

			Eventually(func(g Gomega) {
				fetchedPool := &capsulev1beta2.ResourcePool{}
				g.Expect(k8sClient.Get(
					context.TODO(),
					client.ObjectKey{Name: pool.Name},
					fetchedPool,
				)).To(Succeed())

				claim := &capsulev1beta2.ResourcePoolClaim{}
				g.Expect(k8sClient.Get(
					context.TODO(),
					client.ObjectKey{Name: "simple-2", Namespace: "ns-1-pool-unordered"},
					claim,
				)).To(Succeed())

				g.Expect(fetchedPool.GetClaimFromStatus(claim)).To(BeNil())

				exhausted := claim.Status.Conditions.GetConditionByType(meta.ExhaustedCondition)
				g.Expect(exhausted).NotTo(BeNil(), "Exhausted condition should be present")

				g.Expect(containsAll(
					extractResourcePoolMessage(exhausted.Message),
					expected,
				)).To(BeTrue(), "Actual message: %s", exhausted.Message)

				g.Expect(exhausted.Reason).To(Equal(meta.PoolExhaustedReason))
				g.Expect(exhausted.Status).To(Equal(metav1.ConditionTrue))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
	})

	It("ResourcePool Scheduling - Ordered", func() {
		pool := &capsulev1beta2.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ordered-scheduling",
				Labels: map[string]string{
					"e2e-resourcepool": "test",
				},
			},
			Spec: capsulev1beta2.ResourcePoolSpec{
				Selectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"e2e.capsule.dev/test-suite": "ordered-scheduling",
							},
						},
					},
				},
				Config: capsulev1beta2.ResourcePoolSpecConfiguration{
					OrderedQueue: ptr.To(true),
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
			err := k8sClient.Create(context.TODO(), pool)
			Expect(err).Should(Succeed(), "Failed to create ResourcePool %s", pool)
		})

		By("Create source namespaces", func() {
			ns1 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-1-pool-ordered",
					Labels: map[string]string{
						"e2e-resourcepool":           "test",
						"e2e.capsule.dev/test-suite": "ordered-scheduling",
					},
				},
			}

			err := k8sClient.Create(context.TODO(), ns1)
			Expect(err).Should(Succeed(), "Failed to create Namespace %s", ns1)

			ns2 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-2-pool-ordered",
					Labels: map[string]string{
						"e2e-resourcepool":           "test",
						"e2e.capsule.dev/test-suite": "ordered-scheduling",
					},
				},
			}

			err = k8sClient.Create(context.TODO(), ns2)
			Expect(err).Should(Succeed(), "Failed to create Namespace %s", ns2)

		})

		By("Create claim for limits.memory", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-1",
					Namespace: "ns-1-pool-ordered",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceLimitsMemory: resource.MustParse("512Mi"),
					},
				},
			}

			err := k8sClient.Create(context.TODO(), claim)
			Expect(err).Should(Succeed(), "Failed to create Claim %s", claim)

			isSuccessfullyBoundAndUnsedToPool(pool, claim)
		})

		By("Create claim for requests.requests", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-1",
					Namespace: "ns-2-pool-ordered",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceRequestsMemory: resource.MustParse("750Mi"),
					},
				},
			}

			err := k8sClient.Create(context.TODO(), claim)
			Expect(err).Should(Succeed(), "Failed to create Claim %s", claim)

			isSuccessfullyBoundAndUnsedToPool(pool, claim)
		})

		By("Verify Status was correctly initialized", func() {
			expected := &capsulev1beta2.ResourcePoolQuotaStatus{
				Hard: pool.Spec.Quota.Hard,
				Claimed: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("0"),
					corev1.ResourceRequestsMemory: resource.MustParse("750Mi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("0"),
					corev1.ResourceLimitsMemory:   resource.MustParse("512Mi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("2"),
					corev1.ResourceRequestsMemory: resource.MustParse("1298Mi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("2"),
					corev1.ResourceLimitsMemory:   resource.MustParse("1536Mi"),
				},
			}

			Eventually(func(g Gomega) {
				current := &capsulev1beta2.ResourcePool{}
				g.Expect(k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, current)).To(Succeed())

				ok, msg := DeepCompare(*expected, current.Status.Allocation)
				g.Expect(ok).To(BeTrue(), "Mismatch for expected status allocation: %s", msg)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("Create claim exhausting requests.cpu", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-2",
					Namespace: "ns-2-pool-ordered",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceRequestsCPU: resource.MustParse("4"),
					},
				},
			}

			Eventually(func() error {
				claim.ResourceVersion = ""
				return k8sClient.Create(context.TODO(), claim)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed(), "Failed to create Claim %s", claim)

			assertClaimExhausted(pool, claim, meta.PoolExhaustedReason, []string{
				"requested.requests.cpu=4",
				"available.requests.cpu=2",
			})
		})

		By("Create claim exhausting limits.cpu", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-3",
					Namespace: "ns-1-pool-ordered",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceLimitsCPU: resource.MustParse("4"),
					},
				},
			}

			Eventually(func() error {
				claim.ResourceVersion = ""
				return k8sClient.Create(context.TODO(), claim)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed(), "Failed to create Claim %s", claim)

			assertClaimExhausted(pool, claim, meta.PoolExhaustedReason, []string{
				"requested.limits.cpu=4",
				"available.limits.cpu=2",
			})
		})

		By("Create claim for requests.cpu (attempt to skip exhausting one)", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-4",
					Namespace: "ns-2-pool-ordered",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceLimitsCPU:   resource.MustParse("2"),
						corev1.ResourceRequestsCPU: resource.MustParse("2"),
					},
				},
			}

			Eventually(func() error {
				claim.ResourceVersion = ""
				return k8sClient.Create(context.TODO(), claim)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed(), "Failed to create Claim %s", claim)

			assertClaimExhausted(pool, claim, meta.QueueExhaustedReason, []string{
				"requested.limits.cpu=2",
				"queued.limits.cpu=4",
				"requested.requests.cpu=2",
				"queued.requests.cpu=4",
			})
		})

		By("Verify ResourceQuotas for namespaces", func() {
			status := map[string]corev1.ResourceList{
				"ns-1-pool-ordered": {
					corev1.ResourceLimitsMemory: resource.MustParse("512Mi"),
				},
				"ns-2-pool-ordered": {
					corev1.ResourceRequestsMemory: resource.MustParse("750Mi"),
				},
			}

			Eventually(func(g Gomega) {
				for ns, expected := range status {
					rq := &corev1.ResourceQuota{}

					err := k8sClient.Get(context.TODO(), client.ObjectKey{
						Name:      pool.GetQuotaName(),
						Namespace: ns,
					}, rq)
					g.Expect(err).Should(Succeed())

					ok, msg := DeepCompare(expected, rq.Spec.Hard)
					g.Expect(ok).To(BeTrue(), "Mismatch for resources for resourcequota: %s", msg)
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("Allocate more resources to Resourcepool (requests.cpu)", func() {
			Eventually(func() error {
				current := &capsulev1beta2.ResourcePool{}
				if err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, current); err != nil {
					return err
				}

				current.Spec.Quota.Hard = corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("4"),
					corev1.ResourceLimitsMemory:   resource.MustParse("2Gi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("4"),
					corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
				}

				return k8sClient.Update(context.TODO(), current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("Verify Status was correctly initialized", func() {
			expectedHard := corev1.ResourceList{
				corev1.ResourceLimitsCPU:      resource.MustParse("4"),
				corev1.ResourceLimitsMemory:   resource.MustParse("2Gi"),
				corev1.ResourceRequestsCPU:    resource.MustParse("4"),
				corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
			}

			expected := &capsulev1beta2.ResourcePoolQuotaStatus{
				Hard: expectedHard,
				Claimed: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("4"),
					corev1.ResourceRequestsMemory: resource.MustParse("750Mi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("4"),
					corev1.ResourceLimitsMemory:   resource.MustParse("512Mi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("0"),
					corev1.ResourceRequestsMemory: resource.MustParse("1298Mi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("0"),
					corev1.ResourceLimitsMemory:   resource.MustParse("1536Mi"),
				},
			}

			Eventually(func(g Gomega) {
				current := &capsulev1beta2.ResourcePool{}
				g.Expect(k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, current)).To(Succeed())

				ok, msg := DeepCompare(*expected, current.Status.Allocation)
				g.Expect(ok).To(BeTrue(), "Mismatch for expected status allocation: %s", msg)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("Verify queued claim can be allocated", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{}

			Eventually(func() error {
				return k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: "simple-2", Namespace: "ns-2-pool-ordered"},
					claim,
				)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			isSuccessfullyBoundAndUnsedToPool(pool, claim)
		})

		By("Verify queued claim can be allocated", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{}

			Eventually(func() error {
				return k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: "simple-3", Namespace: "ns-1-pool-ordered"},
					claim,
				)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			isSuccessfullyBoundAndUnsedToPool(pool, claim)
		})

		By("Verify ResourceQuotas for namespaces", func() {
			status := map[string]corev1.ResourceList{
				"ns-1-pool-ordered": {
					corev1.ResourceLimitsMemory: resource.MustParse("512Mi"),
					corev1.ResourceLimitsCPU:    resource.MustParse("4"),
				},
				"ns-2-pool-ordered": {
					corev1.ResourceRequestsMemory: resource.MustParse("750Mi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("4"),
				},
			}

			Eventually(func(g Gomega) {
				for ns, expected := range status {
					rq := &corev1.ResourceQuota{}

					err := k8sClient.Get(context.TODO(), client.ObjectKey{
						Name:      pool.GetQuotaName(),
						Namespace: ns,
					}, rq)
					g.Expect(err).Should(Succeed())

					ok, msg := DeepCompare(expected, rq.Spec.Hard)
					g.Expect(ok).To(BeTrue(), "Mismatch for resources for resourcequota: %s", msg)
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("Verify moved up in queue", func() {
			expected := []string{
				"requested.limits.cpu=2",
				"available.limits.cpu=0",
				"requested.requests.cpu=2",
				"available.requests.cpu=0",
			}

			Eventually(func(g Gomega) {
				currentPool := &capsulev1beta2.ResourcePool{}
				g.Expect(k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, currentPool)).To(Succeed())

				claim := &capsulev1beta2.ResourcePoolClaim{}
				g.Expect(k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: "simple-4", Namespace: "ns-2-pool-ordered"},
					claim,
				)).To(Succeed())

				g.Expect(currentPool.GetClaimFromStatus(claim)).To(BeNil())

				exhausted := claim.Status.Conditions.GetConditionByType(meta.ExhaustedCondition)
				g.Expect(exhausted).NotTo(BeNil(), "Exhausted condition should be present")

				g.Expect(containsAll(
					extractResourcePoolMessage(exhausted.Message),
					expected,
				)).To(BeTrue(), "Actual message: %s", exhausted.Message)

				g.Expect(exhausted.Reason).To(Equal(meta.PoolExhaustedReason))
				g.Expect(exhausted.Status).To(Equal(metav1.ConditionTrue))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
	})

	It("ResourcePool - Namespace Selection", func() {
		pool := &capsulev1beta2.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: "bind-ns-pool-1",
				Labels: map[string]string{
					"e2e-resourcepool": "test",
				},
			},
			Spec: capsulev1beta2.ResourcePoolSpec{
				Selectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"e2e.capsule.dev/test-suite": "bind-namespaces",
							},
						},
					},
				},
				Config: capsulev1beta2.ResourcePoolSpecConfiguration{
					DeleteBoundResources: ptr.To(false),
				},
				Quota: corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{
						corev1.ResourceLimitsMemory: resource.MustParse("2Gi"),
					},
				},
			},
		}

		By("Create the ResourcePool", func() {
			EventuallyCreation(func() error {
				pool.ResourceVersion = ""
				return k8sClient.Create(context.TODO(), pool)
			}).Should(Succeed(), "Failed to create ResourcePool %s", pool)
		})

		By("Create source namespaces", func() {
			ns1 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-1-pool-bind",
					Labels: map[string]string{
						"e2e-resourcepool":           "test",
						"e2e.capsule.dev/test-suite": "bind-namespaces",
					},
				},
			}

			EventuallyCreation(func() error {
				ns1.ResourceVersion = ""
				return k8sClient.Create(context.TODO(), ns1)
			}).Should(Succeed(), "Failed to create Namespace %s", ns1)

			ns2 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-2-pool-bind",
					Labels: map[string]string{
						"e2e-resourcepool":           "test",
						"e2e.capsule.dev/test-suite": "bind-namespaces-no",
					},
				},
			}

			EventuallyCreation(func() error {
				ns2.ResourceVersion = ""
				return k8sClient.Create(context.TODO(), ns2)
			}).Should(Succeed(), "Failed to create Namespace %s", ns2)
		})

		By("Verify only matching namespaces", func() {
			expected := []string{"ns-1-pool-bind"}

			Eventually(func(g Gomega) {
				stat := &capsulev1beta2.ResourcePool{}
				err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, stat)
				g.Expect(err).Should(Succeed())

				g.Expect(stat.Status.Namespaces).To(ConsistOf(expected))
				g.Expect(stat.Status.NamespaceSize).To(Equal(uint(1)))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("Create claim in matching namespace", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-1",
					Namespace: "ns-1-pool-bind",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceLimitsMemory: resource.MustParse("128Mi"),
					},
				},
			}

			EventuallyCreation(func() error {
				claim.ResourceVersion = ""
				return k8sClient.Create(context.TODO(), claim)
			}).Should(Succeed(), "Failed to create Claim %s", claim)

			isSuccessfullyBoundAndUnsedToPool(pool, claim)
		})

		By("Create claim non matching namespace", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-1",
					Namespace: "ns-2-pool-bind",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					Pool: "bind-ns-pool-1",
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceLimitsMemory: resource.MustParse("128Mi"),
					},
				},
			}

			EventuallyCreation(func() error {
				claim.ResourceVersion = ""
				return k8sClient.Create(context.TODO(), claim)
			}).Should(Succeed(), "Failed to create Claim %s", claim)

			Eventually(func(g Gomega) {
				currentPool := &capsulev1beta2.ResourcePool{}
				g.Expect(k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, currentPool)).To(Succeed())

				currentClaim := &capsulev1beta2.ResourcePoolClaim{}
				g.Expect(k8sClient.Get(context.TODO(), client.ObjectKey{
					Name:      claim.Name,
					Namespace: claim.Namespace,
				}, currentClaim)).To(Succeed())

				g.Expect(currentPool.GetClaimFromStatus(currentClaim)).To(BeNil())

				assigned := currentClaim.Status.Conditions.GetConditionByType(meta.ReadyCondition)
				g.Expect(assigned).NotTo(BeNil(), "Ready condition should be present")

				g.Expect(assigned.Reason).To(Equal(meta.FailedReason))
				g.Expect(assigned.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(assigned.Type).To(Equal(meta.ReadyCondition))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("Update Namespace Labels to become matching", func() {
			Eventually(func() error {
				ns := &corev1.Namespace{}
				if err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: "ns-2-pool-bind"}, ns); err != nil {
					return err
				}

				if ns.Labels == nil {
					ns.Labels = map[string]string{}
				}

				ns.Labels["e2e-resourcepool"] = "test"
				ns.Labels["e2e.capsule.dev/test-suite"] = "bind-namespaces"

				return k8sClient.Update(context.TODO(), ns)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("Reverify claim in namespace", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{}

			Eventually(func() error {
				return k8sClient.Get(context.TODO(), client.ObjectKey{
					Name:      "simple-1",
					Namespace: "ns-2-pool-bind",
				}, claim)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			isSuccessfullyBoundAndUnsedToPool(pool, claim)
		})
	})

	It("ResourcePool Deletion - Not Cascading", func() {
		pool := &capsulev1beta2.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: "deletion-pool-1",
				Labels: map[string]string{
					"e2e-resourcepool": "test",
				},
			},
			Spec: capsulev1beta2.ResourcePoolSpec{
				Selectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"e2e.capsule.dev/test-suite": "delete-bound-resources",
							},
						},
					},
				},
				Config: capsulev1beta2.ResourcePoolSpecConfiguration{
					DeleteBoundResources: ptr.To(false),
				},
				Quota: corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{
						corev1.ResourceLimitsMemory: resource.MustParse("2Gi"),
					},
				},
			},
		}

		By("Create the ResourcePool", func() {
			err := k8sClient.Create(context.TODO(), pool)
			Expect(err).Should(Succeed(), "Failed to create ResourcePool %s", pool)
		})

		By("Unbinding From Pool", func() {
			claim1 := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-1",
					Namespace: "ns-1-pool-no-deletion",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceLimitsMemory: resource.MustParse("128Mi"),
					},
				},
			}

			claim2 := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-2",
					Namespace: "ns-2-pool-no-deletion",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceLimitsMemory: resource.MustParse("128Mi"),
					},
				},
			}

			ns1 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: claim1.Namespace,
					Labels: map[string]string{
						"e2e-resourcepool":           "test",
						"e2e.capsule.dev/test-suite": "delete-bound-resources",
					},
				},
			}

			ns2 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: claim2.Namespace,
					Labels: map[string]string{
						"e2e-resourcepool":           "test",
						"e2e.capsule.dev/test-suite": "delete-bound-resources",
					},
				},
			}

			EventuallyCreation(ns1)
			EventuallyCreation(ns2)
			EventuallyCreation(claim1)
			EventuallyCreation(claim2)
			isBoundToPool(pool, claim1)
			isBoundToPool(pool, claim2)

			err := k8sClient.Delete(context.TODO(), pool)
			Expect(err).Should(Succeed(), "Failed to delete Pool %s", claim1)

			Eventually(func() error {
				return k8sClient.Get(
					context.TODO(),
					client.ObjectKey{Name: claim1.Name, Namespace: claim1.Namespace},
					&capsulev1beta2.ResourcePoolClaim{},
				)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed(), "Expected claim1 to be present")

			Eventually(func() error {
				return k8sClient.Get(
					context.TODO(),
					client.ObjectKey{Name: claim2.Name, Namespace: claim2.Namespace},
					&capsulev1beta2.ResourcePoolClaim{},
				)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed(), "Expected claim2 to be present")

			Eventually(func() bool {
				err := k8sClient.Get(
					context.TODO(),
					client.ObjectKey{Name: pool.Name},
					&capsulev1beta2.ResourcePool{},
				)

				return apierrors.IsNotFound(err)
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue(), "Expected pool to be gone")
		})
	})

	It("ResourcePool Deletion - Cascading (DeleteBoundResources)", func() {
		pool := &capsulev1beta2.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: "deletion-pool",
				Labels: map[string]string{
					"e2e-resourcepool": "test",
				},
			},
			Spec: capsulev1beta2.ResourcePoolSpec{
				Selectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"e2e.capsule.dev/test-suite": "delete-bound-resources",
							},
						},
					},
				},
				Config: capsulev1beta2.ResourcePoolSpecConfiguration{
					DeleteBoundResources: ptr.To(true),
				},
				Quota: corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{
						corev1.ResourceLimitsMemory: resource.MustParse("2Gi"),
					},
				},
			},
		}

		By("Create the ResourcePool", func() {
			err := k8sClient.Create(context.TODO(), pool)
			Expect(err).Should(Succeed(), "Failed to create ResourcePool %s", pool)
		})

		By("Cascading Deletion", func() {
			claim1 := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-1",
					Namespace: "ns-1-pool-deletion",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceLimitsMemory: resource.MustParse("128Mi"),
					},
				},
			}

			claim2 := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-2",
					Namespace: "ns-2-pool-deletion",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceLimitsMemory: resource.MustParse("128Mi"),
					},
				},
			}

			ns1 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: claim1.Namespace,
					Labels: map[string]string{
						"e2e-resourcepool":           "test",
						"e2e.capsule.dev/test-suite": "delete-bound-resources",
					},
				},
			}

			ns2 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: claim2.Namespace,
					Labels: map[string]string{
						"e2e-resourcepool":           "test",
						"e2e.capsule.dev/test-suite": "delete-bound-resources",
					},
				},
			}
			EventuallyCreation(ns1)
			EventuallyCreation(ns2)
			EventuallyCreation(claim1)
			EventuallyCreation(claim2)

			isBoundToPool(pool, claim1)
			isBoundToPool(pool, claim2)

			err := k8sClient.Delete(context.TODO(), pool)
			Expect(err).Should(Succeed(), "Failed to delete Pool %s", claim1)

			Eventually(func() error {
				return k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(claim1), &capsulev1beta2.ResourcePoolClaim{})
			}).ShouldNot(Succeed(), "Expected claim1 to be gone")

			Eventually(func() error {
				return k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(claim2), &capsulev1beta2.ResourcePoolClaim{})
			}).ShouldNot(Succeed(), "Expected claim2 to be gone")

			Eventually(func() error {
				return k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(pool), &capsulev1beta2.ResourcePoolClaim{})
			}).ShouldNot(Succeed(), "Expected pool to be gone")
		})
	})

	It("Admission Guards ", func() {
		pool := &capsulev1beta2.ResourcePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: "admission-pool",
				Labels: map[string]string{
					"e2e-resourcepool": "test",
				},
			},
			Spec: capsulev1beta2.ResourcePoolSpec{
				Selectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"e2e.capsule.dev/test-suite": "admission",
							},
						},
					},
				},
				Config: capsulev1beta2.ResourcePoolSpecConfiguration{
					DefaultsAssignZero: ptr.To(true),
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
			err := k8sClient.Create(context.TODO(), pool)
			Expect(err).Should(Succeed(), "Failed to create ResourcePool %s", pool)
		})

		By("Create Namespaces, which are selected by the pool", func() {
			ns1 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-1-admission-pool",
					Labels: map[string]string{
						"e2e-resourcepool":           "test",
						"e2e.capsule.dev/test-suite": "admission",
					},
				},
			}

			err := k8sClient.Create(context.TODO(), ns1)
			Expect(err).Should(Succeed())

			ns2 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ns-2-admission-pool",
					Labels: map[string]string{
						"e2e-resourcepool":           "test",
						"e2e.capsule.dev/test-suite": "admission",
					},
				},
			}

			err = k8sClient.Create(context.TODO(), ns2)
			Expect(err).Should(Succeed())
		})

		By("Create claims", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-1",
					Namespace: "ns-1-admission-pool",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceRequestsMemory: resource.MustParse("512Mi"),
					},
				},
			}

			err := k8sClient.Create(context.TODO(), claim)
			Expect(err).Should(Succeed(), "Failed to create Claim %s", claim)

			isSuccessfullyBoundAndUnsedToPool(pool, claim)
		})

		By("Create claims", func() {
			claim := &capsulev1beta2.ResourcePoolClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-2",
					Namespace: "ns-2-admission-pool",
				},
				Spec: capsulev1beta2.ResourcePoolClaimSpec{
					ResourceClaims: corev1.ResourceList{
						corev1.ResourceLimitsMemory: resource.MustParse("1Gi"),
					},
				},
			}

			err := k8sClient.Create(context.TODO(), claim)
			Expect(err).Should(Succeed(), "Failed to create Claim %s", claim)

			isSuccessfullyBoundAndUnsedToPool(pool, claim)
		})

		By("Verify ResourcePool Status Allocation", func() {
			expectedAllocation := capsulev1beta2.ResourcePoolQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("2"),
					corev1.ResourceLimitsMemory:   resource.MustParse("2Gi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("2"),
					corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
				},
				Claimed: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("0"),
					corev1.ResourceLimitsMemory:   resource.MustParse("1Gi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("0"),
					corev1.ResourceRequestsMemory: resource.MustParse("512Mi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("2"),
					corev1.ResourceLimitsMemory:   resource.MustParse("1Gi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("2"),
					corev1.ResourceRequestsMemory: resource.MustParse("1536Mi"),
				},
			}

			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, pool)
			Expect(err).Should(Succeed())

			ok, msg := DeepCompare(expectedAllocation, pool.Status.Allocation)
			Expect(ok).To(BeTrue(), "Mismatch for resource allocation: %s", msg)
		})

		By("Allow increasing the size of the pool", func() {
			UpdatePoolEventually(pool.Name, corev1.ResourceList{
				corev1.ResourceLimitsCPU:      resource.MustParse("4"),
				corev1.ResourceLimitsMemory:   resource.MustParse("4Gi"),
				corev1.ResourceRequestsCPU:    resource.MustParse("2"),
				corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
			})
		})

		By("Verify ResourcePool Status Allocation", func() {
			expectedAllocation := capsulev1beta2.ResourcePoolQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("4"),
					corev1.ResourceLimitsMemory:   resource.MustParse("4Gi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("2"),
					corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
				},
				Claimed: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("0"),
					corev1.ResourceLimitsMemory:   resource.MustParse("1Gi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("0"),
					corev1.ResourceRequestsMemory: resource.MustParse("512Mi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("4"),
					corev1.ResourceLimitsMemory:   resource.MustParse("3Gi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("2"),
					corev1.ResourceRequestsMemory: resource.MustParse("1536Mi"),
				},
			}

			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, pool)
			Expect(err).Should(Succeed())

			ok, msg := DeepCompare(expectedAllocation, pool.Status.Allocation)
			Expect(ok).To(BeTrue(), "Mismatch for resource allocation: %s", msg)
		})

		By("Allow Decreasing", func() {
			UpdatePoolEventually(pool.Name, corev1.ResourceList{
				corev1.ResourceLimitsCPU:      resource.MustParse("2"),
				corev1.ResourceLimitsMemory:   resource.MustParse("2Gi"),
				corev1.ResourceRequestsCPU:    resource.MustParse("2"),
				corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
			})
		})

		By("Verify ResourcePool Status Allocation", func() {
			expectedAllocation := capsulev1beta2.ResourcePoolQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("2"),
					corev1.ResourceLimitsMemory:   resource.MustParse("2Gi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("2"),
					corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
				},
				Claimed: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("0"),
					corev1.ResourceLimitsMemory:   resource.MustParse("1Gi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("0"),
					corev1.ResourceRequestsMemory: resource.MustParse("512Mi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceLimitsCPU:      resource.MustParse("2"),
					corev1.ResourceLimitsMemory:   resource.MustParse("1Gi"),
					corev1.ResourceRequestsCPU:    resource.MustParse("2"),
					corev1.ResourceRequestsMemory: resource.MustParse("1536Mi"),
				},
			}

			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, pool)
			Expect(err).Should(Succeed())

			ok, msg := DeepCompare(expectedAllocation, pool.Status.Allocation)
			Expect(ok).To(BeTrue(), "Mismatch for resource allocation: %s", msg)
		})

		By("Don't allow Decreasing under claimed usage", func() {
			UpdatePoolShouldFail(pool.Name, corev1.ResourceList{
				corev1.ResourceLimitsCPU:      resource.MustParse("2"),
				corev1.ResourceLimitsMemory:   resource.MustParse("10Mi"),
				corev1.ResourceRequestsCPU:    resource.MustParse("0.5"),
				corev1.ResourceRequestsMemory: resource.MustParse("128Mi"),
			})
		})

		By("May Remove unused resources", func() {
			UpdatePoolEventually(pool.Name, corev1.ResourceList{
				corev1.ResourceLimitsMemory:   resource.MustParse("2Gi"),
				corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
			})
		})

		By("Verify ResourcePool Status Allocation", func() {
			expectedAllocation := capsulev1beta2.ResourcePoolQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceLimitsMemory:   resource.MustParse("2Gi"),
					corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
				},
				Claimed: corev1.ResourceList{
					corev1.ResourceLimitsMemory:   resource.MustParse("1Gi"),
					corev1.ResourceRequestsMemory: resource.MustParse("512Mi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceLimitsMemory:   resource.MustParse("1Gi"),
					corev1.ResourceRequestsMemory: resource.MustParse("1536Mi"),
				},
			}

			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, pool)
			Expect(err).Should(Succeed())

			ok, msg := DeepCompare(expectedAllocation, pool.Status.Allocation)
			Expect(ok).To(BeTrue(), "Mismatch for resource allocation: %s", msg)
		})

		By("May Decrase to actual usage", func() {
			UpdatePoolEventually(pool.Name, corev1.ResourceList{
				corev1.ResourceLimitsMemory:   resource.MustParse("1Gi"),
				corev1.ResourceRequestsMemory: resource.MustParse("512Mi"),
			})
		})

		By("Verify ResourcePool Status Allocation", func() {
			expectedAllocation := capsulev1beta2.ResourcePoolQuotaStatus{
				Hard: corev1.ResourceList{
					corev1.ResourceLimitsMemory:   resource.MustParse("1Gi"),
					corev1.ResourceRequestsMemory: resource.MustParse("512Mi"),
				},
				Claimed: corev1.ResourceList{
					corev1.ResourceLimitsMemory:   resource.MustParse("1Gi"),
					corev1.ResourceRequestsMemory: resource.MustParse("512Mi"),
				},
				Available: corev1.ResourceList{
					corev1.ResourceLimitsMemory:   resource.MustParse("0"),
					corev1.ResourceRequestsMemory: resource.MustParse("0"),
				},
			}

			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, pool)
			Expect(err).Should(Succeed())

			ok, msg := DeepCompare(expectedAllocation, pool.Status.Allocation)
			Expect(ok).To(BeTrue(), "Mismatch for resource allocation: %s", msg)
		})

		By("May not set 0 on usage", func() {
			UpdatePoolShouldFail(pool.Name, corev1.ResourceList{
				corev1.ResourceLimitsMemory:   resource.MustParse("0"),
				corev1.ResourceRequestsMemory: resource.MustParse("0"),
			})
		})

		By("May not remove resource in use", func() {
			UpdatePoolShouldFail(pool.Name, corev1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("1"),
			})
		})
	})
})

func isSuccessfullyBoundAndUnsedToPool(pool *capsulev1beta2.ResourcePool, claim *capsulev1beta2.ResourcePoolClaim) {
	Eventually(func(g Gomega) {
		fetchedPool := &capsulev1beta2.ResourcePool{}
		err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, fetchedPool)
		g.Expect(err).Should(Succeed())

		fetchedClaim := &capsulev1beta2.ResourcePoolClaim{}
		err = k8sClient.Get(context.TODO(), client.ObjectKey{
			Name:      claim.Name,
			Namespace: claim.Namespace,
		}, fetchedClaim)
		g.Expect(err).Should(Succeed())

		isBoundToPool(fetchedPool, fetchedClaim)

		g.Expect(fetchedClaim.Status.Pool.Name.String()).To(Equal(fetchedPool.Name))
		g.Expect(fetchedClaim.Status.Pool.UID).To(Equal(fetchedPool.GetUID()))

		bound := fetchedClaim.Status.Conditions.GetConditionByType(meta.BoundCondition)
		g.Expect(bound).NotTo(BeNil(), "Bound condition should be present")

		g.Expect(bound.Type).To(Equal(meta.BoundCondition))
		g.Expect(bound.Status).To(Equal(metav1.ConditionFalse))
		g.Expect(bound.Reason).To(Equal(meta.UnusedReason))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}
func isSuccessfullyBoundAndUsedToPool(pool *capsulev1beta2.ResourcePool, claim *capsulev1beta2.ResourcePoolClaim) {
	Eventually(func(g Gomega) {
		fetchedPool := &capsulev1beta2.ResourcePool{}
		err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, fetchedPool)
		g.Expect(err).Should(Succeed())

		fetchedClaim := &capsulev1beta2.ResourcePoolClaim{}
		err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: claim.Name, Namespace: claim.Namespace}, fetchedClaim)
		g.Expect(err).Should(Succeed())

		g.Expect(assertBoundToPool(fetchedPool, fetchedClaim)).Should(Succeed())

		g.Expect(fetchedClaim.Status.Pool.Name.String()).To(Equal(fetchedPool.Name))
		g.Expect(fetchedClaim.Status.Pool.UID).To(Equal(fetchedPool.GetUID()))

		bound := fetchedClaim.Status.Conditions.GetConditionByType(meta.BoundCondition)
		g.Expect(bound).NotTo(BeNil(), "Bound condition should be present")

		g.Expect(bound.Type).To(Equal(meta.BoundCondition))
		g.Expect(bound.Status).To(Equal(metav1.ConditionTrue))
		g.Expect(bound.Reason).To(Equal(meta.InUseReason))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func assertBoundToPool(pool *capsulev1beta2.ResourcePool, claim *capsulev1beta2.ResourcePoolClaim) error {
	status := pool.GetClaimFromStatus(claim)
	if status == nil {
		return fmt.Errorf("claim %s/%s not found in pool %s status", claim.Namespace, claim.Name, pool.Name)
	}

	for name, cl := range status.Claims {
		expected, ok := claim.Spec.ResourceClaims[name]
		if !ok {
			return fmt.Errorf("pool status contains unexpected claim key %q", name)
		}

		if !reflect.DeepEqual(cl, expected) {
			return fmt.Errorf("claim %q differs from spec: got %#v, want %#v", name, cl, expected)
		}
	}

	return nil
}

func isNotBoundToPool(pool *capsulev1beta2.ResourcePool, claim *capsulev1beta2.ResourcePoolClaim) bool {
	status := pool.GetClaimFromStatus(claim)
	return status == nil
}

func isBoundToPool(pool *capsulev1beta2.ResourcePool, claim *capsulev1beta2.ResourcePoolClaim) error {
	status := pool.GetClaimFromStatus(claim)
	if status == nil {
		return fmt.Errorf("claim %s/%s not found in pool %s status", claim.Namespace, claim.Name, pool.Name)
	}

	for name, cl := range status.Claims {
		expected, ok := claim.Spec.ResourceClaims[name]
		if !ok {
			return fmt.Errorf("pool status contains unexpected claim key %q", name)
		}

		if !reflect.DeepEqual(cl, expected) {
			return fmt.Errorf("claim %q differs from spec: got %#v, want %#v", name, cl, expected)
		}
	}

	return nil
}
func containsAll[T comparable](haystack []T, needles []T) bool {
	for _, n := range needles {
		if !slices.Contains(haystack, n) {
			return false
		}
	}
	return true
}

func extractResourcePoolMessage(msg string) []string {
	var out []string

	parts := strings.FieldsFunc(msg, func(r rune) bool {
		return r == ',' || r == ';'
	})

	for _, p := range parts {
		p = strings.TrimSpace(p)

		kv := strings.SplitN(p, ": ", 2)
		if len(kv) != 2 {
			continue
		}

		kind := kv[0]
		value := kv[1]

		out = append(out, kind+"."+value)
	}
	return out
}

func assertClaimExhausted(pool *capsulev1beta2.ResourcePool, claim *capsulev1beta2.ResourcePoolClaim, reason string, expected []string) {
	Eventually(func(g Gomega) {
		fetchedPool := &capsulev1beta2.ResourcePool{}
		g.Expect(k8sClient.Get(context.TODO(), client.ObjectKey{Name: pool.Name}, fetchedPool)).To(Succeed())

		fetchedClaim := &capsulev1beta2.ResourcePoolClaim{}
		g.Expect(k8sClient.Get(context.TODO(), client.ObjectKey{
			Name:      claim.Name,
			Namespace: claim.Namespace,
		}, fetchedClaim)).To(Succeed())

		g.Expect(fetchedPool.GetClaimFromStatus(fetchedClaim)).To(BeNil())

		exhausted := fetchedClaim.Status.Conditions.GetConditionByType(meta.ExhaustedCondition)
		g.Expect(exhausted).NotTo(BeNil(), "Exhausted condition should be present")

		g.Expect(containsAll(
			extractResourcePoolMessage(exhausted.Message),
			expected,
		)).To(BeTrue(), "Actual message: %s", exhausted.Message)

		g.Expect(exhausted.Reason).To(Equal(reason))
		g.Expect(exhausted.Status).To(Equal(metav1.ConditionTrue))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func UpdatePoolEventually(name string, hard corev1.ResourceList) {
	Eventually(func() error {
		current := &capsulev1beta2.ResourcePool{}
		if err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: name}, current); err != nil {
			return err
		}

		current.Spec.Quota.Hard = hard

		return k8sClient.Update(context.TODO(), current)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func UpdatePoolShouldFail(name string, hard corev1.ResourceList) {
	current := &capsulev1beta2.ResourcePool{}
	Expect(k8sClient.Get(context.TODO(), client.ObjectKey{Name: name}, current)).To(Succeed())

	current.Spec.Quota.Hard = hard

	Expect(k8sClient.Update(context.TODO(), current)).ShouldNot(Succeed())
}

func ExpectPoolAllocation(name string, expected capsulev1beta2.ResourcePoolQuotaStatus) {
	Eventually(func(g Gomega) {
		current := &capsulev1beta2.ResourcePool{}
		g.Expect(k8sClient.Get(context.TODO(), client.ObjectKey{Name: name}, current)).To(Succeed())

		ok, msg := DeepCompare(expected, current.Status.Allocation)
		g.Expect(ok).To(BeTrue(), "Mismatch for resource allocation: %s", msg)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}
