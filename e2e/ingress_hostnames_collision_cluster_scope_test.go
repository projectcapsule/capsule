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
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

var _ = Describe("when handling Cluster scoped Ingress hostnames collision", func() {
	tnt1 := &capsulev1beta1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "hostnames-collision-cluster-one",
		},
		Spec: capsulev1beta1.TenantSpec{
			Owners: capsulev1beta1.OwnerListSpec{
				{
					Name: "ingress-tenant-one",
					Kind: "User",
				},
			},
			IngressOptions: capsulev1beta1.IngressOptions{
				HostnameCollisionScope: capsulev1beta1.HostnameCollisionScopeCluster,
			},
		},
	}
	tnt2 := &capsulev1beta1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "hostnames-collision-cluster-two",
		},
		Spec: capsulev1beta1.TenantSpec{
			Owners: capsulev1beta1.OwnerListSpec{
				{
					Name: "ingress-tenant-two",
					Kind: "User",
				},
			},
			IngressOptions: capsulev1beta1.IngressOptions{
				HostnameCollisionScope: capsulev1beta1.HostnameCollisionScopeCluster,
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
			tnt1.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), tnt1)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			tnt2.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), tnt2)
		}).Should(Succeed())
	})

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt1)).Should(Succeed())

		Expect(k8sClient.Delete(context.TODO(), tnt2)).Should(Succeed())
	})

	It("should ensure Cluster scope for Ingress hostname and path collision", func() {
		ns1 := NewNamespace("tenant-one-ns")
		cs1 := ownerClient(tnt1.Spec.Owners[0])
		NamespaceCreation(ns1, tnt1.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt1, defaultTimeoutInterval).Should(ContainElement(ns1.GetName()))

		ns2 := NewNamespace("tenant-two-ns")
		cs2 := ownerClient(tnt2.Spec.Owners[0])
		NamespaceCreation(ns2, tnt2.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt2, defaultTimeoutInterval).Should(ContainElement(ns2.GetName()))

		By("testing networking.k8s.io", func() {
			if err := k8sClient.List(context.Background(), &networkingv1.IngressList{}); err != nil {
				missingAPIError := &meta.NoKindMatchError{}
				if errors.As(err, &missingAPIError) {
					Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
				}
			}

			EventuallyCreation(func() (err error) {
				obj := networkingIngress("networking-1", "kubernetes.io", "/path")

				_, err = cs1.NetworkingV1().Ingresses(ns1.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})

				return
			}).Should(Succeed())
			// Creating a second Ingress with same hostname but a different path in a Namespace managed by the same
			// Tenant should not trigger a collision...
			EventuallyCreation(func() (err error) {
				obj := networkingIngress("networking-2", "kubernetes.io", "/docs")

				_, err = cs2.NetworkingV1().Ingresses(ns2.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})

				return
			}).Should(Succeed())
			// ...but it happens if hostname and path collide with the first Ingress,
			// although in a different Namespace
			EventuallyCreation(func() (err error) {
				obj := networkingIngress("networking-3", "kubernetes.io", "/path")

				_, err = cs2.NetworkingV1().Ingresses(ns2.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})

				return
			}).ShouldNot(Succeed())
		})

		By("testing extensions", func() {
			if err := k8sClient.List(context.Background(), &extensionsv1beta1.IngressList{}); err != nil {
				missingAPIError := &meta.NoKindMatchError{}
				if errors.As(err, &missingAPIError) {
					Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
				}
			}

			EventuallyCreation(func() (err error) {
				obj := extensionsIngress("extensions-1", "cncf.io", "/foo")

				_, err = cs1.ExtensionsV1beta1().Ingresses(ns1.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})

				return
			}).Should(Succeed())
			// Creating a second Ingress with same hostname but a different path in a Namespace managed by the same
			// Tenant should not trigger a collision...
			EventuallyCreation(func() (err error) {
				obj := extensionsIngress("extensions-2", "cncf.io", "/bar")

				_, err = cs2.ExtensionsV1beta1().Ingresses(ns2.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})

				return
			}).Should(Succeed())
			// ...but it happens if hostname and path collide with the first Ingress,
			// although in a different Namespace
			EventuallyCreation(func() (err error) {
				obj := extensionsIngress("extensions-3", "cncf.io", "/foo")

				_, err = cs2.ExtensionsV1beta1().Ingresses(ns2.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})

				return
			}).ShouldNot(Succeed())
		})
	})
})
