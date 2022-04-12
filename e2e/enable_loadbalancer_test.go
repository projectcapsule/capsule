//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

var _ = Describe("creating a LoadBalancer service when it is enabled for Tenant", func() {
	tnt := &capsulev1beta1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "enable-loadbalancer-service",
		},
		Spec: capsulev1beta1.TenantSpec{
			Owners: capsulev1beta1.OwnerListSpec{
				{
					Name: "netflix",
					Kind: "User",
				},
			},
			ServiceOptions: &capsulev1beta1.ServiceOptions{
				AllowedServices: &capsulev1beta1.AllowedServices{
					LoadBalancer: pointer.BoolPtr(true),
				},
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
	})

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})

	It("should succeed creating a service with LoadBalancer type", func() {
		ns := NewNamespace("enable-loadbalancer-service")

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

		EventuallyCreation(func() error {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "enable-loadbalancer-service",
					Namespace: ns.GetName(),
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
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

			cs := ownerClient(tnt.Spec.Owners[0])

			_, err := cs.CoreV1().Services(ns.Name).Create(context.Background(), svc, metav1.CreateOptions{})

			return err
		}).Should(Succeed())
	})
})
