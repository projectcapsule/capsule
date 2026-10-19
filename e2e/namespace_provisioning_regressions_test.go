// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

var _ = Describe("namespace provisioning regressions", Label("namespace-rbac-priority", "namespace", "rolebindings", "metadata"), func() {
	It("revokes dynamic ownership while preserving explicit namespace permissions and tenant isolation", func() {
		ctx := context.Background()
		name := "e2e-dynamic-provisioning-" + rand.String(6)
		a := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
			Owners:      rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: name}},
			Permissions: capsulev1beta2.Permissions{MatchOwners: []*metav1.LabelSelector{{MatchLabels: map[string]string{"provisioning.capsule.dev/owner": name}}}},
			Rules:       []*rules.NamespaceRuleBodyTenant{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "reader"}}, Permissions: rules.NamespaceRulePermissionBody{Bindings: []rbac.AdditionalRoleBindingsSpec{{ClusterRoleName: "view", Subjects: []rbacv1.Subject{{Kind: rbacv1.UserKind, Name: name + "-reader"}}}}}}},
		}}
		b := &capsulev1beta2.Tenant{Name: name + "-other", Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: name + "-other"}}}}
		for _, tnt := range []*capsulev1beta2.Tenant{a, b} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
		createNamespace := func(tnt *capsulev1beta2.Tenant, profile string, size uint) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name, "profile": profile})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceReady(tnt, ns, size)
			return ns
		}
		source, foreign := createNamespace(a, "plain", 1), createNamespace(b, "reader", 1)
		owner := &capsulev1beta2.TenantOwner{
			Name: name, Labels: map[string]string{"env": "e2e", "provisioning.capsule.dev/owner": name},
			Spec: capsulev1beta2.TenantOwnerSpec{CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Kind: rbac.UserOwner, Name: name + "-dynamic"}, ClusterRoles: []string{"edit"}}},
		}
		Expect(k8sClient.Create(ctx, owner)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, owner))).To(Succeed()) })
		dynamic := ownerClient(owner.Spec.UserSpec)
		Eventually(func() error {
			_, err := dynamic.CoreV1().ConfigMaps(source.Name).List(ctx, metav1.ListOptions{})
			return err
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		_, err := dynamic.CoreV1().ConfigMaps(foreign.Name).List(ctx, metav1.ListOptions{})
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "dynamic ownership must remain scoped to Tenant A: %v", err)
		Expect(k8sClient.Delete(ctx, owner)).To(Succeed())
		// Create new profiles immediately after revocation, before waiting for owner status.
		selected, plain := createNamespace(a, "reader", 2), createNamespace(a, "plain", 3)
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current)).To(Succeed())
			for _, evaluated := range current.Status.Owners {
				g.Expect(evaluated.Name).NotTo(Equal(owner.Spec.Name))
			}
			for _, ns := range []*corev1.Namespace{source, selected, plain} {
				_, err := dynamic.CoreV1().ConfigMaps(ns.Name).List(ctx, metav1.ListOptions{})
				g.Expect(apierrors.IsForbidden(err)).To(BeTrue(), "dynamic revocation must converge before testing writes: %v", err)
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		reader := ownerClient(rbac.UserSpec{Kind: rbac.UserOwner, Name: name + "-reader"})
		for _, ns := range []*corev1.Namespace{selected, plain} {
			cm := &corev1.ConfigMap{Name: "owner-access", Namespace: ns.Name, Data: map[string]string{"value": "kept"}}
			_, err := ownerClient(a.Spec.Owners[0].UserSpec).CoreV1().ConfigMaps(ns.Name).Create(ctx, cm, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			_, err = reader.CoreV1().ConfigMaps(ns.Name).Get(ctx, cm.Name, metav1.GetOptions{})
			if ns.Name == selected.Name {
				Expect(err).NotTo(HaveOccurred())
			} else {
				Expect(apierrors.IsForbidden(err)).To(BeTrue(), "reader must not receive the plain profile: %v", err)
			}
			_, err = dynamic.CoreV1().ConfigMaps(ns.Name).Create(ctx, &corev1.ConfigMap{Name: "unauthorized"}, metav1.CreateOptions{})
			Expect(apierrors.IsForbidden(err)).To(BeTrue(), "revoked owner must not gain new namespace access: %v", err)
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: "unauthorized"}, &corev1.ConfigMap{}))).To(BeTrue())
		}
		_, err = reader.CoreV1().ConfigMaps(foreign.Name).List(ctx, metav1.ListOptions{})
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "explicit permissions must not cross tenants: %v", err)
	})

	It("tracks managed namespace metadata through policy removal and profile changes", func() {
		ctx := context.Background()
		name := "e2e-metadata-inventory-" + rand.String(6)
		managedKey := "provisioning.capsule.dev/managed"
		metadata := []api.AdditionalMetadataSelectorSpec{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "selected"}}, Labels: map[string]string{managedKey: "first"}, Annotations: map[string]string{managedKey: "first"}}}
		a := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: name}}, NamespaceOptions: &capsulev1beta2.NamespaceOptions{AdditionalMetadataList: metadata}}}
		b := &capsulev1beta2.Tenant{Name: name + "-other", Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: name + "-other"}}}}
		for _, tnt := range []*capsulev1beta2.Tenant{a, b} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
		createNamespace := func(tnt *capsulev1beta2.Tenant, profile string, size uint) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name, "profile": profile, "user.capsule.dev/keep": "yes"})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceReady(tnt, ns, size)
			return ns
		}
		selected, plain, foreign := createNamespace(a, "selected", 1), createNamespace(a, "plain", 2), createNamespace(b, "selected", 1)
		expectMetadata := func(value string) {
			Eventually(func(g Gomega) {
				current := &capsulev1beta2.Tenant{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current)).To(Succeed())
				g.Expect(current.Status.ObservedGeneration).To(Equal(current.Generation))
				for _, ns := range []*corev1.Namespace{selected, plain, foreign} {
					actual := &corev1.Namespace{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(ns), actual)).To(Succeed())
					g.Expect(actual.Labels).To(HaveKeyWithValue("user.capsule.dev/keep", "yes"))
					if ns.Name == selected.Name && value != "" {
						g.Expect(actual.Labels).To(HaveKeyWithValue(managedKey, value))
						g.Expect(actual.Annotations).To(HaveKeyWithValue(managedKey, value))
						inventory := current.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: ns.Name, UID: actual.UID})
						g.Expect(inventory).NotTo(BeNil())
						g.Expect(inventory.UID).To(Equal(actual.UID))
						g.Expect(inventory.Metadata).NotTo(BeNil())
						g.Expect(inventory.Metadata.Labels).To(HaveKeyWithValue(managedKey, value))
						g.Expect(inventory.Metadata.Annotations).To(HaveKeyWithValue(managedKey, value))
					} else {
						g.Expect(actual.Labels).NotTo(HaveKey(managedKey))
						g.Expect(actual.Annotations).NotTo(HaveKey(managedKey))
					}
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		expectMetadata("first")
		updateMetadata := func(items []api.AdditionalMetadataSelectorSpec) {
			Eventually(func() error {
				current := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current); err != nil {
					return err
				}
				current.Spec.NamespaceOptions.AdditionalMetadataList = items
				return k8sClient.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		updateMetadata(nil)
		expectMetadata("")
		metadata[0].Labels[managedKey], metadata[0].Annotations[managedKey] = "second", "second"
		updateMetadata(metadata)
		expectMetadata("second")
		// Change the namespace profile as an administrator; owner permissions are unchanged.
		Eventually(func() error {
			current := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(selected), current); err != nil {
				return err
			}
			current.Labels["profile"] = "plain"
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectMetadata("")
	})
})
