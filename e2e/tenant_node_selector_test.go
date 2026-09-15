// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/utils"
)

var _ = Describe("updating Tenant node selectors", Label("tenant", "namespace", "node-selector"), func() {
	DescribeTable("propagates the selector to existing namespaces",
		func(initialSelector map[string]string) {
			ctx := context.Background()
			tnt := &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "e2e-node-selector-", Labels: map[string]string{"env": "e2e"}},
				Spec: capsulev1beta2.TenantSpec{
					Owners:       rbac.OwnerListSpec{{CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Name: "node-selector-owner", Kind: "User"}}}},
					NodeSelector: initialSelector,
				},
			}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, tnt) }).Should(Succeed())
			DeferCleanup(func() { EventuallyDeletion(tnt) })
			TenantReadyTrue(tnt)

			createNamespace := func(index int) *corev1.Namespace {
				ns := NewNamespace(fmt.Sprintf("%s-%d", tnt.Name, index), map[string]string{meta.TenantLabel: tnt.Name})
				NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
				NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
				return ns
			}
			assertAnnotation := func(ns *corev1.Namespace, value string, present bool) {
				Eventually(func(g Gomega) {
					current := &corev1.Namespace{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ns.Name}, current)).To(Succeed())
					if present {
						g.Expect(current.Annotations).To(HaveKeyWithValue(utils.NodeSelectorAnnotation, value))
					} else {
						g.Expect(current.Annotations).NotTo(HaveKey(utils.NodeSelectorAnnotation))
					}
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}

			By("creating namespaces before changing the Tenant selector")
			namespaces := []*corev1.Namespace{createNamespace(0), createNamespace(1)}
			for _, ns := range namespaces {
				assertAnnotation(ns, "node-type=worker", len(initialSelector) > 0)
			}

			By("updating the Tenant selector and waiting for every existing namespace to reconcile")
			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: tnt.Name}, tnt); err != nil {
					return err
				}
				tnt.Spec.NodeSelector = map[string]string{"node-type": "compute"}
				return k8sClient.Update(ctx, tnt)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			for _, ns := range namespaces {
				assertAnnotation(ns, "node-type=compute", true)
				TenantNamespaceReady(tnt, ns, 2)
			}
			TenantReadyTrue(tnt)

			By("applying the new selector to namespaces created afterward")
			assertAnnotation(createNamespace(2), "node-type=compute", true)

			By("preventing the tenant owner from changing or removing the enforced selector")
			owner := ownerClient(tnt.Spec.Owners[0].UserSpec)
			for _, value := range []any{"node-type=unauthorized", nil} {
				patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]any{utils.NodeSelectorAnnotation: value}}})
				Expect(err).NotTo(HaveOccurred())
				updated, err := owner.CoreV1().Namespaces().Patch(ctx, namespaces[0].Name, types.MergePatchType, patch, metav1.PatchOptions{})
				if err != nil {
					Expect(apierrors.IsForbidden(err)).To(BeTrue(), "unexpected patch error: %v", err)
					Expect(err.Error()).To(ContainSubstring(utils.NodeSelectorAnnotation))
				} else {
					// The mutating webhook may restore the enforced value before
					// validation. It must never admit the requested override.
					Expect(updated.Annotations).To(HaveKeyWithValue(utils.NodeSelectorAnnotation, "node-type=compute"))
				}
				assertAnnotation(namespaces[0], "node-type=compute", true)
			}
		},
		Entry("when changing an existing selector", map[string]string{"node-type": "worker"}),
		Entry("when adding a selector to a Tenant with existing namespaces", map[string]string(nil)),
	)
})
