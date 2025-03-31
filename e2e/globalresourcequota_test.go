//go:build e2e

// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/utils"
)

var _ = Describe("Global ResourceQuotas", func() {
	solar := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "solar-quota",
			Labels: map[string]string{
				"customer-resource-pool": "dev",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "solar-user",
					Kind: "User",
				},
			},
		},
	}

	wind := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "wind-quota",
			Labels: map[string]string{
				"customer-resource-pool": "dev",
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

	grq := &capsulev1beta2.GlobalResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name: "global-quota",
			Labels: map[string]string{
				"replicate": "true",
			},
		},
		Spec: capsulev1beta2.GlobalResourceQuotaSpec{
			Selectors: []capsulev1beta2.GlobalResourceQuotaSelector{
				{
					MustTenantNamespace: true, // Only namespaces belonging to a tenant are considered
					NamespaceSelector: api.NamespaceSelector{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"capsule.clastix.io/tenant": "solar-quota",
							},
						},
					},
				},
				{
					MustTenantNamespace: true, // Only namespaces belonging to a tenant are considered
					NamespaceSelector: api.NamespaceSelector{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"capsule.clastix.io/tenant": "wind-quota",
							},
						},
					},
				},
				{
					MustTenantNamespace: false, // Allow non-tenant namespaces
					NamespaceSelector: api.NamespaceSelector{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"loose-quota": "any",
							},
						},
					},
				},
			},
			Items: map[api.Name]corev1.ResourceQuotaSpec{
				"scheduling": {
					Hard: corev1.ResourceList{
						corev1.ResourceLimitsCPU:      resource.MustParse("2"),
						corev1.ResourceLimitsMemory:   resource.MustParse("2Gi"),
						corev1.ResourceRequestsCPU:    resource.MustParse("2"),
						corev1.ResourceRequestsMemory: resource.MustParse("2Gi"),
					},
				},
				"pods": {
					Hard: corev1.ResourceList{
						corev1.ResourcePods: resource.MustParse("5"),
					},
				},
				"connectivity": {
					Hard: corev1.ResourceList{
						corev1.ResourceServices: resource.MustParse("2"),
					},
				},
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			solar.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), solar)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			wind.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), wind)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			grq.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), grq)
		}).Should(Succeed())
	})

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), solar)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), wind)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), grq)).Should(Succeed())
		Eventually(func() error {
			deploymentList := &appsv1.DeploymentList{}
			labelSelector := client.MatchingLabels{"test-label": "to-delete"}
			if err := k8sClient.List(context.TODO(), deploymentList, labelSelector); err != nil {
				return err
			}

			for _, deployment := range deploymentList.Items {
				if err := k8sClient.Delete(context.TODO(), &deployment); err != nil {
					return err
				}
			}

			return nil
		}, "30s", "5s").Should(Succeed())
	})

	It("handle overprovisioning (eventually)", func() {
		solarNs := []string{"solar-one", "solar-two", "solar-three"}

		By("creating solar Namespaces", func() {
			for _, ns := range solarNs {
				NamespaceCreation(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, solar.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			}
		})

		By("Scheduling services simultaneously in all namespaces", func() {
			wg := sync.WaitGroup{} // Use WaitGroup for concurrency
			for _, ns := range solarNs {
				wg.Add(1)
				go func(namespace string) { // Run in parallel
					defer wg.Done()
					service := &corev1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-service",
							Namespace: namespace,
							Labels: map[string]string{
								"test-label": "to-delete",
							},
						},
						Spec: corev1.ServiceSpec{
							// Select pods with this label (ensure these pods exist in the namespace)
							Selector: map[string]string{"app": "test"},
							Ports: []corev1.ServicePort{
								{
									Port:       80,
									TargetPort: intstr.FromInt(8080),
									Protocol:   corev1.ProtocolTCP,
								},
							},
							Type: corev1.ServiceTypeClusterIP,
						},
					}
					err := k8sClient.Create(context.TODO(), service)
					Expect(err).Should(Succeed(), "Failed to create Service in namespace %s", namespace)
				}(ns)
			}
			wg.Wait() // Ensure all services are scheduled at the same time
		})

		By("Scheduling deployments simultaneously in all namespaces", func() {
			wg := sync.WaitGroup{} // Use WaitGroup for concurrency
			for _, ns := range solarNs {
				wg.Add(1)
				go func(namespace string) { // Run in parallel
					defer wg.Done()
					deployment := &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-deployment",
							Namespace: namespace,
							Labels: map[string]string{
								"test-label": "to-delete",
							},
						},
						Spec: appsv1.DeploymentSpec{
							Replicas: ptr.To(int32(3)), // Adjust the replica count if needed
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "test"},
							},
							Template: corev1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{"app": "test"},
								},
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{
											Name:  "busybox",
											Image: "busybox",
											Args:  []string{"sleep", "3600"},
											Resources: corev1.ResourceRequirements{
												Limits: corev1.ResourceList{
													corev1.ResourceCPU:    resource.MustParse("0.3"),
													corev1.ResourceMemory: resource.MustParse("128Mi"),
												},
												Requests: corev1.ResourceList{
													corev1.ResourceCPU:    resource.MustParse("0.25"),
													corev1.ResourceMemory: resource.MustParse("128Mi"),
												},
											},
										},
									},
								},
							},
						},
					}
					err := k8sClient.Create(context.TODO(), deployment)
					Expect(err).Should(Succeed(), "Failed to create Deployment in namespace %s", namespace)
				}(ns)
			}
			wg.Wait() // Ensure all deployments are scheduled at the same time
		})

		By("Waiting for at least 5 pods with label app=test to be scheduled and in Running state", func() {
			Eventually(func() int {
				podList := &corev1.PodList{}
				err := k8sClient.List(context.TODO(), podList, &client.ListOptions{
					LabelSelector: labels.SelectorFromSet(map[string]string{"app": "test"}),
				})
				if err != nil {
					return 0
				}

				// Count only pods that are in Running phase
				runningPods := 0
				for _, pod := range podList.Items {
					if pod.Status.Phase == corev1.PodRunning {
						runningPods++
					}
				}
				return runningPods
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeNumerically(">=", 5), "Expected at least 5 running pods with label app=test")
		})

		By("Sleeping for 10 minutes", func() {
			time.Sleep(10 * time.Minute)
		})

		By("Collecting and logging ResourceQuota statuses across namespaces (pods item)", func() {
			totalHard := corev1.ResourceList{}
			totalUsed := corev1.ResourceList{}

			// Construct first label requirement (e.g., object type)
			r1, err := labels.NewRequirement(utils.GetGlobalResourceQuotaTypeLabel(), selection.Equals, []string{"pods"})
			Expect(err).Should(Succeed(), "❌ Error creating label requirement for %s: %v\n", utils.GetGlobalResourceQuotaTypeLabel(), err)

			// List ResourceQuotas in the namespace
			quotaList := corev1.ResourceQuotaList{}
			err = k8sClient.List(context.TODO(), &quotaList, &client.ListOptions{
				LabelSelector: labels.NewSelector().Add(*r1),
			})
			Expect(err).Should(Succeed(), "Failed to list resourcequotas: %v", err)

			for _, quota := range quotaList.Items {
				fmt.Printf("Processing ResourceQuota: %s in namespace %s status: %v\n", quota.Name, quota.Namespace, quota.Status)

				// Aggregate Status Used values
				for resourceName, usedValue := range quota.Status.Used {
					if existing, exists := totalUsed[resourceName]; exists {
						existing.Add(usedValue)
						totalUsed[resourceName] = existing
					} else {
						totalUsed[resourceName] = usedValue.DeepCopy()
					}
				}
			}

			fmt.Println("✅ Aggregated ResourceQuotas:")
			fmt.Println("Total Spec Hard Limits:", totalHard)
			fmt.Println("Total Used Resources:", totalUsed)
		})
	})

	It("should replicate resourcequotas to relevant namespaces", func() {
		solarNs := []string{"solar-one", "solar-two", "solar-three"}

		By("creating solar Namespaces", func() {
			for _, ns := range solarNs {
				NamespaceCreation(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, solar.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			}
		})

		// Fetch the GlobalResourceQuota object
		globalQuota := &capsulev1beta2.GlobalResourceQuota{}
		err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: grq.Name}, globalQuota)
		Expect(err).Should(Succeed())

		for _, ns := range solarNs {
			By(fmt.Sprintf("waiting resourcequotas in %s Namespace", ns), func() {
				Eventually(func() []corev1.ResourceQuota {
					// List ResourceQuotas in the namespace
					quotaList := corev1.ResourceQuotaList{}
					err = k8sClient.List(context.TODO(), &quotaList, &client.ListOptions{
						Namespace: ns,
					})
					if err != nil {
						fmt.Printf("Error listing ResourceQuotas in namespace %s: %v\n", ns, err)
						return nil
					}

					// Filter ResourceQuotas based on GlobalResourceQuota validation
					var matchingQuotas []corev1.ResourceQuota
					for _, rq := range quotaList.Items {
						// Validate against GlobalResourceQuota
						if validateQuotaAgainstGlobal(rq, globalQuota) {
							matchingQuotas = append(matchingQuotas, rq)
						} else {
							fmt.Printf("❌ ResourceQuota %s does not match GlobalResourceQuota %s\n", rq.Name, grq.Name)
						}
					}

					return matchingQuotas
				}, defaultTimeoutInterval, defaultPollInterval).Should(HaveLen(2))
			})
		}

		By("Verify General Status", func() {
			// Fetch the GlobalResourceQuota object
			globalQuota := &capsulev1beta2.GlobalResourceQuota{}
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: grq.Name}, globalQuota)
			Expect(err).Should(Succeed())

			// Expected values
			expectedNamespaces := []string{"solar-one", "solar-two", "solar-three"}
			expectedSize := 3

			// Verify `active` field
			Expect(globalQuota.Status.Active).To(BeTrue(), "❌ GlobalResourceQuota should be active")

			// Verify `size` field
			Expect(int(globalQuota.Status.Size)).To(Equal(expectedSize), "❌ GlobalResourceQuota size should be %d", expectedSize)

			// Verify `namespaces` field (ensuring it matches exactly)
			Expect(globalQuota.Status.Namespaces).To(ConsistOf(expectedNamespaces),
				"❌ GlobalResourceQuota namespaces should match %v", expectedNamespaces)
		})

		windNs := []string{"wind-one", "wind-two", "wind-three"}

		By("creating wind Namespaces", func() {
			for _, ns := range windNs {
				NamespaceCreation(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, wind.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			}
		})

		for _, ns := range windNs {
			By(fmt.Sprintf("waiting resourcequotas in %s Namespace", ns), func() {
				Eventually(func() []corev1.ResourceQuota {
					// List ResourceQuotas in the namespace
					quotaList := corev1.ResourceQuotaList{}
					err = k8sClient.List(context.TODO(), &quotaList, &client.ListOptions{
						Namespace: ns,
					})
					if err != nil {
						fmt.Printf("Error listing ResourceQuotas in namespace %s: %v\n", ns, err)
						return nil
					}

					// Filter ResourceQuotas based on GlobalResourceQuota validation
					var matchingQuotas []corev1.ResourceQuota
					for _, rq := range quotaList.Items {
						// Validate against GlobalResourceQuota
						if validateQuotaAgainstGlobal(rq, globalQuota) {
							matchingQuotas = append(matchingQuotas, rq)
						} else {
							fmt.Printf("❌ ResourceQuota %s does not match GlobalResourceQuota %s\n", rq.Name, grq.Name)
						}
					}

					return matchingQuotas
				}, defaultTimeoutInterval, defaultPollInterval).Should(HaveLen(2))
			})
		}

		By("Verify General Status", func() {
			// Fetch the GlobalResourceQuota object
			globalQuota := &capsulev1beta2.GlobalResourceQuota{}
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: grq.Name}, globalQuota)
			Expect(err).Should(Succeed())

			// Expected values
			expectedNamespaces := append(solarNs, windNs...)
			expectedSize := 6

			// Verify `active` field
			Expect(globalQuota.Status.Active).To(BeTrue(), "❌ GlobalResourceQuota should be active")

			// Verify `size` field
			Expect(int(globalQuota.Status.Size)).To(Equal(expectedSize), "❌ GlobalResourceQuota size should be %d", expectedSize)

			// Verify `namespaces` field (ensuring it matches exactly)
			Expect(globalQuota.Status.Namespaces).To(ConsistOf(expectedNamespaces),
				"❌ GlobalResourceQuota namespaces should match %v", expectedNamespaces)
		})

		By("Updating GlobalResourceQuota selectors", func() {
			// Fetch the GlobalResourceQuota object
			globalQuota := &capsulev1beta2.GlobalResourceQuota{}
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: grq.Name}, globalQuota)
			Expect(err).Should(Succeed())

			// Modify the `spec.selectors` field with new values
			globalQuota.Spec.Selectors = []capsulev1beta2.GlobalResourceQuotaSelector{
				{
					MustTenantNamespace: true, // Only namespaces belonging to a tenant are considered
					NamespaceSelector: api.NamespaceSelector{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"capsule.clastix.io/tenant": "wind-quota",
							},
						},
					},
				},
			}

			// Update the GlobalResourceQuota object in Kubernetes
			err = k8sClient.Update(context.TODO(), globalQuota)
			Expect(err).Should(Succeed(), "Failed to update GlobalResourceQuota selectors")
		})

		By("Verify General Status", func() {
			// Fetch the GlobalResourceQuota object
			globalQuota := &capsulev1beta2.GlobalResourceQuota{}
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: grq.Name}, globalQuota)
			Expect(err).Should(Succeed())

			// Expected values
			expectedSize := 3

			// Verify `active` field
			Expect(globalQuota.Status.Active).To(BeTrue(), "❌ GlobalResourceQuota should be active")

			// Verify `size` field
			Expect(int(globalQuota.Status.Size)).To(Equal(expectedSize), "❌ GlobalResourceQuota size should be %d", expectedSize)

			// Verify `namespaces` field (ensuring it matches exactly)
			Expect(globalQuota.Status.Namespaces).To(ConsistOf(windNs),
				"❌ GlobalResourceQuota namespaces should match %v", windNs)
		})

		objectLabel, _ := utils.GetTypeLabel(&capsulev1beta2.GlobalResourceQuota{})

		for _, ns := range solarNs {
			By(fmt.Sprintf("verify resourcequotas in %s Namespace absent", ns), func() {
				Eventually(func() []corev1.ResourceQuota {
					// Construct first label requirement (e.g., object type)
					r1, err := labels.NewRequirement(objectLabel, selection.Equals, []string{grq.Name})
					if err != nil {
						fmt.Printf("❌ Error creating label requirement for %s: %v\n", objectLabel, err)
						return nil
					}

					// List ResourceQuotas in the namespace
					quotaList := corev1.ResourceQuotaList{}
					err = k8sClient.List(context.TODO(), &quotaList, &client.ListOptions{
						LabelSelector: labels.NewSelector().Add(*r1),
						Namespace:     ns,
					})
					if err != nil {
						fmt.Printf("❌ Error listing ResourceQuotas in namespace %s: %v\n", ns, err)
						return nil
					}

					return quotaList.Items
				}, defaultTimeoutInterval, defaultPollInterval).Should(HaveLen(0))
			})
		}

	})

})

func validateQuotaAgainstGlobal(rq corev1.ResourceQuota, grq *capsulev1beta2.GlobalResourceQuota) bool {
	objectLabel, _ := utils.GetTypeLabel(&capsulev1beta2.GlobalResourceQuota{})

	// Fetch the GlobalResourceQuota object
	err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: grq.Name}, grq)
	Expect(err).Should(Succeed())

	// Verify GlobalQuotaReference
	globalQuotaName, exists := rq.ObjectMeta.Labels[objectLabel]
	if !exists {
		fmt.Printf("Skipping ResourceQuota %s: Missing label %s\n", rq.Name, objectLabel)
		return false
	}
	if globalQuotaName != grq.Name {
		fmt.Printf("Skipping ResourceQuota %s: Label mismatch (expected: %s, found: %s)\n", rq.Name, grq.Name, globalQuotaName)
		return false
	}

	// Verify Item is correctly labeled
	itemName, exists := rq.ObjectMeta.Labels[utils.GetGlobalResourceQuotaTypeLabel()]
	if !exists {
		fmt.Printf("Skipping ResourceQuota %s: Missing label %s\n", rq.Name, utils.GetGlobalResourceQuotaTypeLabel())
		return false
	}

	// Check if the GlobalResourceQuota has a matching entry
	_, exists = grq.Spec.Items[api.Name(itemName)]
	if !exists {
		fmt.Printf("Skipping ResourceQuota %s: Item %s: Missing Item in Spec\n", grq.Name, itemName)
		return false
	}

	// Validate that ResourceQuota.Spec.Hard matches GlobalResourceQuota.Status.Quota[labelValue].Hard
	globalQuotaStatus, statusExists := grq.Status.Quota[api.Name(itemName)]
	if !statusExists {
		fmt.Printf("Skipping ResourceQuota %s: Item %s: Missing Item in Status\n", grq.Name, itemName)
		return false
	}

	// Compare ResourceQuota.Spec.Hard with GlobalResourceQuota.Status.Quota[labelValue].Hard
	for resourceName, specHardValue := range rq.Spec.Hard {
		if statusHardValue, exists := globalQuotaStatus.Hard[resourceName]; !exists || specHardValue.Cmp(statusHardValue) != 0 {
			fmt.Printf("❌ ResourceQuota difference state %v\n", specHardValue.Cmp(statusHardValue))
			return false
		}
	}

	return true
}
