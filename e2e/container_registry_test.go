//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/api"
)

type Patch struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value string `json:"value"`
}

var _ = Describe("enforcing a Container Registry", func() {
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
		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
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

	It("should allow patching a matching registry after applying with a matching (Container)", func() {
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

		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})

			return err
		}).Should(Succeed())

		Eventually(func() error {
			payload := []Patch{{
				Op:    "replace",
				Path:  "/spec/containers/0/image",
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
