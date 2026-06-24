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
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsuleapi "github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

var _ = Describe("enforcing service namespace rules", Ordered, Label("tenant", "rules", "enforce", "services"), func() {
	const ownerName = "e2e-rules-services"

	var (
		tnt         *capsulev1beta2.Tenant
		tenantRules []*rules.NamespaceRuleBodyTenant
	)

	externalNameByExpression := func(expression string) capsuleapi.ExpressionMatch {
		return capsuleapi.ExpressionMatch{
			ExpressionRegex: capsuleapi.ExpressionRegex{
				Expression: expression,
			},
		}
	}

	externalNameByNegatedExpression := func(expression string) capsuleapi.ExpressionMatch {
		return capsuleapi.ExpressionMatch{
			ExpressionRegex: capsuleapi.ExpressionRegex{
				Expression: expression,
				Negate:     true,
			},
		}
	}

	externalNameByExact := func(exact ...string) capsuleapi.ExpressionMatch {
		return capsuleapi.ExpressionMatch{
			Exact: exact,
		}
	}

	externalNameByMatch := func(exact []string, expression string) capsuleapi.ExpressionMatch {
		return capsuleapi.ExpressionMatch{
			ExpressionRegex: capsuleapi.ExpressionRegex{
				Expression: expression,
			},
			Exact: exact,
		}
	}

	serviceTypeRule := func(action rules.ActionType, serviceTypes ...rules.ServiceType) *rules.NamespaceRuleBodyTenant {
		return &rules.NamespaceRuleBodyTenant{
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Enforce: &rules.NamespaceRuleEnforceBody{
					Action: action,
					Services: rules.NamespaceRuleEnforceServicesBody{
						Types: serviceTypes,
					},
				},
			},
		}
	}

	loadBalancerCIDRRule := func(action rules.ActionType, cidrs ...string) *rules.NamespaceRuleBodyTenant {
		return &rules.NamespaceRuleBodyTenant{
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Enforce: &rules.NamespaceRuleEnforceBody{
					Action: action,
					Services: rules.NamespaceRuleEnforceServicesBody{
						LoadBalancers: &rules.ServiceLoadBalancerRule{
							CIDRs: cidrs,
						},
					},
				},
			},
		}
	}

	externalNameRule := func(action rules.ActionType, hostnames ...capsuleapi.ExpressionMatch) *rules.NamespaceRuleBodyTenant {
		return &rules.NamespaceRuleBodyTenant{
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Enforce: &rules.NamespaceRuleEnforceBody{
					Action: action,
					Services: rules.NamespaceRuleEnforceServicesBody{
						ExternalNames: &rules.ServiceExternalNameRule{
							Hostnames: hostnames,
						},
					},
				},
			},
		}
	}

	nodePortRule := func(action rules.ActionType, ports ...rules.ServiceNodePortRange) *rules.NamespaceRuleBodyTenant {
		return &rules.NamespaceRuleBodyTenant{
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Enforce: &rules.NamespaceRuleEnforceBody{
					Action: action,
					Services: rules.NamespaceRuleEnforceServicesBody{
						NodePorts: &rules.ServiceNodePortRule{
							Ports: ports,
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
			serviceTypeRule(
				rules.ActionTypeAllow,
				rules.ServiceTypeClusterIP,
				rules.ServiceTypeNodePort,
				rules.ServiceTypeLoadBalancer,
				rules.ServiceTypeExternalName,
			),
			loadBalancerCIDRRule(
				rules.ActionTypeAllow,
				"10.0.0.2/32",
				"10.0.1.0/24",
			),
			externalNameRule(
				rules.ActionTypeAllow,
				externalNameByExact("internal.git.com"),
				externalNameByExpression(".*\\.example\\.com"),
				externalNameByMatch(
					[]string{"combined.internal.git.com"},
					"combined\\..*\\.example\\.com",
				),
			),
			nodePortRule(
				rules.ActionTypeAllow,
				rules.ServiceNodePortRange{
					From: 30000,
					To:   30100,
				},
				rules.ServiceNodePortRange{
					From: 30500,
					To:   30500,
				},
			),
			nodePortRule(
				rules.ActionTypeDeny,
				rules.ServiceNodePortRange{
					From: 30090,
					To:   30090,
				},
			),
			loadBalancerCIDRRule(
				rules.ActionTypeDeny,
				"10.0.66.0/24",
			),
			externalNameRule(
				rules.ActionTypeAudit,
				externalNameByExpression("audit\\..*"),
			),
			selectedRule(
				map[string]string{
					"environment": "prod",
				},
				loadBalancerCIDRRule(
					rules.ActionTypeAllow,
					"10.0.171.0/24",
				),
			),
			selectedRule(
				map[string]string{
					"external-policy": "restricted",
				},
				externalNameRule(
					rules.ActionTypeDeny,
					externalNameByExact("blocked.example.com"),
				),
			),
			selectedRule(
				map[string]string{
					"negate": "true",
				},
				externalNameRule(
					rules.ActionTypeDeny,
					externalNameByNegatedExpression("trusted\\..*"),
				),
			),
			selectedRule(
				map[string]string{
					"negate": "true",
				},
				externalNameRule(
					rules.ActionTypeAllow,
					externalNameByExpression("trusted\\..*"),
				),
			),
		}
	}

	newTenant := func() *capsulev1beta2.Tenant {
		return &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name: "e2e-rule-services",
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
				ServiceOptions: &capsuleapi.ServiceOptions{
					AllowedServices: &capsuleapi.AllowedServices{
						ExternalName: ptr.To(true),
						LoadBalancer: ptr.To(true),
						NodePort:     ptr.To(true),
					},
				},
				Rules: tenantRules,
			},
		}
	}

	type expectedServiceStatusRule struct {
		action              rules.ActionType
		types               []rules.ServiceType
		loadBalancerCIDRs   []string
		nodePortRanges      []rules.ServiceNodePortRange
		externalExpressions []string
		externalExact       [][]string
		externalNegated     []bool
	}

	expectNamespaceStatusRules := func(nsName string, want []expectedServiceStatusRule) {
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

				if len(expected.types) == 0 {
					g.Expect(got.Enforce.Services.Types).To(BeEmpty())
				} else {
					g.Expect(got.Enforce.Services.Types).To(Equal(expected.types))
				}

				if len(expected.loadBalancerCIDRs) == 0 {
					if got.Enforce.Services.LoadBalancers != nil {
						g.Expect(got.Enforce.Services.LoadBalancers.CIDRs).To(BeEmpty())
					}
				} else {
					g.Expect(got.Enforce.Services.LoadBalancers).NotTo(BeNil())
					g.Expect(got.Enforce.Services.LoadBalancers.CIDRs).To(Equal(expected.loadBalancerCIDRs))
				}

				if len(expected.nodePortRanges) == 0 {
					if got.Enforce.Services.NodePorts != nil {
						g.Expect(got.Enforce.Services.NodePorts.Ports).To(BeEmpty())
					}
				} else {
					g.Expect(got.Enforce.Services.NodePorts).NotTo(BeNil())
					g.Expect(got.Enforce.Services.NodePorts.Ports).To(Equal(expected.nodePortRanges))
				}

				wantHostnames := len(expected.externalExpressions)
				if len(expected.externalExact) > wantHostnames {
					wantHostnames = len(expected.externalExact)
				}

				if wantHostnames == 0 {
					if got.Enforce.Services.ExternalNames != nil {
						g.Expect(got.Enforce.Services.ExternalNames.Hostnames).To(BeEmpty())
					}

					continue
				}

				g.Expect(got.Enforce.Services.ExternalNames).NotTo(BeNil())
				g.Expect(got.Enforce.Services.ExternalNames.Hostnames).To(HaveLen(wantHostnames))

				for j := 0; j < wantHostnames; j++ {
					match := got.Enforce.Services.ExternalNames.Hostnames[j]

					if len(expected.externalExpressions) > j {
						g.Expect(match.Expression).To(Equal(expected.externalExpressions[j]))
					} else {
						g.Expect(match.Expression).To(BeEmpty())
					}

					if len(expected.externalExact) > j {
						g.Expect(match.Exact).To(Equal(expected.externalExact[j]))
					} else {
						g.Expect(match.Exact).To(BeEmpty())
					}

					if len(expected.externalNegated) > j {
						g.Expect(match.Negate).To(Equal(expected.externalNegated[j]))
					} else {
						g.Expect(match.Negate).To(BeFalse())
					}
				}
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	baseStatusRules := func() []expectedServiceStatusRule {
		return []expectedServiceStatusRule{
			{
				action: rules.ActionTypeAllow,
				types: []rules.ServiceType{
					rules.ServiceTypeClusterIP,
					rules.ServiceTypeNodePort,
					rules.ServiceTypeLoadBalancer,
					rules.ServiceTypeExternalName,
				},
			},
			{
				action: rules.ActionTypeAllow,
				loadBalancerCIDRs: []string{
					"10.0.0.2/32",
					"10.0.1.0/24",
				},
			},
			{
				action: rules.ActionTypeAllow,
				externalExpressions: []string{
					"",
					".*\\.example\\.com",
					"combined\\..*\\.example\\.com",
				},
				externalExact: [][]string{
					{
						"internal.git.com",
					},
					nil,
					{
						"combined.internal.git.com",
					},
				},
			},
			{
				action: rules.ActionTypeAllow,
				nodePortRanges: []rules.ServiceNodePortRange{
					{
						From: 30000,
						To:   30100,
					},
					{
						From: 30500,
						To:   30500,
					},
				},
			},
			{
				action: rules.ActionTypeDeny,
				nodePortRanges: []rules.ServiceNodePortRange{
					{
						From: 30090,
						To:   30090,
					},
				},
			},
			{
				action: rules.ActionTypeDeny,
				loadBalancerCIDRs: []string{
					"10.0.66.0/24",
				},
			},
			{
				action: rules.ActionTypeAudit,
				externalExpressions: []string{
					"audit\\..*",
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

	createServiceAndExpectAllowed := func(cs kubernetes.Interface, nsName string, svc *corev1.Service) {
		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Services(nsName).Create(context.Background(), svc, metav1.CreateOptions{})

			return err
		}).Should(Succeed())
	}

	updateServiceAndExpectDenied := func(
		cs kubernetes.Interface,
		nsName string,
		svcName string,
		mutate func(*corev1.Service),
		substrings ...string,
	) {
		Eventually(func() error {
			svc, err := cs.CoreV1().Services(nsName).Get(context.Background(), svcName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			mutate(svc)

			_, err = cs.CoreV1().Services(nsName).Update(context.Background(), svc, metav1.UpdateOptions{})
			if err == nil {
				return fmt.Errorf("expected service update to be denied, but it succeeded")
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
		serviceName string,
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

				if e.InvolvedObject.Kind != "Service" {
					continue
				}

				eventServiceName := e.InvolvedObject.Name
				if eventServiceName != serviceName && !strings.HasPrefix(eventServiceName, serviceName+"-") {
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
				"expected audit event for service %q containing %q",
				serviceName,
				substrings,
			)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	servicePort := func() corev1.ServicePort {
		return corev1.ServicePort{
			Name:       "http",
			Protocol:   corev1.ProtocolTCP,
			Port:       8080,
			TargetPort: intstr.FromInt(8080),
		}
	}

	clusterIPService := func(name string) *corev1.Service {
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				Ports: []corev1.ServicePort{
					servicePort(),
				},
			},
		}
	}

	nodePortService := func(name string, nodePort int32) *corev1.Service {
		port := servicePort()
		port.NodePort = nodePort

		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
				Ports: []corev1.ServicePort{
					port,
				},
			},
		}
	}

	nodePortServiceWithoutExplicitNodePort := func(name string) *corev1.Service {
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
				Ports: []corev1.ServicePort{
					servicePort(),
				},
			},
		}
	}

	createServiceAndExpectDeniedOneOf := func(
		cs kubernetes.Interface,
		nsName string,
		svc *corev1.Service,
		alternatives ...[]string,
	) {
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

			for _, alternative := range alternatives {
				matches := true

				for _, substring := range alternative {
					if !strings.Contains(msg, substring) {
						matches = false

						break
					}
				}

				if matches {
					return nil
				}
			}

			return fmt.Errorf(
				"expected error to match one of %v, got: %s",
				alternatives,
				msg,
			)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	loadBalancerService := func(
		name string,
		loadBalancerIP string,
		sourceRanges []string,
		allocateLoadBalancerNodePorts *bool,
	) *corev1.Service {
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: corev1.ServiceSpec{
				Type:                          corev1.ServiceTypeLoadBalancer,
				LoadBalancerIP:                loadBalancerIP,
				LoadBalancerSourceRanges:      sourceRanges,
				AllocateLoadBalancerNodePorts: allocateLoadBalancerNodePorts,
				Ports: []corev1.ServicePort{
					servicePort(),
				},
			},
		}
	}

	externalNameService := func(name string, externalName string) *corev1.Service {
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: corev1.ServiceSpec{
				Type:         corev1.ServiceTypeExternalName,
				ExternalName: externalName,
				Ports: []corev1.ServicePort{
					servicePort(),
				},
			},
		}
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

	It("requires or validates LoadBalancer nodePorts when allocation is enabled and nodePort ranges are configured", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectDeniedOneOf(
			cs,
			ns.Name,
			loadBalancerService("lb-auto-node-port-denied", "10.0.0.2", nil, nil),
			[]string{
				"requires explicit spec.ports[*].nodePort",
				"nodePort ranges are enforced by namespace rule",
			},
			[]string{
				"nodePort",
				"not allowed by namespace rule",
				"Allowed ranges",
				"30000-30100",
				"30500",
			},
		)
	})

	It("stores matching tenant service rules as independent status rule blocks", func() {
		ns := createNamespace(nil)

		expectNamespaceStatusRules(ns.GetName(), baseStatusRules())
	})

	It("stores namespace-selector matched service rules as additional independent status rule blocks", func() {
		ns := createNamespace(map[string]string{
			"environment": "prod",
		})

		want := baseStatusRules()
		want = append(want, expectedServiceStatusRule{
			action: rules.ActionTypeAllow,
			loadBalancerCIDRs: []string{
				"10.0.171.0/24",
			},
		})

		expectNamespaceStatusRules(ns.GetName(), want)
	})

	It("stores namespace-selector matched negated service rules as independent status rule blocks", func() {
		ns := createNamespace(map[string]string{
			"negate": "true",
		})

		want := baseStatusRules()
		want = append(want,
			expectedServiceStatusRule{
				action: rules.ActionTypeDeny,
				externalExpressions: []string{
					"trusted\\..*",
				},
				externalNegated: []bool{
					true,
				},
			},
			expectedServiceStatusRule{
				action: rules.ActionTypeAllow,
				externalExpressions: []string{
					"trusted\\..*",
				},
			},
		)

		expectNamespaceStatusRules(ns.GetName(), want)
	})

	It("allows a listed ClusterIP service type", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			serviceTypeRule(
				rules.ActionTypeAllow,
				rules.ServiceTypeClusterIP,
			),
		})

		ns := createNamespace(nil)

		expectNamespaceStatusRules(ns.Name, []expectedServiceStatusRule{
			{
				action: rules.ActionTypeAllow,
				types: []rules.ServiceType{
					rules.ServiceTypeClusterIP,
				},
			},
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectAllowed(cs, ns.Name, clusterIPService("cluster-ip-allowed"))
	})

	It("denies a service type missing from services.types and reports allowed service types", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			serviceTypeRule(
				rules.ActionTypeAllow,
				rules.ServiceTypeClusterIP,
			),
		})

		ns := createNamespace(nil)

		expectNamespaceStatusRules(ns.Name, []expectedServiceStatusRule{
			{
				action: rules.ActionTypeAllow,
				types: []rules.ServiceType{
					rules.ServiceTypeClusterIP,
				},
			},
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectDenied(cs, ns.Name, externalNameService("external-name-type-denied", "internal.git.com"),
			"service type",
			"ExternalName",
			"not allowed",
			"Allowed service types",
			"ClusterIP",
		)
	})

	It("allows exact, regex, and combined ExternalName hostname matchers", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectAllowed(cs, ns.Name, externalNameService("external-exact-allowed", "internal.git.com"))
		createServiceAndExpectAllowed(cs, ns.Name, externalNameService("external-regex-allowed", "api.example.com"))
		createServiceAndExpectAllowed(cs, ns.Name, externalNameService("external-combined-exact-allowed", "combined.internal.git.com"))
		createServiceAndExpectAllowed(cs, ns.Name, externalNameService("external-combined-regex-allowed", "combined.api.example.com"))
	})

	It("denies non-matching ExternalName hostnames and reports allowed hostname rules", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectDenied(cs, ns.Name, externalNameService("external-denied", "api.bad.com"),
			"externalName hostname",
			"api.bad.com",
			"spec.externalName",
			"not allowed",
			"Allowed hostnames",
			"exact: internal.git.com",
			"exp: .*\\.example\\.com",
		)
	})

	It("audits a matching ExternalName but still denies when no allow rule matches", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			serviceTypeRule(
				rules.ActionTypeAllow,
				rules.ServiceTypeExternalName,
			),
			externalNameRule(
				rules.ActionTypeAudit,
				externalNameByExpression("audit\\..*"),
			),
			externalNameRule(
				rules.ActionTypeAllow,
				externalNameByExpression("allowed\\..*"),
			),
		})

		ns := createNamespace(nil)

		expectNamespaceStatusRules(ns.Name, []expectedServiceStatusRule{
			{
				action: rules.ActionTypeAllow,
				types: []rules.ServiceType{
					rules.ServiceTypeExternalName,
				},
			},
			{
				action: rules.ActionTypeAudit,
				externalExpressions: []string{
					"audit\\..*",
				},
			},
			{
				action: rules.ActionTypeAllow,
				externalExpressions: []string{
					"allowed\\..*",
				},
			},
		})

		svc := externalNameService("external-audit-denied", "audit.internal")

		createServiceAndExpectDenied(clusterAdminClient(), ns.Name, svc,
			"externalName hostname",
			"audit.internal",
			"not allowed",
			"Allowed hostnames",
			"allowed\\..*",
		)

		expectAuditEvent(clusterAdminClient(), ns.Name, svc.Name,
			"matched audit",
			"audit\\..*",
		)
	})

	It("denies a later selected ExternalName deny rule after an earlier allow rule matched", func() {
		ns := createNamespace(map[string]string{
			"external-policy": "restricted",
		})
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectDenied(cs, ns.Name, externalNameService("external-selected-denied", "blocked.example.com"),
			"externalName hostname",
			"blocked.example.com",
			"denied",
			"exact: blocked.example.com",
		)
	})

	It("applies namespace-selector matched negated ExternalName rules after base rules", func() {
		ns := createNamespace(map[string]string{
			"negate": "true",
		})
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectDenied(cs, ns.Name, externalNameService("external-negated-denied", "api.example.com"),
			"externalName hostname",
			"api.example.com",
			"denied",
			"exp: trusted\\..*",
		)

		createServiceAndExpectAllowed(cs, ns.Name, externalNameService("external-negated-allowed", "trusted.api"))
	})

	It("allows LoadBalancer IPs and source ranges contained in configured CIDRs", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectAllowed(cs, ns.Name, loadBalancerService("lb-ip-allowed", "10.0.0.2", nil, ptr.To(false)))
		createServiceAndExpectAllowed(cs, ns.Name, loadBalancerService("lb-ip-range-allowed", "10.0.1.44", nil, ptr.To(false)))
		createServiceAndExpectAllowed(cs, ns.Name, loadBalancerService("lb-source-range-allowed", "", []string{"10.0.1.0/25"}, ptr.To(false)))
	})

	It("denies LoadBalancer IPs outside configured CIDRs and reports allowed CIDRs", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectDenied(cs, ns.Name, loadBalancerService("lb-ip-denied", "10.0.171.239", nil, ptr.To(false)),
			"loadBalancer CIDR",
			"10.0.171.239",
			"spec.loadBalancerIP",
			"not allowed",
			"Allowed CIDRs",
			"10.0.0.2/32",
			"10.0.1.0/24",
		)
	})

	It("denies LoadBalancer source ranges not fully contained in configured CIDRs", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectDenied(cs, ns.Name, loadBalancerService("lb-source-range-denied", "", []string{"10.0.1.0/23"}, ptr.To(false)),
			"loadBalancer CIDR",
			"10.0.1.0/23",
			"spec.loadBalancerSourceRanges[0]",
			"Allowed CIDRs",
		)
	})

	It("requires LoadBalancer IP or source ranges when CIDR constraints are configured", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectDenied(cs, ns.Name, loadBalancerService("lb-required-value-denied", "", nil, ptr.To(false)),
			"requires spec.loadBalancerIP or spec.loadBalancerSourceRanges",
			"loadBalancer CIDR constraints are enforced by namespace rule",
		)
	})

	It("denies a later LoadBalancer CIDR deny rule after an earlier allow rule matched", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectDenied(cs, ns.Name, loadBalancerService("lb-later-deny", "10.0.66.10", nil, ptr.To(false)),
			"loadBalancer CIDR",
			"10.0.66.10",
			"denied",
			"10.0.66.0/24",
		)
	})

	It("allows a later selected LoadBalancer allow rule to override an earlier allow miss", func() {
		ns := createNamespace(map[string]string{
			"environment": "prod",
		})
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectAllowed(cs, ns.Name, loadBalancerService("lb-prod-selected-allowed", "10.0.171.239", nil, ptr.To(false)))
	})

	It("allows explicit nodePorts inside configured ranges", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectAllowed(cs, ns.Name, nodePortService("node-port-range-allowed", 30080))
		createServiceAndExpectAllowed(cs, ns.Name, nodePortService("node-port-single-allowed", 30500))
	})

	It("denies explicit nodePorts outside configured ranges and reports allowed ranges", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectDenied(cs, ns.Name, nodePortService("node-port-range-denied", 32080),
			"nodePort",
			"32080",
			"not allowed",
			"Allowed ranges",
			"30000-30100",
			"30500",
		)
	})

	It("requires explicit nodePorts when nodePort ranges are configured", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectDeniedOneOf(
			cs,
			ns.Name,
			nodePortServiceWithoutExplicitNodePort("node-port-required-denied"),
			[]string{
				"requires explicit spec.ports[*].nodePort",
				"nodePort ranges are enforced by namespace rule",
			},
			[]string{
				"nodePort",
				"not allowed by namespace rule",
				"Allowed ranges",
				"30000-30100",
				"30500",
			},
		)
	})
	It("denies a later nodePort deny rule after an earlier allow rule matched", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectDenied(
			cs,
			ns.Name,
			nodePortService("node-port-later-deny", 30090),
			"nodePort",
			"30090",
			"denied",
			"30090",
		)
	})

	It("requires or validates LoadBalancer nodePorts when allocation is enabled and nodePort ranges are configured", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectDeniedOneOf(
			cs,
			ns.Name,
			loadBalancerService("lb-auto-node-port-denied", "10.0.0.2", nil, nil),
			[]string{
				"requires explicit spec.ports[*].nodePort",
				"nodePort ranges are enforced by namespace rule",
			},
			[]string{
				"nodePort",
				"not allowed by namespace rule",
				"Allowed ranges",
				"30000-30100",
				"30500",
			},
		)
	})

	It("does not require LoadBalancer nodePorts when allocation is explicitly disabled", func() {
		ns := createNamespace(nil)
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectAllowed(
			cs,
			ns.Name,
			loadBalancerService("lb-node-port-allocation-disabled", "10.0.0.2", nil, ptr.To(false)),
		)
	})

	It("denies an update when the new ExternalName no longer matches the allowed hostname rules", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			serviceTypeRule(
				rules.ActionTypeAllow,
				rules.ServiceTypeExternalName,
			),
			externalNameRule(
				rules.ActionTypeAllow,
				externalNameByExact("internal.git.com"),
			),
		})

		ns := createNamespace(nil)

		expectNamespaceStatusRules(ns.Name, []expectedServiceStatusRule{
			{
				action: rules.ActionTypeAllow,
				types: []rules.ServiceType{
					rules.ServiceTypeExternalName,
				},
			},
			{
				action: rules.ActionTypeAllow,
				externalExact: [][]string{
					{
						"internal.git.com",
					},
				},
			},
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		svc := externalNameService("external-update-denied", "internal.git.com")
		createServiceAndExpectAllowed(cs, ns.Name, svc)

		updateServiceAndExpectDenied(
			cs,
			ns.Name,
			svc.Name,
			func(svc *corev1.Service) {
				svc.Spec.ExternalName = "api.bad.com"
			},
			"externalName hostname",
			"api.bad.com",
			"not allowed",
			"Allowed hostnames",
		)
	})

	It("allows service creation when no matching service-specific constraint exists for the allowed type", func() {
		updateTenantRules([]*rules.NamespaceRuleBodyTenant{
			serviceTypeRule(
				rules.ActionTypeAllow,
				rules.ServiceTypeClusterIP,
				rules.ServiceTypeLoadBalancer,
			),
		})

		ns := createNamespace(nil)

		expectNamespaceStatusRules(ns.Name, []expectedServiceStatusRule{
			{
				action: rules.ActionTypeAllow,
				types: []rules.ServiceType{
					rules.ServiceTypeClusterIP,
					rules.ServiceTypeLoadBalancer,
				},
			},
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		createServiceAndExpectAllowed(
			cs,
			ns.Name,
			loadBalancerService("lb-no-cidr-rule-allowed", "", nil, ptr.To(false)),
		)
	})
})
