// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capmeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

// tenantOwnerReady waits until the TenantOwner's Ready condition has the expected status.
func tenantOwnerReady(to *capsulev1beta2.TenantOwner, expected metav1.ConditionStatus) {
	Eventually(func(g Gomega) {
		current := &capsulev1beta2.TenantOwner{}
		g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: to.GetName()}, current)).To(Succeed())

		cond := current.Status.Conditions.GetConditionByType(capmeta.ReadyCondition)
		g.Expect(cond).NotTo(BeNil(), "TenantOwner %q should have a Ready condition", to.GetName())
		g.Expect(cond.Status).To(
			Equal(expected),
			"TenantOwner %q Ready condition should be %s, got %s: %s",
			to.GetName(), expected, cond.Status, cond.Message,
		)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

// tenantOwnerMatchedTenants waits until the TenantOwner's status.matchedTenantNames
// equals the expected sorted list. The controller calls sort.Strings before persisting,
// so the assertion is order-sensitive to validate stable ordering for API consumers.
func tenantOwnerMatchedTenants(to *capsulev1beta2.TenantOwner, expected []string) {
	Eventually(func(g Gomega) {
		current := &capsulev1beta2.TenantOwner{}
		g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: to.GetName()}, current)).To(Succeed())

		actual := current.Status.MatchedTenantNames
		if actual == nil {
			actual = []string{}
		}

		g.Expect(actual).To(
			Equal(expected),
			"TenantOwner %q status.matchedTenantNames mismatch", to.GetName(),
		)
		expectedCount := int64(len(expected))
		g.Expect(current.Status.MatchedTenants).To(
			Equal(&expectedCount),
			"TenantOwner %q status.matchedTenants mismatch", to.GetName(),
		)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

var _ = Describe("TenantOwner status tracks matched Tenants", Ordered, Label("tenantowner", "status"), func() {
	// to is a shared TenantOwner used across the sub-tests in this Ordered block.
	// Two Tenants are created/deleted to verify count changes.
	to := &capsulev1beta2.TenantOwner{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-to-status",
			Labels: map[string]string{
				"e2e-to-status": "true",
			},
		},
		Spec: capsulev1beta2.TenantOwnerSpec{
			Aggregate: true,
			CoreOwnerSpec: rbac.CoreOwnerSpec{
				UserSpec: rbac.UserSpec{
					Kind: rbac.UserOwner,
					Name: "e2e-to-status-user",
				},
				ClusterRoles: []string{"view"},
			},
		},
	}

	tnt1 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-to-status-tnt1",
		},
		Spec: capsulev1beta2.TenantSpec{
			Permissions: capsulev1beta2.Permissions{
				MatchOwners: []*metav1.LabelSelector{
					{MatchLabels: map[string]string{"e2e-to-status": "true"}},
				},
			},
		},
	}

	tnt2 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-to-status-tnt2",
		},
		Spec: capsulev1beta2.TenantSpec{
			Permissions: capsulev1beta2.Permissions{
				MatchOwners: []*metav1.LabelSelector{
					{MatchLabels: map[string]string{"e2e-to-status": "true"}},
				},
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			to.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), to)
		}).Should(Succeed())
	})

	JustAfterEach(func() {
		EventuallyDeletion(to)
		EventuallyDeletion(tnt1)
		EventuallyDeletion(tnt2)
	})

	It("has Ready=True and empty tenant list when no Tenant matches", func() {
		tenantOwnerReady(to, metav1.ConditionTrue)
		tenantOwnerMatchedTenants(to, []string{})
	})

	It("reflects one matched Tenant in status", func() {
		EventuallyCreation(func() error {
			tnt1.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), tnt1)
		}).Should(Succeed())

		TenantReadyTrue(tnt1)

		tenantOwnerReady(to, metav1.ConditionTrue)
		tenantOwnerMatchedTenants(to, []string{tnt1.Name})
	})

	It("reflects two matched Tenants in status", func() {
		EventuallyCreation(func() error {
			tnt1.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), tnt1)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			tnt2.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), tnt2)
		}).Should(Succeed())

		TenantReadyTrue(tnt1)
		TenantReadyTrue(tnt2)

		tenantOwnerReady(to, metav1.ConditionTrue)
		tenantOwnerMatchedTenants(to, []string{tnt1.Name, tnt2.Name})
	})

	It("clears the matched Tenant from status when the Tenant is deleted", func() {
		EventuallyCreation(func() error {
			tnt1.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), tnt1)
		}).Should(Succeed())

		TenantReadyTrue(tnt1)
		tenantOwnerMatchedTenants(to, []string{tnt1.Name})

		EventuallyDeletion(tnt1)

		tenantOwnerReady(to, metav1.ConditionTrue)
		tenantOwnerMatchedTenants(to, []string{})
	})

	It("updates status when a Tenant's matchOwners selector is removed", func() {
		EventuallyCreation(func() error {
			tnt1.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), tnt1)
		}).Should(Succeed())

		TenantReadyTrue(tnt1)
		tenantOwnerMatchedTenants(to, []string{tnt1.Name})

		// Remove the matchOwners selector so the TenantOwner is no longer matched.
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt1.Name}, current)).To(Succeed())
			current.Spec.Permissions.MatchOwners = nil
			g.Expect(k8sClient.Update(context.TODO(), current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		tenantOwnerReady(to, metav1.ConditionTrue)
		tenantOwnerMatchedTenants(to, []string{})
	})

	It("tracks observedGeneration in TenantOwner status", func() {
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.TenantOwner{}
			g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: to.Name}, current)).To(Succeed())
			g.Expect(current.Status.ObservedGeneration).To(
				Equal(current.GetGeneration()),
				"status.observedGeneration should equal metadata.generation",
			)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})
})

var _ = Describe("TenantOwner status with 10 matched Tenants", Ordered, Label("tenantowner", "status", "scale"), func() {
	const tenantCount = 10

	to := &capsulev1beta2.TenantOwner{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-to-status-10match",
			Labels: map[string]string{
				"e2e-to-status-10match": "true",
			},
		},
		Spec: capsulev1beta2.TenantOwnerSpec{
			Aggregate: true,
			CoreOwnerSpec: rbac.CoreOwnerSpec{
				UserSpec: rbac.UserSpec{
					Kind: rbac.UserOwner,
					Name: "e2e-to-status-10match-user",
				},
				ClusterRoles: []string{"view"},
			},
		},
	}

	tenants := func() []*capsulev1beta2.Tenant {
		tnts := make([]*capsulev1beta2.Tenant, tenantCount)
		for i := range tnts {
			tnts[i] = &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("e2e-to-status-10match-tnt-%d", i),
				},
				Spec: capsulev1beta2.TenantSpec{
					Permissions: capsulev1beta2.Permissions{
						MatchOwners: []*metav1.LabelSelector{
							{MatchLabels: map[string]string{"e2e-to-status-10match": "true"}},
						},
					},
				},
			}
		}

		return tnts
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			to.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), to)
		}).Should(Succeed())

		for _, tnt := range tenants() {
			tnt := tnt

			EventuallyCreation(func() error {
				tnt.ResourceVersion = ""

				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
		}
	})

	JustAfterEach(func() {
		EventuallyDeletion(to)
		for _, tnt := range tenants() {
			EventuallyDeletion(tnt)
		}
	})

	It("reports all 10 matched Tenants and Ready=True", func() {
		expectedNames := make([]string, tenantCount)
		for i := range expectedNames {
			expectedNames[i] = fmt.Sprintf("e2e-to-status-10match-tnt-%d", i)
		}

		tenantOwnerReady(to, metav1.ConditionTrue)
		tenantOwnerMatchedTenants(to, expectedNames)
	})
})

var _ = Describe("TenantOwner status fan-out: 10 TenantOwners matched by 1 Tenant", Ordered, Label("tenantowner", "status", "fanout"), func() {
	const ownerCount = 10

	owners := func() []*capsulev1beta2.TenantOwner {
		tos := make([]*capsulev1beta2.TenantOwner, ownerCount)
		for i := range tos {
			tos[i] = &capsulev1beta2.TenantOwner{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("e2e-to-fanout-%d", i),
					Labels: map[string]string{
						"e2e-to-fanout": "true",
					},
				},
				Spec: capsulev1beta2.TenantOwnerSpec{
					Aggregate: true,
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Kind: rbac.UserOwner,
							Name: fmt.Sprintf("e2e-to-fanout-user-%d", i),
						},
						ClusterRoles: []string{"view"},
					},
				},
			}
		}

		return tos
	}

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-tnt-fanout-single",
		},
		Spec: capsulev1beta2.TenantSpec{
			Permissions: capsulev1beta2.Permissions{
				MatchOwners: []*metav1.LabelSelector{
					{MatchLabels: map[string]string{"e2e-to-fanout": "true"}},
				},
			},
		},
	}

	JustBeforeEach(func() {
		for _, to := range owners() {
			to := to

			EventuallyCreation(func() error {
				to.ResourceVersion = ""

				return k8sClient.Create(context.TODO(), to)
			}).Should(Succeed())
		}

		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
	})

	JustAfterEach(func() {
		for _, to := range owners() {
			EventuallyDeletion(to)
		}
		EventuallyDeletion(tnt)
	})

	It("updates all 10 TenantOwners when 1 Tenant is created", func() {
		// All 10 TenantOwners should each report matchedTenants=1 and tenants=[tnt.Name].
		for _, to := range owners() {
			to := to

			tenantOwnerReady(to, metav1.ConditionTrue)
			tenantOwnerMatchedTenants(to, []string{tnt.Name})
		}
	})

	It("clears all 10 TenantOwners when the Tenant is deleted", func() {
		// Establish baseline: all matched.
		for _, to := range owners() {
			tenantOwnerMatchedTenants(to, []string{tnt.Name})
		}

		EventuallyDeletion(tnt)

		// After deletion every TenantOwner should report zero matches.
		for _, to := range owners() {
			tenantOwnerMatchedTenants(to, []string{})
		}
	})
})
