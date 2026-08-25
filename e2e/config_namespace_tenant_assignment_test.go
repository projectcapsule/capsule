// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/utils"
)

var _ = Describe("Capsule administrators changing existing Namespace tenant assignment", Ordered, Serial,
	Label("config", "namespace", "administrators", "assignment", "rolebindings"), func() {
		administrator := rbac.UserSpec{
			Name: "e2e-namespace-assignment-administrator",
			Kind: rbac.UserOwner,
		}
		unprivilegedAdmin := rbac.UserSpec{
			Name: "admin",
			Kind: rbac.UserOwner,
		}
		ownerA := rbac.OwnerSpec{CoreOwnerSpec: rbac.CoreOwnerSpec{
			UserSpec: rbac.UserSpec{
				Name: "e2e-namespace-assignment-owner-a",
				Kind: rbac.UserOwner,
			},
			ClusterRoles: []string{"admin"},
		}}
		ownerB := rbac.OwnerSpec{CoreOwnerSpec: rbac.CoreOwnerSpec{
			UserSpec: rbac.UserSpec{
				Name: "e2e-namespace-assignment-owner-b",
				Kind: rbac.UserOwner,
			},
			ClusterRoles: []string{"view"},
		}}

		tenantA := &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "e2e-namespace-assignment-a",
				Labels: map[string]string{"env": "e2e"},
			},
			Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{ownerA}},
		}
		tenantB := &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "e2e-namespace-assignment-b",
				Labels: map[string]string{"env": "e2e"},
			},
			Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{ownerB}},
		}

		bindingA := ownerA.CoreOwnerSpec.ToAdditionalRolebindings()[0]
		bindingB := ownerB.CoreOwnerSpec.ToAdditionalRolebindings()[0]

		var originalConfigurationSpec *capsulev1beta2.CapsuleConfigurationSpec

		BeforeAll(func() {
			configuration := &capsulev1beta2.CapsuleConfiguration{}
			Expect(k8sClient.Get(
				context.Background(),
				client.ObjectKey{Name: defaultConfigurationName},
				configuration,
			)).To(Succeed())

			originalConfigurationSpec = configuration.Spec.DeepCopy()

		})

		JustBeforeEach(func() {
			ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
				configuration.Spec.Administrators = rbac.UserListSpec{administrator}
			})

			for _, tnt := range []*capsulev1beta2.Tenant{tenantA, tenantB} {
				EventuallyCreation(func() error {
					tnt.ResourceVersion = ""

					return k8sClient.Create(context.Background(), tnt)
				}).Should(Succeed())
				TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			}
		})

		JustAfterEach(func() {
			EventuallyDeletion(tenantA)
			EventuallyDeletion(tenantB)

			ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
				configuration.Spec = *originalConfigurationSpec
			})
		})

		newUnassignedNamespace := func() *corev1.Namespace {
			ns := NewNamespace("")
			NamespaceCreation(ns, administrator, defaultTimeoutInterval).Should(Succeed())
			DeferCleanup(func() { EventuallyDeletion(ns) })

			ExpectNamespaceNotAssignedToTenant(context.Background(), ns.Name)

			return ns
		}

		patchAssignment := func(
			ns *corev1.Namespace,
			tnt *capsulev1beta2.Tenant,
			cs kubernetes.Interface,
		) error {
			return PatchTenantAssignmentForNamespace(tnt, ns, cs)
		}

		patchDetachment := func(ns *corev1.Namespace, cs kubernetes.Interface) error {
			return PatchNamespace(ns, cs, map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						meta.TenantLabel: nil,
					},
					"ownerReferences": []interface{}{},
				},
			})
		}

		expectAssignment := func(
			ns *corev1.Namespace,
			assigned, unassigned *capsulev1beta2.Tenant,
		) {
			Eventually(func(g Gomega) {
				current := &corev1.Namespace{}
				g.Expect(k8sClient.Get(
					context.Background(),
					types.NamespacedName{Name: ns.Name},
					current,
				)).To(Succeed())
				g.Expect(current.Labels).To(HaveKeyWithValue(meta.TenantLabel, assigned.Name))
				g.Expect(tenantOwnerReferences(current)).To(Equal([]string{assigned.Name}))
				g.Expect(hasTenantOwnerReferenceByNameAndUID(current, assigned.Name, assigned.UID)).To(BeTrue())
				g.Expect(hasTenantOwnerReferenceByNameAndUID(current, unassigned.Name, unassigned.UID)).To(BeFalse())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			NamespaceIsPartOfTenant(assigned, ns).Should(Succeed())
			NamespaceIsNotPartOfTenant(unassigned, ns).Should(Succeed())
		}

		expectRoleBindings := func(
			ns *corev1.Namespace,
			present map[*capsulev1beta2.Tenant]rbac.AdditionalRoleBindingsSpec,
			absent ...rbac.AdditionalRoleBindingsSpec,
		) {
			Eventually(func(g Gomega) {
				for tnt, binding := range present {
					name := meta.NameForManagedRoleBindings(utils.RoleBindingHashFunc(binding))
					roleBinding := &rbacv1.RoleBinding{}
					err := k8sClient.Get(context.Background(), client.ObjectKey{
						Namespace: ns.Name,
						Name:      name,
					}, roleBinding)
					g.Expect(err).NotTo(HaveOccurred(),
						"expected managed RoleBinding %s/%s for Tenant %s", ns.Name, name, tnt.Name)
					g.Expect(roleBinding.RoleRef).To(Equal(rbacv1.RoleRef{
						APIGroup: rbacv1.GroupName,
						Kind:     "ClusterRole",
						Name:     binding.ClusterRoleName,
					}))
					g.Expect(roleBinding.Subjects).To(ConsistOf(binding.Subjects))
					g.Expect(roleBinding.Labels).To(HaveKeyWithValue(meta.NewTenantLabel, tnt.Name))
					g.Expect(roleBinding.Labels).To(HaveKeyWithValue(
						meta.NewManagedByCapsuleLabel,
						meta.ValueController,
					))
				}

				for _, binding := range absent {
					name := meta.NameForManagedRoleBindings(utils.RoleBindingHashFunc(binding))
					roleBinding := &rbacv1.RoleBinding{}
					err := k8sClient.Get(context.Background(), client.ObjectKey{
						Namespace: ns.Name,
						Name:      name,
					}, roleBinding)
					g.Expect(apierrors.IsNotFound(err)).To(BeTrue(),
						"expected managed RoleBinding %s/%s to be absent, got %v", ns.Name, name, err)
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}

		expectUnassigned := func(ns *corev1.Namespace) {
			ExpectNamespaceNotAssignedToTenant(context.Background(), ns.Name)
			NamespaceIsNotPartOfTenant(tenantA, ns).Should(Succeed())
			NamespaceIsNotPartOfTenant(tenantB, ns).Should(Succeed())
		}

		It("allows a configured administrator to join, migrate, and unjoin an existing Namespace", func() {
			ns := newUnassignedNamespace()
			adminClient := ownerClient(administrator)

			By("starting without either Tenant's managed RoleBinding")
			expectRoleBindings(ns, nil, bindingA, bindingB)

			By("joining the existing Namespace to the first Tenant")
			Eventually(func() error {
				return patchAssignment(ns, tenantA, adminClient)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			expectAssignment(ns, tenantA, tenantB)
			expectRoleBindings(ns, map[*capsulev1beta2.Tenant]rbac.AdditionalRoleBindingsSpec{
				tenantA: bindingA,
			}, bindingB)

			By("migrating the Namespace from the first Tenant to the second Tenant")
			Eventually(func() error {
				return patchAssignment(ns, tenantB, adminClient)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			expectAssignment(ns, tenantB, tenantA)
			expectRoleBindings(ns, map[*capsulev1beta2.Tenant]rbac.AdditionalRoleBindingsSpec{
				tenantB: bindingB,
			}, bindingA)

			By("unjoining the Namespace from the second Tenant")
			Eventually(func() error {
				return patchDetachment(ns, adminClient)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			expectUnassigned(ns)
			expectRoleBindings(ns, nil, bindingA, bindingB)
		})

		It("rejects join, migration, and unjoin attempts from other users", func() {
			actors := []struct {
				name string
				user rbac.UserSpec
			}{
				{name: "an unconfigured admin user", user: unprivilegedAdmin},
				{name: "a Tenant owner", user: ownerA.UserSpec},
			}

			for _, actor := range actors {
				By(fmt.Sprintf("checking all assignment transitions for %s", actor.name))
				ns := newUnassignedNamespace()
				actorClient := ownerClient(actor.user)
				adminClient := ownerClient(administrator)

				expectRoleBindings(ns, nil, bindingA, bindingB)

				By(fmt.Sprintf("rejecting a join by %s", actor.name))
				Expect(patchAssignment(ns, tenantA, actorClient)).To(HaveOccurred())
				expectUnassigned(ns)
				expectRoleBindings(ns, nil, bindingA, bindingB)

				By("preparing an administrator-owned transition baseline")
				Eventually(func() error {
					return patchAssignment(ns, tenantA, adminClient)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				expectAssignment(ns, tenantA, tenantB)
				expectRoleBindings(ns, map[*capsulev1beta2.Tenant]rbac.AdditionalRoleBindingsSpec{
					tenantA: bindingA,
				}, bindingB)

				By(fmt.Sprintf("rejecting a migration by %s", actor.name))
				Expect(patchAssignment(ns, tenantB, actorClient)).To(HaveOccurred())
				expectAssignment(ns, tenantA, tenantB)
				expectRoleBindings(ns, map[*capsulev1beta2.Tenant]rbac.AdditionalRoleBindingsSpec{
					tenantA: bindingA,
				}, bindingB)

				By(fmt.Sprintf("rejecting an unjoin by %s", actor.name))
				Expect(patchDetachment(ns, actorClient)).To(HaveOccurred())
				expectAssignment(ns, tenantA, tenantB)
				expectRoleBindings(ns, map[*capsulev1beta2.Tenant]rbac.AdditionalRoleBindingsSpec{
					tenantA: bindingA,
				}, bindingB)

				By("detaching with the configured administrator before cleanup")
				Eventually(func() error {
					return patchDetachment(ns, adminClient)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				expectUnassigned(ns)
				expectRoleBindings(ns, nil, bindingA, bindingB)
			}
		})
	})
