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

var (
	targetContainers = []rules.WorkloadValidationTarget{
		"pod/containers",
	}
	targetEphemeralContainers = []rules.WorkloadValidationTarget{
		"pod/ephemeralcontainers",
	}
	targetInitContainers = []rules.WorkloadValidationTarget{
		"pod/initcontainers",
	}
	targetVolumes = []rules.WorkloadValidationTarget{
		"pod/volumes",
	}
)

var _ = Describe("enforcing container registry namespace rules", Ordered, Label("tenant", "rules", "images", "registry"), func() {
	const ownerName = "e2e-rules-registry"

	var tnt *capsulev1beta2.Tenant

	registryByExpression := func(expression string) rules.OCIRegistry {
		return rules.OCIRegistry{
			ExpressionMatch: api.ExpressionMatch{
				ExpressionRegex: api.ExpressionRegex{
					Expression: expression,
				},
			},
		}
	}

	registryByNegatedExpression := func(expression string) rules.OCIRegistry {
		return rules.OCIRegistry{
			ExpressionMatch: api.ExpressionMatch{
				ExpressionRegex: api.ExpressionRegex{
					Expression: expression,
					Negate:     true,
				},
			},
		}
	}

	registryByExact := func(exact ...string) rules.OCIRegistry {
		return rules.OCIRegistry{
			ExpressionMatch: api.ExpressionMatch{
				Exact: exact,
			},
		}
	}

	registryByMatch := func(exact []string, expression string) rules.OCIRegistry {
		return rules.OCIRegistry{
			ExpressionMatch: api.ExpressionMatch{
				ExpressionRegex: api.ExpressionRegex{
					Expression: expression,
				},
				Exact: exact,
			},
		}
	}

	newTenant := func() *capsulev1beta2.Tenant {
		return &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name: "e2e-rule-registry",
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
								Action: rules.ActionTypeAllow,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									Registries: []rules.OCIRegistry{
										registryByExpression("harbor/.*"),
									},
								},
							},
						},
					},
					{
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
							Enforce: &rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeDeny,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									Targets: targetContainers,
									Registries: []rules.OCIRegistry{
										registryByExpression("harbor/customer/containers/.*"),
									},
								},
							},
						},
					},
					{
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
							Enforce: &rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeDeny,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									Targets: targetInitContainers,
									Registries: []rules.OCIRegistry{
										registryByExpression("harbor/customer/init/.*"),
									},
								},
							},
						},
					},
					{
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
							Enforce: &rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeDeny,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									Targets: targetEphemeralContainers,
									Registries: []rules.OCIRegistry{
										registryByExpression("harbor/customer/debug/.*"),
									},
								},
							},
						},
					},
					{
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
							Enforce: &rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeDeny,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									Targets: targetVolumes,
									Registries: []rules.OCIRegistry{
										registryByExpression("harbor/customer/volume/.*"),
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
									Targets: targetContainers,
									Registries: []rules.OCIRegistry{
										registryByExpression("audit/containers/.*"),
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
									Targets: targetVolumes,
									Registries: []rules.OCIRegistry{
										registryByExpression("audit/volumes/.*"),
									},
								},
							},
						},
					},
					{
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
							Enforce: &rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeAllow,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									Targets: targetContainers,
									Registries: []rules.OCIRegistry{
										registryByExact(
											"exact/containers/app:1",
											"exact/containers/app:2",
										),
									},
								},
							},
						},
					},
					{
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
							Enforce: &rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeDeny,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									Targets: targetContainers,
									Registries: []rules.OCIRegistry{
										registryByExact("exact/containers/blocked:1"),
									},
								},
							},
						},
					},
					{
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
							Enforce: &rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeAllow,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									Targets: targetContainers,
									Registries: []rules.OCIRegistry{
										registryByMatch(
											[]string{"combined/exact/app:1"},
											"combined/regex/.*",
										),
									},
								},
							},
						},
					},
					{
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
							Enforce: &rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeAllow,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									Targets: targetContainers,
									Registries: []rules.OCIRegistry{
										{
											ExpressionMatch: api.ExpressionMatch{
												ExpressionRegex: api.ExpressionRegex{
													Expression: "policy/.*",
												},
											},
											Policy: []corev1.PullPolicy{
												corev1.PullNever,
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
								"environment": "prod",
							},
						},
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
							Enforce: &rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeAllow,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									Targets: targetContainers,
									Registries: []rules.OCIRegistry{
										registryByExpression("harbor/customer/containers/prod/.*"),
									},
								},
							},
						},
					},
					{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"negate": "true",
							},
						},
						NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
							Enforce: &rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeDeny,
								Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
									Targets: targetContainers,
									Registries: []rules.OCIRegistry{
										registryByNegatedExpression("trusted/.*"),
									},
								},
							},
						},
					},
				},
			},
		}
	}

	type expectedStatusRule struct {
		action      rules.ActionType
		targets     []rules.WorkloadValidationTarget
		expressions []string
		exact       [][]string
		negated     []bool
	}

	expectNamespaceStatusRules := func(nsName string, want []expectedStatusRule) {
		Eventually(func(g Gomega) {
			nsStatus := &capsulev1beta2.RuleStatus{}

			g.Expect(k8sClient.Get(
				context.Background(),
				client.ObjectKey{
					Name:      meta.NameForManagedRuleStatus(),
					Namespace: nsName,
				},
				nsStatus,
			)).To(Succeed())

			g.Expect(nsStatus.Status.Rules).To(HaveLen(len(want)))

			for i, expected := range want {
				got := nsStatus.Status.Rules[i]

				g.Expect(got).NotTo(BeNil())
				g.Expect(got.Enforce).NotTo(BeNil())
				g.Expect(got.Enforce.Action).To(Equal(expected.action))

				if len(expected.targets) == 0 {
					g.Expect(got.Enforce.Workloads.Targets).To(BeEmpty())
				} else {
					g.Expect(got.Enforce.Workloads.Targets).To(Equal(expected.targets))
				}

				wantRegistries := len(expected.expressions)
				if len(expected.exact) > wantRegistries {
					wantRegistries = len(expected.exact)
				}

				g.Expect(got.Enforce.Workloads.Registries).To(HaveLen(wantRegistries))

				for j := 0; j < wantRegistries; j++ {
					match := got.Enforce.Workloads.Registries[j].ExpressionMatch

					if len(expected.expressions) > j {
						g.Expect(match.Expression).To(Equal(expected.expressions[j]))
					} else {
						g.Expect(match.Expression).To(BeEmpty())
					}

					if len(expected.exact) > j {
						g.Expect(match.Exact).To(Equal(expected.exact[j]))
					} else {
						g.Expect(match.Exact).To(BeEmpty())
					}

					if len(expected.negated) > j {
						g.Expect(match.Negate).To(Equal(expected.negated[j]))
					} else {
						g.Expect(match.Negate).To(BeFalse())
					}
				}
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	baseStatusRules := func() []expectedStatusRule {
		return []expectedStatusRule{
			{
				action:      rules.ActionTypeAllow,
				expressions: []string{"harbor/.*"},
			},
			{
				action:      rules.ActionTypeDeny,
				targets:     targetContainers,
				expressions: []string{"harbor/customer/containers/.*"},
			},
			{
				action:      rules.ActionTypeDeny,
				targets:     targetInitContainers,
				expressions: []string{"harbor/customer/init/.*"},
			},
			{
				action:      rules.ActionTypeDeny,
				targets:     targetEphemeralContainers,
				expressions: []string{"harbor/customer/debug/.*"},
			},
			{
				action:      rules.ActionTypeDeny,
				targets:     targetVolumes,
				expressions: []string{"harbor/customer/volume/.*"},
			},
			{
				action:      rules.ActionTypeAudit,
				targets:     targetContainers,
				expressions: []string{"audit/containers/.*"},
			},
			{
				action:      rules.ActionTypeAudit,
				targets:     targetVolumes,
				expressions: []string{"audit/volumes/.*"},
			},
			{
				action:  rules.ActionTypeAllow,
				targets: targetContainers,
				exact: [][]string{
					{
						"exact/containers/app:1",
						"exact/containers/app:2",
					},
				},
			},
			{
				action:  rules.ActionTypeDeny,
				targets: targetContainers,
				exact: [][]string{
					{
						"exact/containers/blocked:1",
					},
				},
			},
			{
				action:      rules.ActionTypeAllow,
				targets:     targetContainers,
				expressions: []string{"combined/regex/.*"},
				exact: [][]string{
					{
						"combined/exact/app:1",
					},
				},
			},
			{
				action:      rules.ActionTypeAllow,
				targets:     targetContainers,
				expressions: []string{"policy/.*"},
			},
		}
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

	updatePodAndExpectDenied := func(cs kubernetes.Interface, nsName string, podName string, mutate func(*corev1.Pod), substrings ...string) {
		Eventually(func() error {
			pod, err := cs.CoreV1().Pods(nsName).Get(context.Background(), podName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			mutate(pod)

			_, err = cs.CoreV1().Pods(nsName).Update(context.Background(), pod, metav1.UpdateOptions{})
			if err == nil {
				return fmt.Errorf("expected update to be denied, but it succeeded")
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

	restrictedPod := func(name string, image string, pullPolicy corev1.PullPolicy) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: corev1.PodSpec{
				SecurityContext: nobodyPodSecurityContext(),
				Containers: []corev1.Container{
					{
						Name:            "c",
						Image:           image,
						ImagePullPolicy: pullPolicy,
						SecurityContext: restrictedContainerSecurityContext(),
					},
				},
			},
		}
	}

	expectAuditEvent := func(cs kubernetes.Interface, nsName string, podName string, substrings ...string) {
		Eventually(func() error {
			events, err := cs.CoreV1().Events(nsName).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return err
			}

			for _, event := range events.Items {
				if event.InvolvedObject.Name != podName {
					continue
				}

				msg := event.Message
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

	It("stores matching tenant rules as independent status rule blocks", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		expectNamespaceStatusRules(ns.GetName(), baseStatusRules())
	})

	It("stores namespace-selector matched rules as additional independent status rule blocks", func() {
		ns := NewNamespace("", map[string]string{
			"environment":    "prod",
			meta.TenantLabel: tnt.GetName(),
		})

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		want := baseStatusRules()
		want = append(want, expectedStatusRule{
			action:      rules.ActionTypeAllow,
			targets:     targetContainers,
			expressions: []string{"harbor/customer/containers/prod/.*"},
		})

		expectNamespaceStatusRules(ns.GetName(), want)
	})

	It("stores namespace-selector matched negated regex rules as independent status rule blocks", func() {
		ns := NewNamespace("", map[string]string{
			"negate":         "true",
			meta.TenantLabel: tnt.GetName(),
		})

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		want := baseStatusRules()
		want = append(want, expectedStatusRule{
			action:      rules.ActionTypeDeny,
			targets:     targetContainers,
			expressions: []string{"trusted/.*"},
			negated:     []bool{true},
		})

		expectNamespaceStatusRules(ns.GetName(), want)
	})

	It("allows a broad matching allow rule", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		pod := restrictedPod("harbor-allowed", "harbor/platform/app:1", corev1.PullIfNotPresent)

		createPodAndExpectAllowed(cs, ns.Name, pod)
	})

	It("allows an exact array match", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		pod := restrictedPod("exact-array-allowed", "exact/containers/app:2", corev1.PullIfNotPresent)

		createPodAndExpectAllowed(cs, ns.Name, pod)
	})

	It("denies a later exact array deny rule", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		pod := restrictedPod("exact-array-denied", "exact/containers/blocked:1", corev1.PullIfNotPresent)

		createPodAndExpectDenied(cs, ns.Name, pod,
			"containers[0]",
			"exact/containers/blocked:1",
			"denied",
		)
	})

	It("allows a combined exact and regex matcher through the exact branch", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		pod := restrictedPod("combined-exact-allowed", "combined/exact/app:1", corev1.PullIfNotPresent)

		createPodAndExpectAllowed(cs, ns.Name, pod)
	})

	It("allows a combined exact and regex matcher through the regex branch", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		pod := restrictedPod("combined-regex-allowed", "combined/regex/team/app:1", corev1.PullIfNotPresent)

		createPodAndExpectAllowed(cs, ns.Name, pod)
	})

	It("denies a later more specific deny rule even when an earlier broad allow rule matched", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		pod := restrictedPod("customer-denied", "harbor/customer/containers/app:1", corev1.PullIfNotPresent)

		createPodAndExpectDenied(cs, ns.Name, pod,
			"containers[0]",
			"harbor/customer/containers/app:1",
			"denied",
			"harbor/customer/containers/.*",
		)
	})

	It("denies an update when the new image matches a later specific deny rule", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		pod := restrictedPod("update-to-denied", "harbor/platform/app:1", corev1.PullIfNotPresent)

		createPodAndExpectAllowed(cs, ns.Name, pod)

		updatePodAndExpectDenied(cs, ns.Name, pod.Name, func(pod *corev1.Pod) {
			pod.Spec.Containers[0].Image = "harbor/customer/containers/app:1"
		},
			"containers[0]",
			"harbor/customer/containers/app:1",
			"denied",
			"harbor/customer/containers/.*",
		)
	})

	It("allows a later more specific allow rule to override an earlier deny rule in a selected namespace", func() {
		ns := NewNamespace("", map[string]string{
			"environment":    "prod",
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		denied := restrictedPod("prod-customer-denied", "harbor/customer/containers/other/app:1", corev1.PullIfNotPresent)
		createPodAndExpectDenied(cs, ns.Name, denied,
			"containers[0]",
			"harbor/customer/containers/other/app:1",
			"denied",
			"harbor/customer/containers/.*",
		)

		allowed := restrictedPod("prod-customer-allowed", "harbor/customer/containers/prod/app:1", corev1.PullIfNotPresent)
		createPodAndExpectAllowed(cs, ns.Name, allowed)
	})

	It("audits a matching image by allowing admission and emitting an event", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		pod := restrictedPod("audit-allowed", "audit/containers/team/app:1", corev1.PullIfNotPresent)

		createPodAndExpectAllowed(cs, ns.Name, pod)

		expectAuditEvent(cs, ns.Name, pod.Name,
			"matched audit registry rule",
			"audit/containers/.*",
		)
	})

	It("evaluates init containers with the same multi-rule action semantics", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "init-denied",
			},
			Spec: corev1.PodSpec{
				SecurityContext: nobodyPodSecurityContext(),
				InitContainers: []corev1.Container{
					{
						Name:            "init",
						Image:           "harbor/customer/init/app:1",
						ImagePullPolicy: corev1.PullIfNotPresent,
						SecurityContext: restrictedContainerSecurityContext(),
					},
				},
				Containers: []corev1.Container{
					{
						Name:            "c",
						Image:           "harbor/platform/app:1",
						ImagePullPolicy: corev1.PullIfNotPresent,
						SecurityContext: restrictedContainerSecurityContext(),
					},
				},
			},
		}

		createPodAndExpectDenied(cs, ns.Name, pod,
			"initContainers[0]",
			"harbor/customer/init/app:1",
			"denied",
			"harbor/customer/init/.*",
		)
	})

	It("evaluates image volumes with the same multi-rule action semantics", Label("skip-on-openshift"), func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "volume-denied",
			},
			Spec: corev1.PodSpec{
				SecurityContext: nobodyPodSecurityContext(),
				Containers: []corev1.Container{
					{
						Name:            "c",
						Image:           "harbor/platform/app:1",
						ImagePullPolicy: corev1.PullIfNotPresent,
						SecurityContext: restrictedContainerSecurityContext(),
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "imgvol",
						VolumeSource: corev1.VolumeSource{
							Image: &corev1.ImageVolumeSource{
								Reference:  "harbor/customer/volume/app:1",
								PullPolicy: corev1.PullIfNotPresent,
							},
						},
					},
				},
			},
		}

		createPodAndExpectDenied(cs, ns.Name, pod,
			"volumes[0](imgvol)",
			"harbor/customer/volume/app:1",
			"denied",
			"harbor/customer/volume/.*",
		)
	})

	It("audits image volumes independently from container decisions", Label("skip-on-openshift"), func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "volume-audit-allowed",
			},
			Spec: corev1.PodSpec{
				SecurityContext: nobodyPodSecurityContext(),
				Containers: []corev1.Container{
					{
						Name:            "c",
						Image:           "harbor/platform/app:1",
						ImagePullPolicy: corev1.PullIfNotPresent,
						SecurityContext: restrictedContainerSecurityContext(),
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "imgvol",
						VolumeSource: corev1.VolumeSource{
							Image: &corev1.ImageVolumeSource{
								Reference:  "audit/volumes/team/app:1",
								PullPolicy: corev1.PullIfNotPresent,
							},
						},
					},
				},
			},
		}

		createPodAndExpectAllowed(cs, ns.Name, pod)

		expectAuditEvent(cs, ns.Name, pod.Name,
			"matched audit registry rule",
			"audit/volumes/.*",
		)
	})

	It("denies adding an ephemeral container when it matches the later specific deny rule", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		cleanupRBAC := GrantEphemeralContainersUpdate(ns.Name, tnt.Spec.Owners[0].UserSpec.Name)
		defer cleanupRBAC()

		pod := restrictedPod("base", "harbor/platform/app:1", corev1.PullIfNotPresent)
		createPodAndExpectAllowed(cs, ns.Name, pod)

		ephemeral := corev1.EphemeralContainer{
			EphemeralContainerCommon: corev1.EphemeralContainerCommon{
				Name:            "debug",
				Image:           "harbor/customer/debug/app:1",
				ImagePullPolicy: corev1.PullIfNotPresent,
				SecurityContext: restrictedContainerSecurityContext(),
			},
		}

		Eventually(func() error {
			current, err := cs.CoreV1().Pods(ns.Name).Get(context.Background(), pod.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			current.Spec.EphemeralContainers = []corev1.EphemeralContainer{ephemeral}

			_, err = cs.CoreV1().Pods(ns.Name).UpdateEphemeralContainers(
				context.Background(),
				current.Name,
				current,
				metav1.UpdateOptions{},
			)
			if err == nil {
				return fmt.Errorf("expected ephemeral container update to be denied, but it succeeded")
			}

			msg := err.Error()
			for _, substring := range []string{
				"ephemeralContainers[0]",
				"harbor/customer/debug/app:1",
				"denied",
				"harbor/customer/debug/.*",
			} {
				if !strings.Contains(msg, substring) {
					return fmt.Errorf("expected error to contain %q, got: %s", substring, msg)
				}
			}

			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("allows an allowed registry reference when its pull policy is permitted", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		pod := restrictedPod("policy-allowed", "policy/team/app:1", corev1.PullNever)

		createPodAndExpectAllowed(cs, ns.Name, pod)
	})

	It("denies an allowed registry reference when its pull policy is not permitted", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		pod := restrictedPod("policy-denied", "policy/team/app:1", corev1.PullIfNotPresent)

		createPodAndExpectDenied(cs, ns.Name, pod,
			"containers[0]",
			"policy/team/app:1",
			"pullPolicy=IfNotPresent",
			"allowed: Never",
		)
	})

	It("applies namespace-selector matched negated regex rules after the base rules", func() {
		ns := NewNamespace("", map[string]string{
			"negate":         "true",
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		denied := restrictedPod("negated-denied", "harbor/platform/app:1", corev1.PullIfNotPresent)
		createPodAndExpectDenied(cs, ns.Name, denied,
			"containers[0]",
			"harbor/platform/app:1",
			"denied",
			"trusted/.*",
		)

		allowed := restrictedPod("negated-allowed", "trusted/platform/app:1", corev1.PullIfNotPresent)
		createPodAndExpectAllowed(cs, ns.Name, allowed)
	})
})
