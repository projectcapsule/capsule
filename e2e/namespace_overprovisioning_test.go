// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("creating a Namespace in over-quota of three", Ordered, Label("namespace"), func() {
	tnt := &capsulev1beta2.Tenant{
		Name: "e2e-ns-overprovision",
		Labels: map[string]string{
			"env": "e2e",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					Name: "e2e-ns-overprovision",
					Kind: "User",
				},
			},
			NamespaceOptions: &capsulev1beta2.NamespaceOptions{
				Quota: new(int32(3)),
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())

		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
	})
	JustAfterEach(func() {
		EventuallyDeletion(tnt)
	})

	It("should fail", func() {
		By("creating three Namespaces", func() {
			for _, name := range []string{"e2e-ns-overprovision-dev", "e2e-ns-overprovision-prod", "e2e-ns-overprovision-test"} {
				ns := NewNamespace(name, map[string]string{
					meta.TenantLabel: tnt.GetName(),
				})
				NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
				NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
			}
		})

		By("creating additional namespace", func() {
			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})
			cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
			_, err := cs.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
			Expect(err).ShouldNot(Succeed())
		})
	})
})

var _ = Describe("creating Namespaces concurrently in over-quota of three", Ordered, Label("namespace"), func() {
	tnt := &capsulev1beta2.Tenant{
		Name: "e2e-ns-concurrent-overprovision",
		Labels: map[string]string{
			"env": "e2e",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					Name: "e2e-ns-concurrent-overprovision",
					Kind: "User",
				},
			},
			NamespaceOptions: &capsulev1beta2.NamespaceOptions{
				Quota: new(int32(3)),
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
	})

	JustAfterEach(func() {
		EventuallyDeletion(tnt)
	})

	It("should admit no more than the quota", func() {
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		var (
			wg       sync.WaitGroup
			admitted atomic.Int32
		)

		for i := range 6 {
			ns := NewNamespace(fmt.Sprintf("%s-%d", tnt.GetName(), i), map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})

			wg.Go(func() {
				defer GinkgoRecover()

				if _, err := cs.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{}); err == nil {
					admitted.Add(1)
				}
			})
		}

		wg.Wait()

		Expect(admitted.Load()).To(Equal(int32(3)))
		Eventually(func() (uint, error) {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, current); err != nil {
				return 0, err
			}

			return current.Status.Size, nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(uint(3)))
	})
})
