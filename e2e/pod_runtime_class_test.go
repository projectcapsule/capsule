//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/clastix/capsule/pkg/api"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

var _ = Describe("enforcing a Runtime Class", func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "runtime-class",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "george",
					Kind: "User",
				},
			},
			RuntimeClasses: &api.SelectorAllowedListSpec{
				AllowedListSpec: api.AllowedListSpec{
					Exact: []string{"legacy"},
					Regex: "^hardened-.*$",
				},
				LabelSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"env": "customers",
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

	It("should block non allowed Runtime Class", func() {
		runtime := &nodev1.RuntimeClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "disallowed",
			},
			Handler: "custom-handler",
		}
		Expect(k8sClient.Create(context.TODO(), runtime)).Should(Succeed())

		defer func() {
			Expect(k8sClient.Delete(context.TODO(), runtime)).Should(Succeed())
		}()

		ns := NewNamespace("rt-disallow")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		runtimeName := "disallowed"
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
				RuntimeClassName: &runtimeName,
			},
		}

		cs := ownerClient(tnt.Spec.Owners[0])
		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
			return err
		}).ShouldNot(Succeed())
	})

	It("should allow exact match", func() {
		runtime := &nodev1.RuntimeClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "legacy",
			},
			Handler: "custom-handler",
		}
		Expect(k8sClient.Create(context.TODO(), runtime)).Should(Succeed())

		defer func() {
			Expect(k8sClient.Delete(context.TODO(), runtime)).Should(Succeed())
		}()

		ns := NewNamespace("rt-exact-match")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		runtimeName := "legacy"
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
				RuntimeClassName: &runtimeName,
			},
		}

		cs := ownerClient(tnt.Spec.Owners[0])
		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
			return err
		}).Should(Succeed())
	})

	It("should allow regex match", func() {
		ns := NewNamespace("rc-regex-match")

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

		for i, rt := range []string{"hardened-crio", "hardened-containerd", "hardened-dockerd"} {
			runtimeName := strings.Join([]string{rt, "-", strconv.Itoa(i)}, "")
			runtime := &nodev1.RuntimeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: runtimeName,
				},
				Handler: "custom-handler",
			}

			Expect(k8sClient.Create(context.TODO(), runtime)).Should(Succeed())

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: rt,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container",
							Image: "quay.io/google-containers/pause-amd64:3.0",
						},
					},
					RuntimeClassName: &runtimeName,
				},
			}

			cs := ownerClient(tnt.Spec.Owners[0])

			EventuallyCreation(func() error {
				_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
				return err
			}).Should(Succeed())

			Expect(k8sClient.Delete(context.TODO(), runtime)).Should(Succeed())
		}
	})

	It("should allow selector match", func() {
		ns := NewNamespace("rc-selector-match")

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

		for i, rt := range []string{"customer-containerd", "customer-crio", "customer-dockerd"} {
			runtimeName := strings.Join([]string{rt, "-", strconv.Itoa(i)}, "")
			runtime := &nodev1.RuntimeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: runtimeName,
					Labels: map[string]string{
						"name": runtimeName,
						"env":  "customers",
					},
				},
				Handler: "custom-handler",
			}

			Expect(k8sClient.Create(context.TODO(), runtime)).Should(Succeed())

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: rt,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container",
							Image: "quay.io/google-containers/pause-amd64:3.0",
						},
					},
					RuntimeClassName: &runtimeName,
				},
			}

			cs := ownerClient(tnt.Spec.Owners[0])

			EventuallyCreation(func() error {
				_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})
				return err
			}).Should(Succeed())

			Expect(k8sClient.Delete(context.TODO(), runtime)).Should(Succeed())
		}
	})

})
