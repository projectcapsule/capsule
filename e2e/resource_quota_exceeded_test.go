//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

var _ = Describe("exceeding a Tenant resource quota", func() {
	tnt := &capsulev1beta1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-resources-changes",
		},
		Spec: capsulev1beta1.TenantSpec{
			Owners: capsulev1beta1.OwnerListSpec{
				{
					Name: "bobby",
					Kind: "User",
				},
			},
			LimitRanges: capsulev1beta1.LimitRangesSpec{Items: []corev1.LimitRangeSpec{
				{
					Limits: []corev1.LimitRangeItem{
						{
							Type: corev1.LimitTypePod,
							Min: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    resource.MustParse("50m"),
								corev1.ResourceMemory: resource.MustParse("5Mi"),
							},
							Max: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
						{
							Type: corev1.LimitTypeContainer,
							Default: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    resource.MustParse("200m"),
								corev1.ResourceMemory: resource.MustParse("100Mi"),
							},
							DefaultRequest: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("10Mi"),
							},
							Min: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    resource.MustParse("50m"),
								corev1.ResourceMemory: resource.MustParse("5Mi"),
							},
							Max: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
						{
							Type: corev1.LimitTypePersistentVolumeClaim,
							Min: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("1Gi"),
							},
							Max: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("10Gi"),
							},
						},
					},
				},
			},
			},
			ResourceQuota: capsulev1beta1.ResourceQuotaSpec{Items: []corev1.ResourceQuotaSpec{
				{
					Hard: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceLimitsCPU:      resource.MustParse("8"),
						corev1.ResourceLimitsMemory:   resource.MustParse("16Gi"),
						corev1.ResourceRequestsCPU:    resource.MustParse("8"),
						corev1.ResourceRequestsMemory: resource.MustParse("16Gi"),
					},
					Scopes: []corev1.ResourceQuotaScope{
						corev1.ResourceQuotaScopeNotTerminating,
					},
				},
				{
					Hard: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourcePods: resource.MustParse("10"),
					},
				},
				{
					Hard: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceRequestsStorage: resource.MustParse("100Gi"),
					},
				},
			},
			},
		},
	}

	nsl := []string{"easy", "peasy"}
	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
		By("creating the Namespaces", func() {
			for _, i := range nsl {
				ns := NewNamespace(i)
				NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
				TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
			}
		})
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})

	It("should block new Pods", func() {
		cs := ownerClient(tnt.Spec.Owners[0])
		for _, namespace := range nsl {
			Eventually(func() (err error) {
				d := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-pause",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: pointer.Int32Ptr(5),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": "pause",
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app": "pause",
								},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "my-pause",
										Image: "gcr.io/google_containers/pause-amd64:3.0",
									},
								},
							},
						},
					},
				}
				_, err = cs.AppsV1().Deployments(namespace).Create(context.TODO(), d, metav1.CreateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		for _, ns := range nsl {
			n := fmt.Sprintf("capsule-%s-1", tnt.GetName())
			rq := &corev1.ResourceQuota{}
			By("retrieving the Resource Quota", func() {
				Eventually(func() error {
					return k8sClient.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: ns}, rq)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			})
			By("ensuring the status has been blocked with actual usage", func() {
				Eventually(func() bool {
					_ = k8sClient.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: ns}, rq)
					return rq.Status.Hard.Pods().String() == rq.Status.Used.Pods().String()
				}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
			})
			By("creating an exceeded Pod", func() {
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
					_, err := cs.CoreV1().Pods(ns).Create(context.Background(), pod, metav1.CreateOptions{})
					return err
				}).ShouldNot(Succeed())
			})
		}
	})
})
