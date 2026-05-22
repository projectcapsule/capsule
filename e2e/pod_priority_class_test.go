// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("enforcing a Priority Class", Ordered, Label("pod", "classes", "priorityclass"), func() {
	tntWithDefaults := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-priority-class-defaults",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "paul",
							Kind: "User",
						},
					},
				},
			},
			PriorityClasses: &api.DefaultAllowedListSpec{
				Default: "tenant-default",
				SelectorAllowedListSpec: api.SelectorAllowedListSpec{
					LabelSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"environment": "customer",
						},
					},
				},
			},
		},
	}

	tntNoDefaults := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-priority-class-no-defaults",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "george",
							Kind: "User",
						},
					},
				},
			},
			PriorityClasses: &api.DefaultAllowedListSpec{
				SelectorAllowedListSpec: api.SelectorAllowedListSpec{
					AllowedListSpec: api.AllowedListSpec{
						Exact: []string{"customer-gold"},
						Regex: "customer\\-\\w+",
					},
					LabelSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"environment": "customer",
						},
					},
				},
			},
		},
	}

	tntNoRestrictions := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-priority-class-no-restrictions",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: []rbac.OwnerSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-priority-class-no-restrictions",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	pcTenantPreemption := corev1.PreemptionPolicy("PreemptLowerPriority")
	tenantDefault := schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-default",
			Labels: map[string]string{
				"environment": "shared",
				"env":         "e2e",
			},
		},
		Description:      "tenant default priorityclass",
		Value:            1212,
		PreemptionPolicy: &pcTenantPreemption,
		GlobalDefault:    false,
	}

	globalDefault := schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "global-default",
			Labels: map[string]string{
				"environment": "customer",
				"env":         "e2e",
			},
		},
		Description:   "global default priorityclass",
		Value:         100000,
		GlobalDefault: true,
	}

	disallowedGlobalDefault := schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "disallowed-global-default",
			Labels: map[string]string{
				"environment": "internal",
				"env":         "e2e",
			},
		},
		Description:   "global default priorityclass",
		Value:         100000,
		GlobalDefault: true,
	}

	customerBronze := &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "customer-bronze",
			Labels: map[string]string{
				"environment": "customer",
				"env":         "e2e",
			},
		},
		Description: "fake PriorityClass for e2e",
		Value:       100000,
	}

	customerSilver := &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "customer-silver",
			Labels: map[string]string{
				"environment": "internal",
				"env":         "e2e",
			},
		},
		Description: "fake PriorityClass for e2e",
		Value:       100000,
	}

	customerGold := &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "customer-gold",
			Labels: map[string]string{
				"environment": "internal",
				"env":         "e2e",
			},
		},
		Description: "fake PriorityClass for e2e",
		Value:       100000,
	}

	JustBeforeEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{tntWithDefaults, tntNoDefaults, tntNoRestrictions} {
			EventuallyCreation(func() error {
				tnt.ResourceVersion = ""

				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())

			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}

		for _, crd := range []*schedulingv1.PriorityClass{customerBronze, customerSilver, customerGold} {
			Eventually(func() error {
				crd.ResourceVersion = ""
				return k8sClient.Create(context.TODO(), crd)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})

	JustAfterEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{tntWithDefaults, tntNoDefaults, tntNoRestrictions} {
			EventuallyDeletion(tnt)
		}

		req, err := labels.NewRequirement("env", selection.Equals, []string{"e2e"})
		Expect(err).NotTo(HaveOccurred())

		var list schedulingv1.PriorityClassList
		Expect(k8sClient.List(
			context.TODO(),
			&list,
			client.MatchingLabelsSelector{
				Selector: labels.NewSelector().Add(*req),
			},
		)).Should(Succeed())

		for i := range list.Items {
			EventuallyDeletion(&list.Items[i])
		}
	})

	It("should allow all classes", Label("skip-on-openshift"), func() {
		all := []string{"system-cluster-critical", "system-node-critical", customerBronze.GetName(), customerSilver.GetName(), customerGold.GetName()}

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tntNoRestrictions.GetName(),
		})
		NamespaceCreation(ns, tntNoRestrictions.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tntNoRestrictions, ns).Should(Succeed())

		By("Verify Status (Creation)", func() {
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntNoRestrictions.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.PriorityClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf(all))
		})

		By("providing any priorityclass", func() {
			for _, class := range all {
				Eventually(func() (err error) {

					g := &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      class + "-container",
							Namespace: ns.GetName(),
						},
						Spec: corev1.PodSpec{
							SecurityContext:   nobodyPodSecurityContext(),
							PriorityClassName: class,
							Containers: []corev1.Container{
								{
									Name:            "container",
									Image:           "gcr.io/google_containers/pause-amd64:3.0",
									ImagePullPolicy: corev1.PullIfNotPresent,
									SecurityContext: restrictedContainerSecurityContext(),
								},
							},
						},
					}

					return k8sClient.Create(context.TODO(), g)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})

		By("Verify Status (Deletion)", func() {
			for _, crd := range []*schedulingv1.PriorityClass{customerGold} {
				EventuallyDeletion(crd)
			}
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntNoRestrictions.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.PriorityClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf("system-cluster-critical", "system-node-critical", customerBronze.GetName(), customerSilver.GetName()))
		})
	})

	It("should block non allowed Priority Class", Label("skip-on-openshift"), func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tntNoDefaults.GetName(),
		})
		NamespaceCreation(ns, tntNoDefaults.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tntNoDefaults, ns).Should(Succeed())

		By("Verify Status", func() {
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntNoDefaults.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.PriorityClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf(customerGold.GetName(), customerBronze.GetName(), customerSilver.GetName()))
		})

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "container",
			},
			Spec: corev1.PodSpec{
				SecurityContext:   nobodyPodSecurityContext(),
				PriorityClassName: "system-node-critical",
				Containers: []corev1.Container{
					{
						Name:            "container",
						Image:           "gcr.io/google_containers/pause-amd64:3.0",
						ImagePullPolicy: corev1.PullIfNotPresent,
						SecurityContext: restrictedContainerSecurityContext(),
					},
				},
			},
		}

		cs := ownerClient(tntNoDefaults.Spec.Owners[0].UserSpec)
		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
			return err
		}).ShouldNot(Succeed())
	})

	It("should block non matching selector match", func() {
		for i, pc := range []string{"internal-bronze", "internal-silver", "internal-gold"} {
			priorityName := strings.Join([]string{pc, "-", strconv.Itoa(i)}, "")
			class := &schedulingv1.PriorityClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: priorityName,
					Labels: map[string]string{
						"environment": "internal",
						"env":         "e2e",
					},
				},
				Description: "fake PriorityClass for e2e",
				Value:       int32(10000 * (i + 2)),
			}
			Expect(k8sClient.Create(context.TODO(), class)).Should(Succeed())

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: pc,
				},
				Spec: corev1.PodSpec{
					SecurityContext:   nobodyPodSecurityContext(),
					PriorityClassName: class.GetName(),
					Containers: []corev1.Container{
						{
							Name:            "container",
							Image:           "gcr.io/google_containers/pause-amd64:3.0",
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: restrictedContainerSecurityContext(),
						},
					},
				},
			}

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tntNoDefaults.GetName(),
			})
			cs := ownerClient(tntNoDefaults.Spec.Owners[0].UserSpec)

			NamespaceCreation(ns, tntNoDefaults.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tntNoDefaults, ns).Should(Succeed())

			By("Verify Status", func() {
				Eventually(func() ([]string, error) {
					t := &capsulev1beta2.Tenant{}
					if err := k8sClient.Get(
						context.TODO(),
						types.NamespacedName{Name: tntNoDefaults.GetName()},
						t,
					); err != nil {
						return nil, err
					}

					return t.Status.Classes.PriorityClasses, nil
				}, defaultTimeoutInterval, defaultPollInterval).
					Should(ConsistOf(customerGold.GetName(), customerBronze.GetName(), customerSilver.GetName()))
			})

			EventuallyCreation(func() error {
				_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
				return err
			}).ShouldNot(Succeed())
		}
	})

	It("should allow exact match", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tntNoDefaults.GetName(),
		})
		NamespaceCreation(ns, tntNoDefaults.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tntNoDefaults, ns).Should(Succeed())

		By("Verify Status", func() {
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntNoDefaults.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.PriorityClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf(customerGold.GetName(), customerBronze.GetName(), customerSilver.GetName()))
		})

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "container",
			},
			Spec: corev1.PodSpec{
				SecurityContext:   nobodyPodSecurityContext(),
				PriorityClassName: "customer-gold",
				Containers: []corev1.Container{
					{
						Name:            "container",
						Image:           "gcr.io/google_containers/pause-amd64:3.0",
						ImagePullPolicy: corev1.PullIfNotPresent,
						SecurityContext: restrictedContainerSecurityContext(),
					},
				},
			},
		}

		cs := ownerClient(tntNoDefaults.Spec.Owners[0].UserSpec)
		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
			return err
		}).Should(Succeed())
	})

	It("should allow regex match", Label("skip-on-openshift"), func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tntNoDefaults.GetName(),
		})
		NamespaceCreation(ns, tntNoDefaults.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tntNoDefaults, ns).Should(Succeed())

		By("Verify Status", func() {
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntNoDefaults.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.PriorityClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf(customerGold.GetName(), customerBronze.GetName(), customerSilver.GetName()))
		})

		for _, class := range []string{customerBronze.GetName(), customerSilver.GetName(), customerGold.GetName()} {
			EventuallyCreation(func() error {
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      class,
						Namespace: ns.GetName(),
					},
					Spec: corev1.PodSpec{
						SecurityContext:   nobodyPodSecurityContext(),
						PriorityClassName: class,
						Containers: []corev1.Container{
							{
								Name:            "container",
								Image:           "gcr.io/google_containers/pause-amd64:3.0",
								ImagePullPolicy: corev1.PullIfNotPresent,
								SecurityContext: restrictedContainerSecurityContext(),
							},
						},
					},
				}

				return k8sClient.Create(context.Background(), pod)
			}).Should(Succeed())
		}
	})

	It("should allow selector match", func() {
		By("create Custom Classes", func() {
			for i, pc := range []string{"internal-bronze-new", "internal-silver-new", "internal-gold-new"} {
				priorityName := strings.Join([]string{pc, "-", strconv.Itoa(i)}, "")
				class := &schedulingv1.PriorityClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: priorityName,
						Labels: map[string]string{
							"environment": "customer",
							"env":         "e2e",
						},
					},
					Description: "fake PriorityClass for e2e",
					Value:       int32(10000 * (i + 2)),
				}
				Expect(k8sClient.Create(context.TODO(), class)).Should(Succeed())

				ns := NewNamespace("", map[string]string{
					meta.TenantLabel: tntNoDefaults.GetName(),
				})

				NamespaceCreation(ns, tntNoDefaults.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
				NamespaceIsPartOfTenant(tntNoDefaults, ns).Should(Succeed())

				By("Verify Status", func() {
					Eventually(func() ([]string, error) {
						t := &capsulev1beta2.Tenant{}
						if err := k8sClient.Get(
							context.TODO(),
							types.NamespacedName{Name: tntNoDefaults.GetName()},
							t,
						); err != nil {
							return nil, err
						}

						return t.Status.Classes.PriorityClasses, nil
					}, defaultTimeoutInterval, defaultPollInterval).
						Should(ContainElement(class.GetName()))
				})

				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: pc,
					},
					Spec: corev1.PodSpec{
						SecurityContext:   nobodyPodSecurityContext(),
						PriorityClassName: class.GetName(),
						Containers: []corev1.Container{
							{
								Name:            "container",
								Image:           "gcr.io/google_containers/pause-amd64:3.0",
								ImagePullPolicy: corev1.PullIfNotPresent,
								SecurityContext: restrictedContainerSecurityContext(),
							},
						},
					},
				}

				cs := ownerClient(tntNoDefaults.Spec.Owners[0].UserSpec)

				EventuallyCreation(func() error {
					_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
					return err
				}).Should(Succeed())
			}
		})

		By("verify Status (Creation)", func() {
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntNoDefaults.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.PriorityClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf("internal-bronze-new-0", "internal-silver-new-1", "internal-gold-new-2", customerGold.GetName(), customerBronze.GetName(), customerSilver.GetName()))
		})
	})

	It("fail if default tenant PriorityClass is absent", func() {
		By("rejecting default", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "tenant-default",
				},
				Spec: corev1.PodSpec{
					SecurityContext: nobodyPodSecurityContext(),
					Containers: []corev1.Container{
						{
							Name:            "container",
							Image:           "gcr.io/google_containers/pause-amd64:3.0",
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: restrictedContainerSecurityContext(),
						},
					},
				},
			}

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tntWithDefaults.GetName(),
			})
			cs := ownerClient(tntWithDefaults.Spec.Owners[0].UserSpec)

			NamespaceCreation(ns, tntWithDefaults.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tntWithDefaults, ns).Should(Succeed())

			By("verify Status (Creation)", func() {
				Eventually(func() ([]string, error) {
					t := &capsulev1beta2.Tenant{}
					if err := k8sClient.Get(
						context.TODO(),
						types.NamespacedName{Name: tntWithDefaults.GetName()},
						t,
					); err != nil {
						return nil, err
					}

					return t.Status.Classes.PriorityClasses, nil
				}, defaultTimeoutInterval, defaultPollInterval).
					Should(ConsistOf(customerBronze.GetName()))
			})

			EventuallyCreation(func() error {
				_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})

				return err
			}).ShouldNot(Succeed())
		})
	})

	It("should mutate to default tenant PriorityClass", Label("skip-on-openshift"), func() {
		By("creating default tenant class", func() {
			class := tenantDefault.DeepCopy()
			class.SetResourceVersion("")
			Expect(k8sClient.Create(context.TODO(), class)).Should(Succeed())

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tntWithDefaults.GetName(),
			})
			NamespaceCreation(ns, tntWithDefaults.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tntWithDefaults, ns).Should(Succeed())

			By("verify Status (Creation)", func() {
				Eventually(func() ([]string, error) {
					t := &capsulev1beta2.Tenant{}
					if err := k8sClient.Get(
						context.TODO(),
						types.NamespacedName{Name: tntWithDefaults.GetName()},
						t,
					); err != nil {
						return nil, err
					}

					return t.Status.Classes.PriorityClasses, nil
				}, defaultTimeoutInterval, defaultPollInterval).
					Should(ConsistOf(tenantDefault.GetName(), customerBronze.GetName()))
			})

			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tenant-default-present",
					Namespace: ns.GetName(),
				},
				Spec: corev1.PodSpec{
					SecurityContext: nobodyPodSecurityContext(),
					Containers: []corev1.Container{
						{
							Name:            "container",
							Image:           "gcr.io/google_containers/pause-amd64:3.0",
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: restrictedContainerSecurityContext(),
						},
					},
				},
			}

			EventuallyCreation(func() error {
				return k8sClient.Create(context.Background(), &pod)
			}).Should(Succeed())
			// Check if correct mutated
			Expect(pod.Spec.PriorityClassName).To(Equal(class.GetName()))
			Expect(pod.Spec.Priority).To(Equal(&class.Value))
			Expect(pod.Spec.PreemptionPolicy).To(Equal(class.PreemptionPolicy))
		})
	})

	It("should mutate to default tenant PriorityClass although the cluster global one is not allowed", Label("skip-on-openshift"), func() {
		class := tenantDefault.DeepCopy()
		class.SetResourceVersion("")

		global := disallowedGlobalDefault.DeepCopy()
		global.SetResourceVersion("")

		Expect(k8sClient.Create(context.TODO(), class)).Should(Succeed())
		Expect(k8sClient.Create(context.TODO(), global)).Should(Succeed())

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tntWithDefaults.GetName(),
		})
		NamespaceCreation(ns, tntWithDefaults.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tntWithDefaults, ns).Should(Succeed())

		By("verify Status (Creation)", func() {
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntWithDefaults.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.PriorityClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf(tenantDefault.GetName(), customerBronze.GetName()))
		})

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tenant-default-global-default",
				Namespace: ns.GetName(),
			},
			Spec: corev1.PodSpec{
				SecurityContext: nobodyPodSecurityContext(),
				Containers: []corev1.Container{
					{
						Name:            "container",
						Image:           "gcr.io/google_containers/pause-amd64:3.0",
						ImagePullPolicy: corev1.PullIfNotPresent,
						SecurityContext: restrictedContainerSecurityContext(),
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), pod)
		}).Should(Succeed())

		// Check if correct applied
		Expect(pod.Spec.PriorityClassName).To(Equal(class.GetName()))
		Expect(pod.Spec.Priority).To(Equal(&class.Value))
		Expect(pod.Spec.PreemptionPolicy).To(Equal(class.PreemptionPolicy))
	})

	It("should mutate to default tenant PriorityClass although the cluster global one is allowed", Label("skip-on-openshift"), func() {
		class := tenantDefault.DeepCopy()
		class.SetResourceVersion("")
		Expect(k8sClient.Create(context.TODO(), class)).Should(Succeed())

		global := globalDefault.DeepCopy()
		global.SetResourceVersion("")
		Expect(k8sClient.Create(context.TODO(), global)).Should(Succeed())

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tntWithDefaults.GetName(),
		})
		NamespaceCreation(ns, tntWithDefaults.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tntWithDefaults, ns).Should(Succeed())

		By("verify Status (Creation)", func() {
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntWithDefaults.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.PriorityClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf(global.GetName(), tenantDefault.GetName(), customerBronze.GetName()))
		})

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tenant-default-allowed",
				Namespace: ns.GetName(),
			},
			Spec: corev1.PodSpec{
				SecurityContext: nobodyPodSecurityContext(),
				Containers: []corev1.Container{
					{
						Name:            "container",
						Image:           "gcr.io/google_containers/pause-amd64:3.0",
						ImagePullPolicy: corev1.PullIfNotPresent,
						SecurityContext: restrictedContainerSecurityContext(),
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), pod)
		}).Should(Succeed())
		// Check if correctly applied
		Expect(pod.Spec.PriorityClassName).To(Equal(class.GetName()))
		Expect(*pod.Spec.Priority).To(Equal(class.Value))
	})
})
