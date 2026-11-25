// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

// Regression coverage for the OverQuota branch of the Tenant-scoped quota sync:
// pinning an over-quota resource for a namespace without reported usage must
// not drop the hard limits of the other resources in the same ResourceQuota.
var _ = Describe("Tenant quota over-quota pinning preserves other resource limits", Ordered, Label("resourcequota"), func() {
	tnt := &capsulev1beta2.Tenant{
		Name: "e2e-quota-overquota",
		Labels: map[string]string{
			"env": "e2e",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					Name: "e2e-quota-overquota",
					Kind: "User",
				},
			},
			ResourceQuota: api.ResourceQuotaSpec{
				Scope: api.ResourceQuotaScopeTenant,
				Items: []corev1.ResourceQuotaSpec{
					{
						Hard: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceLimitsCPU:    resource.MustParse("1"),
							corev1.ResourceLimitsMemory: resource.MustParse("1Gi"),
						},
					},
				},
			},
		},
	}

	// A second Tenant shares the cluster while the over-quota sync runs: its
	// namespaces and limits must not be affected by the first Tenant's pinning.
	other := &capsulev1beta2.Tenant{
		Name: "e2e-quota-overquota-other",
		Labels: map[string]string{
			"env": "e2e",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					Name: "e2e-quota-overquota-other",
					Kind: "User",
				},
			},
			ResourceQuota: api.ResourceQuotaSpec{
				Scope: api.ResourceQuotaScopeTenant,
				Items: []corev1.ResourceQuotaSpec{
					{
						Hard: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceLimitsCPU:    resource.MustParse("2"),
							corev1.ResourceLimitsMemory: resource.MustParse("2Gi"),
						},
					},
				},
			},
		},
	}

	quotaName := fmt.Sprintf("capsule-%s-0", tnt.GetName())
	otherQuotaName := fmt.Sprintf("capsule-%s-0", other.GetName())

	pauseDeployment := func(name string, replicas int32) *appsv1.Deployment {
		return &appsv1.Deployment{
			Name: name,
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": name},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"app": name},
					},
					Spec: corev1.PodSpec{
						SecurityContext: nobodyPodSecurityContext(),
						Containers: []corev1.Container{
							{
								Name:            "pause",
								Image:           "registry.k8s.io/pause:3.10",
								SecurityContext: restrictedContainerSecurityContext(),
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("100m"),
										corev1.ResourceMemory: resource.MustParse("32Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("500m"),
										corev1.ResourceMemory: resource.MustParse("64Mi"),
									},
								},
							},
						},
					},
				},
			},
		}
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)

		EventuallyCreation(func() error {
			other.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), other)
		}).Should(Succeed())
		TenantReady(other, metav1.ConditionTrue, defaultTimeoutInterval)

		By("creating the namespaces of both tenants", func() {
			for _, ns := range []string{"overquota-a", "overquota-b"} {
				NamespaceCreation(
					NewNamespace(ns, map[string]string{meta.TenantLabel: tnt.GetName()}),
					tnt.Spec.Owners[0].UserSpec,
					defaultTimeoutInterval,
				).Should(Succeed())
			}
			NamespaceCreation(
				NewNamespace("overquota-other", map[string]string{meta.TenantLabel: other.GetName()}),
				other.Spec.Owners[0].UserSpec,
				defaultTimeoutInterval,
			).Should(Succeed())
		})
	})
	JustAfterEach(func() {
		EventuallyDeletion(tnt)
		EventuallyDeletion(other)
	})

	It("should keep the memory limit while pinning CPU and block over-quota workloads", func() {
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		By("consuming the whole tenant CPU budget from the first namespace", func() {
			EventuallyCreation(func() error {
				_, err := cs.AppsV1().Deployments("overquota-a").Create(context.TODO(), pauseDeployment("budget", 2), metav1.CreateOptions{})
				return err
			}).Should(Succeed())
		})

		By("waiting for the quota controller to observe the usage", func() {
			rq := &corev1.ResourceQuota{}
			Eventually(func() bool {
				if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: quotaName, Namespace: "overquota-a"}, rq); err != nil {
					return false
				}
				used := rq.Status.Used[corev1.ResourceLimitsCPU]
				return used.Cmp(resource.MustParse("1")) == 0
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})

		By("ensuring the usage-less namespace keeps its memory limit while CPU is pinned to zero", func() {
			rq := &corev1.ResourceQuota{}
			Eventually(func() bool {
				if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: quotaName, Namespace: "overquota-b"}, rq); err != nil {
					return false
				}
				cpu, cpuOK := rq.Spec.Hard[corev1.ResourceLimitsCPU]
				_, memOK := rq.Spec.Hard[corev1.ResourceLimitsMemory]
				return cpuOK && cpu.IsZero() && memOK
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue(), "memory hard limit must survive the CPU over-quota pinning")

			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: quotaName, Namespace: "overquota-b"}, rq)).To(Succeed())
			mem := rq.Spec.Hard[corev1.ResourceLimitsMemory]
			Expect(mem.IsZero()).To(BeFalse(), "memory hard limit must not be zeroed")
		})

		By("blocking a CPU workload in the pinned namespace", func() {
			EventuallyCreation(func() error {
				_, err := cs.AppsV1().Deployments("overquota-b").Create(context.TODO(), pauseDeployment("blocked-cpu", 1), metav1.CreateOptions{})
				return err
			}).ShouldNot(Succeed())
		})

		By("blocking a memory workload in the pinned namespace", func() {
			d := pauseDeployment("blocked-memory", 1)
			d.Spec.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			}
			d.Spec.Template.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			}
			EventuallyCreation(func() error {
				_, err := cs.AppsV1().Deployments("overquota-b").Create(context.TODO(), d, metav1.CreateOptions{})
				return err
			}).ShouldNot(Succeed())
		})

		By("leaving the second tenant untouched", func() {
			otherCS := ownerClient(other.Spec.Owners[0].UserSpec)
			EventuallyCreation(func() error {
				_, err := otherCS.AppsV1().Deployments("overquota-other").Create(context.TODO(), pauseDeployment("unaffected", 1), metav1.CreateOptions{})
				return err
			}).Should(Succeed())

			// Its own usage is observed asynchronously, so assert the limits
			// stay present and non-zero instead of an exact remaining budget.
			rq := &corev1.ResourceQuota{}
			Eventually(func() bool {
				if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: otherQuotaName, Namespace: "overquota-other"}, rq); err != nil {
					return false
				}
				cpu, cpuOK := rq.Spec.Hard[corev1.ResourceLimitsCPU]
				mem, memOK := rq.Spec.Hard[corev1.ResourceLimitsMemory]
				return cpuOK && memOK && !cpu.IsZero() && !mem.IsZero()
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue(), "the other tenant must keep non-zero CPU and memory limits")
		})
	})
})
