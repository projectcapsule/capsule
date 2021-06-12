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

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("enforcing a defined ImagePullPolicy", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "image-pull-policy",
			Annotations: map[string]string{
				"capsule.clastix.io/allowed-image-pull-policy": "Always",
			},
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "axel",
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

	It("should just allow the defined policy", func() {
		ns := NewNamespace("allow-policies")
		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())

		cs := ownerClient(tnt)

		By("allowing Always", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pull-always",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container",
							Image: "gcr.io/google_containers/pause-amd64:3.0",
							ImagePullPolicy: corev1.PullAlways,
						},
					},
				},
			}

			EventuallyCreation(func() (err error) {
				_, err = cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})

				return
			}).Should(Succeed())
		})


		By("blocking IfNotPresent", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "if-not-present",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container",
							Image: "gcr.io/google_containers/pause-amd64:3.0",
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
				},
			}

			EventuallyCreation(func() (err error) {
				_, err = cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})

				return
			}).ShouldNot(Succeed())
		})

		By("blocking Never", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "never",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container",
							Image: "gcr.io/google_containers/pause-amd64:3.0",
							ImagePullPolicy: corev1.PullNever,
						},
					},
				},
			}

			EventuallyCreation(func() (err error) {
				_, err = cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})

				return
			}).ShouldNot(Succeed())
		})
	})
})
