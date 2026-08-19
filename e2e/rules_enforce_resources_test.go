// Copyright 2020-2026 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

var _ = Describe("enforcing workload resource namespace rules", Ordered, Label("tenant", "rules", "enforce", "workloads", "resources"), func() {
	const ownerName = "e2e-rules-resources"

	var tnt *capsulev1beta2.Tenant

	newTenant := func() *capsulev1beta2.Tenant {
		return &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "e2e-rule-resources",
				Labels: map[string]string{"env": "e2e"},
			},
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{
					Name: ownerName,
					Kind: rbac.UserOwner,
				}}}},
				Rules: []*rules.NamespaceRuleBodyTenant{
					{
						NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"resource-policy": "managed"}},
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
							Action: rules.ActionTypeDeny,
							Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
								Targets: []rules.WorkloadValidationTarget{rules.ValidateContainers},
								Resources: &rules.WorkloadResourceRules{
									Requests: map[corev1.ResourceName]rules.WorkloadResourceRequestPolicy{
										corev1.ResourceCPU: {
											Policy: rules.WorkloadResourceRequestPolicyDefault,
											Value:  e2eResourceQuantity("100m"),
										},
									},
									Limits: map[corev1.ResourceName]rules.WorkloadResourceLimitPolicy{
										corev1.ResourceCPU: {Policy: rules.WorkloadResourceLimitPolicyRemove},
										corev1.ResourceMemory: {
											Policy: rules.WorkloadResourceLimitPolicyMatchRequest,
										},
									},
								},
							},
						}},
					},
					{
						NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"resource-policy": "ratio"}},
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
							Action: rules.ActionTypeDeny,
							Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
								Targets: []rules.WorkloadValidationTarget{rules.ValidateContainers},
								Resources: &rules.WorkloadResourceRules{
									Limits: map[corev1.ResourceName]rules.WorkloadResourceLimitPolicy{
										corev1.ResourceMemory: {
											Policy: rules.WorkloadResourceLimitPolicyRatio,
											Value:  e2eResourceQuantity("1.5"),
										},
									},
								},
							},
						}},
					},
				},
			},
		}
	}

	newPod := func(name string, memoryLimit string) *corev1.Pod {
		resources := corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
		}
		if memoryLimit != "" {
			resources.Limits = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse(memoryLimit),
			}
		}

		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: corev1.PodSpec{
				SecurityContext: nobodyPodSecurityContext(),
				Containers: []corev1.Container{{
					Name:            "app",
					Image:           "registry.k8s.io/pause:3.9",
					ImagePullPolicy: corev1.PullIfNotPresent,
					SecurityContext: restrictedContainerSecurityContext(),
					Resources:       resources,
				}},
			},
		}
	}

	createNamespace := func(policy string) *corev1.Namespace {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel:  tnt.Name,
			"resource-policy": policy,
		})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		return ns
	}

	createPodAndExpectDenied := func(cs kubernetes.Interface, namespace string, pod *corev1.Pod, substrings ...string) {
		Eventually(func() error {
			candidate := pod.DeepCopy()
			candidate.Name = fmt.Sprintf("%s-%d", pod.Name, time.Now().UnixNano()%1e6)

			_, err := cs.CoreV1().Pods(namespace).Create(context.Background(), candidate, metav1.CreateOptions{})
			if err == nil {
				_ = cs.CoreV1().Pods(namespace).Delete(context.Background(), candidate.Name, metav1.DeleteOptions{})

				return fmt.Errorf("expected Pod creation to be denied")
			}
			if apierrors.IsAlreadyExists(err) {
				return err
			}

			for _, substring := range substrings {
				if !strings.Contains(err.Error(), substring) {
					return fmt.Errorf("expected error to contain %q, got %v", substring, err)
				}
			}

			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	JustBeforeEach(func() {
		tnt = newTenant()
		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""

			return k8sClient.Create(context.Background(), tnt)
		}).Should(Succeed())
		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
	})

	JustAfterEach(func() {
		EventuallyDeletion(tnt)
	})

	It("defaults, removes, and matches container resources", func() {
		ns := createNamespace("managed")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		pod := newPod("managed-resources", "2Gi")

		var created *corev1.Pod
		EventuallyCreation(func() error {
			var err error
			created, err = cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})

			return err
		}).Should(Succeed())

		containerResources := created.Spec.Containers[0].Resources
		Expect(containerResources.Requests.Cpu().Cmp(resource.MustParse("100m"))).To(Equal(0))
		Expect(containerResources.Limits).NotTo(HaveKey(corev1.ResourceCPU))
		Expect(containerResources.Limits.Memory().Cmp(resource.MustParse("1Gi"))).To(Equal(0))
	})

	It("defaults a missing limit from Ratio", func() {
		ns := createNamespace("ratio")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		pod := newPod("ratio-default", "")

		var created *corev1.Pod
		EventuallyCreation(func() error {
			var err error
			created, err = cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})

			return err
		}).Should(Succeed())

		Expect(created.Spec.Containers[0].Resources.Limits.Memory().Cmp(resource.MustParse("1536Mi"))).To(Equal(0))
	})

	It("denies an explicitly excessive Ratio limit", func() {
		ns := createNamespace("ratio")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createPodAndExpectDenied(
			cs,
			ns.Name,
			newPod("ratio-denied", "2Gi"),
			"violates policy Ratio",
			"must not exceed 1536Mi",
		)
	})
})

func e2eResourceQuantity(value string) *resource.Quantity {
	quantity := resource.MustParse(value)

	return &quantity
}
