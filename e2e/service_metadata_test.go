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
	discoveryv1beta1 "k8s.io/api/discovery/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("creating a Service/Endpoint/EndpointSlice for a Tenant with additional metadata", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "servicemetadata",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "gatsby",
				Kind: "User",
			},
			IngressClasses:     v1alpha1.IngressClassesSpec{},
			StorageClasses:     v1alpha1.StorageClassesSpec{},
			NamespacesMetadata: v1alpha1.AdditionalMetadata{},
			ServicesMetadata: v1alpha1.AdditionalMetadata{
				AdditionalLabels: map[string]string{
					"k8s.io/custom-label":     "foo",
					"clastix.io/custom-label": "bar",
				},
				AdditionalAnnotations: map[string]string{
					"k8s.io/custom-annotation":     "bizz",
					"clastix.io/custom-annotation": "buzz",
				},
			},
			LimitRanges:    []corev1.LimitRangeSpec{},
			NamespaceQuota: 10,
			NodeSelector:   map[string]string{},
			ResourceQuota:  []corev1.ResourceQuotaSpec{},
		},
	}
	epsCR := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "epsCR",
			Labels: map[string]string{
				"rbac.authorization.k8s.io/aggregate-to-admin": "true",
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"discovery.k8s.io"},
				Resources: []string{"endpointslices"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
		},
	}
	JustBeforeEach(func() {
		Expect(k8sClient.Create(context.TODO(), tnt)).Should(Succeed())
		Expect(k8sClient.Create(context.TODO(), epsCR)).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), epsCR)).Should(Succeed())
	})
	It("service objects should contain additional metadata", func() {
		ns := NewNamespace("serivce-metadata")
		NamespaceCreationShouldSucceed(ns, tnt, defaultTimeoutInterval)
		NamespaceShouldBeManagedByTenant(ns, tnt, defaultTimeoutInterval)

		meta := metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: ns.GetName(),
			Labels: map[string]string{
				"k8s.io/custom-label": "wrong",
			},
		}

		svc := &corev1.Service{
			ObjectMeta: meta,
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				Ports: []corev1.ServicePort{
					{
						Port: 9999,
						TargetPort: intstr.IntOrString{
							Type:   intstr.Int,
							IntVal: 9999,
						},
						Protocol: corev1.ProtocolTCP,
					},
				},
			},
		}

		ep := &corev1.Endpoints{
			ObjectMeta: meta,
			Subsets: []corev1.EndpointSubset{
				{
					Addresses: []corev1.EndpointAddress{
						{
							IP: "10.10.1.1",
						},
					},
					Ports: []corev1.EndpointPort{
						{
							Name: "foo",
							Port: 9999,
						},
					},
				},
			},
		}

		cs := ownerClient(tnt)

		Eventually(func() (err error) {
			_, err = cs.CoreV1().Services(ns.GetName()).Create(context.TODO(), svc, metav1.CreateOptions{})
			return
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func() (err error) {
			_, err = cs.CoreV1().Endpoints(ns.GetName()).Create(context.TODO(), ep, metav1.CreateOptions{})
			return
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: svc.GetName(), Namespace: ns.GetName()}, svc)).Should(Succeed())
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ep.GetName(), Namespace: ns.GetName()}, ep)).Should(Succeed())

		By("checking additional labels on service", func() {
			for _, l := range tnt.Spec.ServicesMetadata.AdditionalLabels {
				Expect(svc.Labels).Should(ContainElement(l))
			}
		})
		By("checking additional annotations service", func() {
			for _, a := range tnt.Spec.NamespacesMetadata.AdditionalAnnotations {
				Expect(svc.Annotations).Should(ContainElement(a))
			}
		})
		By("checking additional labels on endpoint", func() {
			for _, l := range tnt.Spec.ServicesMetadata.AdditionalLabels {
				Expect(ep.Labels).Should(ContainElement(l))
			}
		})
		By("checking additional annotations endpoint", func() {
			for _, a := range tnt.Spec.NamespacesMetadata.AdditionalAnnotations {
				Expect(ep.Annotations).Should(ContainElement(a))
			}
		})

		epsName := "foo"
		epsPort := int32(9999)
		var eps client.Object

		maj, min, _ := GetKubernetesSemVer()
		if maj == 1 && min > 16 {
			eps = &discoveryv1beta1.EndpointSlice{
				ObjectMeta:  meta,
				AddressType: discoveryv1beta1.AddressTypeIPv4,
				Endpoints: []discoveryv1beta1.Endpoint{
					{
						Addresses: []string{"10.10.1.1"},
					},
				},
				Ports: []discoveryv1beta1.EndpointPort{
					{
						Name: &epsName,
						Port: &epsPort,
					},
				},
			}
			Eventually(func() (err error) {
				_, err = cs.DiscoveryV1beta1().EndpointSlices(ns.GetName()).Create(context.TODO(), eps.(*discoveryv1beta1.EndpointSlice), metav1.CreateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: eps.GetName(), Namespace: ns.GetName()}, eps)).Should(Succeed())
			By("checking additional annotations endpointslices", func() {
				for _, a := range tnt.Spec.NamespacesMetadata.AdditionalAnnotations {
					Expect(eps.GetAnnotations()).Should(ContainElement(a))
				}
			})
			By("checking additional labels on endpointslices", func() {
				for _, l := range tnt.Spec.ServicesMetadata.AdditionalLabels {
					Expect(eps.GetLabels()).Should(ContainElement(l))
				}
			})
		}
	})
})
