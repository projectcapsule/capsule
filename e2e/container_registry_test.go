// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

type Patch struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value string `json:"value"`
}

var _ = Describe("enforcing a Container Registry", Label("tenant", "images", "registry"), func() {
	originConfig := &capsulev1beta2.CapsuleConfiguration{}

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "container-registry",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "matt",
					Kind: "User",
				},
			},
			ContainerRegistries: &api.AllowedListSpec{
				Exact: []string{"docker.io", "myregistry.azurecr.io"},
				Regex: `quay\.\w+`,
			},
		},
	}

	JustBeforeEach(func() {
		Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: defaultConfigurationName}, originConfig)).To(Succeed())

		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
	})

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())

		// Restore Configuration
		Eventually(func() error {
			c := &capsulev1beta2.CapsuleConfiguration{}
			if err := k8sClient.Get(context.Background(), client.ObjectKey{Name: originConfig.Name}, c); err != nil {
				return err
			}
			// Apply the initial configuration from originConfig to c
			c.Spec = originConfig.Spec
			return k8sClient.Update(context.Background(), c)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("should add labels to Namespace", func() {
		ns := NewNamespace("")
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
		ns := NewNamespace("")

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

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})

			return err
		}).ShouldNot(Succeed())
	})

	It("should allow using a registry only match", func() {
		ns := NewNamespace("")

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

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})

			return err
		}).Should(Succeed())

		By("verifying the image was correctly mutated", func() {
			created := &corev1.Pod{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{
				Namespace: ns.Name,
				Name:      pod.Name,
			}, created)).To(Succeed())

			Expect(created.Spec.Containers).To(HaveLen(1))
			Expect(created.Spec.Containers[0].Image).To(Equal("myregistry.azurecr.io/myapp:latest"))
		})
	})

	It("should deny patching a not matching registry after applying with a matching (Container)", func() {
		ns := NewNamespace("")

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

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})

			return err
		}).Should(Succeed())

		By("verifying the image was correctly mutated", func() {
			created := &corev1.Pod{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{
				Namespace: ns.Name,
				Name:      pod.Name,
			}, created)).To(Succeed())

			Expect(created.Spec.Containers).To(HaveLen(1))
			Expect(created.Spec.Containers[0].Image).To(Equal("myregistry.azurecr.io/myapp:latest"))
		})

		Eventually(func() error {
			payload := []Patch{{
				Op:    "replace",
				Path:  "/spec/containers/0/image",
				Value: "attacker/google-containers/pause-amd64:3.0",
			}}
			payloadBytes, _ := json.Marshal(payload)
			_, err := cs.CoreV1().Pods(ns.GetName()).Patch(context.TODO(), pod.GetName(), types.JSONPatchType, payloadBytes, metav1.PatchOptions{})
			if err != nil {
				return err
			}
			return nil
		}).ShouldNot(Succeed())
	})

	It("should deny patching a not matching registry after applying with a matching (EphemeralContainer)", func() {
		ns := NewNamespace("")

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "container",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "container",
						Image: "docker.io/google-containers/pause-amd64:3.0",
					},
				},
			},
		}

		cs := ownerClient(tnt.Spec.Owners[0])

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

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

		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})

			return err
		}).Should(Succeed())

		Eventually(func() error {
			pod.Spec.EphemeralContainers = []corev1.EphemeralContainer{
				{
					EphemeralContainerCommon: corev1.EphemeralContainerCommon{
						Name:            "dbg",
						Image:           "attacker/google-containers/pause-amd64:3.0",
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

	It("should deny patching a not matching registry after applying with a matching (initContainer)", func() {
		ns := NewNamespace("")

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "container",
			},
			Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{
					{
						Name:  "init",
						Image: "myregistry.azurecr.io/myapp:latest",
					},
				},
				Containers: []corev1.Container{
					{
						Name:  "container",
						Image: "myregistry.azurecr.io/myapp:latest",
					},
				},
			},
		}

		cs := ownerClient(tnt.Spec.Owners[0])

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})

			return err
		}).Should(Succeed())

		Eventually(func() error {
			payload := []Patch{{
				Op:    "replace",
				Path:  "/spec/initContainers/0/image",
				Value: "attacker/google-containers/pause-amd64:3.0",
			}}
			payloadBytes, _ := json.Marshal(payload)
			_, err := cs.CoreV1().Pods(ns.GetName()).Patch(context.TODO(), pod.GetName(), types.JSONPatchType, payloadBytes, metav1.PatchOptions{})
			if err != nil {
				return err
			}
			return nil
		}).ShouldNot(Succeed())
	})

	It("should deny patching a not matching registry after applying with a matching (Container)", func() {
		ns := NewNamespace("")

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

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})

			return err
		}).Should(Succeed())

		Eventually(func() error {
			payload := []Patch{{
				Op:    "replace",
				Path:  "/spec/initContainers/0/image",
				Value: "attacker/google-containers/pause-amd64:3.0",
			}}
			payloadBytes, _ := json.Marshal(payload)
			_, err := cs.CoreV1().Pods(ns.GetName()).Patch(context.TODO(), pod.GetName(), types.JSONPatchType, payloadBytes, metav1.PatchOptions{})
			if err != nil {
				return err
			}
			return nil
		}).ShouldNot(Succeed())
	})

	It("should allow patching a matching registry after applying with a matching (EphemeralContainer)", func() {
		ns := NewNamespace("")

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "container",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "container",
						Image: "docker.io/google-containers/pause-amd64:3.0",
					},
				},
			},
		}

		cs := ownerClient(tnt.Spec.Owners[0])

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

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

		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})

			return err
		}).Should(Succeed())

		Eventually(func() error {
			pod.Spec.EphemeralContainers = []corev1.EphemeralContainer{
				{
					EphemeralContainerCommon: corev1.EphemeralContainerCommon{
						Name:            "dbg",
						Image:           "myregistry.azurecr.io/google-containers/pause-amd64:3.1",
						ImagePullPolicy: corev1.PullIfNotPresent,
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

	It("should allow patching a matching registry after applying with a matching (initContainer)", func() {
		ns := NewNamespace("")

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "container",
			},
			Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{
					{
						Name:  "init",
						Image: "myregistry.azurecr.io/myapp:latest",
					},
				},
				Containers: []corev1.Container{
					{
						Name:  "container",
						Image: "docker.io/google-containers/pause-amd64:3.0",
					},
				},
			},
		}

		cs := ownerClient(tnt.Spec.Owners[0])

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})

			return err
		}).Should(Succeed())

		Eventually(func() error {
			payload := []Patch{{
				Op:    "replace",
				Path:  "/spec/initContainers/0/image",
				Value: "myregistry.azurecr.io/google-containers/pause-amd64:3.1",
			}}
			payloadBytes, _ := json.Marshal(payload)
			_, err := cs.CoreV1().Pods(ns.GetName()).Patch(context.TODO(), pod.GetName(), types.JSONPatchType, payloadBytes, metav1.PatchOptions{})
			if err != nil {
				return err
			}
			return nil
		}).Should(Succeed())
	})

	It("should allow using an exact match", func() {
		ns := NewNamespace("")
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
		ns := NewNamespace("")
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
