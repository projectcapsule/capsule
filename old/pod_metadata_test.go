//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("adding metadata to Pod objects", func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-metadata",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "gatsby",
					Kind: "User",
				},
			},
			PodOptions: &api.PodOptions{
				AdditionalMetadata: &api.AdditionalMetadataSpec{
					Labels: map[string]string{
						"k8s.io/custom-label":     "foo",
						"clastix.io/custom-label": "bar",
					},
					Annotations: map[string]string{
						"k8s.io/custom-annotation":     "bizz",
						"clastix.io/custom-annotation": "buzz",
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

	It("should apply them to Pod", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		fmt.Sprint("namespace created")
		//TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
		fmt.Sprint("tenant contains list namespace")
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-metadata",
				Namespace: ns.GetName(),
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:            "container",
						Image:           "quay.io/google-containers/pause-amd64:3.0",
						ImagePullPolicy: "IfNotPresent",
					},
				},
				RestartPolicy: "Always",
			},
		}

		EventuallyCreation(func() (err error) {
			_, err = ownerClient(tnt.Spec.Owners[0]).CoreV1().Pods(ns.GetName()).Create(context.Background(), pod, metav1.CreateOptions{})

			return
		}).Should(Succeed())

		By("checking additional labels", func() {
			Eventually(func() (ok bool) {
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: pod.GetName(), Namespace: ns.GetName()}, pod)).Should(Succeed())
				for k, v := range tnt.Spec.PodOptions.AdditionalMetadata.Labels {
					ok, _ = HaveKeyWithValue(k, v).Match(pod.GetLabels())
					if !ok {
						return false
					}
				}
				return true
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})

		By("checking additional annotations", func() {
			Eventually(func() (ok bool) {
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: pod.GetName(), Namespace: ns.GetName()}, pod)).Should(Succeed())
				for k, v := range tnt.Spec.PodOptions.AdditionalMetadata.Annotations {
					ok, _ = HaveKeyWithValue(k, v).Match(pod.GetAnnotations())
					if !ok {
						return false
					}
				}
				return true
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
	})

})
