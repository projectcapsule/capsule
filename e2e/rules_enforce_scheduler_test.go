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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

var _ = Describe("enforcing pod schedulerName namespace rules", Ordered, Label("tenant", "rules", "enforce", "workloads", "scheduler"), func() {
	const ownerName = "e2e-rules-scheduler"

	var tnt *capsulev1beta2.Tenant

	newTenant := func() *capsulev1beta2.Tenant {
		return &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name: "e2e-rule-scheduler",
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
									Schedulers: []api.ExpressionMatch{
										{
											Exact: []string{
												"forbidden-scheduler",
												"legacy-scheduler",
											},
										},
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
									Schedulers: []api.ExpressionMatch{
										{
											Exact: []string{
												"audited-scheduler",
											},
										},
									},
								},
							},
						},
					},
					{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"allow-forbidden-scheduler": "true",
							},
						},
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
							Enforce: &rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeAllow,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									Schedulers: []api.ExpressionMatch{
										{
											Exact: []string{
												"forbidden-scheduler",
											},
										},
									},
								},
							},
						},
					},
					{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"deny-audited-scheduler": "true",
							},
						},
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
							Enforce: &rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeDeny,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									Schedulers: []api.ExpressionMatch{
										{
											Exact: []string{
												"audited-scheduler",
											},
										},
									},
								},
							},
						},
					},
					{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"allow-team-scheduler": "true",
							},
						},
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
							Enforce: &rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeAllow,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									Schedulers: []api.ExpressionMatch{
										{
											ExpressionRegex: api.ExpressionRegex{
												Expression: "^team-[a-z0-9-]+$",
											},
										},
									},
								},
							},
						},
					},
					{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"deny-team-scheduler": "true",
							},
						},
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
							Enforce: &rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeDeny,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									Schedulers: []api.ExpressionMatch{
										{
											ExpressionRegex: api.ExpressionRegex{
												Expression: "^team-blocked-[a-z0-9-]+$",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}
	}

	type expectedSchedulerStatusRule struct {
		action     rules.ActionType
		targets    []rules.WorkloadValidationTarget
		schedulers []api.ExpressionMatch
	}

	expectNamespaceStatusRules := func(nsName string, want []expectedSchedulerStatusRule) {
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
				g.Expect(gotRule.Enforce.Workloads.Schedulers).To(Equal(expected.schedulers))
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

	podWithScheduler := func(name string, schedulerName string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: corev1.PodSpec{
				SchedulerName:   schedulerName,
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

	It("stores scheduler workload rules as independent status rule blocks", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		expectNamespaceStatusRules(ns.GetName(), []expectedSchedulerStatusRule{
			{
				action: rules.ActionTypeDeny,
				schedulers: []api.ExpressionMatch{
					{
						Exact: []string{
							"forbidden-scheduler",
							"legacy-scheduler",
						},
					},
				},
			},
			{
				action: rules.ActionTypeAudit,
				schedulers: []api.ExpressionMatch{
					{
						Exact: []string{
							"audited-scheduler",
						},
					},
				},
			},
		})
	})

	It("stores namespace-selector matched scheduler rules as additional status rule blocks", func() {
		ns := NewNamespace("", map[string]string{
			"allow-forbidden-scheduler": "true",
			meta.TenantLabel:            tnt.GetName(),
		})

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		expectNamespaceStatusRules(ns.GetName(), []expectedSchedulerStatusRule{
			{
				action: rules.ActionTypeDeny,
				schedulers: []api.ExpressionMatch{
					{
						Exact: []string{
							"forbidden-scheduler",
							"legacy-scheduler",
						},
					},
				},
			},
			{
				action: rules.ActionTypeAudit,
				schedulers: []api.ExpressionMatch{
					{
						Exact: []string{
							"audited-scheduler",
						},
					},
				},
			},
			{
				action: rules.ActionTypeAllow,
				schedulers: []api.ExpressionMatch{
					{
						Exact: []string{
							"forbidden-scheduler",
						},
					},
				},
			},
		})
	})

	It("ignores pods without an explicit schedulerName", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		createPodAndExpectAllowed(cs, ns.Name, podWithScheduler("empty-scheduler-ignored", ""))
	})

	It("denies pods using an exact denied schedulerName", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		createPodAndExpectDenied(cs, ns.Name, podWithScheduler("forbidden-denied", "forbidden-scheduler"),
			"forbidden-scheduler",
			"spec.schedulerName",
			"denied",
		)
	})

	It("denies pods using another schedulerName from the same exact list", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		createPodAndExpectDenied(cs, ns.Name, podWithScheduler("legacy-denied", "legacy-scheduler"),
			"legacy-scheduler",
			"spec.schedulerName",
			"denied",
		)
	})

	It("allows pods when no scheduler rule matches them", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		createPodAndExpectAllowed(cs, ns.Name, podWithScheduler("unmatched-allowed", "neutral-scheduler"))
	})

	It("audits matching schedulerName rules by allowing admission and emitting an event", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		pod := podWithScheduler("scheduler-audited", "audited-scheduler")

		createPodAndExpectAllowed(cs, ns.Name, pod)

		expectAuditEvent(cs, ns.Name, pod.Name,
			`scheduler "audited-scheduler"`,
			"spec.schedulerName",
			"matched audit namespace rule",
		)
	})

	It("allows a previously denied schedulerName when a later namespace-selected allow rule matches", func() {
		ns := NewNamespace("", map[string]string{
			"allow-forbidden-scheduler": "true",
			meta.TenantLabel:            tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		createPodAndExpectAllowed(cs, ns.Name, podWithScheduler("forbidden-allowed", "forbidden-scheduler"))
	})

	It("denies an audited schedulerName when a later namespace-selected deny rule matches", func() {
		ns := NewNamespace("", map[string]string{
			"deny-audited-scheduler": "true",
			meta.TenantLabel:         tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		createPodAndExpectDenied(cs, ns.Name, podWithScheduler("audited-denied", "audited-scheduler"),
			"audited-scheduler",
			"spec.schedulerName",
			"denied",
		)
	})

	It("allows schedulerName values matching a namespace-selected regex allow rule", func() {
		ns := NewNamespace("", map[string]string{
			"allow-team-scheduler": "true",
			meta.TenantLabel:       tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		createPodAndExpectAllowed(cs, ns.Name, podWithScheduler("team-scheduler-allowed", "team-solar"))
	})

	It("denies schedulerName values that do not match a namespace-selected regex allow rule", func() {
		ns := NewNamespace("", map[string]string{
			"allow-team-scheduler": "true",
			meta.TenantLabel:       tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		createPodAndExpectDenied(cs, ns.Name, podWithScheduler("team-scheduler-denied", "team_Solar"),
			"team_Solar",
			"spec.schedulerName",
			"not allowed",
		)
	})

	It("denies schedulerName values matching a namespace-selected regex deny rule", func() {
		ns := NewNamespace("", map[string]string{
			"deny-team-scheduler": "true",
			meta.TenantLabel:      tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		createPodAndExpectDenied(cs, ns.Name, podWithScheduler("team-blocked-denied", "team-blocked-solar"),
			"team-blocked-solar",
			"spec.schedulerName",
			"denied",
		)
	})
})
