// Copyright 2020-2026 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

// configStatusCounters returns the current TenantCount and ManagedNamespaceCount
// from the default CapsuleConfiguration, treating a nil pointer as zero.
func configStatusCounters(g Gomega) (tenantCount, namespaceCount int64) {
	cfg := &capsulev1beta2.CapsuleConfiguration{}
	g.Expect(k8sClient.Get(context.TODO(), client.ObjectKey{Name: defaultConfigurationName}, cfg)).To(Succeed())

	if cfg.Status.TenantCount != nil {
		tenantCount = *cfg.Status.TenantCount
	}

	if cfg.Status.ManagedNamespaceCount != nil {
		namespaceCount = *cfg.Status.ManagedNamespaceCount
	}

	return tenantCount, namespaceCount
}

var _ = Describe("CapsuleConfiguration status counters", Ordered, Label("config", "status", "counters"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-cfg-counters-tnt",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-cfg-counters-owner",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	ns := NewNamespace("e2e-cfg-counters-ns")

	JustAfterEach(func() {
		// Safety-net cleanup; EventuallyDeletion is idempotent for already-deleted objects.
		EventuallyDeletion(ns)
		EventuallyDeletion(tnt)
	})

	It("reflects Tenant create/delete and Namespace create/delete in status counters", func() {
		var baseTenantCount, baseNSCount int64

		// Wait for the controller to have populated the counters at least once.
		Eventually(func(g Gomega) {
			cfg := &capsulev1beta2.CapsuleConfiguration{}
			g.Expect(k8sClient.Get(context.TODO(), client.ObjectKey{Name: defaultConfigurationName}, cfg)).To(Succeed())
			g.Expect(cfg.Status.TenantCount).NotTo(BeNil(), "TenantCount must be initialised by the controller before the test")

			baseTenantCount = *cfg.Status.TenantCount

			if cfg.Status.ManagedNamespaceCount != nil {
				baseNSCount = *cfg.Status.ManagedNamespaceCount
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("creating a Tenant and asserting TenantCount increments by one")

		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())

		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)

		Eventually(func(g Gomega) {
			tc, _ := configStatusCounters(g)
			g.Expect(tc).To(Equal(baseTenantCount+1), "TenantCount should be baseTenantCount+1 after Tenant creation")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("creating a Namespace under the Tenant and asserting ManagedNamespaceCount increments by one")

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceReady(tnt, ns, 1)

		Eventually(func(g Gomega) {
			_, nc := configStatusCounters(g)
			g.Expect(nc).To(Equal(baseNSCount+1), "ManagedNamespaceCount should be baseNSCount+1 after Namespace creation")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("deleting the Namespace and asserting ManagedNamespaceCount returns to the baseline")

		EventuallyDeletion(ns)

		Eventually(func(g Gomega) {
			_, nc := configStatusCounters(g)
			g.Expect(nc).To(Equal(baseNSCount), "ManagedNamespaceCount should return to baseline after Namespace deletion")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("deleting the Tenant and asserting TenantCount returns to the baseline")

		EventuallyDeletion(tnt)

		Eventually(func(g Gomega) {
			tc, _ := configStatusCounters(g)
			g.Expect(tc).To(Equal(baseTenantCount), "TenantCount should return to baseline after Tenant deletion")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("does not count Namespaces that belong to no Tenant", func() {
		var baseNSCount int64

		Eventually(func(g Gomega) {
			cfg := &capsulev1beta2.CapsuleConfiguration{}
			g.Expect(k8sClient.Get(context.TODO(), client.ObjectKey{Name: defaultConfigurationName}, cfg)).To(Succeed())

			if cfg.Status.ManagedNamespaceCount != nil {
				baseNSCount = *cfg.Status.ManagedNamespaceCount
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		unmanaged := NewNamespace("e2e-cfg-counters-unmanaged")

		DeferCleanup(func() {
			EventuallyDeletion(unmanaged)
		})

		Expect(k8sClient.Create(context.TODO(), unmanaged)).To(Succeed())

		// Wait long enough for a potential (spurious) reconcile to settle.
		Consistently(func(g Gomega) {
			_, nc := configStatusCounters(g)
			g.Expect(nc).To(Equal(baseNSCount), "ManagedNamespaceCount must not change for unmanaged Namespaces")
		}, "10s", defaultPollInterval).Should(Succeed())

		EventuallyDeletion(unmanaged)
	})

	It("sets status.size on the Tenant reflecting the namespace count", func() {
		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())

		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)

		ns1 := NewNamespace("e2e-cfg-counters-size-ns1")
		ns2 := NewNamespace("e2e-cfg-counters-size-ns2")

		owner := tnt.Spec.Owners[0].UserSpec

		DeferCleanup(func() {
			for _, n := range []*corev1.Namespace{ns1, ns2} {
				EventuallyDeletion(n)
			}
		})

		NamespaceCreation(ns1, owner, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceReady(tnt, ns1, 1)

		NamespaceCreation(ns2, owner, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceReady(tnt, ns2, 2)

		Eventually(func(g Gomega) {
			_, nc := configStatusCounters(g)
			g.Expect(nc).To(BeNumerically(">=", int64(2)), "ManagedNamespaceCount should include both namespaces")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})
})
