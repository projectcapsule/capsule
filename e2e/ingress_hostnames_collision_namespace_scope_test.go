//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/api"
	"github.com/clastix/capsule/pkg/utils"
)

var _ = Describe("when handling Namespace scoped Ingress hostnames collision", func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "hostnames-collision-namespace",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "ingress-namespace",
					Kind: "User",
				},
			},
			IngressOptions: capsulev1beta2.IngressOptions{
				HostnameCollisionScope: api.HostnameCollisionScopeNamespace,
			},
		},
	}
	// scaffold a basic networking.k8s.io Ingress with name and host
	networkingIngress := func(name, hostname, path string) *networkingv1.Ingress {
		return &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: networkingv1.IngressSpec{
				Rules: []networkingv1.IngressRule{
					{
						Host: hostname,
						IngressRuleValue: networkingv1.IngressRuleValue{
							HTTP: &networkingv1.HTTPIngressRuleValue{
								Paths: []networkingv1.HTTPIngressPath{
									{
										Path: path,
										PathType: func(v networkingv1.PathType) *networkingv1.PathType {
											return &v
										}(networkingv1.PathTypeExact),
										Backend: networkingv1.IngressBackend{
											Service: &networkingv1.IngressServiceBackend{
												Name: "example",
												Port: networkingv1.ServiceBackendPort{
													Number: 8080,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}
	}
	// scaffold a basic extensions Ingress with name and host
	extensionsIngress := func(name, hostname, path string) *extensionsv1beta1.Ingress {
		return &extensionsv1beta1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: extensionsv1beta1.IngressSpec{
				Rules: []extensionsv1beta1.IngressRule{
					{
						Host: hostname,
						IngressRuleValue: extensionsv1beta1.IngressRuleValue{
							HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
								Paths: []extensionsv1beta1.HTTPIngressPath{
									{
										Path: path,
										PathType: func(v extensionsv1beta1.PathType) *extensionsv1beta1.PathType {
											return &v
										}(extensionsv1beta1.PathTypeExact),
										Backend: extensionsv1beta1.IngressBackend{
											ServiceName: "example",
											ServicePort: intstr.FromInt(8080),
										},
									},
								},
							},
						},
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
	})

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})

	It("should ensure Namespace scope for Ingress hostname and path collision", func() {
		ns1 := NewNamespace("")
		ns2 := NewNamespace("")
		cs := ownerClient(tnt.Spec.Owners[0])
		NamespaceCreation(ns1, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		NamespaceCreation(ns2, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns1.GetName()))
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns2.GetName()))

		By("testing networking.k8s.io", func() {
			if err := k8sClient.List(context.Background(), &networkingv1.IngressList{}); err != nil {
				if utils.IsUnsupportedAPI(err) {
					Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
				}
			}

			EventuallyCreation(func() (err error) {
				obj := networkingIngress("networking-1", "kubernetes.io", "/path")

				_, err = cs.NetworkingV1().Ingresses(ns1.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})

				return
			}).Should(Succeed())
			// A same Ingress with hostname and path pair can be created in a different Namespace,
			// although of the same Tenant
			EventuallyCreation(func() (err error) {
				obj := networkingIngress("networking-2", "kubernetes.io", "/path")

				_, err = cs.NetworkingV1().Ingresses(ns2.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})

				return
			}).Should(Succeed())
			// ...but a collision occurs if the same pair is created in the same Namespace
			EventuallyCreation(func() (err error) {
				obj := networkingIngress("networking-3", "kubernetes.io", "/path")

				_, err = cs.NetworkingV1().Ingresses(ns1.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})

				return
			}).ShouldNot(Succeed())

			EventuallyCreation(func() (err error) {
				obj := networkingIngress("networking-4", "kubernetes.io", "/path")

				_, err = cs.NetworkingV1().Ingresses(ns2.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})

				return
			}).ShouldNot(Succeed())
		})

		By("testing extensions", func() {
			if err := k8sClient.List(context.Background(), &extensionsv1beta1.IngressList{}); err != nil {
				if utils.IsUnsupportedAPI(err) {
					Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
				}
			}

			EventuallyCreation(func() (err error) {
				obj := extensionsIngress("extensions-1", "cncf.io", "/docs")

				_, err = cs.ExtensionsV1beta1().Ingresses(ns1.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})

				return
			}).Should(Succeed())
			// A same Ingress with hostname and path pair can be created in a different Namespace,
			// although of the same Tenant
			EventuallyCreation(func() (err error) {
				obj := extensionsIngress("extensions-2", "cncf.io", "/docs")

				_, err = cs.ExtensionsV1beta1().Ingresses(ns2.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})

				return
			}).Should(Succeed())
			// ...but a collision occurs if the same pair is created in the same Namespace
			EventuallyCreation(func() (err error) {
				obj := extensionsIngress("extensions-3", "cncf.io", "/docs")

				_, err = cs.ExtensionsV1beta1().Ingresses(ns1.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})

				return
			}).ShouldNot(Succeed())

			EventuallyCreation(func() (err error) {
				obj := extensionsIngress("extensions-4", "cncf.io", "/docs")

				_, err = cs.ExtensionsV1beta1().Ingresses(ns2.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})

				return
			}).ShouldNot(Succeed())
		})
	})
})
