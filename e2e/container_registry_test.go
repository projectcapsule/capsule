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
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

var _ = Describe("enforcing a Container Registry", func() {
	tnt := &capsulev1beta1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "container-registry",
		},
		Spec: capsulev1beta1.TenantSpec{
			Owners: capsulev1beta1.OwnerListSpec{
				{
					Name: "matt",
					Kind: "User",
				},
			},
			ContainerRegistries: &capsulev1beta1.AllowedListSpec{
				Exact: []string{"docker.io", "myregistry.azurecr.io"},
				Regex: `quay\.\w+`,
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

	It("should add labels to Namespace", func() {
		ns := NewNamespace("registry-labels")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		Eventually(func() (ok bool) {
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.Name}, ns)).Should(Succeed())
			ok, _ = HaveKeyWithValue("capsule.clastix.io/allowed-registries", "docker.io,myregistry.azurecr.io").Match(ns.Annotations)
			if !ok {
				return
			}
			ok, _ = HaveKeyWithValue("capsule.clastix.io/allowed-registries-regexp", `quay\.\w+`).Match(ns.Annotations)
			if !ok {
				return
			}
			return true
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	})

	It("should deny running a gcr.io container", func() {
		ns := NewNamespace("registry-deny")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

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
		cs := ownerClient(tnt.Spec.Owners[0])
		_, err := cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})
		Expect(err).ShouldNot(Succeed())
	})

	It("should allow using a registry only match", func() {
		ns := NewNamespace("registry-only")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "container",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "container",
						Image: "myregistry.azurecr.io/myapp:latest",
					},
				},
			},
		}

		cs := ownerClient(tnt.Spec.Owners[0])
		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})
			return err
		}).Should(Succeed())
	})

	It("should allow using an exact match", func() {
		ns := NewNamespace("registry-list")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "container",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "container",
						Image: "docker.io/library/nginx:alpine",
					},
				},
			},
		}

		cs := ownerClient(tnt.Spec.Owners[0])
		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})
			return err
		}).Should(Succeed())
	})

	It("should allow using a regex match", func() {
		ns := NewNamespace("registry-regex")
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
			},
		}

		cs := ownerClient(tnt.Spec.Owners[0])
		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})
			return err
		}).Should(Succeed())
	})
})
