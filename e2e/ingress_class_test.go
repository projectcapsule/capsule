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
	"strconv"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1beta12 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/pointer"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("when Tenant handles Ingress classes", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ingressclass",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "ingress",
				Kind: "User",
			},
			NamespacesMetadata: v1alpha1.AdditionalMetadata{},
			ServicesMetadata:   v1alpha1.AdditionalMetadata{},
			StorageClasses:     []string{},
			IngressClasses: []string{
				"nginx",
				"haproxy",
			},
			LimitRanges:     []corev1.LimitRangeSpec{},
			NamespaceQuota:  3,
			NodeSelector:    map[string]string{},
			NetworkPolicies: []networkingv1.NetworkPolicySpec{},
			ResourceQuota:   []corev1.ResourceQuotaSpec{},
		},
	}
	JustBeforeEach(func() {
		tnt.ResourceVersion = ""
		Expect(k8sClient.Create(context.TODO(), tnt)).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})
	It("should block non allowed Ingress class", func() {
		ns := NewNamespace("ingress-class-disallowed")
		cs := ownerClient(tnt)

		NamespaceCreationShouldSucceed(ns, tnt, defaultTimeoutInterval)
		NamespaceShouldBeManagedByTenant(ns, tnt, defaultTimeoutInterval)

		By("non-specifying the class", func() {
			Eventually(func() (err error) {
				i := &v1beta12.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name: "denied-ingress",
					},
					Spec: v1beta12.IngressSpec{
						Backend: &v1beta12.IngressBackend{
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
				i := &v1beta12.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name: "denied-ingress",
						Annotations: map[string]string{
							"kubernetes.io/ingress.class": "the-worst-ingress-available",
						},
					},
					Spec: v1beta12.IngressSpec{
						Backend: &v1beta12.IngressBackend{
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
				i := &v1beta12.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name: "denied-ingress",
					},
					Spec: v1beta12.IngressSpec{
						IngressClassName: pointer.StringPtr("the-worst-ingress-available"),
						Backend: &v1beta12.IngressBackend{
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

		NamespaceCreationShouldSucceed(ns, tnt, defaultTimeoutInterval)
		NamespaceShouldBeManagedByTenant(ns, tnt, defaultTimeoutInterval)

		for _, c := range tnt.Spec.IngressClasses {
			Eventually(func() (err error) {
				i := &v1beta12.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name: c,
						Annotations: map[string]string{
							"kubernetes.io/ingress.class": c,
						},
					},
					Spec: v1beta12.IngressSpec{
						Backend: &v1beta12.IngressBackend{
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

		cs, err := kubernetes.NewForConfig(cfg)
		Expect(err).ToNot(HaveOccurred())
		v, err := cs.Discovery().ServerVersion()
		Expect(err).ToNot(HaveOccurred())
		major, err := strconv.Atoi(v.Major)
		Expect(err).ToNot(HaveOccurred())
		minor, err := strconv.Atoi(v.Minor)
		Expect(err).ToNot(HaveOccurred())
		if major == 1 && minor < 18 {
			Skip("Running test ont Kubernetes " + v.String() + ", doesn't provide .spec.ingressClassName")
		}

		NamespaceCreationShouldSucceed(ns, tnt, defaultTimeoutInterval)
		NamespaceShouldBeManagedByTenant(ns, tnt, defaultTimeoutInterval)

		for _, c := range tnt.Spec.IngressClasses {
			Eventually(func() (err error) {
				i := &v1beta12.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name: c,
					},
					Spec: v1beta12.IngressSpec{
						IngressClassName: &c,
						Backend: &v1beta12.IngressBackend{
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
})
