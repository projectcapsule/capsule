//+build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("when Tenant handles Ingress classes", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ingress-class",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "ingress",
				Kind: "User",
			},
			IngressClasses: &v1alpha1.AllowedListSpec{
				Exact: []string{
					"nginx",
					"haproxy",
				},
				Regex: "^oil-.*$",
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
		ns := NewNamespace("ingress-class-disallowed")
		cs := ownerClient(tnt)

		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("non-specifying at all", func() {
			Eventually(func() (err error) {
				i := &extensionsv1beta1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name: "denied-ingress",
					},
					Spec: extensionsv1beta1.IngressSpec{
						Backend: &extensionsv1beta1.IngressBackend{
							ServiceName: "foo",
							ServicePort: intstr.FromInt(8080),
						},
					},
				}
				_, err = cs.ExtensionsV1beta1().Ingresses(ns.GetName()).Create(context.TODO(), i, metav1.CreateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
		By("defining as deprecated annotation", func() {
			Eventually(func() (err error) {
				i := &extensionsv1beta1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name: "denied-ingress",
						Annotations: map[string]string{
							"kubernetes.io/ingress.class": "the-worst-ingress-available",
						},
					},
					Spec: extensionsv1beta1.IngressSpec{
						Backend: &extensionsv1beta1.IngressBackend{
							ServiceName: "foo",
							ServicePort: intstr.FromInt(8080),
						},
					},
				}
				_, err = cs.ExtensionsV1beta1().Ingresses(ns.GetName()).Create(context.TODO(), i, metav1.CreateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
		By("using the ingressClassName", func() {
			Eventually(func() (err error) {
				i := &extensionsv1beta1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name: "denied-ingress",
					},
					Spec: extensionsv1beta1.IngressSpec{
						IngressClassName: pointer.StringPtr("the-worst-ingress-available"),
						Backend: &extensionsv1beta1.IngressBackend{
							ServiceName: "foo",
							ServicePort: intstr.FromInt(8080),
						},
					},
				}
				_, err = cs.ExtensionsV1beta1().Ingresses(ns.GetName()).Create(context.TODO(), i, metav1.CreateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
	})

	It("should allow enabled class using the deprecated annotation", func() {
		ns := NewNamespace("ingress-class-allowed-annotation")
		cs := ownerClient(tnt)

		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		for _, c := range tnt.Spec.IngressClasses.Exact {
			Eventually(func() (err error) {
				i := &extensionsv1beta1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name: c,
						Annotations: map[string]string{
							"kubernetes.io/ingress.class": c,
						},
					},
					Spec: extensionsv1beta1.IngressSpec{
						Backend: &extensionsv1beta1.IngressBackend{
							ServiceName: "foo",
							ServicePort: intstr.FromInt(8080),
						},
					},
				}
				_, err = cs.ExtensionsV1beta1().Ingresses(ns.GetName()).Create(context.TODO(), i, metav1.CreateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})

	It("should allow enabled class using the ingressClassName field", func() {
		ns := NewNamespace("ingress-class-allowed-annotation")
		cs := ownerClient(tnt)

		maj, min, v := GetKubernetesSemVer()
		if maj == 1 && min < 18 {
			Skip("Running test on Kubernetes " + v + ", doesn't provide .spec.ingressClassName")
		}

		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		for _, c := range tnt.Spec.IngressClasses.Exact {
			Eventually(func() (err error) {
				i := &extensionsv1beta1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name: c,
					},
					Spec: extensionsv1beta1.IngressSpec{
						IngressClassName: &c,
						Backend: &extensionsv1beta1.IngressBackend{
							ServiceName: "foo",
							ServicePort: intstr.FromInt(8080),
						},
					},
				}
				_, err = cs.ExtensionsV1beta1().Ingresses(ns.GetName()).Create(context.TODO(), i, metav1.CreateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})

	It("should allow enabled Ingress by regex using the deprecated annotation", func() {
		ns := NewNamespace("ingress-class-allowed-annotation")
		cs := ownerClient(tnt)
		ingressClass := "oil-ingress"

		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		Eventually(func() (err error) {
			i := &extensionsv1beta1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: ingressClass,
					Annotations: map[string]string{
						"kubernetes.io/ingress.class": ingressClass,
					},
				},
				Spec: extensionsv1beta1.IngressSpec{
					Backend: &extensionsv1beta1.IngressBackend{
						ServiceName: "foo",
						ServicePort: intstr.FromInt(8080),
					},
				},
			}
			_, err = cs.ExtensionsV1beta1().Ingresses(ns.GetName()).Create(context.TODO(), i, metav1.CreateOptions{})
			return
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("should allow enabled Ingress by regex using the ingressClassName field", func() {
		ns := NewNamespace("ingress-class-allowed-annotation")
		cs := ownerClient(tnt)
		ingressClass := "oil-haproxy"

		maj, min, v := GetKubernetesSemVer()
		if maj == 1 && min < 18 {
			Skip("Running test on Kubernetes " + v + ", doesn't provide .spec.ingressClassName")
		}

		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		Eventually(func() (err error) {
			i := &extensionsv1beta1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: ingressClass,
				},
				Spec: extensionsv1beta1.IngressSpec{
					IngressClassName: &ingressClass,
					Backend: &extensionsv1beta1.IngressBackend{
						ServiceName: "foo",
						ServicePort: intstr.FromInt(8080),
					},
				},
			}
			_, err = cs.ExtensionsV1beta1().Ingresses(ns.GetName()).Create(context.TODO(), i, metav1.CreateOptions{})
			return
		}, 600, defaultPollInterval).Should(Succeed())
	})
})
