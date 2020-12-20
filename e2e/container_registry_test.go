//+build e2e

/*
Copyright 2020 Clastix Labs.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("enforcing a Container Registry", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "container-registry",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "matt",
				Kind: "User",
			},
			ContainerRegistries: &v1alpha1.AllowedListSpec{
				Exact: []string{"docker.io", "docker.tld"},
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
		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		Eventually(func() (ok bool) {
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.Name}, ns)).Should(Succeed())
			ok, _ = HaveKeyWithValue("capsule.clastix.io/allowed-registries", "docker.io,docker.tld").Match(ns.Annotations)
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
		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())

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
		cs := ownerClient(tnt)
		_, err := cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})
		Expect(err).ShouldNot(Succeed())
	})

	It("should allow using an exact match", func() {
		ns := NewNamespace("registry-list")
		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "container",
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "container",
						Image: "docker.io/nginx:alpine",
					},
				},
			},
		}

		cs := ownerClient(tnt)
		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})
			return err
		}).Should(Succeed())
	})

	It("should allow using a regex match", func() {
		ns := NewNamespace("registry-regex")
		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())

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

		cs := ownerClient(tnt)
		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})
			return err
		}).Should(Succeed())
	})
})
