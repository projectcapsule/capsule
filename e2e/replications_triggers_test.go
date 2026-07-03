// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	apimeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	capruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/template"
)

var _ = Describe("GlobalTenantResource triggers", Label("replications", "triggers", "globaltenantresource"), func() {
	ctx := context.Background()

	It("re-renders before resyncPeriod when a watched Secret changes", func() {
		owner := rbac.UserSpec{Name: "e2e-trg-owner", Kind: rbac.OwnerKind("User")}

		tenant := &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "e2e-trg-tenant",
				Labels: map[string]string{"trigger": "e2e"},
			},
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{
					CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: owner},
				}},
			},
		}

		EventuallyCreation(func() error {
			tenant.ResourceVersion = ""

			return k8sClient.Create(ctx, tenant)
		}).Should(Succeed())
		DeferCleanup(func() { EventuallyDeletion(tenant) })
		TenantReady(tenant, metav1.ConditionTrue, defaultTimeoutInterval)

		tenantNs := "e2e-trg-ns"
		namespace := NewNamespace(tenantNs, map[string]string{apimeta.TenantLabel: tenant.GetName()})
		NamespaceCreation(namespace, owner, defaultTimeoutInterval).Should(Succeed())
		DeferCleanup(func() { ForceDeleteNamespace(ctx, tenantNs) })

		sourceNs := "e2e-trg-source"
		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: sourceNs}})
		}).Should(Succeed())
		DeferCleanup(func() { ForceDeleteNamespace(ctx, sourceNs) })

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "trg-secret",
				Namespace: sourceNs,
				Labels: map[string]string{
					"pullsecret.company.com": "true",
					"revision":               "1",
				},
			},
			Type:       corev1.SecretTypeOpaque,
			StringData: map[string]string{"key": "value"},
		}
		EventuallyCreation(func() error { return k8sClient.Create(ctx, secret) }).Should(Succeed())

		gtr := &capsulev1beta2.GlobalTenantResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "e2e-trg-gtr",
				Labels: map[string]string{"e2e.capsule.dev/test-suite": "true"},
			},
			Spec: capsulev1beta2.GlobalTenantResourceSpec{
				Scope:          api.ResourceScopeNamespace,
				TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"trigger": "e2e"}},
				TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
					// Deliberately high: a passing test proves the re-render is
					// driven by the trigger, not by the resync.
					ResyncPeriod:    metav1.Duration{Duration: 10 * time.Minute},
					PruningOnDelete: ptr.To(true),
					Triggers: []capsulev1beta2.TriggerSpec{{
						VersionKinds: capruntime.VersionKinds{Kinds: []string{"Secret"}},
						Operations:   []capsulev1beta2.TriggerOperation{capsulev1beta2.TriggerOperationUpdate},
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"pullsecret.company.com": "true"},
						},
					}},
					Resources: []capsulev1beta2.ResourceSpec{{
						Context: &template.TemplateContext{
							Resources: []*template.TemplateResourceReference{{
								Index: "secrets",
								ResourceReference: template.ResourceReference{
									VersionKind: capruntime.VersionKind{APIVersion: "v1", Kind: "Secret"},
									Namespace:   sourceNs,
									Selector: &metav1.LabelSelector{
										MatchLabels: map[string]string{"pullsecret.company.com": "true"},
									},
								},
							}},
						},
						Generators: []capsulev1beta2.TemplateItemSpec{{
							MissingKey: "error",
							Template: `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: trg-cm
data:
  revision: '{{ (index $.secrets 0).metadata.labels.revision }}'
`,
						}},
					}},
				},
			},
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, gtr) })

		// Initial render carries the secret's current revision label.
		expectConfigMapData(tenantNs, "trg-cm", map[string]string{"revision": "1"})

		// The resource advertises its armed watches through a status condition.
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.GlobalTenantResource{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: gtr.Name}, current)).To(Succeed())

			cond := current.Status.Conditions.GetConditionByType(apimeta.TriggersCondition)
			g.Expect(cond).NotTo(BeNil())
			g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		// Mutate the watched Secret.
		Eventually(func(g Gomega) {
			current := &corev1.Secret{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "trg-secret", Namespace: sourceNs}, current)).To(Succeed())

			if current.Labels == nil {
				current.Labels = map[string]string{}
			}

			current.Labels["revision"] = "2"

			g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		// The rendered ConfigMap must reflect the change well before the 10m resync.
		Eventually(func(g Gomega) {
			cm := &corev1.ConfigMap{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "trg-cm", Namespace: tenantNs}, cm)).To(Succeed())
			g.Expect(cm.Data).To(HaveKeyWithValue("revision", "2"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})
})
