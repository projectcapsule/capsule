// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("creating a Namespace with user-specified labels and annotations", Ordered, Label("namespace", "metadata", "forbidden"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-user-metadata-forbidden",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			NamespaceOptions: &capsulev1beta2.NamespaceOptions{
				ForbiddenLabels: api.ForbiddenListSpec{
					Exact: []string{"foo", "bar"},
					Regex: "^gatsby-.*$",
				},
				ForbiddenAnnotations: api.ForbiddenListSpec{
					Exact: []string{"foo", "bar"},
					Regex: "^gatsby-.*$",
				},
			},
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-user-metadata-forbidden",
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
		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
	})
	JustAfterEach(func() {
		EventuallyDeletion(tnt)
	})

	It("should allow", func() {
		By("specifying non-forbidden labels", func() {
			ns := NewNamespace("", map[string]string{
				"bim":            "baz",
				meta.TenantLabel: tnt.GetName(),
			})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		})
		By("specifying non-forbidden annotations", func() {
			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})
			ns.SetAnnotations(map[string]string{"bim": "baz"})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		})
	})

	It("should fail when creating a Namespace", func() {
		By("specifying forbidden labels using exact match", func() {
			ns := NewNamespace("", map[string]string{
				"foo":            "bar",
				meta.TenantLabel: tnt.GetName(),
			})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceIsNotPartOfTenant(tnt, ns).Should(Succeed())

		})
		By("specifying forbidden labels using regex match", func() {
			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tnt.GetName(),
				"gatsby-foo":     "bar",
			})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceIsNotPartOfTenant(tnt, ns).Should(Succeed())
		})
		By("specifying forbidden annotations using exact match", func() {
			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})
			ns.SetAnnotations(map[string]string{"foo": "bar"})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceIsNotPartOfTenant(tnt, ns).Should(Succeed())

		})
		By("specifying forbidden annotations using regex match", func() {
			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})
			ns.SetAnnotations(map[string]string{"gatsby-foo": "bar"})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceIsNotPartOfTenant(tnt, ns).Should(Succeed())

		})
	})

	It("should fail when updating a Namespace", func() {
		role := &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ns-patch",
			},
			Rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"patch", "update"},
					APIGroups: []string{""},
					Resources: []string{"namespaces"},
				},
			},
		}

		roleBinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ns-patch",
			},
			Subjects: []rbacv1.Subject{
				{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     tnt.Spec.Owners[0].Kind.String(),
					Name:     tnt.Spec.Owners[0].Name,
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     role.GetName(),
			},
		}

		rbacPatch := func(ns string) {
			role := role.DeepCopy()
			role.SetNamespace(ns)
			Expect(k8sClient.Create(context.Background(), role)).To(Succeed())

			roleBinding := roleBinding.DeepCopy()
			roleBinding.SetNamespace(ns)
			Expect(k8sClient.Create(context.Background(), roleBinding)).To(Succeed())
		}

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		By("specifying forbidden labels using exact match", func() {
			ns := NewNamespace("forbidden-labels-exact-match", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

			rbacPatch(ns.GetName())
			Consistently(func() error {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns); err != nil {
					return nil
				}

				ns.SetLabels(map[string]string{"foo": "bar"})

				_, err := cs.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})

				return err
			}, 10*time.Second, time.Second).ShouldNot(Succeed())
		})
		By("specifying forbidden labels using regex match", func() {
			ns := NewNamespace("forbidden-labels-regex-match", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

			rbacPatch(ns.GetName())
			Consistently(func() error {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns); err != nil {
					return nil
				}

				ns.SetLabels(map[string]string{"gatsby-foo": "bar"})

				_, err := cs.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})

				return err
			}, 3*time.Second, time.Second).ShouldNot(Succeed())
		})
		By("specifying forbidden annotations using exact match", func() {
			ns := NewNamespace("forbidden-annotations-exact-match", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

			rbacPatch(ns.GetName())
			Consistently(func() error {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns); err != nil {
					return nil
				}

				ns.SetAnnotations(map[string]string{"foo": "bar"})

				_, err := cs.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})

				return err
			}, 10*time.Second, time.Second).ShouldNot(Succeed())
		})
		By("specifying forbidden annotations using regex match", func() {
			ns := NewNamespace("forbidden-annotations-regex-match", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

			rbacPatch(ns.GetName())
			Consistently(func() error {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns); err != nil {
					return nil
				}

				ns.SetAnnotations(map[string]string{"gatsby-foo": "bar"})

				_, err := cs.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})

				return err
			}, 10*time.Second, time.Second).ShouldNot(Succeed())
		})
	})
})
