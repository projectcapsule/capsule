// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
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
	"github.com/projectcapsule/capsule/pkg/utils"
)

var _ = Describe("namespace profile permission revocation", Label("namespace-rbac-priority", "managed", "rolebindings", "namespace"), func() {
	It("revokes obsolete grants during metadata failures without granting the replacement profile", func() {
		ctx := context.Background()
		name := "e2e-profile-revoke-" + rand.String(6)
		bindingFor := func(subject string) rbac.AdditionalRoleBindingsSpec {
			return rbac.AdditionalRoleBindingsSpec{ClusterRoleName: "view", Subjects: []rbacv1.Subject{{Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: name + "-" + subject}}}
		}
		old, replacement, keeper := bindingFor("old"), bindingFor("replacement"), bindingFor("keeper")
		a := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: name}},
			Rules: []*rules.NamespaceRuleBodyTenant{
				{Permissions: rules.NamespaceRulePermissionBody{Bindings: []rbac.AdditionalRoleBindingsSpec{keeper}}},
				{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "selected"}}, Permissions: rules.NamespaceRulePermissionBody{Bindings: []rbac.AdditionalRoleBindingsSpec{old}}},
			},
		}}
		b := &capsulev1beta2.Tenant{Name: name + "-other", Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: name + "-other"}}}}
		for _, tnt := range []*capsulev1beta2.Tenant{a, b} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
		selected := NewNamespace("", map[string]string{meta.TenantLabel: a.Name, "profile": "selected"})
		plain := NewNamespace("", map[string]string{meta.TenantLabel: a.Name, "profile": "plain"})
		foreign := NewNamespace("", map[string]string{meta.TenantLabel: b.Name, "profile": "selected"})
		for _, ns := range []*corev1.Namespace{selected, plain} {
			NamespaceCreation(ns, a.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		}
		NamespaceCreation(foreign, b.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceReady(a, selected, 2)
		TenantNamespaceReady(a, plain, 2)
		TenantNamespaceReady(b, foreign, 1)
		for _, ns := range []*corev1.Namespace{selected, plain, foreign} {
			Expect(k8sClient.Create(ctx, &corev1.ConfigMap{Name: "access-probe", Namespace: ns.Name})).To(Succeed())
		}
		probe := func(actor client.Client, ns string) error {
			return actor.Get(ctx, client.ObjectKey{Namespace: ns, Name: "access-probe"}, &corev1.ConfigMap{})
		}
		oldReader := impersonationClient(old.Subjects[0].Name, withDefaultGroups(nil))
		newReader := impersonationClient(replacement.Subjects[0].Name, withDefaultGroups(nil))
		keeperReader := impersonationClient(keeper.Subjects[0].Name, withDefaultGroups(nil))
		otherOwner := impersonationClient(b.Name, withDefaultGroups(nil))
		Eventually(func() error { return probe(oldReader, selected.Name) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("rejecting only the selected namespace's new managed metadata")
		const denial = "e2e injected namespace profile failure"
		policy := &admissionregistrationv1.ValidatingAdmissionPolicy{Name: name, Spec: admissionregistrationv1.ValidatingAdmissionPolicySpec{
			FailurePolicy: new(admissionregistrationv1.Fail),
			MatchConstraints: &admissionregistrationv1.MatchResources{ResourceRules: []admissionregistrationv1.NamedRuleWithOperations{{
				ResourceNames: []string{selected.Name},
				Operations:    []admissionregistrationv1.OperationType{admissionregistrationv1.Update},
				APIGroups:     []string{""}, APIVersions: []string{"v1"}, Resources: []string{"namespaces"}, Scope: new(admissionregistrationv1.ClusterScope),
			}}},
			Validations: []admissionregistrationv1.Validation{{Expression: "!has(object.metadata.labels) || !('profile-revision' in object.metadata.labels) || object.metadata.labels['profile-revision'] != 'blocked'", Message: denial}},
		}}
		admissionBinding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{Name: name, Spec: admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{PolicyName: name, ValidationActions: []admissionregistrationv1.ValidationAction{admissionregistrationv1.Deny}}}
		Expect(k8sClient.Create(ctx, policy)).To(Succeed())
		DeferCleanup(func() { ignoreNotFound(k8sClient.Delete(ctx, policy)) })
		Expect(k8sClient.Create(ctx, admissionBinding)).To(Succeed())
		DeferCleanup(func() { ignoreNotFound(k8sClient.Delete(ctx, admissionBinding)) })
		Eventually(func() error {
			current := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(selected), current); err != nil {
				return err
			}
			current.Labels["profile-revision"] = "blocked"
			return k8sClient.Update(ctx, current, client.DryRunAll)
		}, defaultTimeoutInterval, defaultPollInterval).Should(MatchError(ContainSubstring(denial)))

		By("revoking the old rule and retaining valid grants despite the profile failure")
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current); err != nil {
				return err
			}
			current.Spec.Rules[1].Permissions.Bindings = []rbac.AdditionalRoleBindingsSpec{replacement}
			current.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{AdditionalMetadataList: []api.AdditionalMetadataSelectorSpec{{NamespaceSelector: current.Spec.Rules[1].NamespaceSelector, Labels: map[string]string{"profile-revision": "blocked"}}}}
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current)).To(Succeed())
			g.Expect(current.Status.ObservedGeneration).To(Equal(current.Generation))
			stat := current.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: selected.Name})
			g.Expect(stat).NotTo(BeNil())
			ready := stat.Conditions.GetConditionByType(meta.ReadyCondition)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(ready.Message).To(ContainSubstring(denial))
			key := client.ObjectKey{Namespace: selected.Name, Name: meta.NameForManagedRoleBindings(utils.RoleBindingHashFunc(old))}
			g.Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, &rbacv1.RoleBinding{}))).To(BeTrue())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectDenied := func(actor client.Client, namespace string) {
			err := probe(actor, namespace)
			Expect(apierrors.IsForbidden(err)).To(BeTrue(), "%v", err)
			Expect(err).To(MatchError(ContainSubstring(`cannot get resource "configmaps"`)))
		}
		expectDenied(oldReader, selected.Name)
		expectDenied(newReader, selected.Name)
		expectDenied(newReader, plain.Name)
		expectDenied(keeperReader, foreign.Name)
		Expect(probe(keeperReader, selected.Name)).To(Succeed())
		Expect(probe(keeperReader, plain.Name)).To(Succeed())
		Expect(probe(otherOwner, foreign.Name)).To(Succeed())
		stored := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(selected), stored)).To(Succeed())
		Expect(stored.Labels).NotTo(HaveKey("profile-revision"))

		By("granting the replacement only after metadata reconciliation recovers")
		Expect(k8sClient.Delete(ctx, admissionBinding)).To(Succeed())
		TenantReady(a, metav1.ConditionTrue, defaultTimeoutInterval)
		TenantNamespaceReady(a, selected, 2)
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(selected), stored)).To(Succeed())
			g.Expect(stored.Labels).To(HaveKeyWithValue("profile-revision", "blocked"))
			g.Expect(probe(newReader, selected.Name)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectDenied(oldReader, selected.Name)
		expectDenied(newReader, plain.Name)
		expectDenied(newReader, foreign.Name)
		Expect(probe(keeperReader, selected.Name)).To(Succeed())
		Expect(probe(otherOwner, foreign.Name)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plain), stored)).To(Succeed())
		Expect(stored.Labels).NotTo(HaveKey("profile-revision"), fmt.Sprintf("non-selected namespace %s", plain.Name))
	})
})
