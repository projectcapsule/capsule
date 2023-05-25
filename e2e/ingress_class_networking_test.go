//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/api"
	"github.com/clastix/capsule/pkg/utils"
)

var _ = Describe("when Tenant handles Ingress classes with networking.k8s.io/v1", func() {
	tntNoDefault := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ic-selector-networking-v1",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: []capsulev1beta2.OwnerSpec{
				{
					Name: "ingress-selector",
					Kind: "User",
				},
			},
			IngressOptions: capsulev1beta2.IngressOptions{
				AllowedClasses: &api.DefaultAllowedListSpec{
					SelectorAllowedListSpec: api.SelectorAllowedListSpec{
						AllowedListSpec: api.AllowedListSpec{
							Exact: []string{"nginx", "haproxy"},
							Regex: "^oil-.*$",
						},
						LabelSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"env": "customers",
							},
						},
					},
				},
			},
		},
	}

	tntWithDefault := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ic-default-networking-v1",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: []capsulev1beta2.OwnerSpec{
				{
					Name: "ingress-default",
					Kind: "User",
				},
			},
			IngressOptions: capsulev1beta2.IngressOptions{
				AllowedClasses: &api.DefaultAllowedListSpec{
					Default: "tenant-default",
					SelectorAllowedListSpec: api.SelectorAllowedListSpec{
						LabelSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"name": "tenant-default",
							},
						},
					},
				},
			},
		},
	}

	tenantDefault := networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-default",
			Labels: map[string]string{
				"name": "tenant-default",
				"env":  "e2e",
			},
		},
		Spec: networkingv1.IngressClassSpec{
			Controller: "k8s.io/ingress-nginx",
		},
	}

	globalDefault := networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "global-default",
			Labels: map[string]string{
				"name": "global-default",
				"env":  "customers",
			},
			Annotations: map[string]string{
				"ingressclass.kubernetes.io/is-default-class": "true",
			},
		},
		Spec: networkingv1.IngressClassSpec{
			Controller: "k8s.io/ingress-nginx",
		},
	}

	disallowedGlobalDefault := networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "disallowed",
			Labels: map[string]string{
				"name": "disallowed-global-default",
				"env":  "e2e",
			},
			Annotations: map[string]string{
				"ingressclass.kubernetes.io/is-default-class": "true",
			},
		},
		Spec: networkingv1.IngressClassSpec{
			Controller: "k8s.io/ingress-nginx",
		},
	}

	JustBeforeEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{tntWithDefault, tntNoDefault} {
			EventuallyCreation(func() error {
				tnt.ResourceVersion = ""

				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
		}
	})

	JustAfterEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{tntWithDefault, tntNoDefault} {
			Eventually(func() error {
				return k8sClient.Delete(context.TODO(), tnt)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}

		Eventually(func() (err error) {
			req, _ := labels.NewRequirement("env", selection.Exists, nil)

			return k8sClient.DeleteAllOf(context.TODO(), &networkingv1.IngressClass{}, &client.DeleteAllOfOptions{
				ListOptions: client.ListOptions{
					LabelSelector: labels.NewSelector().Add(*req),
				},
			})
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("should block a non allowed class", func() {
		if err := k8sClient.List(context.Background(), &networkingv1.IngressList{}); err != nil {
			if utils.IsUnsupportedAPI(err) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

		ns := NewNamespace("")
		cs := ownerClient(tntNoDefault.Spec.Owners[0])

		NamespaceCreation(ns, tntNoDefault.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntNoDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("non-specifying at all", func() {
			Eventually(func() (err error) {
				i := &networkingv1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name: "denied-ingress",
					},
					Spec: networkingv1.IngressSpec{
						DefaultBackend: &networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: "foo",
								Port: networkingv1.ServiceBackendPort{
									Number: 8080,
								},
							},
						},
					},
				}
				_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), i, metav1.CreateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
		By("defining as deprecated annotation", func() {
			Eventually(func() (err error) {
				i := &networkingv1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name: "denied-ingress",
						Annotations: map[string]string{
							"kubernetes.io/ingress.class": "the-worst-ingress-available",
						},
					},
					Spec: networkingv1.IngressSpec{
						DefaultBackend: &networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: "foo",
								Port: networkingv1.ServiceBackendPort{
									Number: 8080,
								},
							},
						},
					},
				}
				_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), i, metav1.CreateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
		By("using the ingressClassName", func() {
			Eventually(func() (err error) {
				i := &networkingv1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name: "denied-ingress",
					},
					Spec: networkingv1.IngressSpec{
						IngressClassName: pointer.String("the-worst-ingress-available"),
						DefaultBackend: &networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: "foo",
								Port: networkingv1.ServiceBackendPort{
									Number: 8080,
								},
							},
						},
					},
				}
				_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), i, metav1.CreateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
	})

	It("should allow enabled class using the deprecated annotation for networking.k8s.io/v1", func() {
		if err := k8sClient.List(context.Background(), &networkingv1.IngressList{}); err != nil {
			if utils.IsUnsupportedAPI(err) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

		ns := NewNamespace("")
		cs := ownerClient(tntNoDefault.Spec.Owners[0])

		NamespaceCreation(ns, tntNoDefault.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntNoDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		for _, c := range tntNoDefault.Spec.IngressOptions.AllowedClasses.Exact {
			Eventually(func() (err error) {
				i := &networkingv1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name: c,
						Annotations: map[string]string{
							"kubernetes.io/ingress.class": c,
						},
					},
					Spec: networkingv1.IngressSpec{
						DefaultBackend: &networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: "foo",
								Port: networkingv1.ServiceBackendPort{
									Number: 8080,
								},
							},
						},
					},
				}
				_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), i, metav1.CreateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})

	It("should allow enabled class using the ingressClassName field", func() {
		if err := k8sClient.List(context.Background(), &networkingv1.IngressList{}); err != nil {
			if utils.IsUnsupportedAPI(err) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

		ns := NewNamespace("")
		cs := ownerClient(tntNoDefault.Spec.Owners[0])

		NamespaceCreation(ns, tntNoDefault.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntNoDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		for _, c := range tntNoDefault.Spec.IngressOptions.AllowedClasses.Exact {
			Eventually(func() (err error) {
				i := &networkingv1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name: c,
					},
					Spec: networkingv1.IngressSpec{
						IngressClassName: &c,
						DefaultBackend: &networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: "foo",
								Port: networkingv1.ServiceBackendPort{
									Number: 8080,
								},
							},
						},
					},
				}
				_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), i, metav1.CreateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})

	It("should allow enabled Ingress by regex using the deprecated annotation", func() {
		if err := k8sClient.List(context.Background(), &networkingv1.IngressList{}); err != nil {
			if utils.IsUnsupportedAPI(err) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

		ns := NewNamespace("")
		cs := ownerClient(tntNoDefault.Spec.Owners[0])
		ingressClass := "oil-ingress"

		NamespaceCreation(ns, tntNoDefault.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntNoDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		Eventually(func() (err error) {
			i := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: ingressClass,
					Annotations: map[string]string{
						"kubernetes.io/ingress.class": ingressClass,
					},
				},
				Spec: networkingv1.IngressSpec{
					DefaultBackend: &networkingv1.IngressBackend{
						Service: &networkingv1.IngressServiceBackend{
							Name: "foo",
							Port: networkingv1.ServiceBackendPort{
								Number: 8080,
							},
						},
					},
				},
			}
			_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), i, metav1.CreateOptions{})
			return
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("should allow enabled Ingress by regex using the ingressClassName field", func() {
		if err := k8sClient.List(context.Background(), &networkingv1.IngressList{}); err != nil {
			if utils.IsUnsupportedAPI(err) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

		ns := NewNamespace("")
		cs := ownerClient(tntNoDefault.Spec.Owners[0])
		ingressClass := "oil-haproxy"

		NamespaceCreation(ns, tntNoDefault.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntNoDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		Eventually(func() (err error) {
			i := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: ingressClass,
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: &ingressClass,
					DefaultBackend: &networkingv1.IngressBackend{
						Service: &networkingv1.IngressServiceBackend{
							Name: "foo",
							Port: networkingv1.ServiceBackendPort{
								Number: 8080,
							},
						},
					},
				},
			}
			_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), i, metav1.CreateOptions{})
			return
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("should allow enabled Ingress by selector using the deprecated annotation", func() {
		if err := k8sClient.List(context.Background(), &networkingv1.IngressList{}); err != nil {
			if utils.IsUnsupportedAPI(err) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

		for i, sc := range []string{"customer-nginx", "customer-haproxy"} {
			ingressClass := strings.Join([]string{sc, "-", strconv.Itoa(i)}, "")
			class := &networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: ingressClass,
					Labels: map[string]string{
						"name": ingressClass,
						"env":  "customers",
					},
				},
				Spec: networkingv1.IngressClassSpec{
					Controller: "k8s.io/ingress-nginx",
				},
			}
			Expect(k8sClient.Create(context.TODO(), class)).Should(Succeed())

			i := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("allowed-%s", ingressClass),
					Annotations: map[string]string{
						"kubernetes.io/ingress.class": ingressClass,
					},
				},
				Spec: networkingv1.IngressSpec{
					DefaultBackend: &networkingv1.IngressBackend{
						Service: &networkingv1.IngressServiceBackend{
							Name: "foo",
							Port: networkingv1.ServiceBackendPort{
								Number: 8080,
							},
						},
					},
				},
			}

			ns := NewNamespace("")
			cs := ownerClient(tntNoDefault.Spec.Owners[0])

			NamespaceCreation(ns, tntNoDefault.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tntNoDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			EventuallyCreation(func() error {
				_, err := cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), i, metav1.CreateOptions{})
				return err
			}).Should(Succeed())
		}
	})

	It("should allow enabled Ingress by selector using the ingressClassName field", func() {
		if err := k8sClient.List(context.Background(), &networkingv1.IngressList{}); err != nil {
			if utils.IsUnsupportedAPI(err) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

		for i, sc := range []string{"customer-nginx", "customer-haproxy"} {
			ingressClass := strings.Join([]string{sc, "-", strconv.Itoa(i)}, "")
			class := &networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: ingressClass,
					Labels: map[string]string{
						"name": ingressClass,
						"env":  "customers",
					},
				},
				Spec: networkingv1.IngressClassSpec{
					Controller: "k8s.io/ingress-nginx",
				},
			}
			Expect(k8sClient.Create(context.TODO(), class)).Should(Succeed())

			i := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("allowed-%s", ingressClass),
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: &ingressClass,
					DefaultBackend: &networkingv1.IngressBackend{
						Service: &networkingv1.IngressServiceBackend{
							Name: "foo",
							Port: networkingv1.ServiceBackendPort{
								Number: 8080,
							},
						},
					},
				},
			}

			ns := NewNamespace("")
			cs := ownerClient(tntNoDefault.Spec.Owners[0])

			NamespaceCreation(ns, tntNoDefault.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tntNoDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			EventuallyCreation(func() error {
				_, err := cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), i, metav1.CreateOptions{})
				return err
			}).Should(Succeed())
		}
	})

	It("should mutate to default tenant IngressClass (class not does not exist)", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		i := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "e2e-default-ingress",
				Namespace: ns.GetName(),
			},
			Spec: networkingv1.IngressSpec{
				DefaultBackend: &networkingv1.IngressBackend{
					Service: &networkingv1.IngressServiceBackend{
						Name: "foo",
						Port: networkingv1.ServiceBackendPort{
							Number: 8080,
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), i)
		}).Should(Succeed())

		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: i.GetName(), Namespace: ns.GetName()}, i))
		Expect(*i.Spec.IngressClassName).To(Equal("tenant-default"))
	})

	It("should mutate to default tenant IngressClass (class exists)", func() {
		if err := k8sClient.List(context.Background(), &networkingv1.IngressList{}); err != nil {
			if utils.IsUnsupportedAPI(err) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

		class := tenantDefault
		Expect(k8sClient.Create(context.TODO(), &class)).Should(Succeed())

		ns := NewNamespace("")
		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		i := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "e2e-default-ingress",
				Namespace: ns.GetName(),
			},
			Spec: networkingv1.IngressSpec{
				DefaultBackend: &networkingv1.IngressBackend{
					Service: &networkingv1.IngressServiceBackend{
						Name: "foo",
						Port: networkingv1.ServiceBackendPort{
							Number: 8080,
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), i)
		}).Should(Succeed())

		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: i.GetName(), Namespace: ns.GetName()}, i))
		Expect(*i.Spec.IngressClassName).To(Equal(class.GetName()))
	})

	It("shoult mutate to default tenant IngressClass although the cluster global one is not allowed", func() {
		if err := k8sClient.List(context.Background(), &networkingv1.IngressList{}); err != nil {
			if utils.IsUnsupportedAPI(err) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

		class := tenantDefault
		global := disallowedGlobalDefault

		Expect(k8sClient.Create(context.TODO(), &class)).Should(Succeed())
		Expect(k8sClient.Create(context.TODO(), &global)).Should(Succeed())

		ns := NewNamespace("")
		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		i := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "e2e-default-global-ingress",
				Namespace: ns.GetName(),
			},
			Spec: networkingv1.IngressSpec{
				DefaultBackend: &networkingv1.IngressBackend{
					Service: &networkingv1.IngressServiceBackend{
						Name: "foo",
						Port: networkingv1.ServiceBackendPort{
							Number: 8080,
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), i)
		}).Should(Succeed())
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: i.GetName(), Namespace: ns.GetName()}, i))
		Expect(*i.Spec.IngressClassName).To(Equal(class.GetName()))
		// Run Patch To verify same happens on Update
		i.Spec.IngressClassName = nil
		Expect(k8sClient.Update(context.Background(), i)).Should(Succeed())
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: i.GetName(), Namespace: ns.GetName()}, i))
		Expect(*i.Spec.IngressClassName).To(Equal(class.GetName()))
	})

	It("should mutate to default tenant IngressClass although the cluster global one is allowed", func() {
		if err := k8sClient.List(context.Background(), &networkingv1.IngressList{}); err != nil {
			if utils.IsUnsupportedAPI(err) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

		class := tenantDefault
		global := globalDefault

		Expect(k8sClient.Create(context.TODO(), &class)).Should(Succeed())
		Expect(k8sClient.Create(context.TODO(), &global)).Should(Succeed())

		ns := NewNamespace("")
		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		i := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "e2e-default-global-ingress",
				Namespace: ns.GetName(),
			},
			Spec: networkingv1.IngressSpec{
				DefaultBackend: &networkingv1.IngressBackend{
					Service: &networkingv1.IngressServiceBackend{
						Name: "foo",
						Port: networkingv1.ServiceBackendPort{
							Number: 8080,
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), i)
		}).Should(Succeed())
		Expect(*i.Spec.IngressClassName).To(Equal(class.GetName()))
		// Run Patch To verify same happens on Update
		i.Spec.IngressClassName = nil
		Expect(k8sClient.Update(context.Background(), i)).Should(Succeed())
		Expect(*i.Spec.IngressClassName).To(Equal(class.GetName()))
	})
})
