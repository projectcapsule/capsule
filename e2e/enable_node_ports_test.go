//+build e2e

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

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("creating a nodePort service when it is enabled for Tenant", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "enable-node-ports",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "google",
				Kind: "User",
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

	It("should allow creating a service with NodePort type", func() {
		ns := NewNamespace("enable-node-ports")
		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())

		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "enable-node-ports",
				Namespace: ns.GetName(),
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
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
			cs := ownerClient(tnt)
			_, err := cs.CoreV1().Services(ns.Name).Create(context.Background(), svc, metav1.CreateOptions{})
			return err
		}).Should(Succeed())
	})
})
