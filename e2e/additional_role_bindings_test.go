// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/utils"
)

var _ = Describe("creating a Namespace with an additional Role Binding", Ordered, Label("tenant", "permissions", "rolebindings"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-additional-role-binding",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-additional-role-binding",
							Kind: "User",
						},
					},
				},
			},
			AdditionalRoleBindings: []rbac.AdditionalRoleBindingsSpec{
				{
					ClusterRoleName: "view",
					Subjects: []rbacv1.Subject{
						{
							Kind:     "Group",
							APIGroup: rbacv1.GroupName,
							Name:     "system:authenticated",
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

		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
	})

	JustAfterEach(func() {
		EventuallyDeletion(tnt)
	})

	It("should be assigned to each Namespace", func() {
		ns1 := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(ns1, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())

		ns2 := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(ns2, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())

		t := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)).Should(Succeed())

		NamespaceIsPartOfTenant(tnt, ns1).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns2).Should(Succeed())

		VerifyTenantRoleBindings(t)
	})
})

var _ = Describe("creating additional RoleBindings from namespace rules", Ordered, Label("tenant", "rules", "permissions", "rolebindings"), func() {
	const (
		customLabel      = "reflection.proxy.projectcapsule.dev/enabled"
		customAnnotation = "projectcapsule.dev/e2e-rule-binding"
	)

	globalBinding := rbac.AdditionalRoleBindingsSpec{
		ClusterRoleName: "view",
		Subjects: []rbacv1.Subject{
			{
				Kind:     rbacv1.GroupKind,
				APIGroup: rbacv1.GroupName,
				Name:     "system:authenticated",
			},
		},
		Labels:      map[string]string{customLabel: "true"},
		Annotations: map[string]string{customAnnotation: "global"},
	}
	selectedBinding := rbac.AdditionalRoleBindingsSpec{
		ClusterRoleName: "edit",
		Subjects: []rbacv1.Subject{
			{
				Kind:     rbacv1.UserKind,
				APIGroup: rbacv1.GroupName,
				Name:     "e2e-rule-role-binding-user",
			},
		},
		Labels:      map[string]string{customLabel: "true"},
		Annotations: map[string]string{customAnnotation: "selected"},
	}

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "e2e-rule-role-bindings"},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-rule-role-bindings",
							Kind: rbac.UserOwner,
						},
					},
				},
			},
			Rules: []*rules.NamespaceRuleBodyTenant{
				{
					Permissions: rules.NamespaceRulePermissionBody{
						Bindings: []rbac.AdditionalRoleBindingsSpec{globalBinding},
					},
				},
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "prod"},
					},
					Permissions: rules.NamespaceRulePermissionBody{
						Bindings: []rbac.AdditionalRoleBindingsSpec{selectedBinding},
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

		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
	})

	JustAfterEach(func() {
		EventuallyDeletion(tnt)
	})

	It("applies bindings according to each rule's namespace selector without mutating their metadata", func() {
		prod := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"env":            "prod",
		})
		NamespaceCreation(prod, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())

		dev := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"env":            "dev",
		})
		NamespaceCreation(dev, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())

		NamespaceIsPartOfTenant(tnt, prod).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, dev).Should(Succeed())

		assertBinding := func(namespace string, binding rbac.AdditionalRoleBindingsSpec) {
			Eventually(func(g Gomega) {
				roleBinding := &rbacv1.RoleBinding{}
				err := k8sClient.Get(context.Background(), client.ObjectKey{
					Namespace: namespace,
					Name:      meta.NameForManagedRoleBindings(utils.RoleBindingHashFunc(binding)),
				}, roleBinding)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(roleBinding.RoleRef.Name).To(Equal(binding.ClusterRoleName))
				g.Expect(roleBinding.Subjects).To(ConsistOf(binding.Subjects))
				g.Expect(roleBinding.Labels).To(HaveKeyWithValue(customLabel, "true"))
				g.Expect(roleBinding.Labels).To(HaveKeyWithValue(meta.NewTenantLabel, tnt.Name))
				g.Expect(roleBinding.Labels).To(HaveKeyWithValue(meta.NewManagedByCapsuleLabel, meta.ValueController))
				g.Expect(roleBinding.Annotations).To(HaveKeyWithValue(customAnnotation, binding.Annotations[customAnnotation]))
			}).WithTimeout(defaultTimeoutInterval).WithPolling(defaultPollInterval).Should(Succeed())
		}

		assertBinding(prod.Name, globalBinding)
		assertBinding(dev.Name, globalBinding)
		assertBinding(prod.Name, selectedBinding)

		Consistently(func() bool {
			roleBinding := &rbacv1.RoleBinding{}
			err := k8sClient.Get(context.Background(), client.ObjectKey{
				Namespace: dev.Name,
				Name:      meta.NameForManagedRoleBindings(utils.RoleBindingHashFunc(selectedBinding)),
			}, roleBinding)

			return apierrors.IsNotFound(err)
		}, 2*time.Second, defaultPollInterval).Should(BeTrue())

		current := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: tnt.Name}, current)).To(Succeed())
		Expect(current.Spec.Rules[0].Permissions.Bindings[0].Labels).To(Equal(globalBinding.Labels))
		Expect(current.Spec.Rules[0].Permissions.Bindings[0].Annotations).To(Equal(globalBinding.Annotations))
		Expect(current.Spec.Rules[1].Permissions.Bindings[0].Labels).To(Equal(selectedBinding.Labels))
		Expect(current.Spec.Rules[1].Permissions.Bindings[0].Annotations).To(Equal(selectedBinding.Annotations))
	})
})
