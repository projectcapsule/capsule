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

var _ = Describe("when Tenant handles Ingress hostnames", func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ingress-hostnames",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "hostname",
					Kind: "User",
				},
			},
			IngressOptions: capsulev1beta2.IngressOptions{
				AllowedHostnames: &api.AllowedListSpec{
					Exact: []string{"sigs.k8s.io", "operator.sdk", "domain.tld"},
					Regex: `.*\.clastix\.io`,
				},
			},
		},
	}

	// scaffold a basic networking.k8s.io Ingress with name and host
	networkingIngress := func(name string, hostnames ...string) *networkingv1.Ingress {
		ing := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: networkingv1.IngressSpec{
				Rules: []networkingv1.IngressRule{},
			},
		}

		for _, hostname := range hostnames {
			ing.Spec.Rules = append(ing.Spec.Rules, networkingv1.IngressRule{
				Host: hostname,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
							{
								Path: "/",
								PathType: func(v networkingv1.PathType) *networkingv1.PathType {
									return &v
								}(networkingv1.PathTypeExact),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "foo",
										Port: networkingv1.ServiceBackendPort{Name: "http"},
									},
								},
							},
						},
					},
				},
			})
		}
		return ing
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
						IngressRuleValue: extensionsv1beta1.IngressRuleValue{
							HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
								Paths: []extensionsv1beta1.HTTPIngressPath{
									{
										Path: "/",
										PathType: func(v extensionsv1beta1.PathType) *extensionsv1beta1.PathType {
											return &v
										}(extensionsv1beta1.PathTypeExact),
										Backend: extensionsv1beta1.IngressBackend{
											ServiceName: "foo",
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

	It("should block an empty hostname", func() {
		ns := NewNamespace("")
		cs := ownerClient(tnt.Spec.Owners[0])

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("testing networking.k8s.io", func() {
			if err := k8sClient.List(context.Background(), &networkingv1.IngressList{}); err != nil {
				if utils.IsUnsupportedAPI(err) {
					Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
				}
			}

			Eventually(func() (err error) {
				obj := networkingIngress("no-empty-hostname", "", "kubernetes.io")
				_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
	})

	It("should block a non allowed Hostname", func() {
		ns := NewNamespace("")
		cs := ownerClient(tnt.Spec.Owners[0])

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("testing networking.k8s.io", func() {
			if err := k8sClient.List(context.Background(), &networkingv1.IngressList{}); err != nil {
				if utils.IsUnsupportedAPI(err) {
					Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
				}
			}

			Eventually(func() (err error) {
				obj := networkingIngress("denied-networking", "kubernetes.io")
				_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
	})

	It("should block a non allowed Hostname", func() {
		ns := NewNamespace("")
		cs := ownerClient(tnt.Spec.Owners[0])

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("testing extensions", func() {
			if err := k8sClient.List(context.Background(), &extensionsv1beta1.IngressList{}); err != nil {
				if utils.IsUnsupportedAPI(err) {
					Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
				}
			}

			Eventually(func() (err error) {
				obj := extensionsIngress("denied-extensions", "kubernetes.io")
				_, err = cs.ExtensionsV1beta1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
	})

	It("should allow Hostnames in list", func() {
		ns := NewNamespace("")
		cs := ownerClient(tnt.Spec.Owners[0])

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("testing networking.k8s.io", func() {
			if err := k8sClient.List(context.Background(), &networkingv1.IngressList{}); err != nil {
				if utils.IsUnsupportedAPI(err) {
					Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
				}
			}

			for i, h := range tnt.Spec.IngressOptions.AllowedHostnames.Exact {
				Eventually(func() (err error) {
					obj := networkingIngress(fmt.Sprintf("allowed-networking-%d", i), h)
					_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
					return
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})
	})

	It("should allow Hostnames in list", func() {
		ns := NewNamespace("")
		cs := ownerClient(tnt.Spec.Owners[0])

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("testing extensions", func() {
			if err := k8sClient.List(context.Background(), &extensionsv1beta1.IngressList{}); err != nil {
				if utils.IsUnsupportedAPI(err) {
					Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
				}
			}

			for i, h := range tnt.Spec.IngressOptions.AllowedHostnames.Exact {
				Eventually(func() (err error) {
					obj := extensionsIngress(fmt.Sprintf("allowed-extensions-%d", i), h)
					_, err = cs.ExtensionsV1beta1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
					return
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})
	})

	It("should allow Hostnames in regex", func() {
		ns := NewNamespace("")
		cs := ownerClient(tnt.Spec.Owners[0])

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("testing networking.k8s.io", func() {
			if err := k8sClient.List(context.Background(), &networkingv1.IngressList{}); err != nil {
				if utils.IsUnsupportedAPI(err) {
					Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
				}
			}

			for _, h := range []string{"foo", "bar", "bizz"} {
				Eventually(func() (err error) {
					obj := networkingIngress(fmt.Sprintf("allowed-networking-%s", h), fmt.Sprintf("%s.clastix.io", h))
					_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
					return
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})
	})

	It("should allow Hostnames in regex", func() {
		ns := NewNamespace("")
		cs := ownerClient(tnt.Spec.Owners[0])

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("testing extensions", func() {
			if err := k8sClient.List(context.Background(), &extensionsv1beta1.IngressList{}); err != nil {
				if utils.IsUnsupportedAPI(err) {
					Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
				}
			}

			for _, h := range []string{"foo", "bar", "bizz"} {
				Eventually(func() (err error) {
					obj := extensionsIngress(fmt.Sprintf("allowed-extensions-%s", h), fmt.Sprintf("%s.clastix.io", h))
					_, err = cs.ExtensionsV1beta1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
					return
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})
	})
})
