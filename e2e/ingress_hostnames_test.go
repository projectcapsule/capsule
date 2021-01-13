//+build e2e

/*
Copyright 2020 Clastix Labs.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("when Tenant handles Ingress hostnames", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ingress-hostnames",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "hostname",
				Kind: "User",
			},
			IngressHostnames: &v1alpha1.AllowedListSpec{
				Exact:      []string{"sigs.k8s.io", "operator.sdk", "domain.tld"},
				Regex: `.*\.clastix\.io`,
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

	It("should block a non allowed Hostname", func() {
		maj, min, _ := GetKubernetesSemVer()

		ns := NewNamespace("disallowed-hostname")
		cs := ownerClient(tnt)

		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, podRecreationTimeoutInterval).Should(ContainElement(ns.GetName()))

		if maj == 1 && min > 18 {
			By("testing networking.k8s.io", func() {
				Eventually(func() (err error) {
					obj := networkingIngress("denied-networking", "kubernetes.io")
					_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
					return
				}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
			})
		}

		if maj == 1 && min < 22 {
			By("testing extensions", func() {
				Eventually(func() (err error) {
					obj := extensionsIngress("denied-extensions", "kubernetes.io")
					_, err = cs.ExtensionsV1beta1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
					return
				}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
			})
		}
	})

	It("should allow Hostnames in list", func() {
		maj, min, _ := GetKubernetesSemVer()
		ns := NewNamespace("allowed-hostname-list")
		cs := ownerClient(tnt)

		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, podRecreationTimeoutInterval).Should(ContainElement(ns.GetName()))

		if maj == 1 && min > 18 {
			By("testing networking.k8s.io", func() {
				for i, h := range tnt.Spec.IngressHostnames.Exact {
					Eventually(func() (err error) {
						obj := networkingIngress(fmt.Sprintf("allowed-networking-%d", i), h)
						_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
						return
					}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				}
			})
		}

		if maj == 1 && min < 22 {
			By("testing extensions", func() {
				for i, h := range tnt.Spec.IngressHostnames.Exact {
					Eventually(func() (err error) {
						obj := extensionsIngress(fmt.Sprintf("allowed-extensions-%d", i), h)
						_, err = cs.ExtensionsV1beta1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
						return
					}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				}
			})
		}
	})

	It("should allow Hostnames in regex", func() {
		maj, min, _ := GetKubernetesSemVer()
		ns := NewNamespace("allowed-hostname-regex")
		cs := ownerClient(tnt)

		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, podRecreationTimeoutInterval).Should(ContainElement(ns.GetName()))

		if maj == 1 && min > 18 {
			By("testing networking.k8s.io", func() {
				for _, h := range []string{"foo", "bar", "bizz"} {
					Eventually(func() (err error) {
						obj := networkingIngress(fmt.Sprintf("allowed-networking-%s", h), h)
						_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
						return
					}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				}
			})
		}

		if maj == 1 && min < 22 {
			By("testing extensions", func() {
				for _, h := range []string{"foo", "bar", "bizz"} {
					Eventually(func() (err error) {
						obj := extensionsIngress(fmt.Sprintf("allowed-extensions-%s", h), h)
						_, err = cs.ExtensionsV1beta1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
						return
					}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				}
			})
		}
	})
})
