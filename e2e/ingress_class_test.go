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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
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
			NamespacesMetadata: v1alpha1.AdditionalMetadata{},
			ServicesMetadata:   v1alpha1.AdditionalMetadata{},
			StorageClasses:     v1alpha1.StorageClassesSpec{},
			IngressClasses: v1alpha1.IngressClassesSpec{
				Allowed: []string{
					"nginx",
					"haproxy",
				},
				AllowedRegex: "^oil-.*$",
			},
			LimitRanges:     []corev1.LimitRangeSpec{},
			NamespaceQuota:  3,
			NodeSelector:    map[string]string{},
			NetworkPolicies: []networkingv1.NetworkPolicySpec{},
			ResourceQuota:   []corev1.ResourceQuotaSpec{},
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
	It("should block non allowed Ingress class", func() {
		ns := NewNamespace("ingress-class-disallowed")
		cs := ownerClient(tnt)

		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, podRecreationTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("non-specifying the class", func() {
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
		By("using a forbidden class as Annotation", func() {
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
		By("specifying a forbidden class", func() {
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
	It("should allow enabled Ingress class using the deprecated Annotation", func() {
		ns := NewNamespace("ingress-class-allowed-annotation")
		cs := ownerClient(tnt)

		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, podRecreationTimeoutInterval).Should(ContainElement(ns.GetName()))

		for _, c := range tnt.Spec.IngressClasses.Allowed {
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
	It("should allow enabled Ingress class using the IngressClassName field", func() {
		ns := NewNamespace("ingress-class-allowed-annotation")
		cs := ownerClient(tnt)

		maj, min, v := GetKubernetesSemVer()
		if maj == 1 && min < 18 {
			Skip("Running test on Kubernetes " + v + ", doesn't provide .spec.ingressClassName")
		}

		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, podRecreationTimeoutInterval).Should(ContainElement(ns.GetName()))

		for _, c := range tnt.Spec.IngressClasses.Allowed {
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
	It("should allow enabled Ingress class regexp using the deprecated Annotation", func() {
		ns := NewNamespace("ingress-class-allowed-annotation")
		cs := ownerClient(tnt)
		ingressClass := "oil-ingress"

		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, podRecreationTimeoutInterval).Should(ContainElement(ns.GetName()))

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
	It("should allow enabled Ingress class regexp using the IngressClassName field", func() {
		ns := NewNamespace("ingress-class-allowed-annotation")
		cs := ownerClient(tnt)
		ingressClass := "oil-haproxy"

		maj, min, v := GetKubernetesSemVer()
		if maj == 1 && min < 18 {
			Skip("Running test on Kubernetes " + v + ", doesn't provide .spec.ingressClassName")
		}

		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, podRecreationTimeoutInterval).Should(ContainElement(ns.GetName()))

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
