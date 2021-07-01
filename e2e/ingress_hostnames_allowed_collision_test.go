//+build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

var _ = Describe("when handling Ingress hostnames collision", func() {
	tnt := &capsulev1beta1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ingress-hostnames-allowed-collision",
		},
		Spec: capsulev1beta1.TenantSpec{
			Owner: capsulev1beta1.OwnerSpec{
				Name: "ingress-allowed",
				Kind: "User",
			},
		},
	}

	// scaffold a basic networking.k8s.io Ingress with name and host
	networkingIngress := func(name, hostname string) *networkingv1.Ingress {
		return &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: networkingv1.IngressSpec{
				Rules: []networkingv1.IngressRule{
					{
						Host: hostname,
					},
				},
			},
		}
	}
	// scaffold a basic extensions Ingress with name and host
	extensionsIngress := func(name, hostname string) *extensionsv1beta1.Ingress {
		return &extensionsv1beta1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: extensionsv1beta1.IngressSpec{
				Rules: []extensionsv1beta1.IngressRule{
					{
						Host: hostname,
					},
				},
			},
		}

	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())

		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1alpha1.CapsuleConfiguration) {
			configuration.Spec.AllowIngressHostnameCollision = true
		})
	})

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())

		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1alpha1.CapsuleConfiguration) {
			configuration.Spec.AllowIngressHostnameCollision = false
		})
	})

	It("should not allow creating several Ingress with same hostname", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1alpha1.CapsuleConfiguration) {
			configuration.Spec.AllowIngressHostnameCollision = false
		})

		maj, min, _ := GetKubernetesSemVer()

		ns := NewNamespace("denied-collision")
		cs := ownerClient(tnt)

		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		if maj == 1 && min > 18 {
			By("testing networking.k8s.io", func() {
				EventuallyCreation(func() (err error) {
					obj := networkingIngress("networking-1", "kubernetes.io")
					_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
					return
				}).Should(Succeed())
				EventuallyCreation(func() (err error) {
					obj := networkingIngress("networking-2", "kubernetes.io")
					_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
					return
				}).ShouldNot(Succeed())
			})
		}

		if maj == 1 && min < 22 {
			By("testing extensions", func() {
				EventuallyCreation(func() (err error) {
					obj := extensionsIngress("extensions-1", "cncf.io")
					_, err = cs.ExtensionsV1beta1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
					return
				}).Should(Succeed())
				EventuallyCreation(func() (err error) {
					obj := extensionsIngress("extensions-2", "cncf.io")
					_, err = cs.ExtensionsV1beta1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
					return
				}).ShouldNot(Succeed())
			})
		}

	})

	It("should allow creating several Ingress with same hostname", func() {
		maj, min, _ := GetKubernetesSemVer()

		ns := NewNamespace("allowed-collision")
		cs := ownerClient(tnt)

		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		if maj == 1 && min > 18 {
			By("testing networking.k8s.io", func() {
				EventuallyCreation(func() (err error) {
					obj := networkingIngress("networking-1", "kubernetes.io")
					_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
					return
				}).Should(Succeed())
				EventuallyCreation(func() (err error) {
					obj := networkingIngress("networking-2", "kubernetes.io")
					_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
					return
				}).Should(Succeed())
			})
		}

		if maj == 1 && min < 22 {
			By("testing extensions", func() {
				EventuallyCreation(func() (err error) {
					obj := extensionsIngress("extensions-1", "cncf.io")
					_, err = cs.ExtensionsV1beta1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
					return
				}).Should(Succeed())
				EventuallyCreation(func() (err error) {
					obj := extensionsIngress("extensions-2", "cncf.io")
					_, err = cs.ExtensionsV1beta1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
					return
				}).Should(Succeed())
			})
		}
	})
})
