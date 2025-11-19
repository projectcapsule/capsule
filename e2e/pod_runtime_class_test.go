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

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

var _ = Describe("enforcing a Runtime Class", Label("pod", "classes", "current"), func() {
	tntWithDefault := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-runtime-selection",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					UserSpec: api.UserSpec{
						Name: "george",
						Kind: "User",
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
							"env": "customers",
						},
					},
				},
			},
		},
	}

	tntNoRestrictions := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-runtime-no-restrictions",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: []api.OwnerSpec{
				{
					UserSpec: api.UserSpec{
						Name: "e2e-gateway-no-restrictions",
						Kind: "User",
					},
				},
			},
		},
	}

	defaultRuntime := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-runtime",
			Labels: map[string]string{
				"name": "default-runtime",
				"env":  "customers",
			},
		},
		Handler: "custom-handler",
	}

	legacy := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "legacy",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Handler: "custom-handler",
	}

	disallowed := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "disallowed",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Handler: "custom-handler",
	}

	customerContainerd := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "customer-containerd",
			Labels: map[string]string{
				"name": "customer-containerd",
				"env":  "customers",
			},
		},
		Handler: "custom-handler",
	}

	customerKubevirt := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "customer-virt",
			Labels: map[string]string{
				"name": "customer-virt",
				"env":  "customers",
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
			EventuallyCreation(func() error {
				return ignoreNotFound(k8sClient.Delete(context.TODO(), tnt))
			}).Should(Succeed())
		}

		Eventually(func() (err error) {
			req, _ := labels.NewRequirement("env", selection.Exists, nil)

			return k8sClient.DeleteAllOf(context.TODO(), &nodev1.RuntimeClass{}, &client.DeleteAllOfOptions{
				ListOptions: client.ListOptions{
					LabelSelector: labels.NewSelector().Add(*req),
				},
			})
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("should allow all classes", func() {
		all := []string{customerUni.GetName(), customerKubevirt.GetName(), customerContainerd.GetName(), legacy.GetName(), disallowed.GetName()}

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

		ns := NewNamespace("")
		NamespaceCreation(ns, tntNoRestrictions.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntNoRestrictions, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("providing any runtimeclass", func() {
			for _, class := range all {
				Eventually(func() (err error) {

					g := &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      class + "-container",
							Namespace: ns.GetName(),
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "container",
									Image: "quay.io/google-containers/pause-amd64:3.0",
								},
							},
							RuntimeClassName: &class,
						},
					}

					return k8sClient.Create(context.TODO(), g)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})

		By("Verify Status (Deletion)", func() {
			for _, crd := range []*nodev1.RuntimeClass{customerKubevirt} {
				Expect(ignoreNotFound(k8sClient.Delete(context.TODO(), crd))).To(Succeed())
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
			ns := NewNamespace("rt-disallow")
			NamespaceCreation(ns, tntWithDefault.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())

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
					RuntimeClassName: &disallowed.Name,
				},
			}

			cs := ownerClient(tntWithDefault.Spec.Owners[0].UserSpec)
			EventuallyCreation(func() error {
				_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
				return err
			}).ShouldNot(Succeed())
		})

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
	})

	It("should allow exact match", func() {
		ns := NewNamespace("rt-exact-match")
		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
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
				RuntimeClassName: &legacy.Name,
			},
		}

		cs := ownerClient(tntWithDefault.Spec.Owners[0].UserSpec)
		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
			return err
		}).Should(Succeed())
	})

	It("should allow regex match", func() {
		ns := NewNamespace("rc-regex-match")

		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())

		for i, rt := range []string{"hardened-crio", "hardened-containerd", "hardened-dockerd"} {
			runtimeName := strings.Join([]string{rt, "-", strconv.Itoa(i)}, "")
			runtime := &nodev1.RuntimeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: runtimeName,
				},
				Handler: "custom-handler",
			}

			Expect(k8sClient.Create(context.TODO(), runtime)).Should(Succeed())

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: rt,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container",
							Image: "quay.io/google-containers/pause-amd64:3.0",
						},
					},
					RuntimeClassName: &runtimeName,
				},
			}

			cs := ownerClient(tntWithDefault.Spec.Owners[0].UserSpec)

			EventuallyCreation(func() error {
				_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
				return err
			}).Should(Succeed())

			Expect(k8sClient.Delete(context.TODO(), runtime)).Should(Succeed())
		}
	})

	It("should allow selector match", func() {
		ns := NewNamespace("rc-selector-match")

		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())

		for i, rt := range []string{"customer-containerd", "customer-crio", "customer-dockerd"} {
			runtimeName := strings.Join([]string{rt, "-", strconv.Itoa(i)}, "")
			runtime := &nodev1.RuntimeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: runtimeName,
					Labels: map[string]string{
						"name": runtimeName,
						"env":  "customers",
					},
				},
				Handler: "custom-handler",
			}

			Expect(k8sClient.Create(context.TODO(), runtime)).Should(Succeed())

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: rt,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container",
							Image: "quay.io/google-containers/pause-amd64:3.0",
						},
					},
					RuntimeClassName: &runtimeName,
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
		ns := NewNamespace("rc-default")

		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())

		Expect(k8sClient.Create(context.TODO(), defaultRuntime)).Should(Succeed())

		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rc-default",
				Namespace: ns.Name,
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

		cs := ownerClient(tntWithDefault.Spec.Owners[0].UserSpec)

		var createdPod *corev1.Pod

		EventuallyCreation(func() (err error) {
			createdPod, err = cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), &pod, metav1.CreateOptions{})

			return err
		}).Should(Succeed())

		Expect(createdPod.Spec.RuntimeClassName).NotTo(BeNil())
		_, err := Equal(createdPod.Spec.RuntimeClassName).Match(tntWithDefault.Spec.RuntimeClasses.Default)
		Expect(err).NotTo(HaveOccurred())

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
	})
})
