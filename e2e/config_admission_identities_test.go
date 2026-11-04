// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	admissionapi "github.com/projectcapsule/capsule/pkg/runtime/admission"
)

var _ = Describe("Dynamic admission identities", Label("config", "admission-identities", "namespace", "tenant"), func() {
	It("refreshes identity gates while preserving namespace ownership across tenants", func() {
		ctx := context.Background()
		prefix := "e2e-admission-identities-" + rand.String(8)
		original := &capsulev1beta2.CapsuleConfiguration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, original)).To(Succeed())

		var tenants []*capsulev1beta2.Tenant
		for _, suffix := range []string{"a", "b"} {
			tnt := &capsulev1beta2.Tenant{
				Name: prefix + "-" + suffix, Labels: map[string]string{"env": "e2e"},
				Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-" + suffix}}},
			}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(DeleteInBackground, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			tenants = append(tenants, tnt)
		}
		// Restore configuration before deleting Tenants, including after an assertion failure.
		DeferCleanup(func() {
			Eventually(func() error {
				current := &capsulev1beta2.CapsuleConfiguration{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(original), current); err != nil {
					return err
				}
				current.Spec = *original.Spec.DeepCopy()
				return k8sClient.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		options := admissionapi.WebhookOptions{CapsuleUsers: true, Administrators: true}
		for phase := range 2 {
			identity := fmt.Sprintf("%s-%d", prefix, phase)
			legacyUser := identity + "-user"
			legacyGroup := identity + "-group"
			legacyServiceAccount := "system:serviceaccount:default:" + identity
			administrator := identity + "-admin"
			timeout := int32(25 + phase)
			var generation int64
			Eventually(func() error {
				current := &capsulev1beta2.CapsuleConfiguration{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(original), current); err != nil {
					return err
				}
				current.Spec.UserNames = []string{legacyUser, legacyServiceAccount}
				current.Spec.UserGroups = []string{legacyGroup}
				current.Spec.Administrators = append(append(rbac.UserListSpec{}, original.Spec.Administrators...), rbac.UserSpec{Kind: rbac.UserOwner, Name: administrator})
				for _, hook := range current.Spec.Admission.Mutating.Webhooks {
					if hook.Name == "namespaces.mutating.projectcapsule.dev" {
						hook.Options = options
						hook.TimeoutSeconds = &timeout
					}
				}
				for _, hook := range current.Spec.Admission.Validating.Webhooks {
					if hook.Name == "namespaces.validating.projectcapsule.dev" {
						hook.Options = options
						hook.TimeoutSeconds = &timeout
					}
				}
				// Timeout changes enqueue both configuration controllers for each phase.
				if err := k8sClient.Update(ctx, current); err != nil {
					return err
				}
				generation = current.Generation
				return nil
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			expectedUsers := append(append(rbac.UserListSpec{}, original.Spec.Users...),
				rbac.UserSpec{Kind: rbac.UserOwner, Name: legacyUser},
				rbac.UserSpec{Kind: rbac.ServiceAccountOwner, Name: legacyServiceAccount},
				rbac.UserSpec{Kind: rbac.GroupOwner, Name: legacyGroup})
			expectedAdmins := append(append(rbac.UserListSpec{}, original.Spec.Administrators...), rbac.UserSpec{Kind: rbac.UserOwner, Name: administrator})
			expected := admissionapi.BuildGatingUserCondition(options, expectedUsers, expectedAdmins)
			Eventually(func(g Gomega) {
				current := &capsulev1beta2.CapsuleConfiguration{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(original), current)).To(Succeed())
				g.Expect(current.Status.ObservedGeneration).To(Equal(generation))
				mutating := &admissionv1.MutatingWebhookConfiguration{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: string(current.Spec.Admission.Mutating.Name)}, mutating)).To(Succeed())
				g.Expect(mutating.Webhooks).To(ContainElement(SatisfyAll(
					HaveField("Name", "namespaces.mutating.projectcapsule.dev"), HaveField("MatchConditions", Equal(expected)), HaveField("TimeoutSeconds", Equal(&timeout)),
				)))
				validating := &admissionv1.ValidatingWebhookConfiguration{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: string(current.Spec.Admission.Validating.Name)}, validating)).To(Succeed())
				g.Expect(validating.Webhooks).To(ContainElement(SatisfyAll(
					HaveField("Name", "namespaces.validating.projectcapsule.dev"), HaveField("MatchConditions", Equal(expected)), HaveField("TimeoutSeconds", Equal(&timeout)),
				)))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			for _, tnt := range tenants {
				ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name})
				DeferCleanup(DeleteNamespaceInBackground, ctx, ns.Name)
				NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
				TenantNamespaceReady(tnt, ns, uint(phase+1))
				NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
			}
			denied := NewNamespace("", map[string]string{meta.TenantLabel: tenants[1].Name})
			DeferCleanup(DeleteNamespaceInBackground, ctx, denied.Name)
			_, err := ownerClient(tenants[0].Spec.Owners[0].UserSpec).CoreV1().Namespaces().Create(ctx, denied, metav1.CreateOptions{})
			Expect(err).To(HaveOccurred(), "a cross-tenant namespace create must never succeed")
			Expect(err.Error()).To(ContainSubstring("non-owned Tenant"))
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(denied), &corev1.Namespace{}))).To(BeTrue())
		}
	})
})
