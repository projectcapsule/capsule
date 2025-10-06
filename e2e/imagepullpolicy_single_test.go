// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

var _ = Describe("enforcing a defined ImagePullPolicy", Label("tenant", "images", "policy"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "image-pull-policy",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "axel",
					Kind: "User",
				},
			},
			ImagePullPolicies: []api.ImagePullPolicySpec{"Always"},
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
		ns := NewNamespace("")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

		cs := ownerClient(tnt.Spec.Owners[0])

		role := &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ephemeralcontainers-editor",
				Namespace: ns.GetName(),
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""}, // core API group
					Resources: []string{"pods/ephemeralcontainers"},
					Verbs:     []string{"update", "patch"},
				},
			},
		}

		rb := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ephemeralcontainers-editor-binding",
				Namespace: ns.GetName(),
			},
			Subjects: []rbacv1.Subject{
				{
					Kind: rbacv1.UserKind,
					Name: tnt.Spec.Owners[0].Name,
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     role.Name,
			},
		}

		// Create role and binding before test logic
		Expect(k8sClient.Create(context.TODO(), role)).To(Succeed())
		Expect(k8sClient.Create(context.TODO(), rb)).To(Succeed())

		By("allowing Always", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pull-always",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "container",
							Image:           "gcr.io/google_containers/pause-amd64:3.0",
							ImagePullPolicy: corev1.PullAlways,
						},
					},
				},
			}

			EventuallyCreation(func() (err error) {
				_, err = cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})

				return
			}).Should(Succeed())

			Eventually(func() error {
				pod.Spec.EphemeralContainers = []corev1.EphemeralContainer{
					{
						EphemeralContainerCommon: corev1.EphemeralContainerCommon{
							Name:            "dbg",
							Image:           "gcr.io/google_containers/pause-amd64:3.0",
							ImagePullPolicy: corev1.PullAlways,
						},
					},
				}

				_, err := cs.CoreV1().Pods(ns.Name).UpdateEphemeralContainers(context.Background(), pod.Name, pod, metav1.UpdateOptions{})
				if err != nil {
					return err
				}
				return nil
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
							Name:            "container",
							Image:           "gcr.io/google_containers/pause-amd64:3.0",
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
				},
			}

			EventuallyCreation(func() (err error) {
				_, err = cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})

				return
			}).ShouldNot(Succeed())

			Eventually(func() error {
				pod.Spec.EphemeralContainers = []corev1.EphemeralContainer{
					{
						EphemeralContainerCommon: corev1.EphemeralContainerCommon{
							Name:            "dbg",
							Image:           "gcr.io/google_containers/pause-amd64:3.0",
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
				}

				_, err := cs.CoreV1().Pods(ns.Name).UpdateEphemeralContainers(context.Background(), pod.Name, pod, metav1.UpdateOptions{})
				if err != nil {
					return err
				}
				return nil
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
							Name:            "container",
							Image:           "gcr.io/google_containers/pause-amd64:3.0",
							ImagePullPolicy: corev1.PullNever,
						},
					},
				},
			}

			EventuallyCreation(func() (err error) {
				_, err = cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})

				return
			}).ShouldNot(Succeed())

			Eventually(func() error {
				pod.Spec.EphemeralContainers = []corev1.EphemeralContainer{
					{
						EphemeralContainerCommon: corev1.EphemeralContainerCommon{
							Name:            "dbg",
							Image:           "gcr.io/google_containers/pause-amd64:3.0",
							ImagePullPolicy: corev1.PullNever,
						},
					},
				}

				_, err := cs.CoreV1().Pods(ns.Name).UpdateEphemeralContainers(context.Background(), pod.Name, pod, metav1.UpdateOptions{})
				if err != nil {
					return err
				}
				return nil
			}).ShouldNot(Succeed())
		})
	})
})
