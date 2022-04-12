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
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

var _ = Describe("when Tenant handles Ingress classes with extensions/v1beta1", func() {
	tnt := &capsulev1beta1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ingress-class-extensions-v1beta1",
		},
		Spec: capsulev1beta1.TenantSpec{
			Owners: capsulev1beta1.OwnerListSpec{
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

	It("should block a non allowed class for extensions/v1beta1", func() {
		ns := NewNamespace("ingress-class-disallowed-extensions-v1beta1")
		cs := ownerClient(tnt.Spec.Owners[0])

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("non-specifying at all", func() {
			if err := k8sClient.List(context.Background(), &extensionsv1beta1.IngressList{}); err != nil {
				missingAPIError := &meta.NoKindMatchError{}
				if errors.As(err, &missingAPIError) {
					Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
				}
			}

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
			if err := k8sClient.List(context.Background(), &extensionsv1beta1.IngressList{}); err != nil {
				missingAPIError := &meta.NoKindMatchError{}
				if errors.As(err, &missingAPIError) {
					Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
				}
			}

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
			if err := k8sClient.List(context.Background(), &extensionsv1beta1.IngressList{}); err != nil {
				missingAPIError := &meta.NoKindMatchError{}
				if errors.As(err, &missingAPIError) {
					Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
				}
			}

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
		ns := NewNamespace("ingress-class-allowed-annotation-extensions-v1beta1")
		cs := ownerClient(tnt.Spec.Owners[0])

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		for _, c := range tnt.Spec.IngressOptions.AllowedClasses.Exact {
			Eventually(func() (err error) {
				if err := k8sClient.List(context.Background(), &extensionsv1beta1.IngressList{}); err != nil {
					missingAPIError := &meta.NoKindMatchError{}
					if errors.As(err, &missingAPIError) {
						Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
					}
				}

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
		if err := k8sClient.List(context.Background(), &extensionsv1beta1.IngressList{}); err != nil {
			missingAPIError := &meta.NoKindMatchError{}
			if errors.As(err, &missingAPIError) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

		if version := GetKubernetesVersion(); version.Major() == 1 && version.Minor() < 18 {
			Skip("Running test on Kubernetes " + version.String() + ", doesn't provide .spec.ingressClassName")
		}

		ns := NewNamespace("ingress-class-allowed-annotation-extensions-v1beta1")
		cs := ownerClient(tnt.Spec.Owners[0])

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		for _, c := range tnt.Spec.IngressOptions.AllowedClasses.Exact {
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
		ns := NewNamespace("ingress-class-allowed-annotation-extensions-v1beta1")
		cs := ownerClient(tnt.Spec.Owners[0])
		ingressClass := "oil-ingress"

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		Eventually(func() (err error) {
			if err := k8sClient.List(context.Background(), &extensionsv1beta1.IngressList{}); err != nil {
				missingAPIError := &meta.NoKindMatchError{}
				if errors.As(err, &missingAPIError) {
					Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
				}
			}

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
		ns := NewNamespace("ingress-class-allowed-annotation-extensions-v1beta1")
		cs := ownerClient(tnt.Spec.Owners[0])
		ingressClass := "oil-haproxy"

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		Eventually(func() (err error) {
			if err := k8sClient.List(context.Background(), &extensionsv1beta1.IngressList{}); err != nil {
				missingAPIError := &meta.NoKindMatchError{}
				if errors.As(err, &missingAPIError) {
					Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
				}
			}

			if version := GetKubernetesVersion(); version.Major() == 1 && version.Minor() < 18 {
				Skip("Running test on Kubernetes " + version.String() + ", doesn't provide .spec.ingressClassName")
			}

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
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})
})
