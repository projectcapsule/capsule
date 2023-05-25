//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	discoveryv1beta1 "k8s.io/api/discovery/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/api"
	"github.com/clastix/capsule/pkg/utils"
)

var _ = Describe("adding metadata to Service objects", func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "service-metadata",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "gatsby",
					Kind: "User",
				},
			},
			ServiceOptions: &api.ServiceOptions{
				AdditionalMetadata: &api.AdditionalMetadataSpec{
					Labels: map[string]string{
						"k8s.io/custom-label":     "foo",
						"clastix.io/custom-label": "bar",
					},
					Annotations: map[string]string{
						"k8s.io/custom-annotation":     "bizz",
						"clastix.io/custom-annotation": "buzz",
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
		ns := NewNamespace("")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
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
		// Waiting for the reconciliation of required RBAC
		EventuallyCreation(func() (err error) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "container",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container",
							Image: "quay.io/google-containers/pause-amd64:3.0",
						},
					},
				},
			}
			_, err = ownerClient(tnt.Spec.Owners[0]).CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})

			return
		}).Should(Succeed())

		EventuallyCreation(func() (err error) {
			_, err = ownerClient(tnt.Spec.Owners[0]).CoreV1().Services(ns.GetName()).Create(context.Background(), svc, metav1.CreateOptions{})

			return
		}).Should(Succeed())

		By("checking additional labels", func() {
			Eventually(func() (ok bool) {
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: svc.GetName(), Namespace: ns.GetName()}, svc)).Should(Succeed())
				for k, v := range tnt.Spec.ServiceOptions.AdditionalMetadata.Labels {
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
				for k, v := range tnt.Spec.ServiceOptions.AdditionalMetadata.Annotations {
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
		ns := NewNamespace("")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
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
		// Waiting for the reconciliation of required RBAC
		EventuallyCreation(func() (err error) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "container",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container",
							Image: "quay.io/google-containers/pause-amd64:3.0",
						},
					},
				},
			}
			_, err = ownerClient(tnt.Spec.Owners[0]).CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})

			return
		}).Should(Succeed())

		EventuallyCreation(func() (err error) {
			return k8sClient.Create(context.TODO(), ep)
		}).Should(Succeed())

		By("checking additional labels", func() {
			Eventually(func() (ok bool) {
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ep.GetName(), Namespace: ns.GetName()}, ep)).Should(Succeed())
				for k, v := range tnt.Spec.ServiceOptions.AdditionalMetadata.Labels {
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
				for k, v := range tnt.Spec.ServiceOptions.AdditionalMetadata.Annotations {
					ok, _ = HaveKeyWithValue(k, v).Match(ep.Annotations)
					if !ok {
						return false
					}
				}
				return true
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
	})

	It("should apply them to EndpointSlice in v1", func() {
		if err := k8sClient.List(context.Background(), &networkingv1.IngressList{}); err != nil {
			if utils.IsUnsupportedAPI(err) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

		ns := NewNamespace("")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
		// Waiting for the reconciliation of required RBAC
		EventuallyCreation(func() (err error) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "container",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container",
							Image: "quay.io/google-containers/pause-amd64:3.0",
						},
					},
				},
			}
			_, err = ownerClient(tnt.Spec.Owners[0]).CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})

			return
		}).Should(Succeed())

		var eps client.Object

		if err := k8sClient.List(context.Background(), &discoveryv1.EndpointSliceList{}); err != nil {
			if utils.IsUnsupportedAPI(err) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}

			eps = &discoveryv1beta1.EndpointSlice{
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
						Name: pointer.String("foo"),
						Port: pointer.Int32(9999),
					},
				},
			}
		} else {
			eps = &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "endpointslice-metadata",
					Namespace: ns.GetName(),
				},
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints: []discoveryv1.Endpoint{
					{
						Addresses: []string{"10.10.1.1"},
					},
				},
				Ports: []discoveryv1.EndpointPort{
					{
						Name: pointer.String("foo"),
						Port: pointer.Int32(9999),
					},
				},
			}
		}

		EventuallyCreation(func() (err error) {
			return k8sClient.Create(context.TODO(), eps)
		}).Should(Succeed())

		By("checking additional annotations EndpointSlice", func() {
			Eventually(func() (ok bool) {
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: eps.GetName(), Namespace: ns.GetName()}, eps)).Should(Succeed())
				for k, v := range tnt.Spec.ServiceOptions.AdditionalMetadata.Annotations {
					ok, _ = HaveKeyWithValue(k, v).Match(eps.GetAnnotations())
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
				for k, v := range tnt.Spec.ServiceOptions.AdditionalMetadata.Labels {
					ok, _ = HaveKeyWithValue(k, v).Match(eps.GetLabels())
					if !ok {
						return false
					}
				}
				return true
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
	})
})
