// Copyright 2020-2025 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

var _ = Describe("exceeding a Cluster Custom resource quota", Label("clustercustomresourcequota"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-cluster-resources-changes",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "bobby",
							Kind: "User",
						},
					},
				},
			},
		},
	}
	nsl := []string{"cluster-custom-resource-quota-ns1", "cluster-custom-resource-quota-ns2"}

	customresource := &capsulev1beta2.ClusterCustomQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-custom-quota-configmap",
		},
		Spec: capsulev1beta2.ClusterCustomQuotaSpec{
			Selectors: []metav1.LabelSelector{
				{
					MatchLabels: map[string]string{
						"capsule.clastix.io/tenant": "tenant-cluster-resources-changes",
					},
				},
			},
			CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
				Limit: resource.MustParse("1Gi"),
				Source: capsulev1beta2.CustomQuotaSpecSource{
					Version: "v1",
					Kind:    "ConfigMap",
					Path:    ".data.quantity",
				},
				ScopeSelectors: []metav1.LabelSelector{
					{
						MatchLabels: map[string]string{
							"foo": "bar",
						},
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
		By("creating the Namespaces, ClusterCustomResource", func() {
			for _, nsName := range nsl {
				ns := NewNamespace(nsName)
				NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
				TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
			}
			if k8sClient.Create(context.TODO(), customresource) != nil {
				utilruntime.HandleError(fmt.Errorf("failed to create the ClusterCustomResourceQuota"))
			}
		})
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), customresource)).Should(Succeed())
	})

	It("should Allow new ConfigMap", func() {
		cq := &capsulev1beta2.ClusterCustomQuota{}
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		Eventually(func() (err error) {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-configmap",
					Labels: map[string]string{
						"foo": "bar",
					},
				},
				Data: map[string]string{
					"quantity": "1Gi",
				},
			}
			_, err = cs.CoreV1().ConfigMaps(nsl[0]).Create(context.TODO(), cm, metav1.CreateOptions{})
			return
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		By("retrieving the Custom Resource Quota", func() {
			Eventually(func() error {
				return k8sClient.Get(context.TODO(), types.NamespacedName{Name: customresource.Name}, cq)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
		By("ensuring available is at 0", func() {
			Eventually(func() bool {
				_ = k8sClient.Get(context.TODO(), types.NamespacedName{Name: customresource.Name}, cq)
				return cq.Status.Available.String() == "0"
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
		By("creating an exceeded ConfigMap", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-configmap-exceed",
					Labels: map[string]string{
						"foo": "bar",
					},
				},
				Data: map[string]string{
					"quantity": "20Mi",
				},
			}
			cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
			EventuallyCreation(func() error {
				_, err := cs.CoreV1().ConfigMaps(nsl[1]).Create(context.TODO(), cm, metav1.CreateOptions{})
				return err
			}).ShouldNot(Succeed())
		})
		By("Resize the ConfigMap to a smaller size", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-configmap",
					Labels: map[string]string{
						"foo": "bar",
					},
				},
				Data: map[string]string{
					"quantity": "0.5Gi",
				},
			}
			EventuallyCreation(func() error {
				_, err := cs.CoreV1().ConfigMaps(nsl[0]).Update(context.TODO(), cm, metav1.UpdateOptions{})
				return err
			}).Should(Succeed())
		})
		By("creating the previously exceeded ConfigMap", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-configmap-exceed",
					Labels: map[string]string{
						"foo": "bar",
					},
				},
				Data: map[string]string{
					"quantity": "0.5Gi",
				},
			}
			cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
			EventuallyCreation(func() error {
				_, err := cs.CoreV1().ConfigMaps(nsl[1]).Create(context.TODO(), cm, metav1.CreateOptions{})
				return err
			}).Should(Succeed())
		})
	})
})
