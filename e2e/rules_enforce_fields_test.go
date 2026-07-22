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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

var _ = Describe("enforcing generic field namespace rules", Ordered, Label("tenant", "rules", "enforce", "fields", "generic"), func() {
	const ownerName = "e2e-rules-fields"

	var (
		tnt         *capsulev1beta2.Tenant
		tenantRules []*rules.NamespaceRuleBodyTenant
	)

	matchByExpression := func(expression string) runtime.ExpressionMatch {
		return runtime.ExpressionMatch{
			ExpressionRegex: runtime.ExpressionRegex{
				Expression: expression,
			},
		}
	}

	matchByExact := func(exact ...string) runtime.ExpressionMatch {
		return runtime.ExpressionMatch{
			Exact: exact,
		}
	}

	fieldRule := func(
		action rules.ActionType,
		apiVersion string,
		kinds []string,
		path string,
		match ...runtime.ExpressionMatch,
	) *rules.NamespaceRuleBodyTenant {
		return &rules.NamespaceRuleBodyTenant{
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Enforce: &rules.NamespaceRuleEnforceBody{
					Action: action,
					Fields: []rules.FieldRule{
						{
							VersionKinds: runtime.VersionKinds{
								APIGroups: []string{apiVersion},
								Kinds:     kinds,
							},
							Path:  path,
							Match: match,
						},
					},
				},
			},
		}
	}

	baseTenantRules := func() []*rules.NamespaceRuleBodyTenant {
		return []*rules.NamespaceRuleBodyTenant{
			fieldRule(
				rules.ActionTypeDeny,
				"",
				[]string{"ConfigMap"},
				".data.env",
				matchByExact("blocked"),
			),
		}
	}

	newTenant := func() *capsulev1beta2.Tenant {
		return &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name: "e2e-rule-fields",
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
				Rules: tenantRules,
			},
		}
	}

	updateTenantRules := func(next []*rules.NamespaceRuleBodyTenant) {
		UpdateTenantEventually(tnt, func(current *capsulev1beta2.Tenant) {
			current.Spec.Rules = next
		})

		tnt.Spec.Rules = next
	}

	createNamespace := func() *corev1.Namespace {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		return ns
	}

	configMap := func(name string, data map[string]string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Data: data,
		}
	}

	deployment := func(name string, hostNetwork bool, images ...string) *appsv1.Deployment {
		containers := make([]corev1.Container, 0, len(images))
		for i, image := range images {
			containers = append(containers, corev1.Container{
				Name:  fmt.Sprintf("container-%d", i),
				Image: image,
			})
		}

		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To[int32](1),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": name,
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": name,
						},
					},
					Spec: corev1.PodSpec{
						HostNetwork: hostNetwork,
						Containers:  containers,
					},
				},
			},
		}
	}

	createConfigMapAndExpectAllowed := func(cs kubernetes.Interface, nsName string, cm *corev1.ConfigMap) {
		EventuallyCreation(func() error {
			_, err := cs.CoreV1().ConfigMaps(nsName).Create(context.Background(), cm, metav1.CreateOptions{})

			return err
		}).Should(Succeed())
	}

	createConfigMapAndExpectDenied := func(cs kubernetes.Interface, nsName string, cm *corev1.ConfigMap, substrings ...string) {
		base := cm.DeepCopy()

		Eventually(func() error {
			candidate := base.DeepCopy()
			candidate.Name = fmt.Sprintf("%s-%d", base.Name, time.Now().UnixNano()%1e6)

			_, err := cs.CoreV1().ConfigMaps(nsName).Create(context.Background(), candidate, metav1.CreateOptions{})
			if err == nil {
				_ = cs.CoreV1().ConfigMaps(nsName).Delete(context.Background(), candidate.Name, metav1.DeleteOptions{})

				return fmt.Errorf("expected configmap create to be denied, but it succeeded")
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

	createDeploymentAndExpectAllowed := func(nsName string, deploy *appsv1.Deployment) {
		deploy.Namespace = nsName

		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), deploy, &client.CreateOptions{})
		}).Should(Succeed())

		EventuallyDeletion(deploy)
	}

	createDeploymentAndExpectDenied := func(nsName string, deploy *appsv1.Deployment, substrings ...string) {
		base := deploy.DeepCopy()

		Eventually(func() error {
			candidate := base.DeepCopy()
			candidate.Name = fmt.Sprintf("%s-%d", base.Name, time.Now().UnixNano()%1e6)
			candidate.Namespace = nsName
			candidate.Spec.Selector.MatchLabels["app"] = candidate.Name
			candidate.Spec.Template.Labels["app"] = candidate.Name

			err := k8sClient.Create(context.Background(), candidate, &client.CreateOptions{})
			if err == nil {
				_ = k8sClient.Delete(context.Background(), candidate)

				return fmt.Errorf("expected deployment create to be denied, but it succeeded")
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

	expectAuditEvent := func(
		cs kubernetes.Interface,
		namespace string,
		kind string,
		objectName string,
		substrings ...string,
	) {
		Eventually(func() error {
			evt, err := cs.CoreV1().Events(namespace).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return err
			}

			for _, e := range evt.Items {
				if e.Reason != events.ReasonNamespaceRuleAudit {
					continue
				}

				if e.InvolvedObject.Kind != kind {
					continue
				}

				if e.InvolvedObject.Name != objectName {
					continue
				}

				matched := true
				for _, substring := range substrings {
					if !strings.Contains(e.Message, substring) {
						matched = false

						break
					}
				}

				if matched {
					return nil
				}
			}

			return fmt.Errorf(
				"expected audit event for %s %q containing %q",
				kind,
				objectName,
				substrings,
			)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	BeforeEach(func() {
		tenantRules = baseTenantRules()
	})

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

	It("stores tenant field rules in the namespace rule status", func() {
		ns := createNamespace()

		Eventually(func(g Gomega) {
			nsStatus := &capsulev1beta2.RuleStatus{}

			g.Expect(k8sClient.Get(
				context.Background(),
				client.ObjectKey{
					Name:      meta.NameForManagedRuleStatus(),
					Namespace: ns.GetName(),
				},
				nsStatus,
			)).To(Succeed())

			g.Expect(nsStatus.Status.Rules).To(HaveLen(1))

			rule := nsStatus.Status.Rules[0]
			g.Expect(rule.Enforce).NotTo(BeNil())
			g.Expect(rule.Enforce.Action).To(Equal(rules.ActionTypeDeny))
			g.Expect(rule.Enforce.Fields).To(HaveLen(1))

			field := rule.Enforce.Fields[0]
			g.Expect(field.Kinds).To(Equal([]string{"ConfigMap"}))
			g.Expect(field.Path).To(Equal(".data.env"))
			g.Expect(field.Match).To(HaveLen(1))
			g.Expect(field.Match[0].Exact).To(Equal([]string{"blocked"}))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("denies creation when a denied field value matches and reports the configured path", func() {
		ns := createNamespace()
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap("denied-env", map[string]string{
				"env": "blocked",
			}),
			".data.env",
			"blocked",
			"denied",
		)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap("allowed-env", map[string]string{
				"env": "something-else",
			}),
		)
	})

	It("enforces allowed container images across expanded array elements", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			fieldRule(
				rules.ActionTypeAllow,
				"apps",
				[]string{"Deployment"},
				".spec.template.spec.containers[*].image",
				matchByExpression(`^registry\.internal/`),
			),
		})

		ns := createNamespace()

		createDeploymentAndExpectDenied(
			ns.Name,
			deployment("mixed-images", false, "registry.internal/app:v1", "docker.io/nginx:1.25"),
			".spec.template.spec.containers[*].image",
			"docker.io/nginx:1.25",
			"not allowed",
		)

		createDeploymentAndExpectAllowed(
			ns.Name,
			deployment("internal-images", false, "registry.internal/app:v1", "registry.internal/sidecar:v2"),
		)
	})

	It("leaves objects without the configured field untouched but validates present values", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			fieldRule(
				rules.ActionTypeAllow,
				"",
				[]string{"ConfigMap"},
				".data.tier",
				matchByExact("gold", "silver"),
			),
		})

		ns := createNamespace()
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap("tier-missing-allowed", map[string]string{
				"other": "value",
			}),
		)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap("tier-valid-allowed", map[string]string{
				"tier": "gold",
			}),
		)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap("tier-invalid-denied", map[string]string{
				"tier": "bronze",
			}),
			".data.tier",
			"bronze",
			"Allowed field values",
		)
	})

	It("coerces non-string scalars before matching", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			fieldRule(
				rules.ActionTypeDeny,
				"apps",
				[]string{"Deployment"},
				".spec.template.spec.hostNetwork",
				matchByExact("true"),
			),
		})

		ns := createNamespace()

		createDeploymentAndExpectDenied(
			ns.Name,
			deployment("host-network-denied", true, "registry.internal/app:v1"),
			".spec.template.spec.hostNetwork",
			"true",
			"denied",
		)

		createDeploymentAndExpectAllowed(
			ns.Name,
			deployment("host-network-allowed", false, "registry.internal/app:v1"),
		)
	})

	It("does not apply field rules to other kinds", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			fieldRule(
				rules.ActionTypeDeny,
				"apps",
				[]string{"Deployment"},
				".data.env",
				matchByExact("blocked"),
			),
		})

		ns := createNamespace()
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap("other-kind-not-matched", map[string]string{
				"env": "blocked",
			}),
		)
	})

	It("audits matching field values without blocking", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			fieldRule(
				rules.ActionTypeAudit,
				"",
				[]string{"ConfigMap"},
				".data.env",
				matchByExpression("^audit-.*"),
			),
		})

		ns := createNamespace()
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		cm := configMap("field-audit-allowed", map[string]string{
			"env": "audit-this",
		})

		createConfigMapAndExpectAllowed(cs, ns.Name, cm)

		expectAuditEvent(
			clusterAdminClient(),
			ns.Name,
			"ConfigMap",
			cm.Name,
			".data.env",
			"audit-this",
		)
	})

	It("rejects tenant field rules with an invalid path", func() {
		UpdateTenantEventuallyShouldFail(tnt, func(current *capsulev1beta2.Tenant) {
			current.Spec.Rules = []*rules.NamespaceRuleBodyTenant{
				fieldRule(
					rules.ActionTypeDeny,
					"",
					[]string{"ConfigMap"},
					".data.env[",
					matchByExact("blocked"),
				),
			}
		})
	})
})
