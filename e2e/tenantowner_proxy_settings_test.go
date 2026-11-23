// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("Owner proxy settings", Label("tenant", "tenantowner", "owners", "proxy-settings"), func() {
	proxySettingsTenant := func(name string, owners rbac.OwnerListSpec) *capsulev1beta2.Tenant {
		return &capsulev1beta2.Tenant{
			Name:   name,
			Labels: map[string]string{"env": "e2e"},
			Spec: capsulev1beta2.TenantSpec{
				Owners: owners,
			},
		}
	}

	ownerWithProxy := func(kind rbac.OwnerKind, name string, settings ...rbac.ProxySettings) rbac.OwnerSpec {
		return rbac.OwnerSpec{
			CoreOwnerSpec: rbac.CoreOwnerSpec{
				UserSpec: rbac.UserSpec{Kind: kind, Name: name},
			},
			ProxyOperations: settings,
		}
	}

	// ownerIdentities projects the persisted owners onto kind/name pairs so
	// assertions do not depend on defaulted fields such as clusterRoles.
	ownerIdentities := func(owners rbac.OwnerListSpec) []rbac.UserSpec {
		out := make([]rbac.UserSpec, 0, len(owners))
		for _, owner := range owners {
			out = append(out, owner.UserSpec)
		}

		return out
	}

	It("finds every owner kind in a mixed-kind list without reordering the persisted spec", func() {
		ctx := context.Background()
		name := "e2e-proxy-settings-" + rand.String(6)

		user := ownerWithProxy(rbac.UserOwner, name+"-auser",
			rbac.ProxySettings{Kind: rbac.IngressClassesProxy, Operations: []rbac.ProxyOperation{rbac.DeleteOperation}},
		)
		group := ownerWithProxy(rbac.GroupOwner, name+"-zgroup",
			rbac.ProxySettings{Kind: rbac.NodesProxy, Operations: []rbac.ProxyOperation{rbac.ListOperation}},
		)
		sa := ownerWithProxy(rbac.ServiceAccountOwner, "system:serviceaccount:default:"+name+"-msa",
			rbac.ProxySettings{Kind: rbac.StorageClassesProxy, Operations: []rbac.ProxyOperation{rbac.ListOperation, rbac.UpdateOperation}},
		)

		// The list is deliberately not sorted by (kind, name): the Group
		// owner is followed by owners with larger kinds and smaller names,
		// the exact shape that made the previous binary search miss it, and
		// any in-place sorting of the fetched spec is observable.
		tnt := proxySettingsTenant(name, rbac.OwnerListSpec{user, group, sa})
		Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
		DeferCleanup(DeleteInBackground, tnt)
		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)

		current := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tnt), current)).To(Succeed())
		Expect(ownerIdentities(current.Spec.Owners)).To(Equal([]rbac.UserSpec{user.UserSpec, group.UserSpec, sa.UserSpec}))

		Expect(current.GetOwnerProxySettings(group.Name, rbac.GroupOwner)).To(Equal(group.ProxyOperations))
		Expect(current.GetOwnerProxySettings(sa.Name, rbac.ServiceAccountOwner)).To(Equal(sa.ProxyOperations))
		Expect(current.GetOwnerProxySettings(user.Name, rbac.UserOwner)).To(Equal(user.ProxyOperations))

		By("leaving the fetched owner list untouched")
		Expect(ownerIdentities(current.Spec.Owners)).To(Equal([]rbac.UserSpec{user.UserSpec, group.UserSpec, sa.UserSpec}))
	})

	It("returns no proxy settings for unknown or kind-mismatched owners", func() {
		ctx := context.Background()
		name := "e2e-proxy-negative-" + rand.String(6)

		group := ownerWithProxy(rbac.GroupOwner, name+"-zgroup",
			rbac.ProxySettings{Kind: rbac.NodesProxy, Operations: []rbac.ProxyOperation{rbac.ListOperation}},
		)
		user := ownerWithProxy(rbac.UserOwner, name+"-auser")

		tnt := proxySettingsTenant(name, rbac.OwnerListSpec{group, user})
		Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
		DeferCleanup(DeleteInBackground, tnt)
		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)

		current := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tnt), current)).To(Succeed())

		Expect(current.GetOwnerProxySettings(name+"-missing", rbac.GroupOwner)).To(BeEmpty())
		Expect(current.GetOwnerProxySettings(group.Name, rbac.UserOwner)).To(BeEmpty())
		Expect(current.GetOwnerProxySettings(user.Name, rbac.GroupOwner)).To(BeEmpty())
		Expect(current.GetOwnerProxySettings(user.Name, rbac.UserOwner)).To(BeEmpty())
	})

	It("keeps proxy settings scoped to each tenant", func() {
		ctx := context.Background()
		name := "e2e-proxy-isolation-" + rand.String(6)
		sharedGroup := name + "-group"

		groupA := ownerWithProxy(rbac.GroupOwner, sharedGroup,
			rbac.ProxySettings{Kind: rbac.NodesProxy, Operations: []rbac.ProxyOperation{rbac.ListOperation}},
		)
		groupB := ownerWithProxy(rbac.GroupOwner, sharedGroup,
			rbac.ProxySettings{Kind: rbac.NodesProxy, Operations: []rbac.ProxyOperation{rbac.ListOperation, rbac.UpdateOperation}},
		)
		userB := ownerWithProxy(rbac.UserOwner, name+"-buser",
			rbac.ProxySettings{Kind: rbac.StorageClassesProxy, Operations: []rbac.ProxyOperation{rbac.DeleteOperation}},
		)

		tntA := proxySettingsTenant(name+"-a", rbac.OwnerListSpec{groupA})
		tntB := proxySettingsTenant(name+"-b", rbac.OwnerListSpec{groupB, userB})

		for _, tnt := range []*capsulev1beta2.Tenant{tntA, tntB} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(DeleteInBackground, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}

		currentA, currentB := &capsulev1beta2.Tenant{}, &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tntA), currentA)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tntB), currentB)).To(Succeed())

		Expect(currentA.GetOwnerProxySettings(sharedGroup, rbac.GroupOwner)).To(Equal(groupA.ProxyOperations))
		Expect(currentB.GetOwnerProxySettings(sharedGroup, rbac.GroupOwner)).To(Equal(groupB.ProxyOperations))
		Expect(currentB.GetOwnerProxySettings(userB.Name, rbac.UserOwner)).To(Equal(userB.ProxyOperations))
		Expect(currentA.GetOwnerProxySettings(userB.Name, rbac.UserOwner)).To(BeEmpty())
	})

	It("reflects owner and proxy settings updates", func() {
		ctx := context.Background()
		name := "e2e-proxy-update-" + rand.String(6)

		group := ownerWithProxy(rbac.GroupOwner, name+"-zgroup",
			rbac.ProxySettings{Kind: rbac.NodesProxy, Operations: []rbac.ProxyOperation{rbac.ListOperation}},
		)
		user := ownerWithProxy(rbac.UserOwner, name+"-auser",
			rbac.ProxySettings{Kind: rbac.StorageClassesProxy, Operations: []rbac.ProxyOperation{rbac.ListOperation}},
		)

		tnt := proxySettingsTenant(name, rbac.OwnerListSpec{group, user})
		Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
		DeferCleanup(DeleteInBackground, tnt)
		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)

		updatedSettings := []rbac.ProxySettings{
			{Kind: rbac.NodesProxy, Operations: []rbac.ProxyOperation{rbac.ListOperation, rbac.DeleteOperation}},
		}

		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tnt), current)).To(Succeed())
			current.Spec.Owners[0].ProxyOperations = updatedSettings
			current.Spec.Owners = current.Spec.Owners[:1]
			g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tnt), current)).To(Succeed())
			g.Expect(current.Status.ObservedGeneration).To(Equal(current.Generation))
			g.Expect(ownerIdentities(current.Spec.Owners)).To(Equal([]rbac.UserSpec{group.UserSpec}))
			g.Expect(current.GetOwnerProxySettings(group.Name, rbac.GroupOwner)).To(Equal(updatedSettings))
			g.Expect(current.GetOwnerProxySettings(user.Name, rbac.UserOwner)).To(BeEmpty())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})
})
