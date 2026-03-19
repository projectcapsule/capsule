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

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("enforcing an allowed set of Service external IPs", Label("tenant"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "allowed-external-ip",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "google",
							Kind: "User",
						},
					},
				},
			},
			ServiceOptions: &api.ServiceOptions{
				ExternalServiceIPs: &api.ExternalServiceIPsSpec{
					Allowed: []api.AllowedIP{
						"10.20.0.0/16",
						"192.168.1.2/32",
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
		EventuallyDeletion(tnt)
	})

	It("should fail creating an evil service", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())

		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "my-evil-dns-server",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name:       "dns",
						Protocol:   "UDP",
						Port:       53,
						TargetPort: intstr.FromInt(9053),
					},
				},
				Selector: map[string]string{
					"app": "my-evil-dns-server",
				},
				ExternalIPs: []string{
					"8.8.8.8",
					"8.8.4.4",
				},
			},
		}
		EventuallyCreation(func() error {
			cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
			_, err := cs.CoreV1().Services(ns.Name).Create(context.Background(), svc, metav1.CreateOptions{})
			return err
		}).ShouldNot(Succeed())
	})

	It("should allow the first CIDR block", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())

		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "dns-server",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name:       "dns",
						Protocol:   "UDP",
						Port:       53,
						TargetPort: intstr.FromInt(9053),
					},
				},
				Selector: map[string]string{
					"app": "dns-server",
				},
				ExternalIPs: []string{
					"10.20.0.0",
					"10.20.255.255",
				},
			},
		}
		EventuallyCreation(func() error {
			cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
			_, err := cs.CoreV1().Services(ns.Name).Create(context.Background(), svc, metav1.CreateOptions{})
			return err
		}).Should(Succeed())
	})

	It("should allow the /32 CIDR block", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())

		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "dns-server",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name:       "dns",
						Protocol:   "UDP",
						Port:       53,
						TargetPort: intstr.FromInt(9053),
					},
				},
				Selector: map[string]string{
					"app": "dns-server",
				},
				ExternalIPs: []string{
					"192.168.1.2",
				},
			},
		}
		EventuallyCreation(func() error {
			cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
			_, err := cs.CoreV1().Services(ns.Name).Create(context.Background(), svc, metav1.CreateOptions{})
			return err
		}).Should(Succeed())
	})
})
