// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capmeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("observedGeneration is tracked in status", Ordered, Label("observedGeneration"), func() {
	var tnt *capsulev1beta2.Tenant

	JustBeforeEach(func() {
		tnt = &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name: "e2e-observed-generation",
			},
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{
					{
						CoreOwnerSpec: rbac.CoreOwnerSpec{
							UserSpec: rbac.UserSpec{
								Name: "e2e-observed-generation-owner",
								Kind: "User",
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
	})

	JustAfterEach(func() {
		EventuallyDeletion(tnt)
	})

	It("sets observedGeneration after initial reconciliation", func() {
		By("waiting for the tenant to be ready", func() {
			TenantReadyTrue(tnt)
		})

		By("verifying observedGeneration equals metadata.generation", func() {
			Eventually(func(g Gomega) {
				current := &capsulev1beta2.Tenant{}
				g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, current)).To(Succeed())

				g.Expect(current.Status.ObservedGeneration).To(
					Equal(current.GetGeneration()),
					"expected status.observedGeneration (%d) to equal metadata.generation (%d) after initial reconciliation",
					current.Status.ObservedGeneration,
					current.GetGeneration(),
				)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
	})

	It("updates observedGeneration after a spec change", func() {
		By("waiting for the tenant to be ready at generation 1", func() {
			TenantReadyTrue(tnt)
		})

		var genBeforeUpdate int64

		By("recording the current generation", func() {
			current := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, current)).To(Succeed())
			genBeforeUpdate = current.GetGeneration()
			Expect(genBeforeUpdate).To(BeNumerically(">=", int64(1)))
		})

		By("mutating the spec to increment metadata.generation", func() {
			UpdateTenantEventually(tnt, func(t *capsulev1beta2.Tenant) {
				t.Spec.Cordoned = true
			})
		})

		By("verifying observedGeneration advances to the new generation", func() {
			Eventually(func(g Gomega) {
				current := &capsulev1beta2.Tenant{}
				g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, current)).To(Succeed())

				g.Expect(current.GetGeneration()).To(
					BeNumerically(">", genBeforeUpdate),
					"expected metadata.generation to have incremented after spec update",
				)

				g.Expect(current.Status.ObservedGeneration).To(
					Equal(current.GetGeneration()),
					"expected status.observedGeneration (%d) to match updated metadata.generation (%d)",
					current.Status.ObservedGeneration,
					current.GetGeneration(),
				)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
	})
})

var _ = Describe("CapsuleConfiguration observedGeneration is tracked in status", Serial, Ordered, Label("observedGeneration"), func() {
	It("observedGeneration matches metadata.generation for the default CapsuleConfiguration", func() {
		Eventually(func(g Gomega) {
			cfg := &capsulev1beta2.CapsuleConfiguration{}
			g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: defaultConfigurationName}, cfg)).To(Succeed())

			g.Expect(cfg.Status.ObservedGeneration).To(
				Equal(cfg.GetGeneration()),
				"expected status.observedGeneration (%d) to equal metadata.generation (%d)",
				cfg.Status.ObservedGeneration,
				cfg.GetGeneration(),
			)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("advances observedGeneration after a CapsuleConfiguration spec change", func() {
		var genBefore int64

		By("recording the current generation", func() {
			cfg := &capsulev1beta2.CapsuleConfiguration{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: defaultConfigurationName}, cfg)).To(Succeed())
			genBefore = cfg.GetGeneration()
		})

		By("mutating CapsuleConfiguration to increment metadata.generation", func() {
			ModifyCapsuleConfigurationOpts(func(cfg *capsulev1beta2.CapsuleConfiguration) {
				cfg.Spec.IgnoreUserWithGroups = append(cfg.Spec.IgnoreUserWithGroups, "e2e-observed-gen-dummy-group")
			})
		})

		DeferCleanup(func() {
			ModifyCapsuleConfigurationOpts(func(cfg *capsulev1beta2.CapsuleConfiguration) {
				groups := cfg.Spec.IgnoreUserWithGroups
				filtered := groups[:0]
				for _, g := range groups {
					if g != "e2e-observed-gen-dummy-group" {
						filtered = append(filtered, g)
					}
				}
				cfg.Spec.IgnoreUserWithGroups = filtered
			})
		})

		By("verifying observedGeneration advances to the new generation", func() {
			Eventually(func(g Gomega) {
				cfg := &capsulev1beta2.CapsuleConfiguration{}
				g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: defaultConfigurationName}, cfg)).To(Succeed())

				g.Expect(cfg.GetGeneration()).To(
					BeNumerically(">", genBefore),
					"expected metadata.generation to increment after spec update",
				)
				g.Expect(cfg.Status.ObservedGeneration).To(
					Equal(cfg.GetGeneration()),
					"expected status.observedGeneration (%d) to match updated metadata.generation (%d)",
					cfg.Status.ObservedGeneration,
					cfg.GetGeneration(),
				)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
	})
})

var _ = Describe("RuleStatus observedGeneration is tracked in status", Ordered, Label("observedGeneration"), func() {
	ctx := context.TODO()

	var (
		tnt *capsulev1beta2.Tenant
		ns  *corev1.Namespace
	)

	JustBeforeEach(func() {
		tnt = &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name: "e2e-rulestatus-observed-gen",
			},
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{
					{
						CoreOwnerSpec: rbac.CoreOwnerSpec{
							UserSpec: rbac.UserSpec{
								Name: "e2e-rulestatus-observed-gen-owner",
								Kind: "User",
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, tnt)
		}).Should(Succeed())

		TenantReadyTrue(tnt)

		ns = NewNamespace("e2e-rulestatus-observed-gen-ns", map[string]string{
			capmeta.TenantLabel: tnt.GetName(),
		})

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())

		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
	})

	JustAfterEach(func() {
		if ns != nil {
			_ = k8sClient.Delete(ctx, ns)
		}

		EventuallyDeletion(tnt)
	})

	It("sets observedGeneration on RuleStatus after reconciliation", func() {
		By("waiting for RuleStatus to exist and have observedGeneration set", func() {
			Eventually(func(g Gomega) {
				ruleStatus := &capsulev1beta2.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{
					Name:      capmeta.NameForManagedRuleStatus(),
					Namespace: ns.Name,
				}, ruleStatus)).To(Succeed())

				g.Expect(ruleStatus.Status.ObservedGeneration).To(
					BeNumerically(">", int64(0)),
					"expected status.observedGeneration to be non-zero",
				)

				g.Expect(ruleStatus.Status.ObservedGeneration).To(
					Equal(ruleStatus.GetGeneration()),
					"expected status.observedGeneration (%d) to equal metadata.generation (%d)",
					ruleStatus.Status.ObservedGeneration,
					ruleStatus.GetGeneration(),
				)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
	})
})
