// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

var _ = Describe("creating an ExternalName service when it is disabled for Tenant", func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "disable-external-service",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "google",
					Kind: "User",
				},
			},
			ServiceOptions: &api.ServiceOptions{
				AllowedServices: &api.AllowedServices{
					ExternalName: ptr.To(false),
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

	It("should fail creating a service with ExternalService type", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

		EventuallyCreation(func() error {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-ip",
					Namespace: ns.GetName(),
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Port: 8888,
							TargetPort: intstr.IntOrString{
								Type:   intstr.Int,
								IntVal: 8888,
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

		EventuallyCreation(func() error {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "disable-external-service",
					Namespace: ns.GetName(),
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeExternalName,
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
		}).ShouldNot(Succeed())
	})
})
