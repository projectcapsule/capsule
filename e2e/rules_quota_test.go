// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	tenantutils "github.com/projectcapsule/capsule/pkg/tenant"
)

var _ = Describe("rule-generated GlobalResourceQuota", Ordered, Label("resourcequota", "rules", "ledger", "skip-on-openshift"), func() {
	const (
		tenantName = "e2e-rule-quota"

		sharedCPUA = "e2e-rule-quota-cpu-a"
		sharedCPUB = "e2e-rule-quota-cpu-b"
		podCountA  = "e2e-rule-quota-pods-a"
		podCountB  = "e2e-rule-quota-pods-b"
		hpaCountA  = "e2e-rule-quota-hpa-a"
		hpaCountB  = "e2e-rule-quota-hpa-b"
		podLevelA  = "e2e-rule-quota-pod-level-a"
		podLevelB  = "e2e-rule-quota-pod-level-b"
		serviceA   = "e2e-rule-quota-services-a"
		serviceB   = "e2e-rule-quota-services-b"
		unselected = "e2e-rule-quota-unselected"
	)

	ctx := context.Background()
	owner := rbac.UserSpec{Name: tenantName, Kind: "User"}
	quotaScopes := []struct {
		quotaName   string
		selectorKey string
		namespaces  []string
		hard        corev1.ResourceList
	}{
		{
			quotaName:   "shared-cpu",
			selectorKey: "e2e.projectcapsule.dev/shared-cpu",
			namespaces:  []string{sharedCPUA, sharedCPUB},
			hard: corev1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("1"),
			},
		},
		{
			quotaName:   "pod-count",
			selectorKey: "e2e.projectcapsule.dev/pod-count",
			namespaces:  []string{podCountA, podCountB},
			hard: corev1.ResourceList{
				corev1.ResourcePods: resource.MustParse("1"),
			},
		},
		{
			quotaName:   "hpa-count",
			selectorKey: "e2e.projectcapsule.dev/hpa-count",
			namespaces:  []string{hpaCountA, hpaCountB},
			hard: corev1.ResourceList{
				corev1.ResourceName("count/horizontalpodautoscalers.autoscaling"): resource.MustParse("1"),
			},
		},
		{
			quotaName:   "pod-level-cpu",
			selectorKey: "e2e.projectcapsule.dev/pod-level-cpu",
			namespaces:  []string{podLevelA, podLevelB},
			hard: corev1.ResourceList{
				corev1.ResourceRequestsCPU: resource.MustParse("1"),
			},
		},
		{
			quotaName:   "service-count",
			selectorKey: "e2e.projectcapsule.dev/service-count",
			namespaces:  []string{serviceA, serviceB},
			hard: corev1.ResourceList{
				corev1.ResourceServices: resource.MustParse("5"),
			},
		},
	}
	ruleBodies := make([]*rules.NamespaceRuleBodyTenant, 0, len(quotaScopes))
	for _, scope := range quotaScopes {
		ruleBodies = append(ruleBodies, &rules.NamespaceRuleBodyTenant{
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Quota: []rules.ResourceQuotaRule{{
					Name:              scope.quotaName,
					ResourceQuotaSpec: corev1.ResourceQuotaSpec{Hard: scope.hard},
				}},
			},
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{scope.selectorKey: "true"},
			},
		})
	}

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name:   tenantName,
			Labels: map[string]string{"env": "e2e"},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{{CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: owner}}},
			Rules:  ruleBodies,
		},
	}

	BeforeAll(func() {
		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""

			return k8sClient.Create(ctx, tnt)
		}).Should(Succeed())
		TenantReadyTrue(tnt)

		for _, scope := range quotaScopes {
			for _, namespace := range scope.namespaces {
				ns := NewNamespace(namespace, map[string]string{
					meta.TenantLabel:  tenantName,
					scope.selectorKey: "true",
				})
				NamespaceCreation(ns, owner, defaultTimeoutInterval).Should(Succeed())
				NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
			}
		}
		By("creating a namespace outside every quota selector", func() {
			ns := NewNamespace(unselected, map[string]string{meta.TenantLabel: tenantName})
			NamespaceCreation(ns, owner, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
		})

		current := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(ctx, clientKey("", tenantName), current)).To(Succeed())
		tnt.UID = current.UID

		for _, scope := range quotaScopes {
			globalQuota := &capsulev1beta2.GlobalResourceQuota{}
			Eventually(func() error {
				return k8sClient.Get(
					ctx,
					clientKey("", tenantutils.RuleGlobalResourceQuotaName(tnt, scope.quotaName)),
					globalQuota,
				)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			quotaName := globalQuota.GetResourceQuotaName()
			for _, namespace := range scope.namespaces {
				Eventually(func(g Gomega) {
					quota := &corev1.ResourceQuota{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Namespace: namespace,
						Name:      quotaName,
					}, quota)).To(Succeed())
					g.Expect(quota.Spec.Hard).To(HaveLen(len(scope.hard)))
					for name, expected := range scope.hard {
						actual, found := quota.Spec.Hard[name]
						g.Expect(found).To(BeTrue(), "missing hard quota resource %s", name)
						g.Expect(actual.Cmp(expected)).To(Equal(0), "hard quota resource %s", name)
					}
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}

			ledgerName := globalQuota.GetLedgerName()
			Eventually(func(g Gomega) {
				ledger := &capsulev1beta2.QuantityLedger{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Namespace: ControllerNamespace,
					Name:      ledgerName,
				}, ledger)).To(Succeed())
				g.Expect(ledger.Status.ResourceQuota).NotTo(BeNil())
				g.Expect(ledger.Status.ResourceQuota.Initialized).To(BeTrue())
				g.Expect(ledger.Status.ResourceQuota.ObservedGeneration).To(Equal(globalQuota.Generation))
				g.Expect(ledger.Status.ResourceQuota.Namespaces).To(ConsistOf(scope.namespaces))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})

	AfterAll(func() {
		EventuallyDeletion(tnt)
	})

	It("enforces one shared limit during concurrent admissions across selected namespaces", func() {
		const total = 20

		cs := ownerClient(owner)
		results := make(chan error, total)

		for i := 0; i < total; i++ {
			go func(index int) {
				namespace := sharedCPUA
				if index%2 == 1 {
					namespace = sharedCPUB
				}

				pod := MakePod(
					namespace,
					fmt.Sprintf("rule-quota-concurrent-%02d", index),
					nil,
					nil,
					"registry.k8s.io/pause:3.10",
					"100m",
					"",
				)
				_, err := cs.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
				results <- err
			}(i)
		}

		var succeeded, failed int
		for i := 0; i < total; i++ {
			if err := <-results; err == nil {
				succeeded++
			} else {
				failed++
				Expect(err.Error()).To(ContainSubstring("exceeds GlobalResourceQuota"))
			}
		}

		Expect(succeeded).To(Equal(10))
		Expect(failed).To(Equal(10))

		By("not applying the rule quota outside the selected namespace set", func() {
			pod := MakePod(
				unselected,
				"rule-quota-unselected",
				nil,
				nil,
				"registry.k8s.io/pause:3.10",
				"2",
				"",
			)
			_, err := cs.CoreV1().Pods(unselected).Create(ctx, pod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	It("counts Pods without compute resources and intercepts the shared object limit", func() {
		cs := ownerClient(owner)

		first := MakePod(
			podCountA,
			"rule-quota-pod-count-first",
			nil,
			nil,
			"registry.k8s.io/pause:3.10",
			"",
			"",
		)
		_, err := cs.CoreV1().Pods(podCountA).Create(ctx, first, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		second := MakePod(
			podCountB,
			"rule-quota-pod-count-second",
			nil,
			nil,
			"registry.k8s.io/pause:3.10",
			"",
			"",
		)
		_, err = cs.CoreV1().Pods(podCountB).Create(ctx, second, metav1.CreateOptions{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exceeds GlobalResourceQuota"))
		Expect(err.Error()).To(ContainSubstring(
			"pods (requested=1, current=1, projected=2, hard=1, exceededBy=1)",
		))
	})

	It("counts HorizontalPodAutoscalers and intercepts the shared object limit", func() {
		cs := ownerClient(owner)
		makeHPA := func(namespace, name string) *autoscalingv2.HorizontalPodAutoscaler {
			return &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "quota-target",
					},
					MaxReplicas: 3,
				},
			}
		}

		_, err := cs.AutoscalingV2().HorizontalPodAutoscalers(hpaCountA).Create(
			ctx,
			makeHPA(hpaCountA, "rule-quota-hpa-first"),
			metav1.CreateOptions{},
		)
		Expect(err).NotTo(HaveOccurred())

		_, err = cs.AutoscalingV2().HorizontalPodAutoscalers(hpaCountB).Create(
			ctx,
			makeHPA(hpaCountB, "rule-quota-hpa-second"),
			metav1.CreateOptions{},
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exceeds GlobalResourceQuota"))
	})

	It("calculates and intercepts Pod-level resources on controller-generated Pods", func() {
		cs := ownerClient(owner)
		makeDeployment := func(namespace, name string) *appsv1.Deployment {
			deployment := MakeDeployment(namespace, name, 1, nil, "")
			deployment.Spec.Template.Spec.Resources = &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("600m"),
				},
			}

			return deployment
		}

		firstName := "rule-quota-pod-level-first"
		_, err := cs.AppsV1().Deployments(podLevelA).Create(
			ctx,
			makeDeployment(podLevelA, firstName),
			metav1.CreateOptions{},
		)
		Expect(err).NotTo(HaveOccurred())
		ExpectPodsForDeployment(ctx, podLevelA, firstName, 1)

		secondName := "rule-quota-pod-level-second"
		_, err = cs.AppsV1().Deployments(podLevelB).Create(
			ctx,
			makeDeployment(podLevelB, secondName),
			metav1.CreateOptions{},
		)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			failure, getErr := replicaSetFailureForDeployment(ctx, podLevelB, secondName)
			g.Expect(getErr).NotTo(HaveOccurred())
			g.Expect(failure).NotTo(BeNil())
			g.Expect(failure.Status).To(Equal(corev1.ConditionTrue))
			g.Expect(failure.Message).To(ContainSubstring("exceeds GlobalResourceQuota"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		ExpectPodsForDeployment(ctx, podLevelB, secondName, 0)
	})

	It("atomically stalls concurrent Service admissions at the shared limit", func() {
		const total = 10

		cs := ownerClient(owner)
		results := make(chan error, total)

		for i := 0; i < total; i++ {
			go func(index int) {
				namespace := serviceA
				if index%2 == 1 {
					namespace = serviceB
				}

				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      fmt.Sprintf("rule-quota-service-%02d", index),
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 80}},
					},
				}
				_, err := cs.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
				results <- err
			}(i)
		}

		var succeeded, failed int
		for i := 0; i < total; i++ {
			if err := <-results; err == nil {
				succeeded++
			} else {
				failed++
				Expect(err.Error()).To(ContainSubstring("exceeds GlobalResourceQuota"))
			}
		}

		Expect(succeeded).To(Equal(5))
		Expect(failed).To(Equal(5))
	})

	It("keeps the generated quota identity when rules are reordered and limits change", func() {
		quotaKey := clientKey("", tenantutils.RuleGlobalResourceQuotaName(tnt, "service-count"))
		marker := "e2e.projectcapsule.dev/stable-identity"

		Eventually(func() error {
			current := &capsulev1beta2.GlobalResourceQuota{}
			if err := k8sClient.Get(ctx, quotaKey, current); err != nil {
				return err
			}
			if current.Annotations == nil {
				current.Annotations = map[string]string{}
			}
			current.Annotations[marker] = "preserved"

			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, clientKey("", tenantName), current); err != nil {
				return err
			}

			var serviceRule *rules.NamespaceRuleBodyTenant
			remaining := make([]*rules.NamespaceRuleBodyTenant, 0, len(current.Spec.Rules)-1)
			for _, rule := range current.Spec.Rules {
				if rule != nil && len(rule.Quota) == 1 && rule.Quota[0].Name == "service-count" {
					serviceRule = rule
					continue
				}
				remaining = append(remaining, rule)
			}
			if serviceRule == nil {
				return fmt.Errorf("service-count quota rule was not found")
			}
			serviceRule.Quota[0].Hard[corev1.ResourceServices] = resource.MustParse("6")
			current.Spec.Rules = append([]*rules.NamespaceRuleBodyTenant{serviceRule}, remaining...)

			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		Eventually(func(g Gomega) {
			current := &capsulev1beta2.GlobalResourceQuota{}
			g.Expect(k8sClient.Get(ctx, quotaKey, current)).To(Succeed())
			g.Expect(current.Annotations).To(HaveKeyWithValue(marker, "preserved"))
			g.Expect(current.Labels).To(HaveKeyWithValue(meta.RuleQuotaLabel, "service-count"))
			actual := current.Spec.Quota.Hard[corev1.ResourceServices]
			g.Expect(actual.Cmp(resource.MustParse("6"))).To(Equal(0))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})
})

func clientKey(namespace, name string) types.NamespacedName {
	return types.NamespacedName{Namespace: namespace, Name: name}
}
