// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

var _ = Describe("CEL cache invalidation", Serial, Label("config", "cel-cache", "resource-condition"), func() {
	It("preserves condition validation and tenant-scoped evaluation across periodic rebuilds", func() {
		ctx := context.Background()
		original := &capsulev1beta2.CapsuleConfiguration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, original)).To(Succeed())
		DeferCleanup(func() {
			ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
				configuration.Spec.CacheInvalidation = original.Spec.CacheInvalidation
			})
		})
		const interval = 2 * time.Second
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.CacheInvalidation = metav1.Duration{Duration: interval}
		})
		prefix := "e2e-cel-cache-" + rand.String(6)
		var namespaces []string
		var actors []client.Client
		for _, suffix := range []string{"-a", "-b"} {
			name := prefix + suffix
			owner := rbac.UserSpec{Name: name + "-owner", Kind: rbac.UserOwner}
			tenant := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{UserSpec: owner}}}}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			DeferCleanup(func() { EventuallyDeletion(tenant) })
			TenantReady(tenant, metav1.ConditionTrue, defaultTimeoutInterval)
			namespace := NewNamespace(name, map[string]string{meta.TenantLabel: name})
			NamespaceCreation(namespace, owner, defaultTimeoutInterval).Should(Succeed())
			DeferCleanup(func() {
				cleanupTenantResourcesWithDefaultServiceAccount(ctx, name)
				ForceDeleteNamespace(ctx, name)
			})
			NamespaceIsPartOfTenant(tenant, namespace).Should(Succeed())
			ensureServiceAccount(name, "default")
			bindServiceAccountToTenantResourceManager(name, "default", name)
			namespaces = append(namespaces, name)
			actor := impersonationClient(owner.Name, withDefaultGroups([]string{owner.Name}))
			actors = append(actors, actor)
			customQuota := &capsulev1beta2.CustomQuota{Name: "cel-cache-quota", Namespace: name, Labels: map[string]string{"env": "e2e"},
				Spec: capsulev1beta2.CustomQuotaSpec{Limit: resource.MustParse("1"), Sources: []capsulev1beta2.CustomQuotaSpecSource{{
					VersionKind: apiruntime.VersionKind{APIVersion: "v1", Kind: "Pod"},
					CustomQuotaSpecSourceConfig: capsulev1beta2.CustomQuotaSpecSourceConfig{
						Operation: quota.OpAdd, CEL: `quantity('1')`,
						Selectors: []selectors.SelectorWithFields{{CELExpressions: []string{`object.metadata.name.startsWith('quota-')`}}},
					},
				}}},
			}
			Expect(k8sClient.Create(ctx, customQuota)).To(Succeed())
			DeferCleanup(func() { EventuallyDeletion(customQuota) })
			awaitCustomQuotaReady(ctx, name, customQuota.Name)
			Eventually(func(g Gomega) {
				current := &capsulev1beta2.CustomQuota{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(customQuota), current)).To(Succeed())
				g.Expect(current.Status.ObservedGeneration).To(Equal(current.Generation))
				g.Expect(meta.IsStatusConditionTrue(current.Status.Conditions, meta.ReadyCondition)).To(BeTrue())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(actor.Create(ctx, MakePod(name, "quota-first", map[string]string{"env": "e2e", "tenant": name}, nil, "nginx:1.27.0", "", ""))).To(Succeed())
			expectCustomQuotaUsedAndClaims(ctx, name, customQuota.Name, "1", 1)
			expectLedgerSettled(ctx, name, customQuota.Name)
		}
		By("reusing conditions across tenants without sharing evaluation results")
		for i, namespace := range namespaces {
			exerciseTenantResourceConditions(namespace, namespace, namespace, namespaces[1-i])
		}
		By("accepting valid conditions and rejecting invalid ones through repeated cache rebuilds")
		Consistently(func(g Gomega) {
			for i, namespace := range namespaces {
				parent := &capsulev1beta2.TenantResource{Name: "condition-validation", Namespace: namespace,
					Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						Resources: []capsulev1beta2.ResourceSpec{{
							Policy: &apiruntime.ResourceTemplatePolicy{Condition: "object == null"},
							RawItems: []capsulev1beta2.RawExtension{{Object: &corev1.ConfigMap{
								APIVersion: "v1", Kind: "ConfigMap", Name: "validated", Data: map[string]string{"tenant": namespace},
							}}},
						}},
					}},
				}
				invalid := parent.DeepCopy()
				invalid.Spec.Resources[0].Policy.Condition = "42"
				g.Expect(actors[i].Create(ctx, parent, client.DryRunAll)).To(Succeed())
				g.Expect(actors[i].Create(ctx, invalid, client.DryRunAll)).To(MatchError(ContainSubstring("spec.resources[0].policy.condition")))
				g.Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), &capsulev1beta2.TenantResource{}))).To(BeTrue())
				denied := MakePod(namespace, "quota-denied", map[string]string{"env": "e2e"}, nil, "nginx:1.27.0", "", "")
				g.Expect(actors[i].Create(ctx, denied)).To(MatchError(ContainSubstring("creating resource exceeds limit for CustomQuota")))
				g.Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(denied), &corev1.Pod{}))).To(BeTrue())
			}
		}, 3*interval, defaultPollInterval).Should(Succeed())
		for _, namespace := range namespaces {
			pod := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "quota-first"}, pod)).To(Succeed())
			Expect(pod.Labels).To(HaveKeyWithValue("tenant", namespace))
			expectCustomQuotaUsedAndClaims(ctx, namespace, "cel-cache-quota", "1", 1)
		}
	})
})
