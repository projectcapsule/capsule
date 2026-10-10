// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

var _ = Describe("namespace assignment respects tenant ownership", Label("namespace", "tenant", "assignment"), func() {
	It("allows owners and rejects cross-tenant namespace claims", Label("security-assessment"), func() {
		ctx := context.Background()
		prefix := "e2e-ownership-" + rand.String(8)
		var tenants []*capsulev1beta2.Tenant
		var namespaces []*corev1.Namespace
		for _, suffix := range []string{"a", "b"} {
			name := prefix + "-" + suffix
			tnt := &capsulev1beta2.Tenant{
				Name:   name,
				Labels: map[string]string{"env": "e2e"},
				Spec: capsulev1beta2.TenantSpec{
					Owners: rbac.OwnerListSpec{{Name: name, Kind: rbac.UserOwner}},
				},
			}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			tenants = append(tenants, tnt)
		}

		By("persisting each owner's namespace with the intended tenant identity")
		for _, tnt := range tenants {
			ns := NewNamespace(tnt.Name+"-owned", map[string]string{meta.TenantLabel: tnt.Name})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceReady(tnt, ns, 1)
			Eventually(func(g Gomega) {
				current := &corev1.Namespace{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(ns), current)).To(Succeed())
				g.Expect(current.Labels).To(HaveKeyWithValue(meta.TenantLabel, tnt.Name))
				refs := tenant.TenantOwnerReferences(current)
				g.Expect(refs).To(HaveLen(1))
				g.Expect(tenant.IsTenantOwnerReferenceForTenant(refs[0], tnt)).To(BeTrue())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			namespaces = append(namespaces, ns)
		}

		By("rejecting both owners' attempts to create namespaces in the other tenant")
		for index, target := range tenants {
			actor := tenants[1-index].Spec.Owners[0].UserSpec
			ns := NewNamespace(target.Name+"-unauthorized", map[string]string{meta.TenantLabel: target.Name})
			_, err := ownerClient(actor).CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
			if err == nil {
				Fail("cross-tenant namespace creation succeeded")
			}
			Expect(apierrors.IsBadRequest(err)).To(BeTrue(), "expected Capsule assignment rejection: %v", err)
			Expect(err.Error()).To(ContainSubstring("can not assign the desired namespace to a non-owned Tenant"))
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(ns), &corev1.Namespace{}))).To(BeTrue())
			TenantNamespaceReady(target, namespaces[index], 1)
		}
	})
})
