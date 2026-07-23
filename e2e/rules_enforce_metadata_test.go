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
	k8stypes "k8s.io/apimachinery/pkg/types"
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

var _ = Describe("enforcing generic metadata namespace rules", Ordered, Label("tenant", "rules", "enforce", "metadata", "generic"), func() {
	const ownerName = "e2e-rules-metadata"

	var (
		tnt         *capsulev1beta2.Tenant
		tenantRules []*rules.NamespaceRuleBodyTenant
	)

	metadataByExpression := func(expression string) runtime.ExpressionMatch {
		return runtime.ExpressionMatch{
			ExpressionRegex: runtime.ExpressionRegex{
				Expression: expression,
			},
		}
	}

	metadataByNegatedExpression := func(expression string) runtime.ExpressionMatch {
		return runtime.ExpressionMatch{
			ExpressionRegex: runtime.ExpressionRegex{
				Expression: expression,
				Negate:     true,
			},
		}
	}

	metadataByExact := func(exact ...string) runtime.ExpressionMatch {
		return runtime.ExpressionMatch{
			Exact: exact,
		}
	}

	metadataByMatch := func(exact []string, expression string) runtime.ExpressionMatch {
		return runtime.ExpressionMatch{
			ExpressionRegex: runtime.ExpressionRegex{
				Expression: expression,
			},
			Exact: exact,
		}
	}

	metadataValueRule := func(required bool, values ...runtime.ExpressionMatch) rules.MetadataValueRule {
		return rules.MetadataValueRule{
			Required: required,
			Values:   values,
		}
	}

	metadataRule := func(
		action rules.ActionType,
		apiVersion string,
		kinds []string,
		labels map[string]rules.MetadataValueRule,
		annotations map[string]rules.MetadataValueRule,
	) *rules.NamespaceRuleBodyTenant {
		return &rules.NamespaceRuleBodyTenant{
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Enforce: &rules.NamespaceRuleEnforceBody{
					Action: action,
					Metadata: []rules.MetadataRule{
						{
							VersionKinds: runtime.VersionKinds{
								APIGroups: []string{apiVersion},
								Kinds:     kinds,
							},
							Labels:      labels,
							Annotations: annotations,
						},
					},
				},
			},
		}
	}

	selectedRule := func(
		selector map[string]string,
		rule *rules.NamespaceRuleBodyTenant,
	) *rules.NamespaceRuleBodyTenant {
		rule.NamespaceSelector = &metav1.LabelSelector{
			MatchLabels: selector,
		}

		return rule
	}

	baseTenantRules := func() []*rules.NamespaceRuleBodyTenant {
		return []*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"",
				[]string{
					"ConfigMap",
					"Service",
				},
				map[string]rules.MetadataValueRule{
					"example.corp/tenant": metadataValueRule(
						true,
						metadataByExact("prod", "test"),
					),
				},
				map[string]rules.MetadataValueRule{
					"example.corp/cost-center": metadataValueRule(
						false,
						metadataByExpression("^INV-[0-9]{4}$"),
						metadataByExact("prod", "test"),
					),
				},
			),
			metadataRule(
				rules.ActionTypeAudit,
				"*",
				[]string{
					"ConfigMap",
					"Service",
				},
				map[string]rules.MetadataValueRule{
					"example.corp/audit": metadataValueRule(
						false,
						metadataByExpression("^audit-.*"),
					),
				},
				nil,
			),
			selectedRule(
				map[string]string{
					"environment": "prod",
				},
				metadataRule(
					rules.ActionTypeAllow,
					"*",
					[]string{
						"ConfigMap",
					},
					nil,
					map[string]rules.MetadataValueRule{
						"example.corp/approval": metadataValueRule(
							true,
							metadataByExact("approved"),
						),
					},
				),
			),
		}
	}

	newTenant := func() *capsulev1beta2.Tenant {
		return &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name: "e2e-rule-metadata",
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

	type expectedMetadataPolicy struct {
		required    bool
		expressions []string
		exact       [][]string
		negated     []bool
	}

	type expectedMetadataStatusRule struct {
		action      rules.ActionType
		apiGroups   []string
		kinds       []string
		labels      map[string]expectedMetadataPolicy
		annotations map[string]expectedMetadataPolicy
	}

	expectMetadataPolicy := func(g Gomega, got rules.MetadataValueRule, expected expectedMetadataPolicy) {
		g.Expect(got.Required).To(Equal(expected.required))

		wantValues := len(expected.expressions)
		if len(expected.exact) > wantValues {
			wantValues = len(expected.exact)
		}
		if len(expected.negated) > wantValues {
			wantValues = len(expected.negated)
		}

		g.Expect(got.Values).To(HaveLen(wantValues))

		for i := 0; i < wantValues; i++ {
			value := got.Values[i]

			if len(expected.expressions) > i {
				g.Expect(value.Expression).To(Equal(expected.expressions[i]))
			} else {
				g.Expect(value.Expression).To(BeEmpty())
			}

			if len(expected.exact) > i {
				g.Expect(value.Exact).To(Equal(expected.exact[i]))
			} else {
				g.Expect(value.Exact).To(BeEmpty())
			}

			if len(expected.negated) > i {
				g.Expect(value.Negate).To(Equal(expected.negated[i]))
			} else {
				g.Expect(value.Negate).To(BeFalse())
			}
		}
	}

	expectNamespaceStatusRules := func(nsName string, want []expectedMetadataStatusRule) {
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
				g.Expect(got.Enforce.Metadata).To(HaveLen(1))

				metadata := got.Enforce.Metadata[0]

				if len(expected.apiGroups) == 0 {
					g.Expect(metadata.APIGroups).To(BeEmpty())
				} else {
					g.Expect(metadata.APIGroups).To(Equal(expected.apiGroups))
				}

				g.Expect(metadata.Kinds).To(Equal(expected.kinds))

				g.Expect(metadata.Labels).To(HaveLen(len(expected.labels)))
				for key, policy := range expected.labels {
					gotPolicy, ok := metadata.Labels[key]
					g.Expect(ok).To(BeTrue(), "expected label policy %q", key)
					expectMetadataPolicy(g, gotPolicy, policy)
				}

				g.Expect(metadata.Annotations).To(HaveLen(len(expected.annotations)))
				for key, policy := range expected.annotations {
					gotPolicy, ok := metadata.Annotations[key]
					g.Expect(ok).To(BeTrue(), "expected annotation policy %q", key)
					expectMetadataPolicy(g, gotPolicy, policy)
				}
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	baseStatusRules := func() []expectedMetadataStatusRule {
		return []expectedMetadataStatusRule{
			{
				action: rules.ActionTypeAllow,
				apiGroups: []string{
					"v1",
				},
				kinds: []string{
					"ConfigMap",
					"Service",
				},
				labels: map[string]expectedMetadataPolicy{
					"example.corp/tenant": {
						required: true,
						exact: [][]string{
							{
								"prod",
								"test",
							},
						},
					},
				},
				annotations: map[string]expectedMetadataPolicy{
					"example.corp/cost-center": {
						required: false,
						expressions: []string{
							"^INV-[0-9]{4}$",
							"",
						},
						exact: [][]string{
							nil,
							{
								"prod",
								"test",
							},
						},
					},
				},
			},
			{
				action: rules.ActionTypeAudit,
				apiGroups: []string{
					"*",
				},
				kinds: []string{
					"ConfigMap",
					"Service",
				},
				labels: map[string]expectedMetadataPolicy{
					"example.corp/audit": {
						expressions: []string{
							"^audit-.*",
						},
					},
				},
			},
		}
	}

	updateTenantRules := func(next []*rules.NamespaceRuleBodyTenant) {
		UpdateTenantEventually(tnt, func(current *capsulev1beta2.Tenant) {
			current.Spec.Rules = next
		})

		tnt.Spec.Rules = next
	}

	waitForProjectedMetadata := func(nsName, key string, wantDefault, wantManaged *string) {
		Eventually(func(g Gomega) {
			status := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{
				Name:      meta.NameForManagedRuleStatus(),
				Namespace: nsName,
			}, status)).To(Succeed())

			for _, body := range status.Status.Rules {
				if body == nil || body.Enforce == nil {
					continue
				}
				for _, metadata := range body.Enforce.Metadata {
					if policy, ok := metadata.Labels[key]; ok {
						g.Expect(policy.Default).To(Equal(wantDefault))
						g.Expect(policy.Managed).To(Equal(wantManaged))
						return
					}
					if policy, ok := metadata.Annotations[key]; ok {
						g.Expect(policy.Default).To(Equal(wantDefault))
						g.Expect(policy.Managed).To(Equal(wantManaged))
						return
					}
				}
			}

			g.Expect(false).To(BeTrue(), "metadata policy %q was not projected", key)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	createNamespace := func(labels map[string]string) *corev1.Namespace {
		if labels == nil {
			labels = map[string]string{}
		}

		labels[meta.TenantLabel] = tnt.GetName()

		ns := NewNamespace("", labels)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		return ns
	}

	createNamespaceAndExpectDenied := func(labels map[string]string, substrings ...string) {
		if labels == nil {
			labels = map[string]string{}
		}
		labels[meta.TenantLabel] = tnt.GetName()
		base := NewNamespace("", labels)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		Eventually(func() error {
			candidate := base.DeepCopy()
			candidate.Name = fmt.Sprintf("%s-%d", base.Name, time.Now().UnixNano()%1e6)
			_, err := cs.CoreV1().Namespaces().Create(context.Background(), candidate, metav1.CreateOptions{})
			if err == nil {
				_ = cs.CoreV1().Namespaces().Delete(context.Background(), candidate.Name, metav1.DeleteOptions{})
				return fmt.Errorf("expected namespace create to be denied, but it succeeded")
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

	configMap := func(name string, labels map[string]string, annotations map[string]string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Labels:      labels,
				Annotations: annotations,
			},
			Data: map[string]string{
				"key": "value",
			},
		}
	}

	service := func(name string, labels map[string]string, annotations map[string]string) *corev1.Service {
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				Ports: []corev1.ServicePort{
					{
						Name: "http",
						Port: 8080,
					},
				},
			},
		}
	}

	deployment := func(name string, labels map[string]string, annotations map[string]string) *appsv1.Deployment {
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Labels:      labels,
				Annotations: annotations,
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
						Containers: []corev1.Container{
							{
								Name:  "nginx",
								Image: "nginx:1.25",
							},
						},
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
		baseName := base.Name
		if baseName == "" {
			baseName = "cm"
		}

		Eventually(func() error {
			candidate := base.DeepCopy()
			candidate.Name = fmt.Sprintf("%s-%d", baseName, time.Now().UnixNano()%1e6)

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

	updateConfigMapAndExpectDenied := func(
		cs kubernetes.Interface,
		nsName string,
		cmName string,
		mutate func(*corev1.ConfigMap),
		substrings ...string,
	) {
		Eventually(func() error {
			cm, err := cs.CoreV1().ConfigMaps(nsName).Get(context.Background(), cmName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			mutate(cm)

			_, err = cs.CoreV1().ConfigMaps(nsName).Update(context.Background(), cm, metav1.UpdateOptions{})
			if err == nil {
				return fmt.Errorf("expected configmap update to be denied, but it succeeded")
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

	createServiceAndExpectAllowed := func(cs kubernetes.Interface, nsName string, svc *corev1.Service) {
		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Services(nsName).Create(context.Background(), svc, metav1.CreateOptions{})

			return err
		}).Should(Succeed())
	}

	createServiceAndExpectDenied := func(cs kubernetes.Interface, nsName string, svc *corev1.Service, substrings ...string) {
		base := svc.DeepCopy()
		baseName := base.Name
		if baseName == "" {
			baseName = "svc"
		}

		Eventually(func() error {
			candidate := base.DeepCopy()
			candidate.Name = fmt.Sprintf("%s-%d", baseName, time.Now().UnixNano()%1e6)

			_, err := cs.CoreV1().Services(nsName).Create(context.Background(), candidate, metav1.CreateOptions{})
			if err == nil {
				_ = cs.CoreV1().Services(nsName).Delete(context.Background(), candidate.Name, metav1.DeleteOptions{})

				return fmt.Errorf("expected service create to be denied, but it succeeded")
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
		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), deploy, &client.CreateOptions{})
		}).Should(Succeed())

		EventuallyDeletion(deploy)
	}

	createDeploymentAndExpectDenied := func(nsName string, deploy *appsv1.Deployment, substrings ...string) {
		base := deploy.DeepCopy()
		baseName := base.Name
		if baseName == "" {
			baseName = "deployment"
		}

		Eventually(func() error {
			candidate := base.DeepCopy()
			candidate.Name = fmt.Sprintf("%s-%d", baseName, time.Now().UnixNano()%1e6)
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

				eventObjectName := e.InvolvedObject.Name
				if eventObjectName != objectName && !strings.HasPrefix(eventObjectName, objectName+"-") {
					continue
				}

				message := e.Message

				matched := true
				for _, substring := range substrings {
					if !strings.Contains(message, substring) {
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

	It("stores matching tenant metadata rules as independent status rule blocks", func() {
		ns := createNamespace(nil)

		expectNamespaceStatusRules(ns.GetName(), baseStatusRules())
	})

	It("stores namespace-selector matched metadata rules as additional independent status rule blocks", func() {
		ns := createNamespace(map[string]string{
			"environment": "prod",
		})

		want := baseStatusRules()
		want = append(want, expectedMetadataStatusRule{
			action: rules.ActionTypeAllow,
			apiGroups: []string{
				"*",
			},
			kinds: []string{
				"ConfigMap",
			},
			annotations: map[string]expectedMetadataPolicy{
				"example.corp/approval": {
					required: true,
					exact: [][]string{
						{
							"approved",
						},
					},
				},
			},
		})

		expectNamespaceStatusRules(ns.GetName(), want)
	})

	It("denies creation when a required label is missing", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap("required-label-missing", nil, nil),
			"metadata",
			"example.corp/tenant",
			"required",
		)
	})

	It("allows creation when a required label is present and exact value matches", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap(
				"required-label-allowed",
				map[string]string{
					"example.corp/tenant": "prod",
				},
				nil,
			),
		)
	})

	It("denies creation when a required label is present but value is not allowed", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap(
				"required-label-invalid",
				map[string]string{
					"example.corp/tenant": "stage",
				},
				nil,
			),
			"metadata",
			"example.corp/tenant",
			"stage",
			"not allowed",
		)
	})

	It("allows missing optional annotations but denies present non-matching annotation values", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap(
				"optional-annotation-missing",
				map[string]string{
					"example.corp/tenant": "prod",
				},
				nil,
			),
		)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap(
				"optional-annotation-invalid",
				map[string]string{
					"example.corp/tenant": "prod",
				},
				map[string]string{
					"example.corp/cost-center": "BAD-1234",
				},
			),
			"metadata",
			"example.corp/cost-center",
			"BAD-1234",
			"not allowed",
		)
	})

	It("allows optional annotation values matching regex, exact, or combined matchers", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap(
				"annotation-regex-allowed",
				map[string]string{
					"example.corp/tenant": "prod",
				},
				map[string]string{
					"example.corp/cost-center": "INV-1234",
				},
			),
		)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap(
				"annotation-exact-allowed",
				map[string]string{
					"example.corp/tenant": "prod",
				},
				map[string]string{
					"example.corp/cost-center": "test",
				},
			),
		)
	})

	It("matches label and annotation key expressions through the regex cache", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"v1",
				[]string{"ConfigMap"},
				map[string]rules.MetadataValueRule{
					`example\.corp/label-.*`: metadataValueRule(false, metadataByExact("allowed")),
				},
				map[string]rules.MetadataValueRule{
					"example.corp/*": metadataValueRule(false, metadataByExpression("^INV-[0-9]{4}$")),
				},
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectAllowed(cs, ns.Name, configMap(
			"key-regex-allowed",
			map[string]string{"example.corp/label-team": "allowed"},
			map[string]string{"example.corp/cost-center": "INV-1234"},
		))

		createConfigMapAndExpectDenied(cs, ns.Name, configMap(
			"key-regex-label-denied",
			map[string]string{"example.corp/label-team": "blocked"},
			nil,
		), "metadata", "example.corp/label-team", "blocked", "not allowed")

		createConfigMapAndExpectDenied(cs, ns.Name, configMap(
			"key-regex-annotation-denied",
			nil,
			map[string]string{"example.corp/cost-center": "BAD-1234"},
		), "metadata", "example.corp/cost-center", "BAD-1234", "not allowed")
	})

	It("requires Namespace to be explicitly included in kinds", func() {
		policy := map[string]rules.MetadataValueRule{
			"example.corp/namespace-policy": metadataValueRule(true, metadataByExact("allowed")),
		}

		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(rules.ActionTypeAllow, "*", []string{"*"}, policy, nil),
		})
		createNamespace(nil)

		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(rules.ActionTypeAllow, "*", []string{"*", "Namespace"}, policy, nil),
		})
		createNamespaceAndExpectDenied(
			nil,
			"metadata",
			"example.corp/namespace-policy",
			"required",
		)
		createNamespace(map[string]string{"example.corp/namespace-policy": "allowed"})
	})

	It("applies one metadata rule to multiple core kinds", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap("multi-kind-configmap-denied", nil, nil),
			"metadata",
			"example.corp/tenant",
			"required",
		)

		createServiceAndExpectDenied(
			cs,
			ns.Name,
			service("multi-kind-service-denied", nil, nil),
			"metadata",
			"example.corp/tenant",
			"required",
		)

		createServiceAndExpectAllowed(
			cs,
			ns.Name,
			service(
				"multi-kind-service-allowed",
				map[string]string{
					"example.corp/tenant": "prod",
				},
				nil,
			),
		)
	})

	It("treats empty apiVersion as core v1 and does not match grouped resources", Label("skip-on-openshift"), func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"",
				[]string{
					"*",
				},
				map[string]rules.MetadataValueRule{
					"env": metadataValueRule(
						true,
						metadataByExact("prod"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap("core-v1-configmap-denied", nil, nil),
			"metadata",
			"env",
			"required",
		)

		deploy := deployment("apps-deployment-not-matched", nil, nil)
		deploy.Namespace = ns.Name

		createDeploymentAndExpectAllowed(ns.Name, deploy)
	})

	It("matches grouped apiVersion and selected kinds", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"apps/v1",
				[]string{
					"Deployment",
				},
				map[string]rules.MetadataValueRule{
					"env": metadataValueRule(
						true,
						metadataByExact("prod"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)

		createDeploymentAndExpectDenied(
			ns.Name,
			deployment("apps-deployment-missing-env", nil, nil),
			"metadata",
			"env",
			"required",
		)

		deploy := deployment(
			"apps-deployment-env-allowed",
			map[string]string{
				"env": "prod",
			},
			nil,
		)
		deploy.Namespace = ns.Name

		createDeploymentAndExpectAllowed(ns.Name, deploy)
	})

	It("denies a later matching deny rule after an earlier allow rule matched", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{
					"ConfigMap",
				},
				map[string]rules.MetadataValueRule{
					"env": metadataValueRule(
						true,
						metadataByExact("prod", "test"),
					),
				},
				nil,
			),
			metadataRule(
				rules.ActionTypeDeny,
				"*",
				[]string{
					"ConfigMap",
				},
				map[string]rules.MetadataValueRule{
					"env": metadataValueRule(
						false,
						metadataByExact("test"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap(
				"later-deny-prod-allowed",
				map[string]string{
					"env": "prod",
				},
				nil,
			),
		)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap(
				"later-deny-test-denied",
				map[string]string{
					"env": "test",
				},
				nil,
			),
			"metadata",
			"env",
			"test",
			"denied",
		)
	})

	It("allows a later matching allow rule after an earlier deny rule did not match", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeDeny,
				"*",
				[]string{
					"ConfigMap",
				},
				map[string]rules.MetadataValueRule{
					"env": metadataValueRule(
						false,
						metadataByExact("blocked"),
					),
				},
				nil,
			),
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{
					"ConfigMap",
				},
				map[string]rules.MetadataValueRule{
					"env": metadataValueRule(
						true,
						metadataByExact("prod"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap(
				"later-allow-prod-allowed",
				map[string]string{
					"env": "prod",
				},
				nil,
			),
		)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap(
				"later-allow-blocked-denied",
				map[string]string{
					"env": "blocked",
				},
				nil,
			),
			"metadata",
			"env",
			"blocked",
			"denied",
		)
	})

	It("supports negated metadata matchers", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeDeny,
				"*",
				[]string{
					"ConfigMap",
				},
				map[string]rules.MetadataValueRule{
					"team": metadataValueRule(
						false,
						metadataByNegatedExpression("^trusted-.*"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap(
				"negated-denied",
				map[string]string{
					"team": "untrusted",
				},
				nil,
			),
			"metadata",
			"team",
			"untrusted",
			"denied",
		)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap(
				"negated-allowed",
				map[string]string{
					"team": "trusted-platform",
				},
				nil,
			),
		)
	})

	It("audits matching metadata but does not deny when a separate allow rule matches", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAudit,
				"*",
				[]string{
					"ConfigMap",
				},
				map[string]rules.MetadataValueRule{
					"example.corp/audit": metadataValueRule(
						false,
						metadataByExpression("^audit-.*"),
					),
				},
				nil,
			),
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{
					"ConfigMap",
				},
				map[string]rules.MetadataValueRule{
					"env": metadataValueRule(
						true,
						metadataByExact("prod"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		cm := configMap(
			"metadata-audit-allowed",
			map[string]string{
				"env":                "prod",
				"example.corp/audit": "audit-this",
			},
			nil,
		)

		createConfigMapAndExpectAllowed(cs, ns.Name, cm)

		expectAuditEvent(
			clusterAdminClient(),
			ns.Name,
			"ConfigMap",
			cm.Name,
			"metadata",
			"example.corp/audit",
			"audit-this",
		)
	})

	It("applies namespace-selector matched required annotation only to selected namespaces", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			selectedRule(
				map[string]string{
					"environment": "prod",
				},
				metadataRule(
					rules.ActionTypeAllow,
					"*",
					[]string{
						"ConfigMap",
					},
					nil,
					map[string]rules.MetadataValueRule{
						"example.corp/approval": metadataValueRule(
							true,
							metadataByExact("approved"),
						),
					},
				),
			),
		})

		devNS := createNamespace(map[string]string{
			"environment": "dev",
		})
		prodNS := createNamespace(map[string]string{
			"environment": "prod",
		})
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectAllowed(
			cs,
			devNS.Name,
			configMap("selected-rule-dev-allowed", nil, nil),
		)

		createConfigMapAndExpectDenied(
			cs,
			prodNS.Name,
			configMap("selected-rule-prod-denied", nil, nil),
			"metadata",
			"example.corp/approval",
			"required",
		)

		createConfigMapAndExpectAllowed(
			cs,
			prodNS.Name,
			configMap(
				"selected-rule-prod-allowed",
				nil,
				map[string]string{
					"example.corp/approval": "approved",
				},
			),
		)
	})

	It("denies an update when a metadata value becomes invalid", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{
					"ConfigMap",
				},
				map[string]rules.MetadataValueRule{
					"env": metadataValueRule(
						true,
						metadataByExact("prod", "test"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		cm := configMap(
			"metadata-update-denied",
			map[string]string{
				"env": "prod",
			},
			nil,
		)

		createConfigMapAndExpectAllowed(cs, ns.Name, cm)

		updateConfigMapAndExpectDenied(
			cs,
			ns.Name,
			cm.Name,
			func(cm *corev1.ConfigMap) {
				cm.Labels["env"] = "stage"
			},
			"metadata",
			"env",
			"stage",
			"not allowed",
		)
	})

	It("supports required metadata without value constraints", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{
					"ConfigMap",
				},
				map[string]rules.MetadataValueRule{
					"presence-only": metadataValueRule(true),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap("presence-only-missing", nil, nil),
			"metadata",
			"presence-only",
			"required",
		)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap(
				"presence-only-present",
				map[string]string{
					"presence-only": "any-value",
				},
				nil,
			),
		)
	})

	It("does not enforce rules for Capsule managed metadata keys", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{
					"ConfigMap",
				},
				map[string]rules.MetadataValueRule{
					meta.TenantLabel: metadataValueRule(
						true,
						metadataByExact("some-other-tenant"),
					),
				},
				map[string]rules.MetadataValueRule{
					meta.ReconcileAnnotation: metadataValueRule(
						true,
						metadataByExact("must-not-matter"),
					),
				},
			),
			metadataRule(
				rules.ActionTypeDeny,
				"*",
				[]string{
					"ConfigMap",
				},
				map[string]rules.MetadataValueRule{
					meta.ManagedByCapsuleLabel: metadataValueRule(
						false,
						metadataByExact("blocked"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap(
				"managed-metadata-ignored",
				map[string]string{
					meta.ManagedByCapsuleLabel: "blocked",
				},
				nil,
			),
		)
	})

	It("skips generic metadata validation for controller-managed objects", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{
					"ConfigMap",
				},
				map[string]rules.MetadataValueRule{
					"env": metadataValueRule(
						true,
						metadataByExact("prod"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap(
				"controller-managed-skipped",
				map[string]string{
					meta.NewManagedByCapsuleLabel: meta.ValueController,
					"env":                         "prod",
				},
				nil,
			),
		)
	})

	It("enforces non-skipped objects even when a similar managed-by value does not match the skip rule", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{
					"ConfigMap",
				},
				map[string]rules.MetadataValueRule{
					"env": metadataValueRule(
						true,
						metadataByExact("prod"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap(
				"managed-by-other-not-skipped",
				map[string]string{
					meta.ManagedByCapsuleLabel: "human",
				},
				nil,
			),
			"metadata",
			"env",
			"required",
		)
	})

	It("supports combined exact and regex value matchers for metadata labels", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{
					"ConfigMap",
				},
				map[string]rules.MetadataValueRule{
					"release": metadataValueRule(
						true,
						metadataByMatch(
							[]string{
								"stable",
							},
							"^release-[0-9]+$",
						),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap(
				"combined-exact-allowed",
				map[string]string{
					"release": "stable",
				},
				nil,
			),
		)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap(
				"combined-regex-allowed",
				map[string]string{
					"release": "release-42",
				},
				nil,
			),
		)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap(
				"combined-denied",
				map[string]string{
					"release": "canary",
				},
				nil,
			),
			"metadata",
			"release",
			"canary",
			"not allowed",
		)
	})

	It("treats optional allow metadata as non-required but still validates it when present", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{"ConfigMap"},
				map[string]rules.MetadataValueRule{
					"optional-env": metadataValueRule(
						false,
						metadataByExact("prod", "test"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap("optional-label-missing-allowed", nil, nil),
		)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap(
				"optional-label-valid-allowed",
				map[string]string{
					"optional-env": "prod",
				},
				nil,
			),
		)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap(
				"optional-label-invalid-denied",
				map[string]string{
					"optional-env": "stage",
				},
				nil,
			),
			"metadata",
			"optional-env",
			"stage",
			"not allowed",
		)
	})

	It("does not treat required on deny rules as a presence requirement", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeDeny,
				"*",
				[]string{"ConfigMap"},
				map[string]rules.MetadataValueRule{
					"blocked": metadataValueRule(
						true,
						metadataByExact("true"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap("deny-required-missing-allowed", nil, nil),
		)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap(
				"deny-required-present-denied",
				map[string]string{
					"blocked": "true",
				},
				nil,
			),
			"metadata",
			"blocked",
			"true",
			"denied",
		)
	})

	It("does not treat required on audit rules as a presence requirement", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAudit,
				"*",
				[]string{"ConfigMap"},
				map[string]rules.MetadataValueRule{
					"audited": metadataValueRule(
						true,
						metadataByExact("true"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap("audit-required-missing-allowed", nil, nil),
		)

		cm := configMap(
			"audit-required-present-allowed",
			map[string]string{
				"audited": "true",
			},
			nil,
		)

		createConfigMapAndExpectAllowed(cs, ns.Name, cm)

		expectAuditEvent(
			clusterAdminClient(),
			ns.Name,
			"ConfigMap",
			cm.Name,
			"metadata",
			"audited",
			"true",
		)
	})

	It("enforces multiple required metadata keys independently and reports the missing key", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{"ConfigMap"},
				map[string]rules.MetadataValueRule{
					"env": metadataValueRule(
						true,
						metadataByExact("prod"),
					),
					"team": metadataValueRule(
						true,
						metadataByExact("platform"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap(
				"multi-required-team-missing",
				map[string]string{
					"env": "prod",
				},
				nil,
			),
			"metadata",
			"team",
			"required",
		)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap(
				"multi-required-all-present",
				map[string]string{
					"env":  "prod",
					"team": "platform",
				},
				nil,
			),
		)
	})

	It("denies update when a required label is removed", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{"ConfigMap"},
				map[string]rules.MetadataValueRule{
					"env": metadataValueRule(
						true,
						metadataByExact("prod"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		cm := configMap(
			"required-label-remove-denied",
			map[string]string{
				"env": "prod",
			},
			nil,
		)

		createConfigMapAndExpectAllowed(cs, ns.Name, cm)

		updateConfigMapAndExpectDenied(
			cs,
			ns.Name,
			cm.Name,
			func(cm *corev1.ConfigMap) {
				delete(cm.Labels, "env")
			},
			"metadata",
			"env",
			"required",
		)
	})

	It("skips empty metadata values during value evaluation", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{"ConfigMap"},
				map[string]rules.MetadataValueRule{
					"empty-ok": metadataValueRule(
						true,
						metadataByExact(""),
					),
					"empty-not-ok": metadataValueRule(
						false,
						metadataByExact("prod"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap(
				"empty-label-exact-allowed",
				map[string]string{
					"empty-ok": "",
				},
				nil,
			),
		)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap(
				"empty-label-invalid-skipped",
				map[string]string{
					"empty-ok":     "",
					"empty-not-ok": "",
				},
				nil,
			),
		)
	})

	It("matches apiVersion wildcard and kind wildcard across core and grouped resources", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{"*"},
				map[string]rules.MetadataValueRule{
					"global-required": metadataValueRule(
						true,
						metadataByExact("yes"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap("wildcard-core-denied", nil, nil),
			"metadata",
			"global-required",
			"required",
		)

		createDeploymentAndExpectDenied(
			ns.Name,
			deployment("wildcard-grouped-denied", nil, nil),
			"metadata",
			"global-required",
			"required",
		)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap(
				"wildcard-core-allowed",
				map[string]string{
					"global-required": "yes",
				},
				nil,
			),
		)
	})

	It("matches kind wildcard only inside default core v1 apiVersion", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"",
				[]string{"*"},
				map[string]rules.MetadataValueRule{
					"core-required": metadataValueRule(
						true,
						metadataByExact("yes"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap("core-wildcard-configmap-denied", nil, nil),
			"metadata",
			"core-required",
			"required",
		)

		deploy := deployment("core-wildcard-deployment-not-matched", nil, nil)
		deploy.Namespace = ns.Name

		createDeploymentAndExpectAllowed(ns.Name, deploy)
	})

	It("matches partial kind wildcard without matching unrelated kinds", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"v1",
				[]string{"*Map"},
				map[string]rules.MetadataValueRule{
					"map-required": metadataValueRule(
						true,
						metadataByExact("yes"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap("partial-kind-configmap-denied", nil, nil),
			"metadata",
			"map-required",
			"required",
		)

		createServiceAndExpectAllowed(
			cs,
			ns.Name,
			service("partial-kind-service-not-matched", nil, nil),
		)
	})

	It("does not let one optional matching key satisfy another required key", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{"ConfigMap"},
				map[string]rules.MetadataValueRule{
					"optional": metadataValueRule(
						false,
						metadataByExact("ok"),
					),
					"required": metadataValueRule(
						true,
						metadataByExact("ok"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap(
				"optional-does-not-satisfy-required",
				map[string]string{
					"optional": "ok",
				},
				nil,
			),
			"metadata",
			"required",
			"required",
		)
	})

	It("does not let a matching annotation satisfy a required label with the same key", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{"ConfigMap"},
				map[string]rules.MetadataValueRule{
					"shared-key": metadataValueRule(
						true,
						metadataByExact("label-value"),
					),
				},
				map[string]rules.MetadataValueRule{
					"shared-key": metadataValueRule(
						false,
						metadataByExact("annotation-value"),
					),
				},
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap(
				"annotation-does-not-satisfy-label",
				nil,
				map[string]string{
					"shared-key": "annotation-value",
				},
			),
			"metadata",
			"metadata.labels",
			"shared-key",
			"required",
		)
	})

	It("ignores managed annotation prefixes when collecting controlled metadata entries", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{"ConfigMap"},
				nil,
				map[string]rules.MetadataValueRule{
					meta.ResourceQuotaAnnotationPrefix + "cpu": metadataValueRule(
						true,
						metadataByExact("must-not-matter"),
					),
					meta.ResourceUsedAnnotationPrefix + "memory": metadataValueRule(
						true,
						metadataByExact("must-not-matter"),
					),
				},
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap("managed-prefix-annotations-ignored", nil, nil),
		)
	})

	It("skips controller-managed objects even when a deny rule would otherwise match", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeDeny,
				"*",
				[]string{"ConfigMap"},
				map[string]rules.MetadataValueRule{
					"blocked": metadataValueRule(
						false,
						metadataByExact("true"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap(
				"controller-managed-deny-skipped",
				map[string]string{
					meta.ManagedByCapsuleLabel: meta.ValueController,
					"blocked":                  "false",
				},
				nil,
			),
		)
	})

	It("still enforces controller skip rule case-sensitively", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeDeny,
				"*",
				[]string{"ConfigMap"},
				map[string]rules.MetadataValueRule{
					"blocked": metadataValueRule(
						false,
						metadataByExact("true"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap(
				"controller-managed-case-sensitive",
				map[string]string{
					meta.ManagedByCapsuleLabel: "Controller",
					"blocked":                  "true",
				},
				nil,
			),
			"metadata",
			"blocked",
			"true",
			"denied",
		)
	})

	It("allows value after later allow overrides an earlier deny for the same key", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeDeny,
				"*",
				[]string{"ConfigMap"},
				map[string]rules.MetadataValueRule{
					"env": metadataValueRule(
						false,
						metadataByExact("test"),
					),
				},
				nil,
			),
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{"ConfigMap"},
				map[string]rules.MetadataValueRule{
					"env": metadataValueRule(
						true,
						metadataByExact("test"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectAllowed(
			cs,
			ns.Name,
			configMap(
				"later-allow-overrides-earlier-deny",
				map[string]string{
					"env": "test",
				},
				nil,
			),
		)
	})

	It("denies value after later deny overrides an earlier allow for the same key", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{"ConfigMap"},
				map[string]rules.MetadataValueRule{
					"env": metadataValueRule(
						true,
						metadataByExact("test"),
					),
				},
				nil,
			),
			metadataRule(
				rules.ActionTypeDeny,
				"*",
				[]string{"ConfigMap"},
				map[string]rules.MetadataValueRule{
					"env": metadataValueRule(
						false,
						metadataByExact("test"),
					),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createConfigMapAndExpectDenied(
			cs,
			ns.Name,
			configMap(
				"later-deny-overrides-earlier-allow",
				map[string]string{
					"env": "test",
				},
				nil,
			),
			"metadata",
			"env",
			"test",
			"denied",
		)
	})

	It("applies Namespace defaults only when Namespace is explicitly targeted", func() {
		defaultValue := "baseline"
		policy := map[string]rules.MetadataValueRule{
			"example.corp/namespace-default": {
				Default: &defaultValue,
			},
		}

		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(rules.ActionTypeAllow, "*", []string{"*"}, policy, nil),
		})
		withoutExplicitKind := createNamespace(nil)

		Eventually(func(g Gomega) {
			current := &corev1.Namespace{}
			g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: withoutExplicitKind.Name}, current)).To(Succeed())
			g.Expect(current.Labels).NotTo(HaveKey("example.corp/namespace-default"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(rules.ActionTypeAllow, "*", []string{"*", "Namespace"}, policy, nil),
		})
		waitForProjectedMetadata(withoutExplicitKind.Name, "example.corp/namespace-default", &defaultValue, nil)

		withExplicitKind := createNamespace(nil)
		Eventually(func(g Gomega) {
			current := &corev1.Namespace{}
			g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: withExplicitKind.Name}, current)).To(Succeed())
			g.Expect(current.Labels).To(HaveKeyWithValue("example.corp/namespace-default", defaultValue))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("applies defaults, preserves supplied values, and lets managed values win in combination", func() {
		defaultOnly := "defaulted"
		combinedDefault := "fallback"
		combinedManaged := "controlled"
		standaloneManaged := "managed-annotation"

		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"v1",
				[]string{"ConfigMap"},
				map[string]rules.MetadataValueRule{
					"example.corp/default": {
						Default: &defaultOnly,
					},
					"example.corp/combined": {
						Default: &combinedDefault,
						Managed: &combinedManaged,
					},
				},
				map[string]rules.MetadataValueRule{
					"example.corp/managed": {
						Managed: &standaloneManaged,
					},
				},
			),
		})

		ns := createNamespace(nil)
		waitForProjectedMetadata(ns.Name, "example.corp/combined", &combinedDefault, &combinedManaged)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		created, err := cs.CoreV1().ConfigMaps(ns.Name).Create(context.Background(), configMap(
			"metadata-default-managed-combination",
			map[string]string{
				"example.corp/combined": "user-value",
			},
			map[string]string{
				"example.corp/managed": "user-value",
			},
		), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(created.Labels).To(HaveKeyWithValue("example.corp/default", defaultOnly))
		Expect(created.Labels).To(HaveKeyWithValue("example.corp/combined", combinedManaged))
		Expect(created.Annotations).To(HaveKeyWithValue("example.corp/managed", standaloneManaged))

		preserved, err := cs.CoreV1().ConfigMaps(ns.Name).Create(context.Background(), configMap(
			"metadata-default-preserves-user-value",
			map[string]string{
				"example.corp/default": "supplied",
			},
			nil,
		), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(preserved.Labels).To(HaveKeyWithValue("example.corp/default", "supplied"))
		Expect(preserved.Labels).To(HaveKeyWithValue("example.corp/combined", combinedManaged))
		Expect(preserved.Annotations).To(HaveKeyWithValue("example.corp/managed", standaloneManaged))
	})

	It("reconciles standalone managed metadata and removes it when the rule is removed", func() {
		managedLabel := "managed-label"
		managedAnnotation := "managed-annotation"
		managedNamespace := "managed-namespace"

		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"v1",
				[]string{"ConfigMap"},
				map[string]rules.MetadataValueRule{
					"example.corp/lifecycle": {Managed: &managedLabel},
				},
				map[string]rules.MetadataValueRule{
					"example.corp/lifecycle": {Managed: &managedAnnotation},
				},
			),
			metadataRule(
				rules.ActionTypeAllow,
				"v1",
				[]string{"Namespace"},
				map[string]rules.MetadataValueRule{
					"example.corp/namespace-lifecycle": {Managed: &managedNamespace},
				},
				nil,
			),
		})

		ns := createNamespace(map[string]string{"example.corp/user-owned": "keep"})
		waitForProjectedMetadata(ns.Name, "example.corp/lifecycle", nil, &managedLabel)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		cm, err := cs.CoreV1().ConfigMaps(ns.Name).Create(context.Background(), configMap(
			"managed-metadata-lifecycle",
			map[string]string{"example.corp/user-owned": "keep"},
			map[string]string{"example.corp/user-annotation": "keep"},
		), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			current, getErr := cs.CoreV1().ConfigMaps(ns.Name).Get(context.Background(), cm.Name, metav1.GetOptions{})
			g.Expect(getErr).NotTo(HaveOccurred())
			g.Expect(current.Labels).To(HaveKeyWithValue("example.corp/lifecycle", managedLabel))
			g.Expect(current.Annotations).To(HaveKeyWithValue("example.corp/lifecycle", managedAnnotation))

			currentNamespace := &corev1.Namespace{}
			g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: ns.Name}, currentNamespace)).To(Succeed())
			g.Expect(currentNamespace.Labels).To(HaveKeyWithValue("example.corp/namespace-lifecycle", managedNamespace))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		updateTenantRules(nil)

		Eventually(func(g Gomega) {
			current, getErr := cs.CoreV1().ConfigMaps(ns.Name).Get(context.Background(), cm.Name, metav1.GetOptions{})
			g.Expect(getErr).NotTo(HaveOccurred())
			g.Expect(current.Labels).NotTo(HaveKey("example.corp/lifecycle"))
			g.Expect(current.Annotations).NotTo(HaveKey("example.corp/lifecycle"))
			g.Expect(current.Labels).To(HaveKeyWithValue("example.corp/user-owned", "keep"))
			g.Expect(current.Annotations).To(HaveKeyWithValue("example.corp/user-annotation", "keep"))

			currentNamespace := &corev1.Namespace{}
			g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: ns.Name}, currentNamespace)).To(Succeed())
			g.Expect(currentNamespace.Labels).NotTo(HaveKey("example.corp/namespace-lifecycle"))
			g.Expect(currentNamespace.Labels).To(HaveKeyWithValue("example.corp/user-owned", "keep"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("filters mutation and validation by audience across metadata, Service, and workload rules", func() {
		defaultValue := "owner-default"
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			{
				NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
					Audience: []rules.Audience{{Kind: rules.AudienceKindUser, Name: ownerName}},
					Enforce: &rules.NamespaceRuleEnforceBody{
						Action: rules.ActionTypeDeny,
						Metadata: []rules.MetadataRule{{
							VersionKinds: runtime.VersionKinds{APIGroups: []string{"v1"}, Kinds: []string{"ConfigMap"}},
							Labels: map[string]rules.MetadataValueRule{
								"example.corp/audience-default": {Default: &defaultValue},
								"example.corp/audience-blocked": metadataValueRule(false, metadataByExact("true")),
							},
						}},
					},
				},
			},
			{
				NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
					Audience: []rules.Audience{{Kind: rules.AudienceKindGroup, Name: ownerName}},
					Enforce: &rules.NamespaceRuleEnforceBody{
						Action: rules.ActionTypeDeny,
						Services: rules.NamespaceRuleEnforceServicesBody{
							Types: []rules.ServiceType{rules.ServiceTypeClusterIP},
						},
					},
				},
			},
			{
				NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
					Audience: []rules.Audience{{Kind: rules.AudienceKindCustom, Name: string(rules.CustomAudienceTenantOwner)}},
					Enforce: &rules.NamespaceRuleEnforceBody{
						Action: rules.ActionTypeDeny,
						Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
							QoSClasses: []corev1.PodQOSClass{corev1.PodQOSBestEffort},
						},
					},
				},
			},
		})

		ns := createNamespace(nil)
		waitForProjectedMetadata(ns.Name, "example.corp/audience-default", &defaultValue, nil)
		owner := ownerClient(tnt.Spec.Owners[0].UserSpec)
		admin := clusterAdminClient()

		ownerCM, err := owner.CoreV1().ConfigMaps(ns.Name).Create(context.Background(), configMap(
			"audience-owner-mutated",
			nil,
			nil,
		), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(ownerCM.Labels).To(HaveKeyWithValue("example.corp/audience-default", defaultValue))

		createConfigMapAndExpectDenied(owner, ns.Name, configMap(
			"audience-owner-denied",
			map[string]string{"example.corp/audience-blocked": "true"},
			nil,
		), "example.corp/audience-blocked", "denied")

		adminCM, err := admin.CoreV1().ConfigMaps(ns.Name).Create(context.Background(), configMap(
			"audience-admin-bypasses-metadata",
			map[string]string{"example.corp/audience-blocked": "true"},
			nil,
		), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(adminCM.Labels).NotTo(HaveKey("example.corp/audience-default"))

		createServiceAndExpectDenied(owner, ns.Name, service("audience-owner-service-denied", nil, nil), "ClusterIP", "denied")
		createServiceAndExpectAllowed(admin, ns.Name, service("audience-admin-service-allowed", nil, nil))

		ownerPod := MakePod(ns.Name, "audience-owner-pod-denied", nil, nil, "nginx:1.25", "", "")
		Eventually(func() error {
			_, createErr := owner.CoreV1().Pods(ns.Name).Create(context.Background(), ownerPod, metav1.CreateOptions{})
			if createErr == nil {
				_ = owner.CoreV1().Pods(ns.Name).Delete(context.Background(), ownerPod.Name, metav1.DeleteOptions{})
				return fmt.Errorf("expected TenantOwner audience to deny the BestEffort Pod")
			}
			if !strings.Contains(createErr.Error(), "BestEffort") || !strings.Contains(createErr.Error(), "denied") {
				return createErr
			}
			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		adminPod := MakePod(ns.Name, "audience-admin-pod-allowed", nil, nil, "nginx:1.25", "", "")
		EventuallyCreation(func() error {
			_, createErr := admin.CoreV1().Pods(ns.Name).Create(context.Background(), adminPod, metav1.CreateOptions{})
			return createErr
		}).Should(Succeed())
	})

	It("rejects forbidden namespace metadata injected through the status subresource", func() {
		const policyKey = "pod-security.kubernetes.io/enforce"

		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"v1",
				[]string{"Namespace"},
				map[string]rules.MetadataValueRule{
					policyKey: metadataValueRule(true, metadataByExact("baseline", "restricted")),
				},
				nil,
			),
		})

		ns := createNamespace(map[string]string{policyKey: "baseline"})
		waitForProjectedMetadata(ns.Name, policyKey, nil, nil)

		createNamespaceStatusRBACForOwner(tnt)
		DeferCleanup(deleteNamespaceStatusRBACForOwner, tnt)

		owner := ownerClient(tnt.Spec.Owners[0].UserSpec)
		patchStatus := func(value string) error {
			patch := []byte(fmt.Sprintf(
				`[{"op":"add","path":"/metadata/labels/pod-security.kubernetes.io~1enforce","value":%q}]`,
				value,
			))
			_, err := owner.CoreV1().Namespaces().Patch(
				context.Background(),
				ns.Name,
				k8stypes.JSONPatchType,
				patch,
				metav1.PatchOptions{},
				"status",
			)

			return err
		}

		By("allowing a compliant metadata update through namespaces/status")
		Eventually(func() error {
			return patchStatus("restricted")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("rejecting a forbidden metadata update through namespaces/status")
		Eventually(func() error {
			err := patchStatus("privileged")
			if err == nil {
				return fmt.Errorf("expected namespace status patch to be denied")
			}
			if !strings.Contains(err.Error(), policyKey) ||
				!strings.Contains(err.Error(), "privileged") ||
				!strings.Contains(err.Error(), "not allowed") {
				return err
			}

			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("preserving the last allowed metadata value")
		Eventually(func(g Gomega) {
			current := &corev1.Namespace{}
			g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: ns.Name}, current)).To(Succeed())
			g.Expect(current.Labels).To(HaveKeyWithValue(policyKey, "restricted"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("ignores metadata enforcement for non-Namespace subresource updates", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			metadataRule(
				rules.ActionTypeAllow,
				"*",
				[]string{"*"},
				map[string]rules.MetadataValueRule{
					"example.corp/subresource-required": metadataValueRule(true, metadataByExact("true")),
				},
				nil,
			),
		})

		ns := createNamespace(nil)
		owner := ownerClient(tnt.Spec.Owners[0].UserSpec)
		deploy := deployment(
			"metadata-scale-subresource",
			map[string]string{"example.corp/subresource-required": "true"},
			nil,
		)
		deploy.Namespace = ns.Name

		EventuallyCreation(func() error {
			_, err := owner.AppsV1().Deployments(ns.Name).Create(context.Background(), deploy, metav1.CreateOptions{})
			return err
		}).Should(Succeed())

		Eventually(func() error {
			scale, err := owner.AppsV1().Deployments(ns.Name).GetScale(context.Background(), deploy.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			scale.Spec.Replicas = 2
			_, err = owner.AppsV1().Deployments(ns.Name).UpdateScale(context.Background(), deploy.Name, scale, metav1.UpdateOptions{})
			return err
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})
})
