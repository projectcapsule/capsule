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

var _ = Describe("enforcing container registry namespace rules", Ordered, Label("tenant", "rules", "images", "registry"), func() {
	const ownerName = "e2e-rules-registry"

	var tnt *capsulev1beta2.Tenant

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
						NamespaceRuleBodyNamespace: rules.NamespaceRuleBodyNamespace{
							Enforce: rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeAllow,
								Registries: []rules.OCIRegistry{
									{
										Registry: "harbor/.*",
										Validation: []rules.RegistryValidationTarget{
											rules.ValidateImages,
											rules.ValidateVolumes,
										},
									},
								},
							},
						},
					},
					{
						NamespaceRuleBodyNamespace: rules.NamespaceRuleBodyNamespace{
							Enforce: rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeDeny,
								Registries: []rules.OCIRegistry{
									{
										Registry: "harbor/customer/.*",
										Policy: []corev1.PullPolicy{
											corev1.PullNever,
										},
										Validation: []rules.RegistryValidationTarget{
											rules.ValidateImages,
											rules.ValidateVolumes,
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
						NamespaceRuleBodyNamespace: rules.NamespaceRuleBodyNamespace{
							Enforce: rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeAllow,
								Registries: []rules.OCIRegistry{
									{
										Registry: "harbor/customer/prod-image/.*",
										Validation: []rules.RegistryValidationTarget{
											rules.ValidateImages,
											rules.ValidateVolumes,
										},
									},
								},
							},
						},
					},
					{
						NamespaceRuleBodyNamespace: rules.NamespaceRuleBodyNamespace{
							Enforce: rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeAudit,
								Registries: []rules.OCIRegistry{
									{
										Registry: "audit/.*",
										Validation: []rules.RegistryValidationTarget{
											rules.ValidateImages,
											rules.ValidateVolumes,
										},
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
						NamespaceRuleBodyNamespace: rules.NamespaceRuleBodyNamespace{
							Enforce: rules.NamespaceRuleEnforceBody{
								Action: rules.ActionTypeDeny,
								Registries: []rules.OCIRegistry{
									{
										RegExpression: api.RegExpression{
											Expression: "trusted/.*",
											Negate:     true,
										},
										Validation: []rules.RegistryValidationTarget{
											rules.ValidateImages,
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

	type expectedStatusRule struct {
		action      rules.ActionType
		expressions []string
		negated     []bool
	}

	expectNamespaceStatusRules := func(nsName string, want []expectedStatusRule) {
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
				g.Expect(gotRule.Enforce.Registries).To(HaveLen(len(expected.expressions)))

				for j, expectedExpression := range expected.expressions {
					expr := gotRule.Enforce.Registries[j].Expression()
					g.Expect(expr.Expression).To(Equal(expectedExpression))

					if len(expected.negated) > j {
						g.Expect(expr.Negate).To(Equal(expected.negated[j]))
					}
				}
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

		expectNamespaceStatusRules(ns.GetName(), []expectedStatusRule{
			{
				action:      rules.ActionTypeAllow,
				expressions: []string{"harbor/.*"},
			},
			{
				action:      rules.ActionTypeDeny,
				expressions: []string{"harbor/customer/.*"},
			},
			{
				action:      rules.ActionTypeAudit,
				expressions: []string{"audit/.*"},
			},
		})
	})

	It("stores namespace-selector matched rules as additional independent status rule blocks", func() {
		ns := NewNamespace("", map[string]string{
			"environment":    "prod",
			meta.TenantLabel: tnt.GetName(),
		})

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		expectNamespaceStatusRules(ns.GetName(), []expectedStatusRule{
			{
				action:      rules.ActionTypeAllow,
				expressions: []string{"harbor/.*"},
			},
			{
				action:      rules.ActionTypeDeny,
				expressions: []string{"harbor/customer/.*"},
			},
			{
				action:      rules.ActionTypeAllow,
				expressions: []string{"harbor/customer/prod-image/.*"},
			},
			{
				action:      rules.ActionTypeAudit,
				expressions: []string{"audit/.*"},
			},
		})
	})

	It("stores namespace-selector matched negated regex rules as independent status rule blocks", func() {
		ns := NewNamespace("", map[string]string{
			"negate":         "true",
			meta.TenantLabel: tnt.GetName(),
		})

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		expectNamespaceStatusRules(ns.GetName(), []expectedStatusRule{
			{
				action:      rules.ActionTypeAllow,
				expressions: []string{"harbor/.*"},
			},
			{
				action:      rules.ActionTypeDeny,
				expressions: []string{"harbor/customer/.*"},
			},
			{
				action:      rules.ActionTypeAudit,
				expressions: []string{"audit/.*"},
			},
			{
				action:      rules.ActionTypeDeny,
				expressions: []string{"trusted/.*"},
				negated:     []bool{true},
			},
		})
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

	It("denies a later more specific deny rule even when an earlier broad allow rule matched", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		pod := restrictedPod("customer-denied", "harbor/customer/app:1", corev1.PullIfNotPresent)

		createPodAndExpectDenied(cs, ns.Name, pod,
			"containers[0]",
			"harbor/customer/app:1",
			"denied",
			"harbor/customer/.*",
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
			pod.Spec.Containers[0].Image = "harbor/customer/adad:1"
		},
			"containers[0]",
			"harbor/customer/adad:1",
			"denied",
			"harbor/customer/.*",
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

		denied := restrictedPod("prod-customer-denied", "harbor/customer/other-image/app:1", corev1.PullIfNotPresent)
		createPodAndExpectDenied(cs, ns.Name, denied,
			"containers[0]",
			"harbor/customer/other-image/app:1",
			"denied",
			"harbor/customer/.*",
		)

		allowed := restrictedPod("prod-customer-allowed", "harbor/customer/prod-image/app:1", corev1.PullIfNotPresent)
		createPodAndExpectAllowed(cs, ns.Name, allowed)
	})

	It("audits a matching image by allowing admission and emitting an event", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		pod := restrictedPod("audit-allowed", "audit/team/app:1", corev1.PullIfNotPresent)

		createPodAndExpectAllowed(cs, ns.Name, pod)

		expectAuditEvent(cs, ns.Name, pod.Name,
			"matched audit registry rule",
			"audit/.*",
		)
	})

	It("applies negated regex rules using the nested regex expression", func() {
		ns := NewNamespace("", map[string]string{
			"negate":         "true",
			meta.TenantLabel: tnt.GetName(),
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		allowed := restrictedPod("negate-trusted-allowed", "trusted/team/app:1", corev1.PullIfNotPresent)
		createPodAndExpectAllowed(cs, ns.Name, allowed)

		denied := restrictedPod("negate-untrusted-denied", "untrusted/team/app:1", corev1.PullIfNotPresent)
		createPodAndExpectDenied(cs, ns.Name, denied,
			"containers[0]",
			"untrusted/team/app:1",
			"denied",
			"trusted/.*",
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
						Image:           "harbor/customer/init:1",
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
			"harbor/customer/init:1",
			"denied",
			"harbor/customer/.*",
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
								Reference:  "harbor/customer/volume:1",
								PullPolicy: corev1.PullIfNotPresent,
							},
						},
					},
				},
			},
		}

		createPodAndExpectDenied(cs, ns.Name, pod,
			"volumes[0](imgvol)",
			"harbor/customer/volume:1",
			"denied",
			"harbor/customer/.*",
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
				Image:           "harbor/customer/debug:1",
				ImagePullPolicy: corev1.PullIfNotPresent,
				SecurityContext: restrictedContainerSecurityContext(),
			},
		}

		Eventually(func() error {
			current, err := cs.CoreV1().Pods(ns.Name).Get(context.Background(), pod.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			current.Spec.EphemeralContainers = append(current.Spec.EphemeralContainers, ephemeral)

			_, err = cs.CoreV1().Pods(ns.Name).UpdateEphemeralContainers(
				context.Background(),
				current.Name,
				current,
				metav1.UpdateOptions{},
			)
			if err == nil {
				return fmt.Errorf("expected UpdateEphemeralContainers to be denied, but it succeeded")
			}

			msg := err.Error()
			for _, substring := range []string{
				"ephemeralContainers[0]",
				"harbor/customer/debug:1",
				"denied",
				"harbor/customer/.*",
			} {
				if !strings.Contains(msg, substring) {
					return fmt.Errorf("expected error to contain %q, got: %s", substring, msg)
				}
			}

			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})
})
