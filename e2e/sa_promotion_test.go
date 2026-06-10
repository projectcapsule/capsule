// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"sort"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	otypes "github.com/onsi/gomega/types"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

var serviceAccountPromotionClusterRoles = []string{
	"prod-view",
	"prod-edit",
	"dev-view",
}

func expectPromotionTargets(
	tenantName string,
	serviceAccount *corev1.ServiceAccount,
	clusterRoles []string,
	targets []string,
) {
	expectedClusterRoles := append([]string(nil), clusterRoles...)
	expectedTargets := append([]string(nil), targets...)

	sort.Strings(expectedClusterRoles)
	sort.Strings(expectedTargets)

	Eventually(func(g Gomega) rbac.PromotionStatusListSpec {
		t := &capsulev1beta2.Tenant{}
		err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tenantName}, t)
		g.Expect(err).NotTo(HaveOccurred())

		return t.Status.Promotions
	}, defaultTimeoutInterval, defaultPollInterval).Should(
		ContainElement(rbac.PromotionSpec{
			UserSpec: rbac.UserSpec{
				Kind: rbac.ServiceAccountOwner,
				Name: "system:serviceaccount:" + serviceAccount.GetNamespace() + ":" + serviceAccount.GetName(),
			},
			ClusterRoles: expectedClusterRoles,
			Targets:      expectedTargets,
		}),
		"expected promotion for ServiceAccount %s/%s with clusterRoles=%v targets=%v",
		serviceAccount.Namespace,
		serviceAccount.Name,
		expectedClusterRoles,
		expectedTargets,
	)
}

func expectNoPromotionTargets(
	tenantName string,
	serviceAccount *corev1.ServiceAccount,
	clusterRoles []string,
	targets []string,
) {
	expectedClusterRoles := append([]string(nil), clusterRoles...)
	expectedTargets := append([]string(nil), targets...)

	sort.Strings(expectedClusterRoles)
	sort.Strings(expectedTargets)

	Consistently(func(g Gomega) rbac.PromotionStatusListSpec {
		t := &capsulev1beta2.Tenant{}
		err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tenantName}, t)
		g.Expect(err).NotTo(HaveOccurred())

		return t.Status.Promotions
	}, 2*time.Second, defaultPollInterval).ShouldNot(
		ContainElement(rbac.PromotionSpec{
			UserSpec: rbac.UserSpec{
				Kind: rbac.ServiceAccountOwner,
				Name: "system:serviceaccount:" + serviceAccount.GetNamespace() + ":" + serviceAccount.GetName(),
			},
			ClusterRoles: expectedClusterRoles,
			Targets:      expectedTargets,
		}),
		"did not expect promotion for ServiceAccount %s/%s with clusterRoles=%v targets=%v",
		serviceAccount.Namespace,
		serviceAccount.Name,
		expectedClusterRoles,
		expectedTargets,
	)
}

func containPromotion(
	serviceAccount *corev1.ServiceAccount,
	clusterRoles []string,
	targets []string,
) otypes.GomegaMatcher {
	return WithTransform(func(promotions rbac.PromotionStatusListSpec) bool {
		expectedName := "system:serviceaccount:" + serviceAccount.GetNamespace() + ":" + serviceAccount.GetName()

		for _, promotion := range promotions {
			if promotion.Kind != rbac.ServiceAccountOwner {
				continue
			}

			if promotion.Name != expectedName {
				continue
			}

			if !sameStringSet(promotion.ClusterRoles, clusterRoles) {
				continue
			}

			if !sameStringSet(promotion.Targets, targets) {
				continue
			}

			return true
		}

		return false
	}, BeTrue())
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	seen := map[string]int{}

	for _, item := range a {
		seen[item]++
	}

	for _, item := range b {
		seen[item]--
	}

	for _, count := range seen {
		if count != 0 {
			return false
		}
	}

	return true
}

func promoteServiceAccount(
	actor client.Client,
	serviceAccount *corev1.ServiceAccount,
	labels map[string]string,
) error {
	saCopy := &corev1.ServiceAccount{}
	if err := actor.Get(context.TODO(), client.ObjectKeyFromObject(serviceAccount), saCopy); err != nil {
		return err
	}

	if saCopy.Labels == nil {
		saCopy.Labels = map[string]string{}
	}

	saCopy.Labels[meta.ServiceAccountPromotionLabel] = meta.ValueTrue

	for key, value := range labels {
		saCopy.Labels[key] = value
	}

	return actor.Update(context.TODO(), saCopy)
}

func setServiceAccountPromotionLabel(
	actor client.Client,
	serviceAccount *corev1.ServiceAccount,
	value string,
) error {
	saCopy := &corev1.ServiceAccount{}
	if err := actor.Get(context.TODO(), client.ObjectKeyFromObject(serviceAccount), saCopy); err != nil {
		return err
	}

	if saCopy.Labels == nil {
		saCopy.Labels = map[string]string{}
	}

	saCopy.Labels[meta.ServiceAccountPromotionLabel] = value

	return actor.Update(context.TODO(), saCopy)
}

func expectRoleBindingForPromotion(
	namespace string,
	clusterRole string,
	serviceAccount *corev1.ServiceAccount,
) {
	Eventually(func(g Gomega) {
		roleBindings := &rbacv1.RoleBindingList{}
		err := k8sClient.List(context.TODO(), roleBindings, client.InNamespace(namespace))
		g.Expect(err).NotTo(HaveOccurred())

		g.Expect(roleBindings.Items).To(ContainElement(SatisfyAll(
			WithTransform(func(roleBinding rbacv1.RoleBinding) string {
				return roleBinding.RoleRef.Kind
			}, Equal("ClusterRole")),
			WithTransform(func(roleBinding rbacv1.RoleBinding) string {
				return roleBinding.RoleRef.Name
			}, Equal(clusterRole)),
			WithTransform(func(roleBinding rbacv1.RoleBinding) []rbacv1.Subject {
				return roleBinding.Subjects
			}, ContainElement(rbacv1.Subject{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      serviceAccount.Name,
				Namespace: serviceAccount.Namespace,
			})),
		)))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func expectNoRoleBindingForPromotion(
	namespace string,
	clusterRole string,
	serviceAccount *corev1.ServiceAccount,
) {
	Consistently(func(g Gomega) {
		roleBindings := &rbacv1.RoleBindingList{}
		err := k8sClient.List(context.TODO(), roleBindings, client.InNamespace(namespace))
		g.Expect(err).NotTo(HaveOccurred())

		g.Expect(roleBindings.Items).NotTo(ContainElement(SatisfyAll(
			WithTransform(func(roleBinding rbacv1.RoleBinding) string {
				return roleBinding.RoleRef.Kind
			}, Equal("ClusterRole")),
			WithTransform(func(roleBinding rbacv1.RoleBinding) string {
				return roleBinding.RoleRef.Name
			}, Equal(clusterRole)),
			WithTransform(func(roleBinding rbacv1.RoleBinding) []rbacv1.Subject {
				return roleBinding.Subjects
			}, ContainElement(rbacv1.Subject{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      serviceAccount.Name,
				Namespace: serviceAccount.Namespace,
			})),
		)))
	}, 2*time.Second, defaultPollInterval).Should(Succeed())
}

var _ = Describe("Promoting ServiceAccounts", Ordered, Label("config", "permissions", "promotion", "rbac"), func() {
	originConfig := &capsulev1beta2.CapsuleConfiguration{}

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-sa-promotion",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Rules: []*rules.NamespaceRuleBodyTenant{
				{
					Permissions: rules.NamespaceRulePermissionBody{
						Promotions: []*rules.NamespaceRulePromotionRule{
							{
								ClusterRoles: []string{"view"},
							},
							{
								ClusterRoles: []string{"edit"},
								Selector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"selective": "edit",
									},
								},
							},
						},
					},
				},
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"environment": "prod",
						},
					},
					Permissions: rules.NamespaceRulePermissionBody{
						Promotions: []*rules.NamespaceRulePromotionRule{
							{
								ClusterRoles: []string{"prod-view"},
							},
							{
								ClusterRoles: []string{"prod-edit"},
								Selector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"super": "account",
									},
								},
							},
						},
					},
				},
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"environment": "dev",
						},
					},
					Permissions: rules.NamespaceRulePermissionBody{
						Promotions: []*rules.NamespaceRulePromotionRule{
							{
								ClusterRoles: []string{"dev-view"},
							},
						},
					},
				},
			},
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-sa-promotion",
							Kind: "User",
						},
					},
				},
			},
			AdditionalRoleBindings: []rbac.AdditionalRoleBindingsSpec{
				{
					ClusterRoleName: "admin",
					Subjects: []rbacv1.Subject{
						{
							Kind: "ServiceAccount",
							Name: "default",
						},
						{
							Kind: "User",
							Name: "bob",
						},
					},
				},
			},
		},
	}

	BeforeEach(func() {
		for _, name := range serviceAccountPromotionClusterRoles {
			clusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{
							"configmaps",
							"secrets",
							"serviceaccounts",
						},
						Verbs: []string{
							"get",
							"list",
							"watch",
							"create",
							"update",
							"patch",
							"delete",
						},
					},
				},
			}

			err := k8sClient.Create(context.TODO(), clusterRole)
			if apierrors.IsAlreadyExists(err) {
				continue
			}

			Expect(err).NotTo(HaveOccurred())
		}
	})

	AfterEach(func() {
		for _, name := range serviceAccountPromotionClusterRoles {
			clusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
			}

			err := k8sClient.Delete(context.TODO(), clusterRole)
			if apierrors.IsNotFound(err) {
				continue
			}

			Expect(err).NotTo(HaveOccurred())
		}
	})

	JustBeforeEach(func() {
		Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: defaultConfigurationName}, originConfig)).To(Succeed())

		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())

		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
	})

	JustAfterEach(func() {
		EventuallyDeletion(tnt)

		Eventually(func() error {
			c := &capsulev1beta2.CapsuleConfiguration{}
			if err := k8sClient.Get(context.Background(), client.ObjectKey{Name: originConfig.Name}, c); err != nil {
				return err
			}

			c.Spec = originConfig.Spec

			return k8sClient.Update(context.Background(), c)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("denies ServiceAccount promotion when the feature is globally disabled", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = false
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sa",
				Namespace: ns.Name,
			},
		}
		Expect(k8sClient.Create(context.TODO(), sa)).Should(Succeed())

		personas := map[string]struct {
			client  client.Client
			matcher otypes.GomegaMatcher
		}{
			"owner": {
				client:  impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(make([]string, 0))),
				matcher: Not(Succeed()),
			},
			"rb-user": {
				client:  impersonationClient("bob", withDefaultGroups(make([]string, 0))),
				matcher: Not(Succeed()),
			},
			"rb-sa": {
				client:  impersonationClient("system:serviceaccount:"+sa.GetNamespace()+":default", withDefaultGroups(make([]string, 0))),
				matcher: Not(Succeed()),
			},
		}

		for name, tc := range personas {
			By(fmt.Sprintf("trying to promote ServiceAccount as %s", name))

			Eventually(func() error {
				return promoteServiceAccount(tc.client, sa, nil)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}

		Eventually(func(g Gomega) {
			t := &capsulev1beta2.Tenant{}
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)
			g.Expect(err).NotTo(HaveOccurred())

			g.Expect(t.Status.Promotions).To(HaveLen(0), "expected no promotions to be present")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		for name, tc := range personas {
			By(fmt.Sprintf("trying to set non-trigger promotion label as %s", name))

			Eventually(func() error {
				return setServiceAccountPromotionLabel(tc.client, sa, "false")
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}

		Eventually(func(g Gomega) {
			t := &capsulev1beta2.Tenant{}
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)
			g.Expect(err).NotTo(HaveOccurred())

			g.Expect(t.Status.Promotions).To(HaveLen(0), "expected no promotions to be present")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("allows ServiceAccount promotion only by tenant owners", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = true
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sa",
				Namespace: ns.Name,
			},
		}
		Expect(k8sClient.Create(context.TODO(), sa)).Should(Succeed())

		personas := map[string]struct {
			client  client.Client
			matcher otypes.GomegaMatcher
		}{
			"rb-user": {
				client:  impersonationClient("bob", withDefaultGroups(make([]string, 0))),
				matcher: Not(Succeed()),
			},
			"rb-sa": {
				client:  impersonationClient("system:serviceaccount:"+sa.GetNamespace()+":default", withDefaultGroups(make([]string, 0))),
				matcher: Not(Succeed()),
			},
			"owner": {
				client:  impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(make([]string, 0))),
				matcher: Succeed(),
			},
		}

		for name, tc := range personas {
			By(fmt.Sprintf("trying to promote ServiceAccount as %s", name))

			Eventually(func() error {
				return promoteServiceAccount(tc.client, sa, nil)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}

		expectPromotionTargets(tnt.GetName(), sa, []string{"view"}, []string{ns.Name})
	})
	It("does not apply namespace-scoped promotion rules to ServiceAccounts from non-matching source namespaces", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = true
		})

		dev := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "dev",
		})
		NamespaceCreation(dev, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, dev).Should(Succeed())

		test := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "test",
		})
		NamespaceCreation(test, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, test).Should(Succeed())

		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dev-sa",
				Namespace: dev.Name,
			},
		}
		Expect(k8sClient.Create(context.TODO(), sa)).Should(Succeed())

		ownerClient := impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(make([]string, 0)))

		Eventually(func() error {
			return promoteServiceAccount(ownerClient, sa, nil)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		expectPromotionTargets(tnt.GetName(), sa, []string{"view"}, []string{dev.Name, test.Name})
		expectPromotionTargets(tnt.GetName(), sa, []string{"dev-view"}, []string{dev.Name})
	})

	It("promotes ServiceAccounts to all tenant namespaces for global promotion rules", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = true
		})

		dev := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "dev",
		})
		NamespaceCreation(dev, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, dev).Should(Succeed())

		prod := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "prod",
		})
		NamespaceCreation(prod, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, prod).Should(Succeed())

		stage := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "stage",
		})
		NamespaceCreation(stage, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, stage).Should(Succeed())

		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sa",
				Namespace: dev.Name,
			},
		}
		Expect(k8sClient.Create(context.TODO(), sa)).Should(Succeed())

		ownerClient := impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(make([]string, 0)))

		Eventually(func() error {
			return promoteServiceAccount(ownerClient, sa, nil)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		expectPromotionTargets(tnt.GetName(), sa, []string{"view"}, []string{dev.Name, prod.Name, stage.Name})
	})

	It("promotes ServiceAccounts to all tenant namespaces for matching global selector rules", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = true
		})

		dev := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "dev",
		})
		NamespaceCreation(dev, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, dev).Should(Succeed())

		prod := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "prod",
		})
		NamespaceCreation(prod, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, prod).Should(Succeed())

		stage := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "stage",
		})
		NamespaceCreation(stage, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, stage).Should(Succeed())

		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "selective-sa",
				Namespace: dev.Name,
			},
		}
		Expect(k8sClient.Create(context.TODO(), sa)).Should(Succeed())

		ownerClient := impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(make([]string, 0)))

		Eventually(func() error {
			return promoteServiceAccount(ownerClient, sa, map[string]string{
				"selective": "edit",
			})
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		expectPromotionTargets(tnt.GetName(), sa, []string{"view", "edit"}, []string{dev.Name, prod.Name, stage.Name})
	})

	It("does not apply selector-based global promotion rules when the ServiceAccount labels do not match", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = true
		})

		dev := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "dev",
		})
		NamespaceCreation(dev, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, dev).Should(Succeed())

		prod := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "prod",
		})
		NamespaceCreation(prod, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, prod).Should(Succeed())

		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "normal-sa",
				Namespace: dev.Name,
			},
		}
		Expect(k8sClient.Create(context.TODO(), sa)).Should(Succeed())

		ownerClient := impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(make([]string, 0)))

		Eventually(func() error {
			return promoteServiceAccount(ownerClient, sa, nil)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		expectPromotionTargets(tnt.GetName(), sa, []string{"view"}, []string{dev.Name, prod.Name})
		expectNoPromotionTargets(tnt.GetName(), sa, []string{"edit"}, []string{dev.Name, prod.Name})
	})

	It("promotes ServiceAccounts only to namespaces matching the namespace selector", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = true
		})

		dev := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "dev",
		})
		NamespaceCreation(dev, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, dev).Should(Succeed())

		prod := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "prod",
		})
		NamespaceCreation(prod, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, prod).Should(Succeed())

		stage := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "prod",
		})
		NamespaceCreation(stage, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, stage).Should(Succeed())

		saTest := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sa",
				Namespace: dev.Name,
			},
		}
		Expect(k8sClient.Create(context.TODO(), saTest)).Should(Succeed())

		saProd := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-prod",
				Namespace: prod.Name,
			},
		}
		Expect(k8sClient.Create(context.TODO(), saProd)).Should(Succeed())

		ownerClient := impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(make([]string, 0)))

		Eventually(func() error {
			return promoteServiceAccount(ownerClient, saTest, nil)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		Eventually(func() error {
			return promoteServiceAccount(ownerClient, saProd, nil)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		expectPromotionTargets(tnt.GetName(), saTest, []string{"view"}, []string{dev.Name, prod.Name, stage.Name})
		expectPromotionTargets(tnt.GetName(), saTest, []string{"dev-view"}, []string{dev.Name})

		expectPromotionTargets(tnt.GetName(), saProd, []string{"view"}, []string{dev.Name, prod.Name, stage.Name})
		expectPromotionTargets(tnt.GetName(), saProd, []string{"prod-view"}, []string{prod.Name, stage.Name})
	})

	It("promotes ServiceAccounts by combined ServiceAccount selector and namespace selector", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = true
		})

		dev := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "dev",
		})
		NamespaceCreation(dev, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, dev).Should(Succeed())

		prodA := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "prod",
		})
		NamespaceCreation(prodA, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, prodA).Should(Succeed())

		prodB := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "prod",
		})
		NamespaceCreation(prodB, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, prodB).Should(Succeed())

		stage := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "stage",
		})
		NamespaceCreation(stage, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())

		saDev := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "super-sa",
				Namespace: dev.Name,
			},
		}
		Expect(k8sClient.Create(context.TODO(), saDev)).Should(Succeed())

		saProd := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "super-sa",
				Namespace: prodA.Name,
			},
		}
		Expect(k8sClient.Create(context.TODO(), saProd)).Should(Succeed())

		ownerClient := impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(make([]string, 0)))

		Eventually(func() error {
			return promoteServiceAccount(ownerClient, saDev, map[string]string{
				"super": "account",
			})
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		Eventually(func() error {
			return promoteServiceAccount(ownerClient, saProd, map[string]string{
				"super": "account",
			})
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		expectPromotionTargets(tnt.GetName(), saDev, []string{"view"}, []string{dev.Name, prodA.Name, prodB.Name, stage.Name})
		expectPromotionTargets(tnt.GetName(), saDev, []string{"dev-view"}, []string{dev.Name})

		expectPromotionTargets(tnt.GetName(), saProd, []string{"view"}, []string{dev.Name, prodA.Name, prodB.Name, stage.Name})
		expectPromotionTargets(tnt.GetName(), saProd, []string{"prod-edit", "prod-view"}, []string{prodA.Name, prodB.Name})
	})

	It("does not apply combined selector promotion rules when the ServiceAccount selector does not match", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = true
		})

		dev := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "dev",
		})
		NamespaceCreation(dev, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, dev).Should(Succeed())

		prod := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "prod",
		})
		NamespaceCreation(prod, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, prod).Should(Succeed())

		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "not-super-sa",
				Namespace: prod.Name,
			},
		}
		Expect(k8sClient.Create(context.TODO(), sa)).Should(Succeed())

		ownerClient := impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(make([]string, 0)))

		Eventually(func() error {
			return promoteServiceAccount(ownerClient, sa, nil)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		expectPromotionTargets(tnt.GetName(), sa, []string{"view"}, []string{dev.Name, prod.Name})
		expectPromotionTargets(tnt.GetName(), sa, []string{"prod-view"}, []string{prod.Name})
	})

	It("creates RoleBindings only in targeted namespaces", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = true
		})

		dev := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "dev",
		})
		NamespaceCreation(dev, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, dev).Should(Succeed())

		prod := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "prod",
		})
		NamespaceCreation(prod, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, prod).Should(Succeed())

		stage := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
			"environment":    "prod",
		})
		NamespaceCreation(stage, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, stage).Should(Succeed())

		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "super-sa",
				Namespace: prod.Name,
			},
		}
		Expect(k8sClient.Create(context.TODO(), sa)).Should(Succeed())

		ownerClient := impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(make([]string, 0)))

		Eventually(func() error {
			return promoteServiceAccount(ownerClient, sa, map[string]string{
				"super": "account",
			})
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		expectPromotionTargets(tnt.GetName(), sa, []string{"view"}, []string{dev.Name, prod.Name, stage.Name})
		expectPromotionTargets(tnt.GetName(), sa, []string{"prod-view", "prod-edit"}, []string{prod.Name, stage.Name})

		expectRoleBindingForPromotion(dev.Name, "view", sa)
		expectRoleBindingForPromotion(prod.Name, "view", sa)
		expectRoleBindingForPromotion(stage.Name, "view", sa)

		expectRoleBindingForPromotion(prod.Name, "prod-view", sa)
		expectRoleBindingForPromotion(prod.Name, "prod-edit", sa)

		expectRoleBindingForPromotion(stage.Name, "prod-view", sa)
		expectRoleBindingForPromotion(stage.Name, "prod-edit", sa)
	})
})
