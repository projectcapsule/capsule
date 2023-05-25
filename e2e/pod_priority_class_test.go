//go:build e2e

// Copyright 2020-2021 Clastix Labs
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
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/api"
)

var _ = Describe("enforcing a Priority Class", func() {
	tntWithDefaults := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "priority-class-defaults",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "paul",
					Kind: "User",
				},
			},
			PriorityClasses: &api.DefaultAllowedListSpec{
				Default: "tenant-default",
				SelectorAllowedListSpec: api.SelectorAllowedListSpec{
					LabelSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"env": "customer",
						},
					},
				},
			},
		},
	}

	tntNoDefaults := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "priority-class-no-defaults",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "george",
					Kind: "User",
				},
			},
			PriorityClasses: &api.DefaultAllowedListSpec{
				SelectorAllowedListSpec: api.SelectorAllowedListSpec{
					AllowedListSpec: api.AllowedListSpec{
						Exact: []string{"gold"},
						Regex: "pc\\-\\w+",
					},
					LabelSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"env": "customer",
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
				"env": "e2e",
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
				"env": "customer",
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
				"env": "e2e",
			},
		},
		Description:   "global default priorityclass",
		Value:         100000,
		GlobalDefault: true,
	}

	JustBeforeEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{tntWithDefaults, tntNoDefaults} {
			EventuallyCreation(func() error {
				tnt.ResourceVersion = ""

				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
		}
	})

	JustAfterEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{tntWithDefaults, tntNoDefaults} {
			Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
		}

		Eventually(func() (err error) {
			req, _ := labels.NewRequirement("env", selection.Exists, nil)

			return k8sClient.DeleteAllOf(context.TODO(), &schedulingv1.PriorityClass{}, &client.DeleteAllOfOptions{
				ListOptions: client.ListOptions{
					LabelSelector: labels.NewSelector().Add(*req),
				},
			})
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("should block non allowed Priority Class", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tntNoDefaults.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "container",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "container",
						Image: "quay.io/google-containers/pause-amd64:3.0",
					},
				},
				PriorityClassName: "system-node-critical",
			},
		}

		cs := ownerClient(tntNoDefaults.Spec.Owners[0])
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
						"env": "internal",
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
					Containers: []corev1.Container{
						{
							Name:  "container",
							Image: "quay.io/google-containers/pause-amd64:3.0",
						},
					},
					PriorityClassName: class.GetName(),
				},
			}

			ns := NewNamespace("")
			cs := ownerClient(tntNoDefaults.Spec.Owners[0])

			NamespaceCreation(ns, tntNoDefaults.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tntNoDefaults, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			EventuallyCreation(func() error {
				_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
				return err
			}).ShouldNot(Succeed())
		}
	})

	It("should allow exact match", func() {
		pc := &schedulingv1.PriorityClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gold",
				Labels: map[string]string{
					"env": "e2e",
				},
			},
			Description: "fake PriorityClass for e2e",
			Value:       10000,
		}
		Expect(k8sClient.Create(context.TODO(), pc)).Should(Succeed())

		ns := NewNamespace("")
		NamespaceCreation(ns, tntNoDefaults.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "container",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "container",
						Image: "quay.io/google-containers/pause-amd64:3.0",
					},
				},
				PriorityClassName: "gold",
			},
		}

		cs := ownerClient(tntNoDefaults.Spec.Owners[0])
		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
			return err
		}).Should(Succeed())
	})

	It("should allow regex match", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tntNoDefaults.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

		for i, pc := range []string{"pc-bronze", "pc-silver", "pc-gold"} {
			class := &schedulingv1.PriorityClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: pc,
					Labels: map[string]string{
						"env": "e2e",
					},
				},
				Description: "fake PriorityClass for e2e",
				Value:       int32(10000 * (i + 2)),
			}

			Expect(k8sClient.Create(context.TODO(), class)).Should(Succeed())

			EventuallyCreation(func() error {
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      pc,
						Namespace: ns.GetName(),
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "container",
								Image: "quay.io/google-containers/pause-amd64:3.0",
							},
						},
						PriorityClassName: class.GetName(),
					},
				}

				return k8sClient.Create(context.Background(), pod)
			}).Should(Succeed())
		}
	})

	It("should allow selector match", func() {
		for i, pc := range []string{"customer-bronze", "customer-silver", "customer-gold"} {
			priorityName := strings.Join([]string{pc, "-", strconv.Itoa(i)}, "")
			class := &schedulingv1.PriorityClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: priorityName,
					Labels: map[string]string{
						"env": "customer",
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
					Containers: []corev1.Container{
						{
							Name:  "container",
							Image: "quay.io/google-containers/pause-amd64:3.0",
						},
					},
					PriorityClassName: class.GetName(),
				},
			}

			ns := NewNamespace("")
			cs := ownerClient(tntNoDefaults.Spec.Owners[0])

			NamespaceCreation(ns, tntNoDefaults.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tntNoDefaults, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			EventuallyCreation(func() error {
				_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
				return err
			}).Should(Succeed())
		}
	})

	It("fail if default tenant PriorityClass is absent", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "tenant-default",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "container",
						Image: "quay.io/google-containers/pause-amd64:3.0",
					},
				},
			},
		}

		ns := NewNamespace("")
		cs := ownerClient(tntWithDefaults.Spec.Owners[0])

		NamespaceCreation(ns, tntWithDefaults.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithDefaults, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})

			return err
		}).ShouldNot(Succeed())

		Expect(k8sClient.Delete(context.TODO(), ns)).Should(Succeed())
	})

	It("should mutate to default tenant PriorityClass", func() {
		class := tenantDefault.DeepCopy()
		class.SetResourceVersion("")
		Expect(k8sClient.Create(context.TODO(), class)).Should(Succeed())

		ns := NewNamespace("")
		NamespaceCreation(ns, tntWithDefaults.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithDefaults, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tenant-default-present",
				Namespace: ns.GetName(),
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "container",
						Image: "quay.io/google-containers/pause-amd64:3.0",
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

	It("should mutate to default tenant PriorityClass although the cluster global one is not allowed", func() {
		class := tenantDefault.DeepCopy()
		class.SetResourceVersion("")

		global := disallowedGlobalDefault.DeepCopy()
		global.SetResourceVersion("")

		Expect(k8sClient.Create(context.TODO(), class)).Should(Succeed())
		Expect(k8sClient.Create(context.TODO(), global)).Should(Succeed())

		ns := NewNamespace("")
		NamespaceCreation(ns, tntWithDefaults.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithDefaults, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tenant-default-global-default",
				Namespace: ns.GetName(),
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "container",
						Image: "quay.io/google-containers/pause-amd64:3.0",
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

	It("should mutate to default tenant PriorityClass although the cluster global one is allowed", func() {
		class := tenantDefault.DeepCopy()
		class.SetResourceVersion("")
		Expect(k8sClient.Create(context.TODO(), class)).Should(Succeed())

		global := globalDefault.DeepCopy()
		global.SetResourceVersion("")
		Expect(k8sClient.Create(context.TODO(), global)).Should(Succeed())

		ns := NewNamespace("")
		NamespaceCreation(ns, tntWithDefaults.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithDefaults, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tenant-default-allowed",
				Namespace: ns.GetName(),
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "container",
						Image: "quay.io/google-containers/pause-amd64:3.0",
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
