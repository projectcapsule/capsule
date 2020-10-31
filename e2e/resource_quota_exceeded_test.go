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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("exceeding Tenant resource quota", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenantresourceschanges",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "bobby",
				Kind: "User",
			},
			NamespacesMetadata: v1alpha1.AdditionalMetadata{},
			ServicesMetadata:   v1alpha1.AdditionalMetadata{},
			IngressClasses:     v1alpha1.IngressClassesSpec{},
			StorageClasses:     v1alpha1.StorageClassesSpec{},
			LimitRanges: []corev1.LimitRangeSpec{
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
			NetworkPolicies: []networkingv1.NetworkPolicySpec{},
			NamespaceQuota:  2,
			NodeSelector:    map[string]string{},
			ResourceQuota: []corev1.ResourceQuotaSpec{
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
				NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
				TenantNamespaceList(tnt, podRecreationTimeoutInterval).Should(ContainElement(ns.GetName()))
			}
		})
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})
	It("should block new Pods if limit is reached", func() {
		cs := ownerClient(tnt)
		for _, namespace := range nsl {
			Eventually(func() (err error) {
				d := &v1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-pause",
					},
					Spec: v1.DeploymentSpec{
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
			}, 15*time.Second, time.Second).Should(Succeed())
		}
		for _, ns := range nsl {
			n := fmt.Sprintf("capsule-%s-1", tnt.GetName())
			rq := &corev1.ResourceQuota{}
			By("retrieving the Resource Quota", func() {
				Eventually(func() error {
					return k8sClient.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: ns}, rq)
				}, 15*time.Second, time.Second).Should(Succeed())
			})
			By("ensuring the status has been blocked with actual usage", func() {
				Eventually(func() corev1.ResourceList {
					_ = k8sClient.Get(context.TODO(), types.NamespacedName{Name: n, Namespace: ns}, rq)
					return rq.Status.Hard
				}, 15*time.Second, time.Second).Should(Equal(rq.Status.Used))
			})
			By("creating an exceeded Pod", func() {
				d := &v1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-exceeded",
					},
					Spec: v1.DeploymentSpec{
						Replicas: pointer.Int32Ptr(5),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app": "exceeded",
							},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app": "exceeded",
								},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "my-exceeded",
										Image: "gcr.io/google_containers/pause-amd64:3.0",
									},
								},
							},
						},
					},
				}
				_, err := cs.AppsV1().Deployments(ns).Create(context.TODO(), d, metav1.CreateOptions{})
				Expect(err).Should(Succeed())
				Eventually(func() (condition *v1.DeploymentCondition) {
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: d.GetName(), Namespace: ns}, d)).Should(Succeed())
					for _, i := range d.Status.Conditions {
						if i.Type == v1.DeploymentReplicaFailure {
							condition = &i
							break
						}
					}
					return
				}, 30*time.Second, time.Second).ShouldNot(BeNil())
			})
		}
	})
})
