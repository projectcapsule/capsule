// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("Owners", Label("tenant", "permissions", "owners"), func() {
	originConfig := &capsulev1beta2.CapsuleConfiguration{}

	tnt1 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-owners-1",
		},
		Spec: capsulev1beta2.TenantSpec{
			Permissions: capsulev1beta2.Permissions{
				MatchOwners: []*metav1.LabelSelector{
					{
						MatchLabels: map[string]string{
							"customer": "x",
						},
					},
					{
						MatchLabels: map[string]string{
							"team": "devops",
						},
					},
				},
			},
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-owners-1",
							Kind: "User",
						},
					},
				},
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-owners-1-group",
							Kind: "Group",
						},
					},
				},
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "system:serviceaccount:capsule-system:capsule",
							Kind: "ServiceAccount",
						},
					},
				},
			},
		},
	}

	tnt2 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-owners-2",
		},
		Spec: capsulev1beta2.TenantSpec{
			Permissions: capsulev1beta2.Permissions{
				MatchOwners: []*metav1.LabelSelector{
					{
						MatchLabels: map[string]string{
							"customer": "x",
						},
					},
					{
						MatchLabels: map[string]string{
							"team": "infrastructure",
						},
					},
				},
			},
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-owners-2",
							Kind: "User",
						},
					},
				},
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-owners-2-group",
							Kind: "Group",
						},
					},
				},
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "system:serviceaccount:capsule-system:capsule",
							Kind: "ServiceAccount",
						},
					},
				},
			},
		},
	}

	ownersInfra := &capsulev1beta2.TenantOwner{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-owners-infra",
			Labels: map[string]string{
				"team": "infrastructure",
			},
		},
		Spec: capsulev1beta2.TenantOwnerSpec{
			Aggregate: true,
			CoreOwnerSpec: rbac.CoreOwnerSpec{
				UserSpec: rbac.UserSpec{
					Kind: rbac.GroupOwner,
					Name: "oidc:comp:administrators",
				},
				ClusterRoles: []string{
					"admin",
				},
			},
		},
	}

	ownersDevops := &capsulev1beta2.TenantOwner{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-owners-devops",
			Labels: map[string]string{
				"team": "devops",
			},
		},
		Spec: capsulev1beta2.TenantOwnerSpec{
			Aggregate: true,
			CoreOwnerSpec: rbac.CoreOwnerSpec{
				UserSpec: rbac.UserSpec{
					Kind: rbac.GroupOwner,
					Name: "oidc:comp:devops",
				},
				ClusterRoles: []string{
					"view",
				},
			},
		},
	}

	ownersCommon := &capsulev1beta2.TenantOwner{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-owners-common",
			Labels: map[string]string{
				"team":     "infrastructure",
				"customer": "x",
			},
		},
		Spec: capsulev1beta2.TenantOwnerSpec{
			Aggregate: true,
			CoreOwnerSpec: rbac.CoreOwnerSpec{
				UserSpec: rbac.UserSpec{
					Kind: rbac.ServiceAccountOwner,
					Name: "system:serviceaccount:capsule-system:capsule",
				},
				ClusterRoles: []string{
					"edit",
				},
			},
		},
	}

	tnt1Owner := &capsulev1beta2.TenantOwner{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-owners-tnt",
			Labels: map[string]string{
				meta.NewTenantLabel: tnt1.GetName(),
			},
		},
		Spec: capsulev1beta2.TenantOwnerSpec{
			Aggregate: true,
			CoreOwnerSpec: rbac.CoreOwnerSpec{
				UserSpec: rbac.UserSpec{
					Kind: rbac.UserOwner,
					Name: "tnt-1-user",
				},
				ClusterRoles: []string{
					"edit",
				},
			},
		},
	}

	userOwnersCommon := &capsulev1beta2.TenantOwner{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-owners-common-user",
			Labels: map[string]string{
				"team":     "infrastructure",
				"customer": "x",
			},
		},
		Spec: capsulev1beta2.TenantOwnerSpec{
			Aggregate: false,
			CoreOwnerSpec: rbac.CoreOwnerSpec{
				UserSpec: rbac.UserSpec{
					Kind: rbac.UserOwner,
					Name: "some-user",
				},
				ClusterRoles: []string{
					"view",
				},
			},
		},
	}

	JustBeforeEach(func() {
		Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: defaultConfigurationName}, originConfig)).To(Succeed())

		for _, tnt := range []*capsulev1beta2.Tenant{tnt1, tnt2} {
			EventuallyCreation(func() error {
				tnt.ResourceVersion = ""

				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
		}

		for _, tnt := range []*capsulev1beta2.TenantOwner{ownersInfra, ownersDevops, ownersCommon, userOwnersCommon, tnt1Owner} {
			EventuallyCreation(func() error {
				tnt.ResourceVersion = ""

				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
		}
	})

	JustAfterEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{tnt1, tnt2} {
			EventuallyDeletion(tnt)
		}

		for _, owners := range []*capsulev1beta2.TenantOwner{ownersInfra, ownersDevops, ownersCommon, userOwnersCommon, tnt1Owner} {
			EventuallyDeletion(owners)
		}
	})

	It("Verify owners for", func() {
		By("checking configuration", func() {
			Eventually(func(g Gomega) {
				cfg := &capsulev1beta2.CapsuleConfiguration{}
				err := k8sClient.Get(
					context.Background(),
					client.ObjectKey{Name: defaultConfigurationName},
					cfg,
				)
				g.Expect(err).ToNot(HaveOccurred())

				expected := rbac.UserListSpec{
					{Kind: ownersInfra.Spec.Kind, Name: ownersInfra.Spec.Name},
					{Kind: ownersDevops.Spec.Kind, Name: ownersDevops.Spec.Name},
					{Kind: ownersCommon.Spec.Kind, Name: ownersCommon.Spec.Name},
					{Kind: tnt1Owner.Spec.Kind, Name: tnt1Owner.Spec.Name},
					{Kind: rbac.GroupOwner, Name: "projectcapsule.dev"},
				}

				g.Expect(cfg.Status.Users).To(ConsistOf(expected))

				g.Expect(cfg.Status.Users).NotTo(ContainElement(
					rbac.UserSpec{Kind: userOwnersCommon.Spec.Kind, Name: userOwnersCommon.Spec.Name},
				))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("checking owners (e2e-owners-1)", func() {
			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt1.GetName()}, t)).Should(Succeed())

			expectedOwners := rbac.OwnerStatusListSpec{
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.GroupOwner,
						Name: "e2e-owners-1-group",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.GroupOwner,
						Name: "oidc:comp:devops",
					},
					ClusterRoles: []string{"view"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.ServiceAccountOwner,
						Name: "system:serviceaccount:capsule-system:capsule",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter", "edit"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.UserOwner,
						Name: "e2e-owners-1",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: userOwnersCommon.Spec.Kind,
						Name: userOwnersCommon.Spec.Name,
					},
					ClusterRoles: []string{"view"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: tnt1Owner.Spec.Kind,
						Name: tnt1Owner.Spec.Name,
					},
					ClusterRoles: []string{"edit"},
				},
			}

			Expect(normalizeOwners(t.Status.Owners)).
				To(Equal(normalizeOwners(expectedOwners)))

			VerifyTenantRoleBindings(t)
		})

		By("creating namespaces (e2e-owners-1)", func() {
			for _, u := range []rbac.UserSpec{
				rbac.UserSpec{
					Kind: rbac.GroupOwner,
					Name: "e2e-owners-1-group",
				},
				rbac.UserSpec{
					Kind: rbac.GroupOwner,
					Name: "oidc:comp:devops",
				},
				rbac.UserSpec{
					Kind: rbac.ServiceAccountOwner,
					Name: "system:serviceaccount:capsule-system:capsule",
				},
				rbac.UserSpec{
					Kind: rbac.UserOwner,
					Name: "e2e-owners-1",
				},
				rbac.UserSpec{
					Kind: userOwnersCommon.Spec.Kind,
					Name: userOwnersCommon.Spec.Name,
				},
				rbac.UserSpec{
					Kind: tnt1Owner.Spec.Kind,
					Name: tnt1Owner.Spec.Name,
				},
			} {
				ns := NewNamespace("", map[string]string{
					meta.TenantLabel: tnt1.GetName(),
				})
				NamespaceCreation(ns, u, defaultTimeoutInterval).Should(Succeed())
				TenantNamespaceList(tnt1, defaultTimeoutInterval).Should(ContainElements(ns.GetName()))
			}
		})

		By("checking owners (e2e-owners-2)", func() {
			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt2.GetName()}, t)).Should(Succeed())

			expectedOwners := rbac.OwnerStatusListSpec{
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.GroupOwner,
						Name: "e2e-owners-2-group",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.GroupOwner,
						Name: "oidc:comp:administrators",
					},
					ClusterRoles: []string{"admin"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.ServiceAccountOwner,
						Name: "system:serviceaccount:capsule-system:capsule",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter", "edit"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.UserOwner,
						Name: "e2e-owners-2",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},

				{
					UserSpec: rbac.UserSpec{
						Kind: userOwnersCommon.Spec.Kind,
						Name: userOwnersCommon.Spec.Name,
					},
					ClusterRoles: []string{"view"},
				},
			}

			Expect(normalizeOwners(t.Status.Owners)).
				To(Equal(normalizeOwners(expectedOwners)))

			VerifyTenantRoleBindings(t)
		})

		By("creating namespaces (e2e-owners-2)", func() {
			for _, u := range []rbac.UserSpec{
				rbac.UserSpec{
					Kind: rbac.GroupOwner,
					Name: "e2e-owners-2-group",
				},
				rbac.UserSpec{
					Kind: rbac.GroupOwner,
					Name: "oidc:comp:administrators",
				},
				rbac.UserSpec{
					Kind: rbac.ServiceAccountOwner,
					Name: "system:serviceaccount:capsule-system:capsule",
				},
				rbac.UserSpec{
					Kind: rbac.UserOwner,
					Name: "e2e-owners-2",
				},
				rbac.UserSpec{
					Kind: userOwnersCommon.Spec.Kind,
					Name: userOwnersCommon.Spec.Name,
				},
			} {
				ns := NewNamespace("", map[string]string{
					meta.TenantLabel: tnt2.GetName(),
				})
				NamespaceCreation(ns, u, defaultTimeoutInterval).Should(Succeed())
				TenantNamespaceList(tnt2, defaultTimeoutInterval).Should(ContainElements(ns.GetName()))
			}
		})

		By("remove common tenant-owners", func() {
			Expect(k8sClient.Delete(context.TODO(), ownersCommon)).Should(Succeed())
		})

		By("checking configuration", func() {
			Eventually(func(g Gomega) {
				cfg := &capsulev1beta2.CapsuleConfiguration{}
				err := k8sClient.Get(
					context.Background(),
					client.ObjectKey{Name: defaultConfigurationName},
					cfg,
				)
				g.Expect(err).ToNot(HaveOccurred())

				expected := rbac.UserListSpec{
					{Kind: ownersInfra.Spec.Kind, Name: ownersInfra.Spec.Name},
					{Kind: ownersDevops.Spec.Kind, Name: ownersDevops.Spec.Name},
					{Kind: tnt1Owner.Spec.Kind, Name: tnt1Owner.Spec.Name},
					{Kind: rbac.GroupOwner, Name: "projectcapsule.dev"},
				}

				g.Expect(cfg.Status.Users).To(ConsistOf(expected))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("checking owners (e2e-owners-1)", func() {
			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt1.GetName()}, t)).Should(Succeed())

			expectedOwners := rbac.OwnerStatusListSpec{
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.GroupOwner,
						Name: "e2e-owners-1-group",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.GroupOwner,
						Name: "oidc:comp:devops",
					},
					ClusterRoles: []string{"view"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.ServiceAccountOwner,
						Name: "system:serviceaccount:capsule-system:capsule",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.UserOwner,
						Name: "e2e-owners-1",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: userOwnersCommon.Spec.Kind,
						Name: userOwnersCommon.Spec.Name,
					},
					ClusterRoles: []string{"view"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: tnt1Owner.Spec.Kind,
						Name: tnt1Owner.Spec.Name,
					},
					ClusterRoles: []string{"edit"},
				},
			}

			Expect(normalizeOwners(t.Status.Owners)).
				To(Equal(normalizeOwners(expectedOwners)))

			VerifyTenantRoleBindings(t)
		})

		By("checking owners (e2e-owners-2)", func() {
			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt2.GetName()}, t)).Should(Succeed())

			expectedOwners := rbac.OwnerStatusListSpec{
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.GroupOwner,
						Name: "e2e-owners-2-group",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.GroupOwner,
						Name: "oidc:comp:administrators",
					},
					ClusterRoles: []string{"admin"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.ServiceAccountOwner,
						Name: "system:serviceaccount:capsule-system:capsule",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.UserOwner,
						Name: "e2e-owners-2",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: userOwnersCommon.Spec.Kind,
						Name: userOwnersCommon.Spec.Name,
					},
					ClusterRoles: []string{"view"},
				},
			}

			Expect(normalizeOwners(t.Status.Owners)).
				To(Equal(normalizeOwners(expectedOwners)))

			VerifyTenantRoleBindings(t)
		})

		By("remove admin tenant-owners", func() {
			Expect(k8sClient.Delete(context.TODO(), ownersInfra)).Should(Succeed())
		})

		By("checking configuration", func() {
			Eventually(func(g Gomega) {
				cfg := &capsulev1beta2.CapsuleConfiguration{}
				err := k8sClient.Get(
					context.Background(),
					client.ObjectKey{Name: defaultConfigurationName},
					cfg,
				)
				g.Expect(err).ToNot(HaveOccurred())

				expected := rbac.UserListSpec{
					{Kind: ownersDevops.Spec.Kind, Name: ownersDevops.Spec.Name},
					{Kind: tnt1Owner.Spec.Kind, Name: tnt1Owner.Spec.Name},
					{Kind: rbac.GroupOwner, Name: "projectcapsule.dev"},
				}

				g.Expect(cfg.Status.Users).To(ConsistOf(expected))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("checking owners (e2e-owners-1)", func() {
			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt1.GetName()}, t)).Should(Succeed())

			expectedOwners := rbac.OwnerStatusListSpec{
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.GroupOwner,
						Name: "e2e-owners-1-group",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.GroupOwner,
						Name: "oidc:comp:devops",
					},
					ClusterRoles: []string{"view"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.ServiceAccountOwner,
						Name: "system:serviceaccount:capsule-system:capsule",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.UserOwner,
						Name: "e2e-owners-1",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: userOwnersCommon.Spec.Kind,
						Name: userOwnersCommon.Spec.Name,
					},
					ClusterRoles: []string{"view"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: tnt1Owner.Spec.Kind,
						Name: tnt1Owner.Spec.Name,
					},
					ClusterRoles: []string{"edit"},
				},
			}

			Expect(normalizeOwners(t.Status.Owners)).
				To(Equal(normalizeOwners(expectedOwners)))

			VerifyTenantRoleBindings(t)
		})

		By("checking owners (e2e-owners-2)", func() {
			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt2.GetName()}, t)).Should(Succeed())

			expectedOwners := rbac.OwnerStatusListSpec{
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.GroupOwner,
						Name: "e2e-owners-2-group",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.ServiceAccountOwner,
						Name: "system:serviceaccount:capsule-system:capsule",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.UserOwner,
						Name: "e2e-owners-2",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: userOwnersCommon.Spec.Kind,
						Name: userOwnersCommon.Spec.Name,
					},
					ClusterRoles: []string{"view"},
				},
			}

			Expect(normalizeOwners(t.Status.Owners)).
				To(Equal(normalizeOwners(expectedOwners)))

			VerifyTenantRoleBindings(t)
		})

		By("remove admin tenant-owners", func() {
			Expect(k8sClient.Delete(context.TODO(), ownersDevops)).Should(Succeed())
		})

		By("checking configuration", func() {
			Eventually(func(g Gomega) {
				cfg := &capsulev1beta2.CapsuleConfiguration{}
				err := k8sClient.Get(
					context.Background(),
					client.ObjectKey{Name: defaultConfigurationName},
					cfg,
				)
				g.Expect(err).ToNot(HaveOccurred())

				expected := rbac.UserListSpec{
					{Kind: tnt1Owner.Spec.Kind, Name: tnt1Owner.Spec.Name},
					{Kind: rbac.GroupOwner, Name: "projectcapsule.dev"},
				}

				g.Expect(cfg.Status.Users).To(ConsistOf(expected))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("checking owners (e2e-owners-1)", func() {
			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt1.GetName()}, t)).Should(Succeed())

			expectedOwners := rbac.OwnerStatusListSpec{
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.GroupOwner,
						Name: "e2e-owners-1-group",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.ServiceAccountOwner,
						Name: "system:serviceaccount:capsule-system:capsule",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.UserOwner,
						Name: "e2e-owners-1",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: userOwnersCommon.Spec.Kind,
						Name: userOwnersCommon.Spec.Name,
					},
					ClusterRoles: []string{"view"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: tnt1Owner.Spec.Kind,
						Name: tnt1Owner.Spec.Name,
					},
					ClusterRoles: []string{"edit"},
				},
			}

			Expect(normalizeOwners(t.Status.Owners)).
				To(Equal(normalizeOwners(expectedOwners)))

			VerifyTenantRoleBindings(t)
		})

		By("checking owners (e2e-owners-2)", func() {
			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt2.GetName()}, t)).Should(Succeed())

			expectedOwners := rbac.OwnerStatusListSpec{
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.GroupOwner,
						Name: "e2e-owners-2-group",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.ServiceAccountOwner,
						Name: "system:serviceaccount:capsule-system:capsule",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: rbac.UserOwner,
						Name: "e2e-owners-2",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: rbac.UserSpec{
						Kind: userOwnersCommon.Spec.Kind,
						Name: userOwnersCommon.Spec.Name,
					},
					ClusterRoles: []string{"view"},
				},
			}

			Expect(normalizeOwners(t.Status.Owners)).
				To(Equal(normalizeOwners(expectedOwners)))

			VerifyTenantRoleBindings(t)
		})
	})
})
