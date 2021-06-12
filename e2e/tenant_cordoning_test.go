//+build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"time"

	"github.com/clastix/capsule/api/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("cordoning a Tenant", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-cordoning",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "jim",
				Kind: "User",
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

	It("should block or allow operations", func() {
		cs := ownerClient(tnt)

		ns := NewNamespace("cordoned-namespace")

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "container",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "container",
						Image: "gcr.io/google_containers/pause-amd64:3.0",
					},
				},
			},
		}

		By("creating a Namespace", func() {
			NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())

			EventuallyCreation(func() error {
				_, err := cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})

				return err
			}).Should(Succeed())
		})

		By("cordoning the Tenant deletion must be blocked", func() {
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.Name}, tnt)).Should(Succeed())

			tnt.Labels = map[string]string{
				"capsule.clastix.io/cordon": "enabled",
			}

			Expect(k8sClient.Update(context.TODO(), tnt)).Should(Succeed())

			time.Sleep(2 * time.Second)

			Expect(cs.CoreV1().Pods(ns.Name).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})).ShouldNot(Succeed())
		})

		By("uncordoning the Tenant deletion must be allowed", func() {
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.Name}, tnt)).Should(Succeed())

			tnt.Labels = map[string]string{}

			Expect(k8sClient.Update(context.TODO(), tnt)).Should(Succeed())

			time.Sleep(2 * time.Second)

			Expect(cs.CoreV1().Pods(ns.Name).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})).Should(Succeed())
		})
	})
})
