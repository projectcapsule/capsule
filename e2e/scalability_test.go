// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

var _ = Describe("verify scalability", Label("scalability"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-scalability",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					UserSpec: api.UserSpec{
						Name: "gatsby",
						Kind: "User",
					},
				},
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})

	It("verify lifecycle (scalability)", func() {
		const amount = 50

		getTenant := func() *capsulev1beta2.Tenant {
			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)).To(Succeed())
			return t
		}

		waitSize := func(expected uint) {
			Eventually(func() uint {
				return getTenant().Status.Size
			}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(expected))
		}

		waitInstancePresent := func(ns *corev1.Namespace) {
			Eventually(func() error {
				t := getTenant()
				inst := t.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{
					Name: ns.GetName(),
					UID:  ns.GetUID(),
				})
				if inst == nil {
					return fmt.Errorf("instance not found for ns=%q uid=%q", ns.GetName(), ns.GetUID())
				}

				condition := inst.Conditions.GetConditionByType(meta.ReadyCondition)
				Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")
				if inst == nil {
					return fmt.Errorf("instance not found for ns=%q uid=%q", ns.GetName(), ns.GetUID())
				}

				if inst.Name != ns.GetName() {
					return fmt.Errorf("instance.Name=%q, want %q", inst.Name, ns.GetName())
				}

				cond := inst.Conditions.GetConditionByType(meta.ReadyCondition)
				if cond == nil {
					return fmt.Errorf("missing %q condition", meta.ReadyCondition)
				}
				if cond.Type != meta.ReadyCondition {
					return fmt.Errorf("cond.Type=%q, want %q", cond.Type, meta.ReadyCondition)
				}
				if cond.Status != metav1.ConditionTrue {
					return fmt.Errorf("cond.Status=%q, want %q", cond.Status, metav1.ConditionTrue)
				}
				if cond.Reason != meta.SucceededReason {
					return fmt.Errorf("cond.Reason=%q, want %q", cond.Reason, meta.SucceededReason)
				}

				return nil
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}

		waitInstanceAbsent := func(ns *corev1.Namespace) {
			Eventually(func() bool {
				t := getTenant()
				inst := t.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{
					Name: ns.GetName(),
					UID:  ns.GetUID(),
				})
				return inst == nil
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		}

		// --- Scale up: create N namespaces and verify Tenant status each time ---
		namespaces := make([]*corev1.Namespace, 0, amount)
		for i := 0; i < amount; i++ {
			ns := NewNamespace(fmt.Sprintf("scale-%d", i))
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			// Expect size bumped to i+1 and instance present
			waitSize(uint(i + 1))
			waitInstancePresent(ns)

			namespaces = append(namespaces, ns)
		}

		// --- Scale down: delete N namespaces and verify Tenant status each time ---
		for i := 0; i < amount; i++ {
			ns := namespaces[i]
			Expect(k8sClient.Delete(context.TODO(), ns)).To(Succeed())

			// Expect size decremented and instance absent
			waitSize(uint(amount - i - 1))
			waitInstanceAbsent(ns)
		}

	})

})
