// Copyright 2020-2026 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capmeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

// configStatus returns the current CapsuleConfiguration status.
func configStatus(g Gomega) capsulev1beta2.CapsuleConfigurationStatus {
	cfg := &capsulev1beta2.CapsuleConfiguration{}
	g.Expect(k8sClient.Get(context.TODO(), client.ObjectKey{Name: defaultConfigurationName}, cfg)).To(Succeed())

	return cfg.Status
}

// configTenantStatus returns the current Tenants list from the default CapsuleConfiguration.
func configTenantStatus(g Gomega) []string {
	return configStatus(g).Tenants
}

var _ = Describe("CapsuleConfiguration status tenants", Ordered, Label("config", "status", "tenants"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-cfg-tenants-tnt",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-cfg-tenants-owner",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	JustAfterEach(func() {
		// Safety-net cleanup; EventuallyDeletion is idempotent for already-deleted objects.
		EventuallyDeletion(tnt)
	})

	It("reflects Tenant create/delete in status.tenants list", func() {
		var baseNames []string

		// Wait for the controller to have populated the tenants list at least once.
		// status.tenants is nil until the first reconcile runs; a non-nil slice
		// (even empty) means the controller has initialised it.
		Eventually(func(g Gomega) {
			baseNames = configTenantStatus(g)
			g.Expect(baseNames).NotTo(BeNil(), "status.tenants must be initialized before taking baseline")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("creating a Tenant and asserting its name appears in status.tenants")

		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())

		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)

		Eventually(func(g Gomega) {
			names := configTenantStatus(g)
			g.Expect(names).To(HaveLen(len(baseNames)+1), "status.tenants should grow by one after Tenant creation")
			g.Expect(names).To(ContainElement(tnt.Name), "status.tenants should contain the new Tenant name")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("deleting the Tenant and asserting its name is removed from status.tenants")

		EventuallyDeletion(tnt)

		Eventually(func(g Gomega) {
			names := configTenantStatus(g)
			g.Expect(names).To(HaveLen(len(baseNames)), "status.tenants should return to baseline after Tenant deletion")
			g.Expect(names).NotTo(ContainElement(tnt.Name), "status.tenants must not contain the deleted Tenant name")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})
})

var _ = Describe("CapsuleConfiguration status Ready condition", Ordered, Label("config", "status", "ready"), func() {
	It("has Ready=True with Reason=Succeeded after a successful reconcile", func() {
		// After the controller has reconciled at least once the Ready condition
		// must be True with the Succeeded reason.  This guards against regressions
		// where a prior failure leaves the condition stuck at Ready=False.
		Eventually(func(g Gomega) {
			st := configStatus(g)
			cond := st.Conditions.GetConditionByType(capmeta.ReadyCondition)
			g.Expect(cond).NotTo(BeNil(), "Ready condition must be present")
			g.Expect(cond.Status).To(Equal(metav1.ConditionTrue), "Ready condition must be True after successful reconcile")
			g.Expect(cond.Reason).To(Equal(capmeta.SucceededReason), "Ready condition reason must be Succeeded")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})
})
