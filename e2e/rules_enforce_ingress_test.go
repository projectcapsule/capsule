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
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/utils"
)

var _ = Describe("enforcing ingress hostname namespace rules", Ordered, Label("tenant", "rules", "enforce", "ingress"), func() {
	const ownerName = "e2e-rules-ingress"

	var tnt *capsulev1beta2.Tenant

	hostnameByExact := func(hostnames ...string) runtime.ExpressionMatch {
		return runtime.ExpressionMatch{Exact: hostnames}
	}

	hostnameByExpression := func(expression string) runtime.ExpressionMatch {
		return runtime.ExpressionMatch{
			ExpressionRegex: runtime.ExpressionRegex{Expression: expression},
		}
	}

	hostnameByMatch := func(exact []string, expression string) runtime.ExpressionMatch {
		return runtime.ExpressionMatch{
			Exact: exact,
			ExpressionRegex: runtime.ExpressionRegex{
				Expression: expression,
			},
		}
	}

	hostnameByNegatedExpression := func(expression string) runtime.ExpressionMatch {
		return runtime.ExpressionMatch{
			ExpressionRegex: runtime.ExpressionRegex{
				Expression: expression,
				Negate:     true,
			},
		}
	}

	ingressRule := func(
		action rules.ActionType,
		types []rules.IngressType,
		hostnames ...runtime.ExpressionMatch,
	) *rules.NamespaceRuleBodyTenant {
		return &rules.NamespaceRuleBodyTenant{
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Enforce: &rules.NamespaceRuleEnforceBody{
					Action: action,
					Ingress: rules.NamespaceRuleEnforceIngressBody{
						Types:     types,
						Hostnames: hostnames,
					},
				},
			},
		}
	}

	selectedRule := func(
		selector map[string]string,
		rule *rules.NamespaceRuleBodyTenant,
	) *rules.NamespaceRuleBodyTenant {
		rule.NamespaceSelector = &metav1.LabelSelector{MatchLabels: selector}

		return rule
	}

	targetTypes := []rules.IngressType{
		rules.IngressTypeIngress,
		rules.IngressTypeHTTPRoute,
		rules.IngressTypeGateway,
	}

	baseTenantRules := func() []*rules.NamespaceRuleBodyTenant {
		return []*rules.NamespaceRuleBodyTenant{
			ingressRule(
				rules.ActionTypeAudit,
				targetTypes,
				hostnameByExact("audited.example.com", "audited.example.net"),
			),
			selectedRule(
				map[string]string{"enforce-hostnames": "true"},
				ingressRule(
					rules.ActionTypeAllow,
					targetTypes,
					hostnameByExact("internal.example.com"),
					hostnameByExpression("^[a-z0-9-]+\\.example\\.com$"),
				),
			),
			selectedRule(
				map[string]string{"enforce-hostnames": "true"},
				ingressRule(
					rules.ActionTypeDeny,
					targetTypes,
					hostnameByExact("blocked.example.com"),
				),
			),
			selectedRule(
				map[string]string{"allow-blocked-hostname": "true"},
				ingressRule(
					rules.ActionTypeAllow,
					targetTypes,
					hostnameByExact("blocked.example.com"),
				),
			),
			selectedRule(
				map[string]string{"combined-hostname-match": "true"},
				ingressRule(
					rules.ActionTypeAllow,
					targetTypes,
					hostnameByMatch(
						[]string{"combined-exact.example.net"},
						"^combined-[a-z0-9-]+\\.example\\.org$",
					),
				),
			),
			selectedRule(
				map[string]string{"negated-hostname-match": "true"},
				ingressRule(
					rules.ActionTypeDeny,
					targetTypes,
					hostnameByNegatedExpression("^([a-z0-9-]+\\.)*trusted\\.example$"),
				),
			),
		}
	}

	newTenant := func() *capsulev1beta2.Tenant {
		return &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name: "e2e-rule-ingress",
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
				Rules: baseTenantRules(),
			},
		}
	}

	type expectedIngressStatusRule struct {
		action    rules.ActionType
		hostnames []runtime.ExpressionMatch
	}

	expectNamespaceStatusRules := func(nsName string, want []expectedIngressStatusRule) {
		Eventually(func(g Gomega) {
			nsStatus := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(
				context.Background(),
				client.ObjectKey{Name: meta.NameForManagedRuleStatus(), Namespace: nsName},
				nsStatus,
			)).To(Succeed())

			g.Expect(nsStatus.Status.Rules).To(HaveLen(len(want)))

			for i, expected := range want {
				got := nsStatus.Status.Rules[i]
				g.Expect(got).NotTo(BeNil())
				g.Expect(got.Enforce).NotTo(BeNil())
				g.Expect(got.Enforce.Action).To(Equal(expected.action))
				g.Expect(got.Enforce.Ingress.Types).To(Equal(targetTypes))
				g.Expect(got.Enforce.Ingress.Hostnames).To(Equal(expected.hostnames))
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	auditStatusRule := expectedIngressStatusRule{
		action: rules.ActionTypeAudit,
		hostnames: []runtime.ExpressionMatch{
			hostnameByExact("audited.example.com", "audited.example.net"),
		},
	}

	allowStatusRule := expectedIngressStatusRule{
		action: rules.ActionTypeAllow,
		hostnames: []runtime.ExpressionMatch{
			hostnameByExact("internal.example.com"),
			hostnameByExpression("^[a-z0-9-]+\\.example\\.com$"),
		},
	}

	denyStatusRule := expectedIngressStatusRule{
		action: rules.ActionTypeDeny,
		hostnames: []runtime.ExpressionMatch{
			hostnameByExact("blocked.example.com"),
		},
	}

	exceptionStatusRule := expectedIngressStatusRule{
		action: rules.ActionTypeAllow,
		hostnames: []runtime.ExpressionMatch{
			hostnameByExact("blocked.example.com"),
		},
	}

	combinedStatusRule := expectedIngressStatusRule{
		action: rules.ActionTypeAllow,
		hostnames: []runtime.ExpressionMatch{
			hostnameByMatch(
				[]string{"combined-exact.example.net"},
				"^combined-[a-z0-9-]+\\.example\\.org$",
			),
		},
	}

	negatedStatusRule := expectedIngressStatusRule{
		action: rules.ActionTypeDeny,
		hostnames: []runtime.ExpressionMatch{
			hostnameByNegatedExpression("^([a-z0-9-]+\\.)*trusted\\.example$"),
		},
	}

	createNamespace := func(labels map[string]string, expectedRules ...expectedIngressStatusRule) *corev1.Namespace {
		if labels == nil {
			labels = map[string]string{}
		}
		labels[meta.TenantLabel] = tnt.GetName()

		ns := NewNamespace("", labels)
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
		expectNamespaceStatusRules(ns.Name, expectedRules)

		return ns
	}

	ingress := func(name string, hostnames ...string) *networkingv1.Ingress {
		obj := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: name},
		}

		for _, hostname := range hostnames {
			obj.Spec.Rules = append(obj.Spec.Rules, networkingv1.IngressRule{
				Host: hostname,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
							{
								Path:     "/",
								PathType: ptr.To(networkingv1.PathTypePrefix),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "tenant-api",
										Port: networkingv1.ServiceBackendPort{Number: 8080},
									},
								},
							},
						},
					},
				},
			})
		}

		return obj
	}

	hostlessIngress := func(name string) *networkingv1.Ingress {
		obj := ingress(name)
		obj.Spec.DefaultBackend = &networkingv1.IngressBackend{
			Service: &networkingv1.IngressServiceBackend{
				Name: "tenant-api",
				Port: networkingv1.ServiceBackendPort{Number: 8080},
			},
		}

		return obj
	}

	createIngressAndExpectAllowed := func(cs kubernetes.Interface, nsName string, obj *networkingv1.Ingress) {
		EventuallyCreation(func() error {
			_, err := cs.NetworkingV1().Ingresses(nsName).Create(context.Background(), obj, metav1.CreateOptions{})

			return err
		}).Should(Succeed())
	}

	createIngressAndExpectDenied := func(
		cs kubernetes.Interface,
		nsName string,
		obj *networkingv1.Ingress,
		substrings ...string,
	) {
		base := obj.DeepCopy()
		baseName := base.Name

		Eventually(func() error {
			candidate := base.DeepCopy()
			candidate.Name = fmt.Sprintf("%s-%d", baseName, time.Now().UnixNano()%1e6)

			_, err := cs.NetworkingV1().Ingresses(nsName).Create(context.Background(), candidate, metav1.CreateOptions{})
			if err == nil {
				_ = cs.NetworkingV1().Ingresses(nsName).Delete(context.Background(), candidate.Name, metav1.DeleteOptions{})

				return fmt.Errorf("expected ingress create to be denied, but it succeeded")
			}
			if apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("unexpected AlreadyExists: %v", err)
			}

			for _, substring := range substrings {
				if !strings.Contains(err.Error(), substring) {
					return fmt.Errorf("expected error to contain %q, got: %s", substring, err)
				}
			}

			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	expectAuditEvent := func(cs kubernetes.Interface, nsName, kind, objectName string, substrings ...string) {
		Eventually(func() error {
			eventList, err := cs.EventsV1().Events(nsName).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				return err
			}

			for _, event := range eventList.Items {
				if event.Reason != events.ReasonNamespaceRuleAudit ||
					event.Regarding.Kind != kind ||
					event.Regarding.Name != objectName {
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

			return fmt.Errorf("expected audit event for %s %q containing %q", kind, objectName, substrings)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	requireOptionalAPI := func(nsName string, list client.ObjectList, description string) {
		if err := k8sClient.List(context.Background(), list, client.InNamespace(nsName)); err != nil {
			if utils.IsUnsupportedAPI(err) {
				Skip(fmt.Sprintf("%s is not available: %s", description, err))
			}

			Expect(err).NotTo(HaveOccurred())
		}
	}

	BeforeAll(func() {
		utilruntime.Must(gatewayv1.Install(scheme.Scheme))
	})

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

	It("projects ingress rules into namespace RuleStatus", func() {
		createNamespace(
			map[string]string{"enforce-hostnames": "true"},
			auditStatusRule,
			allowStatusRule,
			denyStatusRule,
		)
	})

	It("allows exact and regex hostnames across Ingress rules and TLS hosts", func() {
		ns := createNamespace(
			map[string]string{"enforce-hostnames": "true"},
			auditStatusRule,
			allowStatusRule,
			denyStatusRule,
		)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		obj := ingress("allowed-hostnames", "internal.example.com", "api.example.com")
		obj.Spec.TLS = []networkingv1.IngressTLS{{Hosts: []string{"api.example.com"}}}

		createIngressAndExpectAllowed(cs, ns.Name, obj)
	})

	It("supports exact and regex alternatives in the same hostname matcher", func() {
		ns := createNamespace(
			map[string]string{"combined-hostname-match": "true"},
			auditStatusRule,
			combinedStatusRule,
		)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createIngressAndExpectAllowed(cs, ns.Name, ingress("combined-exact", "combined-exact.example.net"))
		createIngressAndExpectAllowed(cs, ns.Name, ingress("combined-regex", "combined-team-a.example.org"))
		createIngressAndExpectDenied(cs, ns.Name, ingress("combined-miss", "combined.example.com"),
			"combined.example.com",
			"not allowed",
			"combined-exact.example.net",
			"combined-[a-z0-9-]+",
		)
	})

	It("applies negation to hostname expressions", func() {
		ns := createNamespace(
			map[string]string{"negated-hostname-match": "true"},
			auditStatusRule,
			negatedStatusRule,
		)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createIngressAndExpectAllowed(cs, ns.Name, ingress("negated-trusted", "api.trusted.example"))
		createIngressAndExpectDenied(cs, ns.Name, ingress("negated-denied", "api.untrusted.example"),
			"api.untrusted.example",
			"denied",
			"not exp:",
		)
	})

	It("denies a hostname outside the allow-list", func() {
		ns := createNamespace(
			map[string]string{"enforce-hostnames": "true"},
			auditStatusRule,
			allowStatusRule,
			denyStatusRule,
		)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createIngressAndExpectDenied(cs, ns.Name, ingress("allow-miss", "api.example.net"),
			"api.example.net",
			"spec.rules[0].host",
			"not allowed",
		)
	})

	It("reports the index of a rejected hostname in a multi-rule Ingress", func() {
		ns := createNamespace(
			map[string]string{"enforce-hostnames": "true"},
			auditStatusRule,
			allowStatusRule,
			denyStatusRule,
		)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createIngressAndExpectDenied(
			cs,
			ns.Name,
			ingress("multi-rule-allow-miss", "api.example.com", "api.example.net"),
			"api.example.net",
			"spec.rules[1].host",
			"not allowed",
		)
	})

	It("denies a disallowed TLS hostname even when the routing hostname is allowed", func() {
		ns := createNamespace(
			map[string]string{"enforce-hostnames": "true"},
			auditStatusRule,
			allowStatusRule,
			denyStatusRule,
		)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		obj := ingress("tls-allow-miss", "api.example.com")
		obj.Spec.TLS = []networkingv1.IngressTLS{{Hosts: []string{"legacy.example.net"}}}

		createIngressAndExpectDenied(cs, ns.Name, obj,
			"legacy.example.net",
			"spec.tls[0].hosts[0]",
			"not allowed",
		)
	})

	It("reports the index of a rejected hostname in a multi-host TLS entry", func() {
		ns := createNamespace(
			map[string]string{"enforce-hostnames": "true"},
			auditStatusRule,
			allowStatusRule,
			denyStatusRule,
		)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		obj := ingress("multi-tls-allow-miss", "api.example.com")
		obj.Spec.TLS = []networkingv1.IngressTLS{{
			Hosts: []string{"api.example.com", "legacy.example.net"},
		}}

		createIngressAndExpectDenied(cs, ns.Name, obj,
			"legacy.example.net",
			"spec.tls[0].hosts[1]",
			"not allowed",
		)
	})

	It("applies later deny and namespace-selected allow precedence", func() {
		deniedNS := createNamespace(
			map[string]string{"enforce-hostnames": "true"},
			auditStatusRule,
			allowStatusRule,
			denyStatusRule,
		)
		allowedNS := createNamespace(
			map[string]string{
				"enforce-hostnames":      "true",
				"allow-blocked-hostname": "true",
			},
			auditStatusRule,
			allowStatusRule,
			denyStatusRule,
			exceptionStatusRule,
		)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createIngressAndExpectDenied(cs, deniedNS.Name, ingress("blocked-denied", "blocked.example.com"),
			"blocked.example.com",
			"denied",
		)
		createIngressAndExpectAllowed(cs, allowedNS.Name, ingress("blocked-allowed", "blocked.example.com"))
	})

	It("denies an update that changes an allowed hostname to an allow-list miss", func() {
		ns := createNamespace(
			map[string]string{"enforce-hostnames": "true"},
			auditStatusRule,
			allowStatusRule,
			denyStatusRule,
		)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		obj := ingress("hostname-update", "api.example.com")

		createIngressAndExpectAllowed(cs, ns.Name, obj)

		Eventually(func() error {
			current, err := cs.NetworkingV1().Ingresses(ns.Name).Get(context.Background(), obj.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			current.Spec.Rules[0].Host = "api.example.net"

			_, err = cs.NetworkingV1().Ingresses(ns.Name).Update(context.Background(), current, metav1.UpdateOptions{})
			if err == nil {
				return fmt.Errorf("expected ingress update to be denied, but it succeeded")
			}
			for _, substring := range []string{"api.example.net", "spec.rules[0].host", "not allowed"} {
				if !strings.Contains(err.Error(), substring) {
					return fmt.Errorf("expected error to contain %q, got: %s", substring, err)
				}
			}

			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("audits a matching hostname without blocking admission", func() {
		ns := createNamespace(nil, auditStatusRule)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		obj := ingress("hostname-audited", "audited.example.com")

		createIngressAndExpectAllowed(cs, ns.Name, obj)
		expectAuditEvent(clusterAdminClient(), ns.Name, "Ingress", obj.Name,
			`ingress hostname "audited.example.com"`,
			"spec.rules[0].host",
			"matched audit namespace rule",
		)
	})

	It("emits an audit event even when an allow-list denies the request", func() {
		ns := createNamespace(
			map[string]string{"enforce-hostnames": "true"},
			auditStatusRule,
			allowStatusRule,
			denyStatusRule,
		)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		obj := ingress("audited-and-denied", "audited.example.net")

		_, err := cs.NetworkingV1().Ingresses(ns.Name).Create(context.Background(), obj, metav1.CreateOptions{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("audited.example.net"))
		Expect(err.Error()).To(ContainSubstring("not allowed"))

		expectAuditEvent(clusterAdminClient(), ns.Name, "Ingress", obj.Name,
			`ingress hostname "audited.example.net"`,
			"matched audit namespace rule",
		)
	})

	It("admits an empty hostname for audit-only rules and emits an audit event", func() {
		ns := createNamespace(nil, auditStatusRule)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		obj := hostlessIngress("empty-hostname-audited")

		createIngressAndExpectAllowed(cs, ns.Name, obj)
		expectAuditEvent(clusterAdminClient(), ns.Name, "Ingress", obj.Name,
			"empty hostname detected",
			"spec.rules[].host",
			"audit namespace rule",
		)
	})

	It("audits an empty TLS hosts entry without blocking audit-only admission", func() {
		ns := createNamespace(nil, auditStatusRule)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		obj := ingress("empty-tls-hosts-audited", "neutral.example.org")
		obj.Spec.TLS = []networkingv1.IngressTLS{{SecretName: "tenant-api-tls"}}

		createIngressAndExpectAllowed(cs, ns.Name, obj)
		expectAuditEvent(clusterAdminClient(), ns.Name, "Ingress", obj.Name,
			"empty hostname detected",
			"spec.tls[0].hosts",
			"audit namespace rule",
		)
	})

	It("denies an empty TLS hosts entry when allow or deny rules apply", func() {
		ns := createNamespace(
			map[string]string{"enforce-hostnames": "true"},
			auditStatusRule,
			allowStatusRule,
			denyStatusRule,
		)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		obj := ingress("empty-tls-hosts-denied", "api.example.com")
		obj.Spec.TLS = []networkingv1.IngressTLS{{SecretName: "tenant-api-tls"}}

		createIngressAndExpectDenied(cs, ns.Name, obj,
			"hostname is required",
			"spec.tls[0].hosts",
			"Ingress",
		)
	})

	It("denies an empty hostname when allow or deny rules apply", func() {
		ns := createNamespace(
			map[string]string{"enforce-hostnames": "true"},
			auditStatusRule,
			allowStatusRule,
			denyStatusRule,
		)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createIngressAndExpectDenied(cs, ns.Name, hostlessIngress("empty-hostname-denied"),
			"hostname is required",
			"spec.rules[].host",
			"Ingress",
		)
	})

	It("enforces every HTTPRoute hostname when Gateway API is available", func() {
		ns := createNamespace(
			map[string]string{"enforce-hostnames": "true"},
			auditStatusRule,
			allowStatusRule,
			denyStatusRule,
		)

		requireOptionalAPI(ns.Name, &gatewayv1.HTTPRouteList{}, "Gateway API HTTPRoute")

		owner := impersonationClient(ownerName, withDefaultGroups(nil))
		allowed := &gatewayv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{Name: "route-hostnames-allowed", Namespace: ns.Name},
			Spec: gatewayv1.HTTPRouteSpec{
				Hostnames: []gatewayv1.Hostname{"api.example.com", "internal.example.com"},
			},
		}
		EventuallyCreation(func() error {
			return owner.Create(context.Background(), allowed)
		}).Should(Succeed())

		Eventually(func() error {
			denied := &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("route-hostnames-denied-%d", time.Now().UnixNano()%1e6),
					Namespace: ns.Name,
				},
				Spec: gatewayv1.HTTPRouteSpec{
					Hostnames: []gatewayv1.Hostname{"api.example.com", "api.example.net"},
				},
			}

			err := owner.Create(context.Background(), denied)
			if err == nil {
				_ = owner.Delete(context.Background(), denied)

				return fmt.Errorf("expected HTTPRoute create to be denied, but it succeeded")
			}
			for _, substring := range []string{"api.example.net", "spec.hostnames[1]", "not allowed"} {
				if !strings.Contains(err.Error(), substring) {
					return fmt.Errorf("expected error to contain %q, got: %s", substring, err)
				}
			}

			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("audits or rejects an HTTPRoute without hostnames according to action", func() {
		auditNS := createNamespace(nil, auditStatusRule)
		enforceNS := createNamespace(
			map[string]string{"enforce-hostnames": "true"},
			auditStatusRule,
			allowStatusRule,
			denyStatusRule,
		)
		requireOptionalAPI(auditNS.Name, &gatewayv1.HTTPRouteList{}, "Gateway API HTTPRoute")

		owner := impersonationClient(ownerName, withDefaultGroups(nil))
		audited := &gatewayv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{Name: "route-empty-hostnames-audited", Namespace: auditNS.Name},
		}
		EventuallyCreation(func() error {
			return owner.Create(context.Background(), audited)
		}).Should(Succeed())
		expectAuditEvent(clusterAdminClient(), auditNS.Name, "HTTPRoute", audited.Name,
			"empty hostname detected",
			"spec.hostnames[]",
			"audit namespace rule",
		)

		denied := &gatewayv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{Name: "route-empty-hostnames-denied", Namespace: enforceNS.Name},
		}
		err := owner.Create(context.Background(), denied)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("hostname is required"))
		Expect(err.Error()).To(ContainSubstring("spec.hostnames[]"))
		Expect(err.Error()).To(ContainSubstring("HTTPRoute"))
	})

	It("enforces every Gateway listener hostname when Gateway API is available", func() {
		ns := createNamespace(
			map[string]string{"enforce-hostnames": "true"},
			auditStatusRule,
			allowStatusRule,
			denyStatusRule,
		)
		requireOptionalAPI(ns.Name, &gatewayv1.GatewayList{}, "Gateway API Gateway")

		owner := impersonationClient(ownerName, withDefaultGroups(nil))
		allowed := &gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{Name: "gateway-hostnames-allowed", Namespace: ns.Name},
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: "unmanaged-e2e-class",
				Listeners: []gatewayv1.Listener{
					{
						Name:     "http",
						Protocol: gatewayv1.HTTPProtocolType,
						Port:     80,
						Hostname: ptr.To(gatewayv1.Hostname("api.example.com")),
					},
					{
						Name:     "http-alt",
						Protocol: gatewayv1.HTTPProtocolType,
						Port:     8080,
						Hostname: ptr.To(gatewayv1.Hostname("internal.example.com")),
					},
				},
			},
		}
		EventuallyCreation(func() error {
			return owner.Create(context.Background(), allowed)
		}).Should(Succeed())

		Eventually(func() error {
			denied := allowed.DeepCopy()
			denied.ResourceVersion = ""
			denied.Name = fmt.Sprintf("gateway-hostnames-denied-%d", time.Now().UnixNano()%1e6)
			denied.Spec.Listeners[1].Hostname = ptr.To(gatewayv1.Hostname("api.example.net"))

			err := owner.Create(context.Background(), denied)
			if err == nil {
				_ = owner.Delete(context.Background(), denied)

				return fmt.Errorf("expected Gateway create to be denied, but it succeeded")
			}
			for _, substring := range []string{"api.example.net", "spec.listeners[1].hostname", "not allowed"} {
				if !strings.Contains(err.Error(), substring) {
					return fmt.Errorf("expected error to contain %q, got: %s", substring, err)
				}
			}

			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("audits a Gateway listener without a hostname without blocking admission", func() {
		ns := createNamespace(nil, auditStatusRule)
		requireOptionalAPI(ns.Name, &gatewayv1.GatewayList{}, "Gateway API Gateway")

		owner := impersonationClient(ownerName, withDefaultGroups(nil))
		obj := &gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{Name: "gateway-empty-hostname-audited", Namespace: ns.Name},
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: "unmanaged-e2e-class",
				Listeners: []gatewayv1.Listener{
					{
						Name:     "http",
						Protocol: gatewayv1.HTTPProtocolType,
						Port:     80,
					},
				},
			},
		}
		EventuallyCreation(func() error {
			return owner.Create(context.Background(), obj)
		}).Should(Succeed())
		expectAuditEvent(clusterAdminClient(), ns.Name, "Gateway", obj.Name,
			"empty hostname detected",
			"spec.listeners[0].hostname",
			"audit namespace rule",
		)
	})
})
