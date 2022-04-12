//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

var _ = Describe("when Tenant handles Ingress classes with networking.k8s.io/v1", func() {
	tnt := &capsulev1beta1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ingress-class-networking-v1",
		},
		Spec: capsulev1beta1.TenantSpec{
			Owners: []capsulev1beta1.OwnerSpec{
				{
					Name: "ingress",
					Kind: "User",
				},
			},
			IngressOptions: capsulev1beta1.IngressOptions{
				AllowedClasses: &capsulev1beta1.AllowedListSpec{
					Exact: []string{
						"nginx",
						"haproxy",
					},
					Regex: "^oil-.*$",
				},
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})

	It("should block a non allowed class", func() {
		if err := k8sClient.List(context.Background(), &networkingv1.IngressList{}); err != nil {
			missingAPIError := &meta.NoKindMatchError{}
			if errors.As(err, &missingAPIError) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

		ns := NewNamespace("ingress-class-disallowed-networking-v1")
		cs := ownerClient(tnt.Spec.Owners[0])

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

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
						IngressClassName: pointer.StringPtr("the-worst-ingress-available"),
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
			missingAPIError := &meta.NoKindMatchError{}
			if errors.As(err, &missingAPIError) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

		ns := NewNamespace("ingress-class-allowed-annotation-networking-v1")
		cs := ownerClient(tnt.Spec.Owners[0])

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		for _, c := range tnt.Spec.IngressOptions.AllowedClasses.Exact {
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
			missingAPIError := &meta.NoKindMatchError{}
			if errors.As(err, &missingAPIError) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

		ns := NewNamespace("ingress-class-allowed-annotation-networking-v1")
		cs := ownerClient(tnt.Spec.Owners[0])

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		for _, c := range tnt.Spec.IngressOptions.AllowedClasses.Exact {
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
			missingAPIError := &meta.NoKindMatchError{}
			if errors.As(err, &missingAPIError) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

		ns := NewNamespace("ingress-class-allowed-annotation-networking-v1")
		cs := ownerClient(tnt.Spec.Owners[0])
		ingressClass := "oil-ingress"

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

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
			missingAPIError := &meta.NoKindMatchError{}
			if errors.As(err, &missingAPIError) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

		ns := NewNamespace("ingress-class-allowed-annotation-networking-v1")
		cs := ownerClient(tnt.Spec.Owners[0])
		ingressClass := "oil-haproxy"

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

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
})
