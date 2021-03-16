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
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("when handling Ingress hostnames collision", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ingress-hostnames-denied-collision",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "ingress-denied",
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
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
		ModifyCapsuleManagerPodArgs(append(defaulManagerPodArgs, []string{"--allow-ingress-hostname-collision=false"}...))
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
		ModifyCapsuleManagerPodArgs(defaulManagerPodArgs)
	})

	It("should not allow creating several Ingress with same hostname", func() {
		maj, min, _ := GetKubernetesSemVer()

		ns := NewNamespace("allowed-collision")
		cs := ownerClient(tnt)

		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, podRecreationTimeoutInterval).Should(ContainElement(ns.GetName()))

		if maj == 1 && min > 18 {
			By("testing networking.k8s.io", func() {
				Eventually(func() (err error) {
					obj := networkingIngress("networking-1", "kubernetes.io")
					_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
					return
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				Eventually(func() (err error) {
					obj := networkingIngress("networking-2", "kubernetes.io")
					_, err = cs.NetworkingV1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
					return
				}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
			})
		}

		if maj == 1 && min < 22 {
			By("testing extensions", func() {
				Eventually(func() (err error) {
					obj := extensionsIngress("extensions-1", "cncf.io")
					_, err = cs.ExtensionsV1beta1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
					return
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				Eventually(func() (err error) {
					obj := extensionsIngress("extensions-2", "cncf.io")
					_, err = cs.ExtensionsV1beta1().Ingresses(ns.GetName()).Create(context.TODO(), obj, metav1.CreateOptions{})
					return
				}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
			})
		}
	})
})
