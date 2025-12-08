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
	"github.com/projectcapsule/capsule/pkg/api"
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
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "e2e-owners-1",
							Kind: "User",
						},
					},
				},
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "e2e-owners-1-group",
							Kind: "Group",
						},
					},
				},
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
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
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "e2e-owners-2",
							Kind: "User",
						},
					},
				},
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "e2e-owners-2-group",
							Kind: "Group",
						},
					},
				},
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
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
			CoreOwnerSpec: api.CoreOwnerSpec{
				UserSpec: api.UserSpec{
					Kind: api.GroupOwner,
					Name: "oidc:comp:administrators",
				},
				ClusterRoles: []string{
					"mega-admin",
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
			CoreOwnerSpec: api.CoreOwnerSpec{
				UserSpec: api.UserSpec{
					Kind: api.GroupOwner,
					Name: "oidc:comp:devops",
				},
				ClusterRoles: []string{
					"namespaced-admin",
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
			CoreOwnerSpec: api.CoreOwnerSpec{
				UserSpec: api.UserSpec{
					Kind: api.ServiceAccountOwner,
					Name: "system:serviceaccount:capsule-system:capsule",
				},
				ClusterRoles: []string{
					"service-admin",
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

		for _, tnt := range []*capsulev1beta2.TenantOwner{ownersInfra, ownersDevops, ownersCommon} {
			EventuallyCreation(func() error {
				tnt.ResourceVersion = ""

				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
		}
	})

	JustAfterEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{tnt1, tnt2} {
			err := k8sClient.Delete(context.TODO(), tnt)
			Expect(client.IgnoreNotFound(err)).To(Succeed())
		}

		for _, owners := range []*capsulev1beta2.TenantOwner{ownersInfra, ownersDevops, ownersCommon} {
			err := k8sClient.Delete(context.TODO(), owners)
			Expect(client.IgnoreNotFound(err)).To(Succeed())
		}
	})

	It("Verify owners for", func() {
		By("checking owners (e2e-owners-1)", func() {
			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt1.GetName()}, t)).Should(Succeed())

			expectedOwners := api.OwnerStatusListSpec{
				{
					UserSpec: api.UserSpec{
						Kind: api.GroupOwner,
						Name: "e2e-owners-1-group",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.GroupOwner,
						Name: "oidc:comp:devops",
					},
					ClusterRoles: []string{"namespaced-admin"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.ServiceAccountOwner,
						Name: "system:serviceaccount:capsule-system:capsule",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter", "service-admin"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.UserOwner,
						Name: "e2e-owners-1",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
			}

			Expect(normalizeOwners(t.Status.Owners)).
				To(Equal(normalizeOwners(expectedOwners)))

			VerifyTenantRoleBindings(t)
		})

		By("checking owners (e2e-owners-2)", func() {
			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt2.GetName()}, t)).Should(Succeed())

			expectedOwners := api.OwnerStatusListSpec{
				{
					UserSpec: api.UserSpec{
						Kind: api.GroupOwner,
						Name: "e2e-owners-2-group",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.GroupOwner,
						Name: "oidc:comp:administrators",
					},
					ClusterRoles: []string{"mega-admin"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.ServiceAccountOwner,
						Name: "system:serviceaccount:capsule-system:capsule",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter", "service-admin"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.UserOwner,
						Name: "e2e-owners-2",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
			}

			Expect(normalizeOwners(t.Status.Owners)).
				To(Equal(normalizeOwners(expectedOwners)))

			VerifyTenantRoleBindings(t)
		})

		By("remove common tenant-owners", func() {
			Expect(k8sClient.Delete(context.TODO(), ownersCommon)).Should(Succeed())
		})

		By("checking owners (e2e-owners-1)", func() {
			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt1.GetName()}, t)).Should(Succeed())

			expectedOwners := api.OwnerStatusListSpec{
				{
					UserSpec: api.UserSpec{
						Kind: api.GroupOwner,
						Name: "e2e-owners-1-group",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.GroupOwner,
						Name: "oidc:comp:devops",
					},
					ClusterRoles: []string{"namespaced-admin"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.ServiceAccountOwner,
						Name: "system:serviceaccount:capsule-system:capsule",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.UserOwner,
						Name: "e2e-owners-1",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
			}

			Expect(normalizeOwners(t.Status.Owners)).
				To(Equal(normalizeOwners(expectedOwners)))

			VerifyTenantRoleBindings(t)
		})

		By("checking owners (e2e-owners-2)", func() {
			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt2.GetName()}, t)).Should(Succeed())

			expectedOwners := api.OwnerStatusListSpec{
				{
					UserSpec: api.UserSpec{
						Kind: api.GroupOwner,
						Name: "e2e-owners-2-group",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.GroupOwner,
						Name: "oidc:comp:administrators",
					},
					ClusterRoles: []string{"mega-admin"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.ServiceAccountOwner,
						Name: "system:serviceaccount:capsule-system:capsule",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.UserOwner,
						Name: "e2e-owners-2",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
			}

			Expect(normalizeOwners(t.Status.Owners)).
				To(Equal(normalizeOwners(expectedOwners)))

			VerifyTenantRoleBindings(t)
		})

		By("remove admin tenant-owners", func() {
			Expect(k8sClient.Delete(context.TODO(), ownersInfra)).Should(Succeed())
		})

		By("checking owners (e2e-owners-1)", func() {
			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt1.GetName()}, t)).Should(Succeed())

			expectedOwners := api.OwnerStatusListSpec{
				{
					UserSpec: api.UserSpec{
						Kind: api.GroupOwner,
						Name: "e2e-owners-1-group",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.GroupOwner,
						Name: "oidc:comp:devops",
					},
					ClusterRoles: []string{"namespaced-admin"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.ServiceAccountOwner,
						Name: "system:serviceaccount:capsule-system:capsule",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.UserOwner,
						Name: "e2e-owners-1",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
			}

			Expect(normalizeOwners(t.Status.Owners)).
				To(Equal(normalizeOwners(expectedOwners)))

			VerifyTenantRoleBindings(t)
		})

		By("checking owners (e2e-owners-2)", func() {
			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt2.GetName()}, t)).Should(Succeed())

			expectedOwners := api.OwnerStatusListSpec{
				{
					UserSpec: api.UserSpec{
						Kind: api.GroupOwner,
						Name: "e2e-owners-2-group",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.ServiceAccountOwner,
						Name: "system:serviceaccount:capsule-system:capsule",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.UserOwner,
						Name: "e2e-owners-2",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
			}

			Expect(normalizeOwners(t.Status.Owners)).
				To(Equal(normalizeOwners(expectedOwners)))

			VerifyTenantRoleBindings(t)
		})

		By("remove admin tenant-owners", func() {
			Expect(k8sClient.Delete(context.TODO(), ownersDevops)).Should(Succeed())
		})

		By("checking owners (e2e-owners-1)", func() {
			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt1.GetName()}, t)).Should(Succeed())

			expectedOwners := api.OwnerStatusListSpec{
				{
					UserSpec: api.UserSpec{
						Kind: api.GroupOwner,
						Name: "e2e-owners-1-group",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.ServiceAccountOwner,
						Name: "system:serviceaccount:capsule-system:capsule",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.UserOwner,
						Name: "e2e-owners-1",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
			}

			Expect(normalizeOwners(t.Status.Owners)).
				To(Equal(normalizeOwners(expectedOwners)))

			VerifyTenantRoleBindings(t)
		})

		By("checking owners (e2e-owners-2)", func() {
			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt2.GetName()}, t)).Should(Succeed())

			expectedOwners := api.OwnerStatusListSpec{
				{
					UserSpec: api.UserSpec{
						Kind: api.GroupOwner,
						Name: "e2e-owners-2-group",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.ServiceAccountOwner,
						Name: "system:serviceaccount:capsule-system:capsule",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
				{
					UserSpec: api.UserSpec{
						Kind: api.UserOwner,
						Name: "e2e-owners-2",
					},
					ClusterRoles: []string{"admin", "capsule-namespace-deleter"},
				},
			}

			Expect(normalizeOwners(t.Status.Owners)).
				To(Equal(normalizeOwners(expectedOwners)))

			VerifyTenantRoleBindings(t)
		})
	})
})
