//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

var _ = Describe("enforcing a Priority Class", func() {
	tnt := &capsulev1beta1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "priority-class",
		},
		Spec: capsulev1beta1.TenantSpec{
			Owners: capsulev1beta1.OwnerListSpec{
				{
					Name: "george",
					Kind: "User",
				},
			},
			PriorityClasses: &capsulev1beta1.AllowedListSpec{
				Exact: []string{"gold"},
				Regex: "pc\\-\\w+",
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

	It("should block non allowed Priority Class", func() {
		ns := NewNamespace("system-node-critical")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

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
				PriorityClassName: "system-node-critical",
			},
		}

		cs := ownerClient(tnt.Spec.Owners[0])
		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
			return err
		}).ShouldNot(Succeed())
	})

	It("should allow exact match", func() {
		pc := &v1.PriorityClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gold",
			},
			Description: "fake PriorityClass for e2e",
			Value:       10000,
		}
		Expect(k8sClient.Create(context.TODO(), pc)).Should(Succeed())

		defer func() {
			Expect(k8sClient.Delete(context.TODO(), pc)).Should(Succeed())
		}()

		ns := NewNamespace("pc-exact-match")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

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
				PriorityClassName: "gold",
			},
		}

		cs := ownerClient(tnt.Spec.Owners[0])
		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
			return err
		}).Should(Succeed())
	})

	It("should allow regex match", func() {
		ns := NewNamespace("pc-regex-match")

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

		for i, pc := range []string{"pc-bronze", "pc-silver", "pc-gold"} {
			class := &v1.PriorityClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: pc,
				},
				Description: "fake PriorityClass for e2e",
				Value:       int32(10000 * (i + 2)),
			}

			Expect(k8sClient.Create(context.TODO(), class)).Should(Succeed())

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: pc,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container",
							Image: "quay.io/google-containers/pause-amd64:3.0",
						},
					},
					PriorityClassName: class.GetName(),
				},
			}

			cs := ownerClient(tnt.Spec.Owners[0])

			EventuallyCreation(func() error {
				_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
				return err
			}).Should(Succeed())

			Expect(k8sClient.Delete(context.TODO(), class)).Should(Succeed())
		}
	})
})
