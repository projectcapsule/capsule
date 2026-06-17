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

var _ = Describe("enforcing pod QoS namespace rules", Ordered, Label("tenant", "rules", "enforce", "workloads", "qos"), func() {
	const ownerName = "e2e-rules-qos"

	var tnt *capsulev1beta2.Tenant

	newTenant := func() *capsulev1beta2.Tenant {
		return &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name: "e2e-rule-qos",
				Labels: map[string]string{
					"env": "e2e",
				},
			},
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{
					{
						CoreOwnerSpec: rbac.CoreOwnerSpec{
							UserSpec: rbac.UserSpec{
								Name: ownerName,
								Kind: "User",
							},
						},
					},
				},
				Rules: []*rules.NamespaceRuleBodyTenant{
					{
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
							Enforce: &rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeDeny,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									QoSClasses: []corev1.PodQOSClass{
										corev1.PodQOSBestEffort,
									},
								},
							},
						},
					},
					{
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
							Enforce: &rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeAudit,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									QoSClasses: []corev1.PodQOSClass{
										corev1.PodQOSBurstable,
									},
								},
							},
						},
					},
					{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"allow-best-effort": "true",
							},
						},
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
							Enforce: &rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeAllow,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									QoSClasses: []corev1.PodQOSClass{
										corev1.PodQOSBestEffort,
									},
								},
							},
						},
					},
					{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"deny-burstable": "true",
							},
						},
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
							Enforce: &rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeDeny,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									QoSClasses: []corev1.PodQOSClass{
										corev1.PodQOSBurstable,
									},
								},
							},
						},
					},
					{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"containers-target": "true",
							},
						},
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
							Enforce: &rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeDeny,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									Targets: []rules.WorkloadValidationTarget{
										rules.ValidateContainers,
									},
									QoSClasses: []corev1.PodQOSClass{
										corev1.PodQOSBestEffort,
									},
								},
							},
						},
					},
				},
			},
		}
	}

	type expectedQoSStatusRule struct {
		action     rules.ActionType
		targets    []rules.WorkloadValidationTarget
		qosClasses []corev1.PodQOSClass
	}

	expectNamespaceStatusRules := func(nsName string, want []expectedQoSStatusRule) {
		Eventually(func(g Gomega) {
			nsStatus := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(
				context.Background(),
				client.ObjectKey{Name: meta.NameForManagedRuleStatus(), Namespace: nsName},
				nsStatus,
			)).To(Succeed())

			g.Expect(nsStatus.Status.Rules).To(HaveLen(len(want)))

			for i, expected := range want {
				gotRule := nsStatus.Status.Rules[i]
				g.Expect(gotRule).NotTo(BeNil())
				g.Expect(gotRule.Enforce.Action).To(Equal(expected.action))
				g.Expect(gotRule.Enforce.Workloads.Targets).To(Equal(expected.targets))
				g.Expect(gotRule.Enforce.Workloads.QoSClasses).To(Equal(expected.qosClasses))
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	createPodAndExpectDenied := func(cs kubernetes.Interface, nsName string, pod *corev1.Pod, substrings ...string) {
		base := pod.DeepCopy()
		baseName := base.Name
		if baseName == "" {
			baseName = "pod"
		}

		Eventually(func() error {
			p := base.DeepCopy()
			p.Name = fmt.Sprintf("%s-%d", baseName, time.Now().UnixNano()%1e6)

			_, err := cs.CoreV1().Pods(nsName).Create(context.Background(), p, metav1.CreateOptions{})
			if err == nil {
				_ = cs.CoreV1().Pods(nsName).Delete(context.Background(), p.Name, metav1.DeleteOptions{})

				return fmt.Errorf("expected create to be denied, but it succeeded")
			}

			if apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("unexpected AlreadyExists: %v", err)
			}

			msg := err.Error()
			for _, substring := range substrings {
				if !strings.Contains(msg, substring) {
					return fmt.Errorf("expected error to contain %q, got: %s", substring, msg)
				}
			}

			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	createPodAndExpectAllowed := func(cs kubernetes.Interface, nsName string, pod *corev1.Pod) {
		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(nsName).Create(context.Background(), pod, metav1.CreateOptions{})

			return err
		}).Should(Succeed())
	}

	expectAuditEvent := func(cs kubernetes.Interface, nsName string, podName string, substrings ...string) {
		Eventually(func() error {
			events, err := cs.EventsV1().Events(nsName).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return err
			}

			for _, event := range events.Items {
				if event.Regarding.Name != podName {
					continue
				}

				if event.Reason != "NamespaceRuleAudit" {
					continue
				}

				msg := event.Note
				matched := true

				for _, substring := range substrings {
					if !strings.Contains(msg, substring) {
						matched = false

						break
					}
				}

				if matched {
					return nil
				}
			}

			return fmt.Errorf("expected audit event for pod %q containing %q", podName, substrings)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	bestEffortPod := func(name string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: corev1.PodSpec{
				SecurityContext: nobodyPodSecurityContext(),
				Containers: []corev1.Container{
					{
						Name:            "c",
						Image:           "registry.k8s.io/pause:3.9",
						ImagePullPolicy: corev1.PullIfNotPresent,
						SecurityContext: restrictedContainerSecurityContext(),
					},
				},
			},
		}
	}

	burstablePod := func(name string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: corev1.PodSpec{
				SecurityContext: nobodyPodSecurityContext(),
				Containers: []corev1.Container{
					{
						Name:            "c",
						Image:           "registry.k8s.io/pause:3.9",
						ImagePullPolicy: corev1.PullIfNotPresent,
						SecurityContext: restrictedContainerSecurityContext(),
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("10m"),
								corev1.ResourceMemory: resource.MustParse("16Mi"),
							},
						},
					},
				},
			},
		}
	}

	guaranteedPod := func(name string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: corev1.PodSpec{
				SecurityContext: nobodyPodSecurityContext(),
				Containers: []corev1.Container{
					{
						Name:            "c",
						Image:           "registry.k8s.io/pause:3.9",
						ImagePullPolicy: corev1.PullIfNotPresent,
						SecurityContext: restrictedContainerSecurityContext(),
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("10m"),
								corev1.ResourceMemory: resource.MustParse("16Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("10m"),
								corev1.ResourceMemory: resource.MustParse("16Mi"),
							},
						},
					},
				},
			},
		}
	}

	podWithInitContainerQoS := func(name string, initResources corev1.ResourceRequirements, containerResources corev1.ResourceRequirements) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: corev1.PodSpec{
				SecurityContext: nobodyPodSecurityContext(),
				InitContainers: []corev1.Container{
					{
						Name:            "init",
						Image:           "registry.k8s.io/pause:3.9",
						ImagePullPolicy: corev1.PullIfNotPresent,
						SecurityContext: restrictedContainerSecurityContext(),
						Resources:       initResources,
					},
				},
				Containers: []corev1.Container{
					{
						Name:            "c",
						Image:           "registry.k8s.io/pause:3.9",
						ImagePullPolicy: corev1.PullIfNotPresent,
						SecurityContext: restrictedContainerSecurityContext(),
						Resources:       containerResources,
					},
				},
			},
		}
	}

	burstableResources := func() corev1.ResourceRequirements {
		return corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("16Mi"),
			},
		}
	}

	guaranteedResources := func() corev1.ResourceRequirements {
		return corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("16Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("16Mi"),
			},
		}
	}

	JustBeforeEach(func() {
		tnt = newTenant()

		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())

		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
	})

	JustAfterEach(func() {
		EventuallyDeletion(tnt)
	})

	It("stores QoS workload rules as independent status rule blocks", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		expectNamespaceStatusRules(ns.GetName(), []expectedQoSStatusRule{
			{
				action: rules.ActionTypeDeny,
				qosClasses: []corev1.PodQOSClass{
					corev1.PodQOSBestEffort,
				},
			},
			{
				action: rules.ActionTypeAudit,
				qosClasses: []corev1.PodQOSClass{
					corev1.PodQOSBurstable,
				},
			},
		})
	})

	It("stores namespace-selector matched QoS rules as additional status rule blocks", func() {
		ns := NewNamespace("", map[string]string{
			"allow-best-effort": "true",
			meta.TenantLabel:    tnt.GetName(),
		})

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		expectNamespaceStatusRules(ns.GetName(), []expectedQoSStatusRule{
			{
				action: rules.ActionTypeDeny,
				qosClasses: []corev1.PodQOSClass{
					corev1.PodQOSBestEffort,
				},
			},
			{
				action: rules.ActionTypeAudit,
				qosClasses: []corev1.PodQOSClass{
					corev1.PodQOSBurstable,
				},
			},
			{
				action: rules.ActionTypeAllow,
				qosClasses: []corev1.PodQOSClass{
					corev1.PodQOSBestEffort,
				},
			},
		})
	})

	It("denies BestEffort pods by default", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		createPodAndExpectDenied(cs, ns.Name, bestEffortPod("besteffort-denied"),
			"BestEffort",
			"denied",
		)
	})

	It("allows Guaranteed pods when no QoS rule matches them", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		createPodAndExpectAllowed(cs, ns.Name, guaranteedPod("guaranteed-allowed"))
	})

	It("audits Burstable pods by allowing admission and emitting an event", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		pod := burstablePod("burstable-audited")

		createPodAndExpectAllowed(cs, ns.Name, pod)

		expectAuditEvent(cs, ns.Name, pod.Name,
			`QoS class "Burstable"`,
			"status.qosClass",
			"matched audit namespace rule",
		)
	})

	It("allows BestEffort pods when a later namespace-selected allow rule matches", func() {
		ns := NewNamespace("", map[string]string{
			"allow-best-effort": "true",
			meta.TenantLabel:    tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		createPodAndExpectAllowed(cs, ns.Name, bestEffortPod("besteffort-allowed"))
	})

	It("denies Burstable pods when a later namespace-selected deny rule overrides an earlier audit rule", func() {
		ns := NewNamespace("", map[string]string{
			"deny-burstable": "true",
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		createPodAndExpectDenied(cs, ns.Name, burstablePod("burstable-denied"),
			"Burstable",
			"denied",
		)
	})

	It("computes QoS across init containers and regular containers", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		pod := podWithInitContainerQoS(
			"init-causes-burstable",
			burstableResources(),
			guaranteedResources(),
		)

		createPodAndExpectAllowed(cs, ns.Name, pod)

		expectAuditEvent(cs, ns.Name, pod.Name,
			`QoS class "Burstable"`,
			"status.qosClass",
			"matched audit namespace rule",
		)
	})

	It("uses empty targets as all targets for QoS rules", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		createPodAndExpectDenied(cs, ns.Name, bestEffortPod("empty-targets-denied"),
			"BestEffort",
			"denied",
		)
	})

	It("applies QoS rules when explicit workload targets are configured", func() {
		ns := NewNamespace("", map[string]string{
			"containers-target": "true",
			meta.TenantLabel:    tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		expectNamespaceStatusRules(ns.GetName(), []expectedQoSStatusRule{
			{
				action: rules.ActionTypeDeny,
				qosClasses: []corev1.PodQOSClass{
					corev1.PodQOSBestEffort,
				},
			},
			{
				action: rules.ActionTypeAudit,
				qosClasses: []corev1.PodQOSClass{
					corev1.PodQOSBurstable,
				},
			},
			{
				action: rules.ActionTypeDeny,
				targets: []rules.WorkloadValidationTarget{
					rules.ValidateContainers,
				},
				qosClasses: []corev1.PodQOSClass{
					corev1.PodQOSBestEffort,
				},
			},
		})

		createPodAndExpectDenied(cs, ns.Name, bestEffortPod("explicit-target-denied"),
			"BestEffort",
			"denied",
		)
	})
})
