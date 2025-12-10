// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

var _ = Describe("creating namespace with status lifecycle", Label("namespace", "status"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-status",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "gatsby",
							Kind: "User",
						},
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

	It("verify namespace lifecycle (functionality)", func() {
		ns1 := NewNamespace("")
		By("creating first namespace", func() {
			NamespaceCreation(ns1, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElements(ns1.GetName()))

			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)).Should(Succeed())

			Expect(t.Status.Size).To(Equal(uint(1)))

			instance := t.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: ns1.GetName(), UID: ns1.GetUID()})
			Expect(instance).NotTo(BeNil(), "Namespace instance should not be nil")

			condition := instance.Conditions.GetConditionByType(meta.ReadyCondition)
			Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

			Expect(instance.Name).To(Equal(ns1.GetName()))
			Expect(condition.Status).To(Equal(metav1.ConditionTrue), "Expected namespace condition status to be True")
			Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected namespace condition type to be Ready")
			Expect(condition.Reason).To(Equal(meta.SucceededReason), "Expected namespace condition reason to be Succeeded")
		})

		ns2 := NewNamespace("")
		By("creating second namespace", func() {
			NamespaceCreation(ns2, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElements(ns2.GetName()))

			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)).Should(Succeed())

			Expect(t.Status.Size).To(Equal(uint(2)))

			instance := t.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: ns2.GetName(), UID: ns2.GetUID()})
			Expect(instance).NotTo(BeNil(), "Namespace instance should not be nil")

			condition := instance.Conditions.GetConditionByType(meta.ReadyCondition)
			Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

			Expect(instance.Name).To(Equal(ns2.GetName()))
			Expect(condition.Status).To(Equal(metav1.ConditionTrue), "Expected namespace condition status to be True")
			Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected namespace condition type to be Ready")
			Expect(condition.Reason).To(Equal(meta.SucceededReason), "Expected namespace condition reason to be Succeeded")
		})

		By("removing first namespace", func() {
			cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

			err := cs.CoreV1().
				Namespaces().
				Delete(context.TODO(), ns1.Name, metav1.DeleteOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)).Should(Succeed())

			Expect(t.Status.Size).To(Equal(uint(1)))

			instance := t.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: ns1.GetName(), UID: ns1.GetUID()})
			Expect(instance).To(BeNil(), "Namespace instance should be nil")
		})

		By("removing second namespace", func() {
			cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

			err := cs.CoreV1().
				Namespaces().
				Delete(context.TODO(), ns2.Name, metav1.DeleteOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)).Should(Succeed())

			Expect(t.Status.Size).To(Equal(uint(0)))

			instance := t.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: ns2.GetName(), UID: ns2.GetUID()})
			Expect(instance).To(BeNil(), "Namespace instance should be nil")
		})
	})
})
