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
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

var _ = Describe("GlobalResourceQuota", Ordered, Label("globalresourcequota", "resourcequota", "ledger", "skip-on-openshift"), func() {
	const (
		tenantAName = "e2e-global-resource-quota-a"
		tenantBName = "e2e-global-resource-quota-b"

		computeQuotaName   = "e2e-global-resource-quota-compute"
		serviceQuotaName   = "e2e-global-resource-quota-services"
		countQuotaName     = "e2e-global-resource-quota-counts"
		ephemeralQuotaName = "e2e-global-resource-quota-ephemeral"

		computeA   = "e2e-global-quota-compute-a"
		computeB   = "e2e-global-quota-compute-b"
		serviceA   = "e2e-global-quota-services-a"
		serviceB   = "e2e-global-quota-services-b"
		countA     = "e2e-global-quota-counts-a"
		countB     = "e2e-global-quota-counts-b"
		ephemeralA = "e2e-global-quota-ephemeral-a"
		ephemeralB = "e2e-global-quota-ephemeral-b"

		computeSelector   = "e2e.projectcapsule.dev/global-quota-compute"
		serviceSelector   = "e2e.projectcapsule.dev/global-quota-services"
		countSelector     = "e2e.projectcapsule.dev/global-quota-counts"
		ephemeralSelector = "e2e.projectcapsule.dev/global-quota-ephemeral"
	)

	ctx := context.Background()
	tenantAOwner := rbac.UserSpec{Name: tenantAName, Kind: rbac.OwnerKind("User")}
	tenantBOwner := rbac.UserSpec{Name: tenantBName, Kind: rbac.OwnerKind("User")}
	tenantA := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name:   tenantAName,
			Labels: map[string]string{"env": "e2e"},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{{
				CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: tenantAOwner},
			}},
		},
	}
	tenantB := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name:   tenantBName,
			Labels: map[string]string{"env": "e2e"},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{{
				CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: tenantBOwner},
			}},
		},
	}
	tenants := []*capsulev1beta2.Tenant{tenantA, tenantB}
	computeHard := corev1.ResourceList{
		corev1.ResourceRequestsCPU:    resource.MustParse("1"),
		corev1.ResourceRequestsMemory: resource.MustParse("1Gi"),
		corev1.ResourceLimitsCPU:      resource.MustParse("2"),
		corev1.ResourceLimitsMemory:   resource.MustParse("2Gi"),
	}
	serviceHard := corev1.ResourceList{
		corev1.ResourceServices: resource.MustParse("5"),
	}
	countHard := corev1.ResourceList{
		corev1.ResourceSecrets:                                            resource.MustParse("2"),
		corev1.ResourceName("count/deployments.apps"):                     resource.MustParse("2"),
		corev1.ResourceName("count/horizontalpodautoscalers.autoscaling"): resource.MustParse("1"),
	}
	ephemeralHard := corev1.ResourceList{
		corev1.ResourceEphemeralStorage:         resource.MustParse("1Gi"),
		corev1.ResourceRequestsEphemeralStorage: resource.MustParse("1Gi"),
		corev1.ResourceLimitsEphemeralStorage:   resource.MustParse("2Gi"),
	}
	computeQuota := &capsulev1beta2.GlobalResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:   computeQuotaName,
			Labels: map[string]string{"env": "e2e"},
		},
		Spec: capsulev1beta2.GlobalResourceQuotaSpec{
			NamespaceSelectors: []selectors.NamespaceSelector{{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{computeSelector: "true"},
				},
			}},
			Quota: corev1.ResourceQuotaSpec{Hard: computeHard},
		},
	}
	serviceQuota := &capsulev1beta2.GlobalResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:   serviceQuotaName,
			Labels: map[string]string{"env": "e2e"},
		},
		Spec: capsulev1beta2.GlobalResourceQuotaSpec{
			NamespaceSelectors: []selectors.NamespaceSelector{{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{serviceSelector: "true"},
				},
			}},
			Quota: corev1.ResourceQuotaSpec{Hard: serviceHard},
		},
	}
	countQuota := &capsulev1beta2.GlobalResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:   countQuotaName,
			Labels: map[string]string{"env": "e2e"},
		},
		Spec: capsulev1beta2.GlobalResourceQuotaSpec{
			NamespaceSelectors: []selectors.NamespaceSelector{{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{countSelector: "true"},
				},
			}},
			Quota: corev1.ResourceQuotaSpec{Hard: countHard},
		},
	}
	ephemeralQuota := &capsulev1beta2.GlobalResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ephemeralQuotaName,
			Labels: map[string]string{"env": "e2e"},
		},
		Spec: capsulev1beta2.GlobalResourceQuotaSpec{
			NamespaceSelectors: []selectors.NamespaceSelector{{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{ephemeralSelector: "true"},
				},
			}},
			Quota: corev1.ResourceQuotaSpec{Hard: ephemeralHard},
		},
	}
	quotaCases := []struct {
		quota      *capsulev1beta2.GlobalResourceQuota
		namespaces []string
		hard       corev1.ResourceList
	}{
		{quota: computeQuota, namespaces: []string{computeA, computeB}, hard: computeHard},
		{quota: serviceQuota, namespaces: []string{serviceA, serviceB}, hard: serviceHard},
		{quota: countQuota, namespaces: []string{countA, countB}, hard: countHard},
		{quota: ephemeralQuota, namespaces: []string{ephemeralA, ephemeralB}, hard: ephemeralHard},
	}
	namespaceCases := []struct {
		name     string
		labelKey string
		tenant   *capsulev1beta2.Tenant
		owner    rbac.UserSpec
	}{
		{name: computeA, labelKey: computeSelector, tenant: tenantA, owner: tenantAOwner},
		{name: computeB, labelKey: computeSelector, tenant: tenantB, owner: tenantBOwner},
		{name: serviceA, labelKey: serviceSelector, tenant: tenantA, owner: tenantAOwner},
		{name: serviceB, labelKey: serviceSelector, tenant: tenantB, owner: tenantBOwner},
		{name: countA, labelKey: countSelector, tenant: tenantA, owner: tenantAOwner},
		{name: countB, labelKey: countSelector, tenant: tenantB, owner: tenantBOwner},
		{name: ephemeralA, labelKey: ephemeralSelector, tenant: tenantA, owner: tenantAOwner},
		{name: ephemeralB, labelKey: ephemeralSelector, tenant: tenantB, owner: tenantBOwner},
	}

	BeforeAll(func() {
		for _, tenant := range tenants {
			EventuallyCreation(func() error {
				tenant.ResourceVersion = ""

				return k8sClient.Create(ctx, tenant)
			}).Should(Succeed())
			TenantReadyTrue(tenant)
		}

		for _, namespace := range namespaceCases {
			ns := NewNamespace(namespace.name, map[string]string{
				meta.TenantLabel:   namespace.tenant.Name,
				namespace.labelKey: "true",
			})
			NamespaceCreation(ns, namespace.owner, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(namespace.tenant, ns).Should(Succeed())
		}

		for quotaIndex := range quotaCases {
			quotaCase := &quotaCases[quotaIndex]
			EventuallyCreation(func() error {
				quotaCase.quota.ResourceVersion = ""

				return k8sClient.Create(ctx, quotaCase.quota)
			}).Should(Succeed())

			Eventually(func(g Gomega) {
				current := &capsulev1beta2.GlobalResourceQuota{}
				g.Expect(k8sClient.Get(
					ctx,
					types.NamespacedName{Name: quotaCase.quota.Name},
					current,
				)).To(Succeed())
				g.Expect(current.Status.ObservedGeneration).To(Equal(current.Generation))
				g.Expect(current.Status.Namespaces).To(ConsistOf(quotaCase.namespaces))
				g.Expect(current.Status.NamespaceSize).To(Equal(uint(len(quotaCase.namespaces))))

				ready := current.Status.Conditions.GetConditionByType(meta.ReadyCondition)
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			current := &capsulev1beta2.GlobalResourceQuota{}
			Expect(k8sClient.Get(
				ctx,
				types.NamespacedName{Name: quotaCase.quota.Name},
				current,
			)).To(Succeed())
			quotaCase.quota = current

			for _, namespace := range quotaCase.namespaces {
				Eventually(func(g Gomega) {
					nativeQuota := &corev1.ResourceQuota{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Namespace: namespace,
						Name:      current.GetResourceQuotaName(),
					}, nativeQuota)).To(Succeed())
					expectResourceListEqual(g, nativeQuota.Spec.Hard, quotaCase.hard)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}

			Eventually(func(g Gomega) {
				ledger := &capsulev1beta2.QuantityLedger{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Namespace: ControllerNamespace,
					Name:      current.GetLedgerName(),
				}, ledger)).To(Succeed())
				g.Expect(ledger.Status.ResourceQuota).NotTo(BeNil())
				g.Expect(ledger.Status.ResourceQuota.Initialized).To(BeTrue())
				g.Expect(ledger.Status.ResourceQuota.ObservedGeneration).To(Equal(current.Generation))
				g.Expect(ledger.Status.ResourceQuota.Namespaces).To(ConsistOf(quotaCase.namespaces))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})

	AfterAll(func() {
		for _, quotaCase := range quotaCases {
			EventuallyDeletion(quotaCase.quota)
		}
		for _, namespace := range namespaceCases {
			ForceDeleteNamespace(ctx, namespace.name)
		}
		for _, tenant := range tenants {
			EventuallyDeletion(tenant)
		}
	})

	It("reports aggregate and per-namespace quota state", func() {
		for _, quotaCase := range quotaCases {
			current := &capsulev1beta2.GlobalResourceQuota{}
			Expect(k8sClient.Get(
				ctx,
				types.NamespacedName{Name: quotaCase.quota.Name},
				current,
			)).To(Succeed())

			expectResourceListEqual(Default, current.Status.Total.Hard, quotaCase.hard)
			Expect(current.Status.NamespaceUsage).To(HaveLen(len(quotaCase.namespaces)))
			for _, namespace := range quotaCase.namespaces {
				Expect(current.Status.NamespaceUsage).To(HaveKey(namespace))
			}
		}
	})

	It("accounts Pod-level resources on Pods generated by Deployments", func() {
		cs := clusterAdminClient()
		makeDeployment := func(namespace, name string) *appsv1.Deployment {
			deployment := MakeDeployment(namespace, name, 1, nil, "")
			deployment.Spec.Template.Spec.Resources = &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("600m"),
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			}

			return deployment
		}

		firstName := "global-quota-pod-level-first"
		_, err := cs.AppsV1().Deployments(computeA).Create(
			ctx,
			makeDeployment(computeA, firstName),
			metav1.CreateOptions{},
		)
		Expect(err).NotTo(HaveOccurred())
		ExpectPodsForDeployment(ctx, computeA, firstName, 1)

		secondName := "global-quota-pod-level-second"
		_, err = cs.AppsV1().Deployments(computeB).Create(
			ctx,
			makeDeployment(computeB, secondName),
			metav1.CreateOptions{},
		)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			failure, getErr := replicaSetFailureForDeployment(ctx, computeB, secondName)
			g.Expect(getErr).NotTo(HaveOccurred())
			g.Expect(failure).NotTo(BeNil())
			g.Expect(failure.Status).To(Equal(corev1.ConditionTrue))
			g.Expect(failure.Message).To(ContainSubstring("exceeds GlobalResourceQuota"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		ExpectPodsForDeployment(ctx, computeB, secondName, 0)
	})

	It("atomically admits only five of ten concurrent Services", func() {
		const total = 10

		cs := clusterAdminClient()
		results := make(chan error, total)

		for index := 0; index < total; index++ {
			go func(index int) {
				namespace := serviceA
				if index%2 == 1 {
					namespace = serviceB
				}

				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      fmt.Sprintf("global-quota-service-%02d", index),
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 80}},
					},
				}
				_, createErr := cs.CoreV1().Services(namespace).Create(
					ctx,
					service,
					metav1.CreateOptions{},
				)
				results <- createErr
			}(index)
		}

		var succeeded, failed int
		for index := 0; index < total; index++ {
			if createErr := <-results; createErr == nil {
				succeeded++
			} else {
				failed++
				Expect(createErr.Error()).To(ContainSubstring("exceeds GlobalResourceQuota"))
			}
		}

		Expect(succeeded).To(Equal(5))
		Expect(failed).To(Equal(5))
	})

	It("enforces legacy core object counts across namespaces", func() {
		const total = 4

		cs := clusterAdminClient()
		results := make(chan error, total)

		for index := 0; index < total; index++ {
			go func(index int) {
				namespace := countA
				if index%2 == 1 {
					namespace = countB
				}

				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      fmt.Sprintf("global-quota-secret-%02d", index),
					},
					Type: corev1.SecretTypeOpaque,
				}
				_, createErr := cs.CoreV1().Secrets(namespace).Create(
					ctx,
					secret,
					metav1.CreateOptions{},
				)
				results <- createErr
			}(index)
		}

		expectConcurrentAdmissions(results, total, 2)
	})

	It("enforces qualified generic object counts across namespaces", func() {
		const total = 4

		cs := clusterAdminClient()
		results := make(chan error, total)

		for index := 0; index < total; index++ {
			go func(index int) {
				namespace := countA
				if index%2 == 1 {
					namespace = countB
				}

				deployment := MakeDeployment(
					namespace,
					fmt.Sprintf("global-quota-deployment-%02d", index),
					0,
					nil,
					"",
				)
				_, createErr := cs.AppsV1().Deployments(namespace).Create(
					ctx,
					deployment,
					metav1.CreateOptions{},
				)
				results <- createErr
			}(index)
		}

		expectConcurrentAdmissions(results, total, 2)
	})

	It("enforces API-group-qualified HorizontalPodAutoscaler counts", func() {
		cs := clusterAdminClient()
		makeHPA := func(namespace, name string) *autoscalingv2.HorizontalPodAutoscaler {
			return &autoscalingv2.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
				Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
					ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "unused-target",
					},
					MaxReplicas: 3,
				},
			}
		}

		_, err := cs.AutoscalingV2().HorizontalPodAutoscalers(countA).Create(
			ctx,
			makeHPA(countA, "global-quota-hpa-first"),
			metav1.CreateOptions{},
		)
		Expect(err).NotTo(HaveOccurred())

		_, err = cs.AutoscalingV2().HorizontalPodAutoscalers(countB).Create(
			ctx,
			makeHPA(countB, "global-quota-hpa-second"),
			metav1.CreateOptions{},
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exceeds GlobalResourceQuota"))
		Expect(err.Error()).To(ContainSubstring(
			"count/horizontalpodautoscalers.autoscaling (requested=1, current=1, projected=2, hard=1, exceededBy=1)",
		))
	})

	It("accounts ephemeral-storage requests and limits across namespaces", func() {
		cs := clusterAdminClient()
		makePod := func(namespace, name string) *corev1.Pod {
			pod := MakePod(
				namespace,
				name,
				nil,
				nil,
				"registry.k8s.io/pause:3.10",
				"",
				"4Gi",
			)
			pod.Spec.Containers[0].Resources = corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceEphemeralStorage: resource.MustParse("600Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
				},
			}

			return pod
		}

		_, err := cs.CoreV1().Pods(ephemeralA).Create(
			ctx,
			makePod(ephemeralA, "global-quota-ephemeral-first"),
			metav1.CreateOptions{},
		)
		Expect(err).NotTo(HaveOccurred())

		_, err = cs.CoreV1().Pods(ephemeralB).Create(
			ctx,
			makePod(ephemeralB, "global-quota-ephemeral-second"),
			metav1.CreateOptions{},
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exceeds GlobalResourceQuota"))
		Expect(err.Error()).To(ContainSubstring(
			"requests.ephemeral-storage (requested=600Mi, current=600Mi, projected=1200Mi, hard=1Gi, exceededBy=176Mi)",
		))
	})

	It("rejects direct hard-limit reductions below allocated usage and allows removals", func() {
		quotaKey := client.ObjectKey{Name: ephemeralQuotaName}
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.GlobalResourceQuota{}
			g.Expect(k8sClient.Get(ctx, quotaKey, current)).To(Succeed())
			used := current.Status.Total.Used[corev1.ResourceRequestsEphemeralStorage]
			g.Expect(used.Cmp(resource.MustParse("600Mi"))).To(Equal(0))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("rejecting a decrease below usage", func() {
			current := &capsulev1beta2.GlobalResourceQuota{}
			Expect(k8sClient.Get(ctx, quotaKey, current)).To(Succeed())
			current.Spec.Quota.Hard[corev1.ResourceRequestsEphemeralStorage] = resource.MustParse("500Mi")

			err := k8sClient.Update(ctx, current)
			Expect(err).To(MatchError(ContainSubstring(
				`spec.quota.hard["requests.ephemeral-storage"] cannot be reduced to 500Mi while 600Mi is allocated`,
			)))
		})

		By("allowing a decrease exactly to allocated usage", func() {
			current := &capsulev1beta2.GlobalResourceQuota{}
			Expect(k8sClient.Get(ctx, quotaKey, current)).To(Succeed())
			current.Spec.Quota.Hard[corev1.ResourceRequestsEphemeralStorage] = resource.MustParse("600Mi")

			Expect(k8sClient.Update(ctx, current)).To(Succeed())
			Eventually(func(g Gomega) {
				reconciled := &capsulev1beta2.GlobalResourceQuota{}
				g.Expect(k8sClient.Get(ctx, quotaKey, reconciled)).To(Succeed())
				g.Expect(reconciled.Status.ObservedGeneration).To(Equal(reconciled.Generation))
				hard := reconciled.Status.Total.Hard[corev1.ResourceRequestsEphemeralStorage]
				g.Expect(hard.Cmp(resource.MustParse("600Mi"))).To(Equal(0))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("allowing removal of a resource with usage", func() {
			current := &capsulev1beta2.GlobalResourceQuota{}
			Expect(k8sClient.Get(ctx, quotaKey, current)).To(Succeed())
			delete(current.Spec.Quota.Hard, corev1.ResourceRequestsEphemeralStorage)

			Expect(k8sClient.Update(ctx, current)).To(Succeed())
			Eventually(func(g Gomega) {
				reconciled := &capsulev1beta2.GlobalResourceQuota{}
				g.Expect(k8sClient.Get(ctx, quotaKey, reconciled)).To(Succeed())
				g.Expect(reconciled.Status.ObservedGeneration).To(Equal(reconciled.Generation))
				g.Expect(reconciled.Spec.Quota.Hard).NotTo(HaveKey(corev1.ResourceRequestsEphemeralStorage))
				g.Expect(reconciled.Status.Total.Hard).NotTo(HaveKey(corev1.ResourceRequestsEphemeralStorage))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
	})
})

func expectResourceListEqual(g Gomega, actual, expected corev1.ResourceList) {
	g.Expect(actual).To(HaveLen(len(expected)))
	for name, expectedQuantity := range expected {
		actualQuantity, found := actual[name]
		g.Expect(found).To(BeTrue(), "missing resource %s", name)
		g.Expect(actualQuantity.Cmp(expectedQuantity)).To(Equal(0), "resource %s", name)
	}
}

func replicaSetFailureForDeployment(
	ctx context.Context,
	namespace string,
	deploymentName string,
) (*appsv1.ReplicaSetCondition, error) {
	deployment := &appsv1.Deployment{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      deploymentName,
	}, deployment); err != nil {
		return nil, err
	}

	replicaSets := &appsv1.ReplicaSetList{}
	if err := k8sClient.List(ctx, replicaSets, client.InNamespace(namespace)); err != nil {
		return nil, err
	}

	for replicaSetIndex := range replicaSets.Items {
		replicaSet := &replicaSets.Items[replicaSetIndex]
		if !metav1.IsControlledBy(replicaSet, deployment) {
			continue
		}

		for conditionIndex := range replicaSet.Status.Conditions {
			condition := &replicaSet.Status.Conditions[conditionIndex]
			if condition.Type == appsv1.ReplicaSetReplicaFailure {
				return condition, nil
			}
		}
	}

	return nil, nil
}

func expectConcurrentAdmissions(results <-chan error, total, expectedSuccess int) {
	var succeeded, failed int
	for index := 0; index < total; index++ {
		if createErr := <-results; createErr == nil {
			succeeded++
		} else {
			failed++
			Expect(createErr.Error()).To(ContainSubstring("exceeds GlobalResourceQuota"))
		}
	}

	Expect(succeeded).To(Equal(expectedSuccess))
	Expect(failed).To(Equal(total - expectedSuccess))
}
