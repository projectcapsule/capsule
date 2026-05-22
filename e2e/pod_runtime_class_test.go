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
	nodev1 "k8s.io/api/node/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

var _ = Describe("enforcing a Runtime Class", Ordered, Label("pod", "classes", "runtimeclass"), func() {
	tntWithDefault := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-runtime-selection",
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
			RuntimeClasses: &api.DefaultAllowedListSpec{
				Default: "default-runtime",
				SelectorAllowedListSpec: api.SelectorAllowedListSpec{
					AllowedListSpec: api.AllowedListSpec{
						Exact: []string{"legacy"},
						Regex: "^hardened-.*$",
					},
					LabelSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"environment": "customers",
						},
					},
				},
			},
		},
	}

	tntNoRestrictions := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-runtime-no-restrictions",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: []rbac.OwnerSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-gateway-no-restrictions",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	defaultRuntime := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-runtime",
			Labels: map[string]string{
				"name":        "default-runtime",
				"environment": "customers",
				"env":         "e2e",
			},
		},
		Handler: "custom-handler",
	}

	legacy := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "legacy",
			Labels: map[string]string{
				"environment": "disallowed",
				"env":         "e2e",
			},
		},
		Handler: "custom-handler",
	}

	disallowed := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "disallowed",
			Labels: map[string]string{
				"environment": "disallowed",
				"env":         "e2e",
			},
		},
		Handler: "custom-handler",
	}

	customerContainerd := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "customer-containerd",
			Labels: map[string]string{
				"name":        "customer-containerd",
				"environment": "customers",
				"env":         "e2e",
			},
		},
		Handler: "custom-handler",
	}

	customerKubevirt := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "customer-virt",
			Labels: map[string]string{
				"name":        "customer-virt",
				"environment": "customers",
				"env":         "e2e",
			},
		},
		Handler: "custom-handler",
	}

	customerUni := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "customer-runu",
			Labels: map[string]string{
				"name": "customer-runu",
				"env":  "e2e",
			},
		},
		Handler: "custom-handler",
	}

	JustBeforeEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{tntWithDefault, tntNoRestrictions} {
			tnt.ResourceVersion = ""
			EventuallyCreation(func() error {
				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())

			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}

		for _, crd := range []*nodev1.RuntimeClass{legacy, disallowed, customerUni, customerKubevirt, customerContainerd} {
			Eventually(func() error {
				crd.ResourceVersion = ""
				return k8sClient.Create(context.TODO(), crd)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})
	JustAfterEach(func() {

		for _, tnt := range []*capsulev1beta2.Tenant{tntWithDefault, tntNoRestrictions} {
			EventuallyDeletion(tnt)
		}

		req, err := labels.NewRequirement("env", selection.Equals, []string{"e2e"})
		Expect(err).NotTo(HaveOccurred())

		var list nodev1.RuntimeClassList
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
		all := []string{customerUni.GetName(), customerKubevirt.GetName(), customerContainerd.GetName(), legacy.GetName(), disallowed.GetName()}

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

				return t.Status.Classes.RuntimeClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf(all))
		})

		By("providing any runtimeclass", func() {
			for _, class := range all {
				Eventually(func() (err error) {

					g := &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      class + "-container",
							Namespace: ns.GetName(),
						},
						Spec: corev1.PodSpec{
							SecurityContext:  nobodyPodSecurityContext(),
							RuntimeClassName: &class,
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
			for _, crd := range []*nodev1.RuntimeClass{customerKubevirt} {
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

				return t.Status.Classes.RuntimeClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf(customerUni.GetName(), customerContainerd.GetName(), legacy.GetName(), disallowed.GetName()))
		})
	})

	It("should block non allowed Runtime Class", func() {
		By("blocked disallowed runtime", func() {
			ns := NewNamespace("rt-disallow", map[string]string{
				meta.TenantLabel: tntWithDefault.GetName(),
			})
			NamespaceCreation(ns, tntWithDefault.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tntWithDefault, ns).Should(Succeed())

			By("verify status", func() {
				Eventually(func() ([]string, error) {
					t := &capsulev1beta2.Tenant{}
					if err := k8sClient.Get(
						context.TODO(),
						types.NamespacedName{Name: tntWithDefault.GetName()},
						t,
					); err != nil {
						return nil, err
					}

					return t.Status.Classes.RuntimeClasses, nil
				}, defaultTimeoutInterval, defaultPollInterval).
					Should(ConsistOf(customerContainerd.GetName(), customerKubevirt.GetName(), legacy.GetName()))
			})

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "container",
				},
				Spec: corev1.PodSpec{
					SecurityContext:  nobodyPodSecurityContext(),
					RuntimeClassName: &disallowed.Name,
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

			cs := ownerClient(tntWithDefault.Spec.Owners[0].UserSpec)
			EventuallyCreation(func() error {
				_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
				return err
			}).ShouldNot(Succeed())
		})
	})

	It("should allow exact match", func() {
		ns := NewNamespace("rt-exact-match", map[string]string{
			meta.TenantLabel: tntWithDefault.GetName(),
		})
		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tntWithDefault, ns).Should(Succeed())

		By("verify status", func() {
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntWithDefault.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.RuntimeClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf(customerContainerd.GetName(), customerKubevirt.GetName(), legacy.GetName()))
		})

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "container",
			},
			Spec: corev1.PodSpec{
				SecurityContext:  nobodyPodSecurityContext(),
				RuntimeClassName: &legacy.Name,
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

		cs := ownerClient(tntWithDefault.Spec.Owners[0].UserSpec)
		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
			return err
		}).Should(Succeed())
	})

	It("should allow regex match", func() {
		ns := NewNamespace("rc-regex-match", map[string]string{
			meta.TenantLabel: tntWithDefault.GetName(),
		})
		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tntWithDefault, ns).Should(Succeed())

		By("verify status", func() {
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntWithDefault.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.RuntimeClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf(customerContainerd.GetName(), customerKubevirt.GetName(), legacy.GetName()))
		})

		for i, rt := range []string{"hardened-crio", "hardened-containerd", "hardened-dockerd"} {
			runtimeName := strings.Join([]string{rt, "-", strconv.Itoa(i)}, "")
			runtime := &nodev1.RuntimeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: runtimeName,
					Labels: map[string]string{
						"env": "e2e",
					},
				},
				Handler: "custom-handler",
			}

			Expect(k8sClient.Create(context.TODO(), runtime)).Should(Succeed())

			By("verify status", func() {
				Eventually(func() ([]string, error) {
					t := &capsulev1beta2.Tenant{}
					if err := k8sClient.Get(
						context.TODO(),
						types.NamespacedName{Name: tntWithDefault.GetName()},
						t,
					); err != nil {
						return nil, err
					}

					return t.Status.Classes.RuntimeClasses, nil
				}, defaultTimeoutInterval, defaultPollInterval).
					Should(ContainElement(runtime.GetName()))
			})

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: rt,
				},
				Spec: corev1.PodSpec{
					SecurityContext:  nobodyPodSecurityContext(),
					RuntimeClassName: &runtimeName,
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

			cs := ownerClient(tntWithDefault.Spec.Owners[0].UserSpec)

			EventuallyCreation(func() error {
				_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
				return err
			}).Should(Succeed())

			EventuallyDeletion(runtime)
		}
	})

	It("should allow selector match", func() {
		ns := NewNamespace("rc-selector-match", map[string]string{
			meta.TenantLabel: tntWithDefault.GetName(),
		})
		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tntWithDefault, ns).Should(Succeed())

		for i, rt := range []string{"customer-containerd", "customer-crio", "customer-dockerd"} {
			runtimeName := strings.Join([]string{rt, "-", strconv.Itoa(i)}, "")
			runtime := &nodev1.RuntimeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: runtimeName,
					Labels: map[string]string{
						"name":        runtimeName,
						"env":         "e2e",
						"environment": "customers",
					},
				},
				Handler: "custom-handler",
			}

			Expect(k8sClient.Create(context.TODO(), runtime)).Should(Succeed())

			By("verify status", func() {
				Eventually(func() ([]string, error) {
					t := &capsulev1beta2.Tenant{}
					if err := k8sClient.Get(
						context.TODO(),
						types.NamespacedName{Name: tntWithDefault.GetName()},
						t,
					); err != nil {
						return nil, err
					}

					return t.Status.Classes.RuntimeClasses, nil
				}, defaultTimeoutInterval, defaultPollInterval).
					Should(ContainElement(runtime.GetName()))
			})

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: rt,
				},
				Spec: corev1.PodSpec{
					SecurityContext:  nobodyPodSecurityContext(),
					RuntimeClassName: &runtimeName,
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

			cs := ownerClient(tntWithDefault.Spec.Owners[0].UserSpec)

			EventuallyCreation(func() error {
				_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
				return err
			}).Should(Succeed())
		}

		By("verify status", func() {
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntWithDefault.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.RuntimeClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf("customer-containerd-0", "customer-crio-1", "customer-dockerd-2", customerContainerd.GetName(), customerKubevirt.GetName(), legacy.GetName()))
		})
	})

	It("should auto assign the default", func() {
		ns := NewNamespace("rc-default", map[string]string{
			meta.TenantLabel: tntWithDefault.GetName(),
		})
		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tntWithDefault, ns).Should(Succeed())

		Expect(k8sClient.Create(context.TODO(), defaultRuntime)).Should(Succeed())

		By("verify status", func() {
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntWithDefault.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.RuntimeClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf(defaultRuntime.GetName(), customerContainerd.GetName(), customerKubevirt.GetName(), legacy.GetName()))
		})

		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rc-default",
				Namespace: ns.Name,
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

		cs := ownerClient(tntWithDefault.Spec.Owners[0].UserSpec)

		var createdPod *corev1.Pod

		EventuallyCreation(func() (err error) {
			createdPod, err = cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), &pod, metav1.CreateOptions{})

			return err
		}).Should(Succeed())

		Expect(createdPod.Spec.RuntimeClassName).NotTo(BeNil())
		_, err := Equal(createdPod.Spec.RuntimeClassName).Match(tntWithDefault.Spec.RuntimeClasses.Default)
		Expect(err).NotTo(HaveOccurred())
	})
})
