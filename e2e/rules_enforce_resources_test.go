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
	"sigs.k8s.io/controller-runtime/pkg/client"

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
						NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"resource-policy": "ephemeral-managed"}},
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
							Action: rules.ActionTypeDeny,
							Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
								Targets: []rules.WorkloadValidationTarget{
									rules.ValidateContainers,
									rules.ValidateInitContainers,
								},
								Resources: &rules.WorkloadResourceRules{
									Requests: map[corev1.ResourceName]rules.WorkloadResourceRequestPolicy{
										corev1.ResourceEphemeralStorage: {
											Policy: rules.WorkloadResourceRequestPolicyDefault,
											Value:  e2eResourceQuantity("1Gi"),
										},
									},
									Limits: map[corev1.ResourceName]rules.WorkloadResourceLimitPolicy{
										corev1.ResourceEphemeralStorage: {
											Policy: rules.WorkloadResourceLimitPolicyDefault,
											Value:  e2eResourceQuantity("2Gi"),
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
					{
						NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"resource-policy": "init-only"}},
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
							Action: rules.ActionTypeDeny,
							Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
								Targets: []rules.WorkloadValidationTarget{rules.ValidateInitContainers},
								Resources: &rules.WorkloadResourceRules{
									Requests: map[corev1.ResourceName]rules.WorkloadResourceRequestPolicy{
										corev1.ResourceCPU:    {Policy: rules.WorkloadResourceRequestPolicyRemove},
										corev1.ResourceMemory: {Policy: rules.WorkloadResourceRequestPolicyPreserve},
									},
									Limits: map[corev1.ResourceName]rules.WorkloadResourceLimitPolicy{
										corev1.ResourceCPU: {Policy: rules.WorkloadResourceLimitPolicyRemove},
										corev1.ResourceMemory: {
											Policy: rules.WorkloadResourceLimitPolicyDefault,
											Value:  e2eResourceQuantity("256Mi"),
										},
										corev1.ResourceEphemeralStorage: {Policy: rules.WorkloadResourceLimitPolicyPreserve},
									},
								},
							},
						}},
					},
					{
						NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"resource-policy": "pod-ratio"}},
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
							Action: rules.ActionTypeDeny,
							Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
								Targets: []rules.WorkloadValidationTarget{rules.ValidatePod},
								Resources: &rules.WorkloadResourceRules{
									Limits: map[corev1.ResourceName]rules.WorkloadResourceLimitPolicy{
										corev1.ResourceCPU: {
											Policy: rules.WorkloadResourceLimitPolicyRatio,
											Value:  e2eResourceQuantity("1.5"),
										},
									},
								},
							},
						}},
					},
					{
						NamespaceSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"resource-policy": "ratio-allow"}},
						NamespaceRuleBodyNamespace: resourceRatioRule(rules.ActionTypeAllow, rules.ValidateContainers, "1.5"),
					},
					{
						NamespaceSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"resource-policy": "ratio-audit"}},
						NamespaceRuleBodyNamespace: resourceRatioRule(rules.ActionTypeAudit, rules.ValidateContainers, "1.5"),
					},
					{
						NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"resource-policy": "ephemeral-ratio"}},
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
							Action: rules.ActionTypeDeny,
							Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
								Resources: &rules.WorkloadResourceRules{
									Limits: map[corev1.ResourceName]rules.WorkloadResourceLimitPolicy{
										corev1.ResourceEphemeralStorage: {
											Policy: rules.WorkloadResourceLimitPolicyRatio,
											Value:  e2eResourceQuantity("1.5"),
										},
									},
								},
							},
						}},
					},
					{
						NamespaceSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"resource-policy": "ratio-override"}},
						NamespaceRuleBodyNamespace: resourceRatioRule(rules.ActionTypeDeny, rules.ValidateContainers, "1.5"),
					},
					{
						NamespaceSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"resource-policy": "ratio-override"}},
						NamespaceRuleBodyNamespace: resourceRatioRule(rules.ActionTypeAllow, rules.ValidateContainers, "2"),
					},
					{
						NamespaceSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"resource-policy": "ratio-preserve"}},
						NamespaceRuleBodyNamespace: resourceRatioRule(rules.ActionTypeDeny, rules.ValidateContainers, "1.5"),
					},
					{
						NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"resource-policy": "ratio-preserve"}},
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
							Action: rules.ActionTypeAllow,
							Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
								Targets: []rules.WorkloadValidationTarget{rules.ValidateContainers},
								Resources: &rules.WorkloadResourceRules{
									Limits: map[corev1.ResourceName]rules.WorkloadResourceLimitPolicy{
										corev1.ResourceMemory: {Policy: rules.WorkloadResourceLimitPolicyPreserve},
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

	createPodAndExpectAllowed := func(cs kubernetes.Interface, namespace string, pod *corev1.Pod) *corev1.Pod {
		var created *corev1.Pod
		EventuallyCreation(func() error {
			var err error
			created, err = cs.CoreV1().Pods(namespace).Create(context.Background(), pod, metav1.CreateOptions{})

			return err
		}).Should(Succeed())

		return created
	}

	expectResourceAuditEvent := func(namespace string, podName string, substrings ...string) {
		Eventually(func() error {
			events, err := clusterAdminClient().EventsV1().Events(namespace).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return err
			}

			for _, event := range events.Items {
				if event.Regarding.Name != podName || event.Reason != "NamespaceRuleAudit" {
					continue
				}

				matched := true
				for _, substring := range substrings {
					if !strings.Contains(event.Note, substring) {
						matched = false

						break
					}
				}
				if matched {
					return nil
				}
			}

			return fmt.Errorf("expected resource audit event for Pod %q containing %q", podName, substrings)
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

	It("projects ordered resource policies into the namespace RuleStatus", func() {
		ns := createNamespace("ratio-override")

		Eventually(func(g Gomega) {
			status := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{
				Name:      meta.NameForManagedRuleStatus(),
				Namespace: ns.Name,
			}, status)).To(Succeed())
			g.Expect(status.Status.Rules).To(HaveLen(2))

			for index, expected := range []struct {
				action rules.ActionType
				ratio  string
			}{
				{action: rules.ActionTypeDeny, ratio: "1.5"},
				{action: rules.ActionTypeAllow, ratio: "2"},
			} {
				projected := status.Status.Rules[index]
				g.Expect(projected).NotTo(BeNil())
				g.Expect(projected.Enforce).NotTo(BeNil())
				g.Expect(projected.Enforce.Action).To(Equal(expected.action))
				g.Expect(projected.Enforce.Workloads.Targets).To(Equal(
					[]rules.WorkloadValidationTarget{rules.ValidateContainers},
				))

				policy := projected.Enforce.Workloads.Resources.Limits[corev1.ResourceMemory]
				g.Expect(policy.Policy).To(Equal(rules.WorkloadResourceLimitPolicyRatio))
				g.Expect(policy.Value).NotTo(BeNil())
				g.Expect(policy.Value.Cmp(resource.MustParse(expected.ratio))).To(Equal(0))
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("defaults, removes, and matches resources at all default targets", func() {
		ns := createNamespace("managed")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		pod := newPod("managed-resources", "2Gi")
		pod.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("250m")
		pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{
			Name:            "sidecar",
			Image:           "registry.k8s.io/pause:3.9",
			ImagePullPolicy: corev1.PullIfNotPresent,
			SecurityContext: restrictedContainerSecurityContext(),
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi")},
				Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("512Mi")},
			},
		})
		pod.Spec.InitContainers = []corev1.Container{{
			Name:            "init-with-limit",
			Image:           "registry.k8s.io/pause:3.9",
			ImagePullPolicy: corev1.PullIfNotPresent,
			SecurityContext: restrictedContainerSecurityContext(),
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("50m"),
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			},
		}, {
			Name:            "init-defaulted",
			Image:           "registry.k8s.io/pause:3.9",
			ImagePullPolicy: corev1.PullIfNotPresent,
			SecurityContext: restrictedContainerSecurityContext(),
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
			},
		}}
		pod.Spec.Resources = &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		}

		created := createPodAndExpectAllowed(cs, ns.Name, pod)

		containerResources := created.Spec.Containers[0].Resources
		Expect(containerResources.Requests.Cpu().Cmp(resource.MustParse("250m"))).To(Equal(0))
		Expect(containerResources.Limits).NotTo(HaveKey(corev1.ResourceCPU))
		Expect(containerResources.Limits.Memory().Cmp(resource.MustParse("1Gi"))).To(Equal(0))

		sidecarResources := created.Spec.Containers[1].Resources
		Expect(sidecarResources.Requests.Cpu().Cmp(resource.MustParse("100m"))).To(Equal(0))
		Expect(sidecarResources.Limits).NotTo(HaveKey(corev1.ResourceCPU))
		Expect(sidecarResources.Limits.Memory().Cmp(resource.MustParse("128Mi"))).To(Equal(0))

		initResources := created.Spec.InitContainers[0].Resources
		Expect(initResources.Requests.Cpu().Cmp(resource.MustParse("50m"))).To(Equal(0))
		Expect(initResources.Limits).NotTo(HaveKey(corev1.ResourceCPU))
		Expect(initResources.Limits.Memory().Cmp(resource.MustParse("512Mi"))).To(Equal(0))

		defaultedInitResources := created.Spec.InitContainers[1].Resources
		Expect(defaultedInitResources.Requests.Cpu().Cmp(resource.MustParse("100m"))).To(Equal(0))
		Expect(defaultedInitResources.Limits).NotTo(HaveKey(corev1.ResourceCPU))
		Expect(defaultedInitResources.Limits.Memory().Cmp(resource.MustParse("256Mi"))).To(Equal(0))

		Expect(created.Spec.Resources).NotTo(BeNil())
		Expect(created.Spec.Resources.Requests.Cpu().Cmp(resource.MustParse("2"))).To(Equal(0))
		Expect(created.Spec.Resources.Limits).NotTo(HaveKey(corev1.ResourceCPU))
		Expect(created.Spec.Resources.Limits.Memory().Cmp(resource.MustParse("2Gi"))).To(Equal(0))
	})

	It("defaults ephemeral storage only on explicitly targeted containers", func() {
		ns := createNamespace("ephemeral-managed")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		pod := newPod("managed-ephemeral-storage", "")
		pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{
			Name:            "sidecar",
			Image:           "registry.k8s.io/pause:3.9",
			ImagePullPolicy: corev1.PullIfNotPresent,
			SecurityContext: restrictedContainerSecurityContext(),
			Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceEphemeralStorage: resource.MustParse("1536Mi"),
			}},
		})
		pod.Spec.InitContainers = []corev1.Container{{
			Name:            "init-with-limit",
			Image:           "registry.k8s.io/pause:3.9",
			ImagePullPolicy: corev1.PullIfNotPresent,
			SecurityContext: restrictedContainerSecurityContext(),
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceEphemeralStorage: resource.MustParse("1Gi")},
				Limits:   corev1.ResourceList{corev1.ResourceEphemeralStorage: resource.MustParse("3Gi")},
			},
		}, {
			Name:            "init-defaulted",
			Image:           "registry.k8s.io/pause:3.9",
			ImagePullPolicy: corev1.PullIfNotPresent,
			SecurityContext: restrictedContainerSecurityContext(),
		}}

		created := createPodAndExpectAllowed(cs, ns.Name, pod)

		Expect(created.Spec.Containers[0].Resources.Requests.StorageEphemeral().Cmp(resource.MustParse("1Gi"))).To(Equal(0))
		Expect(created.Spec.Containers[0].Resources.Limits.StorageEphemeral().Cmp(resource.MustParse("2Gi"))).To(Equal(0))
		Expect(created.Spec.Containers[1].Resources.Requests.StorageEphemeral().Cmp(resource.MustParse("1536Mi"))).To(Equal(0))
		Expect(created.Spec.Containers[1].Resources.Limits.StorageEphemeral().Cmp(resource.MustParse("2Gi"))).To(Equal(0))
		Expect(created.Spec.InitContainers[0].Resources.Requests.StorageEphemeral().Cmp(resource.MustParse("1Gi"))).To(Equal(0))
		Expect(created.Spec.InitContainers[0].Resources.Limits.StorageEphemeral().Cmp(resource.MustParse("3Gi"))).To(Equal(0))
		Expect(created.Spec.InitContainers[1].Resources.Requests.StorageEphemeral().Cmp(resource.MustParse("1Gi"))).To(Equal(0))
		Expect(created.Spec.InitContainers[1].Resources.Limits.StorageEphemeral().Cmp(resource.MustParse("2Gi"))).To(Equal(0))
		Expect(created.Spec.Resources).To(BeNil())
	})

	It("applies Preserve, Remove, and Default only to an explicitly targeted init container", func() {
		ns := createNamespace("init-only")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		pod := newPod("init-only-resources", "2Gi")
		pod.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("250m")
		pod.Spec.InitContainers = []corev1.Container{{
			Name:            "init",
			Image:           "registry.k8s.io/pause:3.9",
			ImagePullPolicy: corev1.PullIfNotPresent,
			SecurityContext: restrictedContainerSecurityContext(),
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("100m"),
					corev1.ResourceMemory:           resource.MustParse("64Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
				},
				Limits: corev1.ResourceList{corev1.ResourceEphemeralStorage: resource.MustParse("3Gi")},
			},
		}}

		created := createPodAndExpectAllowed(cs, ns.Name, pod)

		containerResources := created.Spec.Containers[0].Resources
		Expect(containerResources.Requests.Cpu().Cmp(resource.MustParse("250m"))).To(Equal(0))
		Expect(containerResources.Limits.Cpu().Cmp(resource.MustParse("1"))).To(Equal(0))
		Expect(containerResources.Limits.Memory().Cmp(resource.MustParse("2Gi"))).To(Equal(0))

		initResources := created.Spec.InitContainers[0].Resources
		Expect(initResources.Requests).NotTo(HaveKey(corev1.ResourceCPU))
		Expect(initResources.Requests.Memory().Cmp(resource.MustParse("64Mi"))).To(Equal(0))
		Expect(initResources.Limits).NotTo(HaveKey(corev1.ResourceCPU))
		Expect(initResources.Limits.Memory().Cmp(resource.MustParse("256Mi"))).To(Equal(0))
		Expect(initResources.Requests.StorageEphemeral().Cmp(resource.MustParse("1Gi"))).To(Equal(0))
		Expect(initResources.Limits.StorageEphemeral().Cmp(resource.MustParse("3Gi"))).To(Equal(0))
	})

	It("accepts MatchRequest after Kubernetes defaults a missing request from its limit", func() {
		ns := createNamespace("managed")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		pod := newPod("match-request-missing", "1Gi")
		delete(pod.Spec.Containers[0].Resources.Requests, corev1.ResourceMemory)
		delete(pod.Spec.Containers[0].Resources.Limits, corev1.ResourceCPU)

		created := createPodAndExpectAllowed(cs, ns.Name, pod)
		Expect(created.Spec.Containers[0].Resources.Requests.Memory().Cmp(resource.MustParse("1Gi"))).To(Equal(0))
		Expect(created.Spec.Containers[0].Resources.Limits.Memory().Cmp(resource.MustParse("1Gi"))).To(Equal(0))
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

	It("preserves a compliant explicit Ratio limit and ignores untargeted locations", func() {
		ns := createNamespace("ratio")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		pod := newPod("ratio-target-isolation", "1280Mi")
		pod.Spec.InitContainers = []corev1.Container{{
			Name:            "init",
			Image:           "registry.k8s.io/pause:3.9",
			ImagePullPolicy: corev1.PullIfNotPresent,
			SecurityContext: restrictedContainerSecurityContext(),
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
				Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("2Gi")},
			},
		}}
		pod.Spec.Resources = &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
			Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("2Gi")},
		}

		created := createPodAndExpectAllowed(cs, ns.Name, pod)
		Expect(created.Spec.Containers[0].Resources.Limits.Memory().Cmp(resource.MustParse("1280Mi"))).To(Equal(0))
		Expect(created.Spec.InitContainers[0].Resources.Limits.Memory().Cmp(resource.MustParse("2Gi"))).To(Equal(0))
		Expect(created.Spec.Resources.Limits.Memory().Cmp(resource.MustParse("2Gi"))).To(Equal(0))
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

	It("denies Ratio when the targeted resource request is missing", func() {
		ns := createNamespace("ratio")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		pod := newPod("ratio-missing-request", "")
		delete(pod.Spec.Containers[0].Resources.Requests, corev1.ResourceMemory)

		createPodAndExpectDenied(
			cs,
			ns.Name,
			pod,
			"violates policy Ratio",
			"requires a request greater than zero",
		)
	})

	It("defaults and enforces Ratio at the explicit Pod target without affecting containers", func() {
		ns := createNamespace("pod-ratio")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		pod := newPod("pod-ratio-default", "")
		pod.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("100m")
		pod.Spec.Containers[0].Resources.Limits = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m")}
		pod.Spec.Resources = &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
		}

		created := createPodAndExpectAllowed(cs, ns.Name, pod)
		Expect(created.Spec.Resources.Limits.Cpu().Cmp(resource.MustParse("750m"))).To(Equal(0))
		Expect(created.Spec.Containers[0].Resources.Limits.Cpu().Cmp(resource.MustParse("200m"))).To(Equal(0))

		excessive := newPod("pod-ratio-denied", "")
		excessive.Spec.Resources = &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
			Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
		}
		createPodAndExpectDenied(
			cs,
			ns.Name,
			excessive,
			`spec.resources.limits["cpu"]`,
			"must not exceed 750m",
		)
	})

	It("uses Ratio as an allow-list policy", func() {
		ns := createNamespace("ratio-allow")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createPodAndExpectAllowed(cs, ns.Name, newPod("ratio-allow-compliant", "1280Mi"))
		createPodAndExpectDenied(
			cs,
			ns.Name,
			newPod("ratio-allow-denied", "2Gi"),
			"does not satisfy any allowed resource policy",
			`limits["memory"]`,
		)
	})

	It("audits an excessive Ratio without blocking admission", func() {
		ns := createNamespace("ratio-audit")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		pod := newPod("ratio-audited", "2Gi")

		created := createPodAndExpectAllowed(cs, ns.Name, pod)
		Expect(created.Spec.Containers[0].Resources.Limits.Memory().Cmp(resource.MustParse("2Gi"))).To(Equal(0))
		expectResourceAuditEvent(
			ns.Name,
			pod.Name,
			"workload resource limit",
			"violates policy Ratio",
			"must not exceed 1536Mi",
		)
	})

	It("uses a later Allow policy to override an earlier Ratio denial", func() {
		ns := createNamespace("ratio-override")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		pod := newPod("ratio-allow-override", "1792Mi")

		created := createPodAndExpectAllowed(cs, ns.Name, pod)
		Expect(created.Spec.Containers[0].Resources.Limits.Memory().Cmp(resource.MustParse("1792Mi"))).To(Equal(0))
	})

	It("uses a later Preserve policy to clear an earlier Ratio constraint", func() {
		ns := createNamespace("ratio-preserve")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		pod := newPod("ratio-preserve-override", "2Gi")

		created := createPodAndExpectAllowed(cs, ns.Name, pod)
		Expect(created.Spec.Containers[0].Resources.Limits.Memory().Cmp(resource.MustParse("2Gi"))).To(Equal(0))
	})

	It("applies ephemeral-storage Ratio to default container targets but not Pod-level resources", func() {
		ns := createNamespace("ephemeral-ratio")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		pod := newPod("ephemeral-ratio-default", "")
		pod.Spec.Containers[0].Resources.Requests[corev1.ResourceEphemeralStorage] = resource.MustParse("2Gi")
		pod.Spec.InitContainers = []corev1.Container{{
			Name:            "init",
			Image:           "registry.k8s.io/pause:3.9",
			ImagePullPolicy: corev1.PullIfNotPresent,
			SecurityContext: restrictedContainerSecurityContext(),
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceEphemeralStorage: resource.MustParse("1Gi")},
			},
		}}
		created := createPodAndExpectAllowed(cs, ns.Name, pod)
		Expect(created.Spec.Containers[0].Resources.Limits.StorageEphemeral().Cmp(resource.MustParse("3Gi"))).To(Equal(0))
		Expect(created.Spec.InitContainers[0].Resources.Limits.StorageEphemeral().Cmp(resource.MustParse("1536Mi"))).To(Equal(0))
		Expect(created.Spec.Resources).To(BeNil())

		excessive := newPod("ephemeral-ratio-denied", "")
		excessive.Spec.Containers[0].Resources.Requests[corev1.ResourceEphemeralStorage] = resource.MustParse("2Gi")
		excessive.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
			corev1.ResourceEphemeralStorage: resource.MustParse("4Gi"),
		}
		createPodAndExpectDenied(
			cs,
			ns.Name,
			excessive,
			`limits["ephemeral-storage"]`,
			"must not exceed 3Gi",
		)
	})
})

func e2eResourceQuantity(value string) *resource.Quantity {
	quantity := resource.MustParse(value)

	return &quantity
}

func resourceRatioRule(
	action rules.ActionType,
	target rules.WorkloadValidationTarget,
	ratio string,
) *rules.NamespaceRuleBodyNamespace {
	return &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
		Action: action,
		Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
			Targets: []rules.WorkloadValidationTarget{target},
			Resources: &rules.WorkloadResourceRules{
				Limits: map[corev1.ResourceName]rules.WorkloadResourceLimitPolicy{
					corev1.ResourceMemory: {
						Policy: rules.WorkloadResourceLimitPolicyRatio,
						Value:  e2eResourceQuantity(ratio),
					},
				},
			},
		},
	}}
}
