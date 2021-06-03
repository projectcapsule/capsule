//+build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

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
	"k8s.io/utils/pointer"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("adding metadata to Service objects", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "service-metadata",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "gatsby",
				Kind: "User",
			},
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
			AdditionalRoleBindings: []v1alpha1.AdditionalRoleBindings{
				{
					ClusterRoleName: "system:controller:endpointslice-controller",
					Subjects: []rbacv1.Subject{
						{
							Kind: "User",
							Name: "gatsby",
						},
					},
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

	It("should apply them to Service", func() {
		ns := NewNamespace("service-metadata")
		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service-metadata",
				Namespace: ns.GetName(),
			},
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
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), svc)
		}).Should(Succeed())

		By("checking additional labels", func() {
			Eventually(func() (ok bool) {
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: svc.GetName(), Namespace: ns.GetName()}, svc)).Should(Succeed())
				for k, v := range tnt.Spec.ServicesMetadata.AdditionalLabels {
					ok, _ = HaveKeyWithValue(k, v).Match(svc.Labels)
					if !ok {
						return false
					}
				}
				return true
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
		By("checking additional annotations", func() {
			Eventually(func() (ok bool) {
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: svc.GetName(), Namespace: ns.GetName()}, svc)).Should(Succeed())
				for k, v := range tnt.Spec.ServicesMetadata.AdditionalAnnotations {
					ok, _ = HaveKeyWithValue(k, v).Match(svc.Annotations)
					if !ok {
						return false
					}
				}
				return true
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
	})

	It("should apply them to Endpoints", func() {
		ns := NewNamespace("endpoints-metadata")
		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		ep := &corev1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "endpoints-metadata",
				Namespace: ns.GetName(),
			},
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
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), ep)
		}).Should(Succeed())
		By("checking additional labels", func() {
			Eventually(func() (ok bool) {
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ep.GetName(), Namespace: ns.GetName()}, ep)).Should(Succeed())
				for k, v := range tnt.Spec.ServicesMetadata.AdditionalLabels {
					ok, _ = HaveKeyWithValue(k, v).Match(ep.Labels)
					if !ok {
						return false
					}
				}
				return true
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
		By("checking additional annotations", func() {
			Eventually(func() (ok bool) {
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ep.GetName(), Namespace: ns.GetName()}, ep)).Should(Succeed())
				for k, v := range tnt.Spec.ServicesMetadata.AdditionalAnnotations {
					ok, _ = HaveKeyWithValue(k, v).Match(ep.Annotations)
					if !ok {
						return false
					}
				}
				return true
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
	})

	It("should apply them to EndpointSlice", func() {
		maj, min, v := GetKubernetesSemVer()
		if maj == 1 && min <= 16 {
			Skip("Running test on Kubernetes " + v + ", doesn't provide EndpointSlice resource")
		}

		ns := NewNamespace("endpointslice-metadata")
		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		eps := &discoveryv1beta1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "endpointslice-metadata",
				Namespace: ns.GetName(),
			},
			AddressType: discoveryv1beta1.AddressTypeIPv4,
			Endpoints: []discoveryv1beta1.Endpoint{
				{
					Addresses: []string{"10.10.1.1"},
				},
			},
			Ports: []discoveryv1beta1.EndpointPort{
				{
					Name: pointer.StringPtr("foo"),
					Port: pointer.Int32Ptr(9999),
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), eps)
		}).Should(Succeed())

		By("checking additional annotations EndpointSlice", func() {
			Eventually(func() (ok bool) {
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: eps.GetName(), Namespace: ns.GetName()}, eps)).Should(Succeed())
				for k, v := range tnt.Spec.ServicesMetadata.AdditionalAnnotations {
					ok, _ = HaveKeyWithValue(k, v).Match(eps.Annotations)
					if !ok {
						return false
					}
				}
				return true
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
		By("checking additional labels on EndpointSlice", func() {
			Eventually(func() (ok bool) {
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: eps.GetName(), Namespace: ns.GetName()}, eps)).Should(Succeed())
				for k, v := range tnt.Spec.ServicesMetadata.AdditionalLabels {
					ok, _ = HaveKeyWithValue(k, v).Match(eps.Labels)
					if !ok {
						return false
					}
				}
				return true
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
	})
})
