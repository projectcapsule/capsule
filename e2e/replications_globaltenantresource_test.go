// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apimeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	capruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/template"
)

const (
	managedByLabel = "projectcapsule.dev/managed-by"
	createdByLabel = "projectcapsule.dev/created-by"
	resourcesLabel = "resources"
)

var _ = Describe("GlobalTenantResource", Ordered, Label("replications", "global", "globaltenantresource"), Ordered, func() {
	var (
		ctx          context.Context
		originConfig *capsulev1beta2.CapsuleConfiguration

		tenantA *capsulev1beta2.Tenant
		tenantB *capsulev1beta2.Tenant

		tenantAOwner rbac.UserSpec
		tenantBOwner rbac.UserSpec

		tenantANamespaces []string
		tenantBNamespaces []string
		allNamespaces     []string
	)

	BeforeEach(func() {
		ctx = context.Background()
		originConfig = &capsulev1beta2.CapsuleConfiguration{}

		tenantAOwner = rbac.UserSpec{Name: "e2e-gtr-tenant-a", Kind: rbac.OwnerKind("User")}
		tenantBOwner = rbac.UserSpec{Name: "e2e-gtr-tenant-b", Kind: rbac.OwnerKind("User")}

		tenantANamespaces = []string{"e2e-gtr-tenant-a-one", "e2e-gtr-tenant-a-two", "e2e-gtr-tenant-a-system"}
		tenantBNamespaces = []string{"e2e-gtr-tenant-b-one", "e2e-gtr-tenant-b-two"}
		allNamespaces = append(append([]string{}, tenantANamespaces...), tenantBNamespaces...)

		tenantA = &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name: "e2e-gtr-tenant-a",
				Labels: map[string]string{
					"energy": "solar",
					"group":  "alpha",
					"env":    "e2e",
				},
			},
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{
					CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: tenantAOwner},
				}},
				AdditionalRoleBindings: []rbac.AdditionalRoleBindingsSpec{{
					ClusterRoleName: "admin",
					Subjects: []rbacv1.Subject{{
						Kind: "User",
						Name: "bob",
					}},
				}},
			},
		}

		tenantB = &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name: "e2e-gtr-tenant-b",
				Labels: map[string]string{
					"energy": "lunar",
					"group":  "beta",
					"env":    "e2e",
				},
			},
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{
					CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: tenantBOwner},
				}},
			},
		}

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, originConfig)).To(Succeed())

		EventuallyCreation(func() error {
			tenantA.ResourceVersion = ""
			return k8sClient.Create(ctx, tenantA)
		}).Should(Succeed())
		TenantReady(tenantA, metav1.ConditionTrue, defaultTimeoutInterval)

		EventuallyCreation(func() error {
			tenantB.ResourceVersion = ""
			return k8sClient.Create(ctx, tenantB)
		}).Should(Succeed())
		TenantReady(tenantB, metav1.ConditionTrue, defaultTimeoutInterval)

		for _, ns := range tenantANamespaces {
			namespace := NewNamespace(ns, map[string]string{
				apimeta.TenantLabel: tenantA.GetName(),
			})
			NamespaceCreation(namespace, tenantAOwner, defaultTimeoutInterval).Should(Succeed())
		}

		for _, ns := range tenantBNamespaces {
			namespace := NewNamespace(ns, map[string]string{
				apimeta.TenantLabel: tenantB.GetName(),
			})
			NamespaceCreation(namespace, tenantBOwner, defaultTimeoutInterval).Should(Succeed())
		}
	})

	AfterEach(func() {
		for _, ns := range allNamespaces {
			ForceDeleteNamespace(ctx, ns)
		}

		EventuallyDeletion(tenantA)
		EventuallyDeletion(tenantB)

		Eventually(func() error {
			poolList := &capsulev1beta2.GlobalTenantResourceList{}
			labelSelector := client.MatchingLabels{"e2e.capsule.dev/test-suite": "true"}
			if err := k8sClient.List(context.TODO(), poolList, labelSelector); err != nil {
				return err
			}

			for _, pool := range poolList.Items {
				if err := k8sClient.Delete(context.TODO(), &pool); err != nil {
					return err
				}
			}

			return nil
		}, "30s", "5s").Should(Succeed())

		Eventually(func() error {
			cfg := &capsulev1beta2.CapsuleConfiguration{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: originConfig.Name}, cfg); err != nil {
				return err
			}
			cfg.Spec = originConfig.Spec
			return k8sClient.Update(ctx, cfg)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

	})

	It("skips applying resources to terminating namespaces and removes them from processedItems", func() {
		terminatingNamespace := tenantANamespaces[2]
		releaseNamespace := holdNamespaceTerminating(ctx, terminatingNamespace)
		defer releaseNamespace()

		gtr := newRawConfigMapGlobalTenantResource("gtr-skip-terminating-namespace", map[string]string{
			"mode": "active",
		})

		gtr.Spec.TenantSelector = metav1.LabelSelector{
			MatchLabels: map[string]string{"energy": "solar"},
		}

		renameFirstRawConfigMap(gtr, "gtr-skip-terminating")

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, gtr)
		}).Should(Succeed())

		By("verifying non-terminating selected namespaces still receive the resource")
		for _, ns := range tenantANamespaces[:2] {
			expectConfigMapData(ns, "gtr-skip-terminating", map[string]string{
				"mode": "active",
			})
		}

		By("verifying the terminating selected namespace is skipped")
		Consistently(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{
				Name:      "gtr-skip-terminating",
				Namespace: terminatingNamespace,
			}, &corev1.ConfigMap{})
		}, 2*resyncPeriod.Duration, defaultPollInterval).Should(HaveOccurred())

		By("verifying non-selected tenants do not receive the resource")
		for _, ns := range tenantBNamespaces {
			expectConfigMapAbsent(ns, "gtr-skip-terminating")
		}

		By("verifying the terminating namespace item is not kept in processedItems")
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.GlobalTenantResource{}

			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: gtr.Name,
			}, current)).To(Succeed())

			for _, item := range current.Status.ProcessedItems {
				g.Expect(item.Name).To(Equal("gtr-skip-terminating"))

				g.Expect(item.Namespace).ToNot(Equal(terminatingNamespace))
				g.Expect(item.Status).To(Equal(metav1.ConditionTrue))
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("fails to replicate namespacedItems when the impersonated service account cannot read source resources", func() {
		saName := "gtr-no-namespaceditem-read"
		ensureServiceAccount("capsule-system", saName)

		sourceNs := "gtr-source-items"
		sourceNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: sourceNs},
		}
		EventuallyCreation(func() error { return k8sClient.Create(ctx, sourceNamespace) }).Should(Succeed())
		defer ForceDeleteNamespace(ctx, sourceNs)

		sourceSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "source-secret",
				Namespace: sourceNs,
				Labels: map[string]string{
					"replicate": "true",
				},
			},
			Type:       corev1.SecretTypeOpaque,
			StringData: map[string]string{"token": "abc"},
		}
		EventuallyCreation(func() error { return k8sClient.Create(ctx, sourceSecret) }).Should(Succeed())

		for _, ns := range tenantANamespaces {
			bindServiceAccountToSecretWriter("capsule-system", saName, ns)
		}

		gtr := &capsulev1beta2.GlobalTenantResource{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gtr-sa-no-namespaceditem-read",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "true",
				},
			},
			Spec: capsulev1beta2.GlobalTenantResourceSpec{
				Scope: api.ResourceScopeNamespace,
				ServiceAccount: &apimeta.NamespacedRFC1123ObjectReferenceWithNamespace{
					Name:      apimeta.RFC1123Name(saName),
					Namespace: apimeta.RFC1123SubdomainName("capsule-system"),
				},
				TenantSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"energy": "solar"},
				},
				TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
					ResyncPeriod:    metav1.Duration{Duration: 5 * time.Second},
					PruningOnDelete: ptr.To(true),
					Resources: []capsulev1beta2.ResourceSpec{{
						NamespacedItems: []template.ResourceReference{{
							VersionKind: capruntime.VersionKind{
								APIVersion: "v1",
								Kind:       "Secret",
							},
							Namespace: sourceNs,
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"replicate": "true",
								},
							},
						}},
					}},
				},
			},
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

		expectGlobalTenantResourceFailed("gtr-sa-no-namespaceditem-read", "forbidden")

		for _, ns := range tenantANamespaces {
			expectSecretAbsent(ns, "source-secret")
		}
	})

	Context("scope handling", func() {
		It("reconciles with scope Namespace", func() {
			gtr := newRawConfigMapGlobalTenantResourceWithScope(
				"gtr-scope-namespace",
				api.ResourceScopeNamespace,
				map[string]string{"mode": "namespace"},
			)
			gtr.Spec.TenantSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"energy": "solar"},
			}
			renameFirstRawConfigMap(gtr, "gtr-scope-namespace")

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

			for _, ns := range tenantANamespaces {
				expectConfigMapData(ns, "gtr-scope-namespace", map[string]string{"mode": "namespace"})
			}
			for _, ns := range tenantBNamespaces {
				expectConfigMapAbsent(ns, "gtr-scope-namespace")
			}
		})

		It("accepts scope Tenant", func() {
			gtr := newRawConfigMapGlobalTenantResourceWithScope(
				"gtr-scope-tenant",
				api.ResourceScopeTenant,
				map[string]string{"mode": "tenant"},
			)
			gtr.Spec.TenantSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"energy": "solar"},
			}
			renameFirstRawConfigMap(gtr, "gtr-scope-tenant")

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

			Eventually(func(g Gomega) {
				current := &capsulev1beta2.GlobalTenantResource{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: gtr.Name}, current)).To(Succeed())
				rdy := current.Status.Conditions.GetConditionByType(apimeta.ReadyCondition)
				g.Expect(rdy).ToNot(BeNil())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		It("accepts scope None", func() {
			gtr := newRawConfigMapGlobalTenantResourceWithScope(
				"gtr-scope-none",
				api.ResourceScopeNone,
				map[string]string{"mode": "none"},
			)
			gtr.Spec.TenantSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"energy": "solar"},
			}
			renameFirstRawConfigMap(gtr, "gtr-scope-none")

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

			Eventually(func(g Gomega) {
				current := &capsulev1beta2.GlobalTenantResource{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: gtr.Name}, current)).To(Succeed())
				rdy := current.Status.Conditions.GetConditionByType(apimeta.ReadyCondition)
				g.Expect(rdy).ToNot(BeNil())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
	})

	Context("multiple GlobalTenantResources", func() {
		It("fails when multiple GlobalTenantResources target the same preexisting object without adoption", func() {
			for _, ns := range tenantANamespaces {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtr-shared-preexisting",
						Namespace: ns,
					},
					Data: map[string]string{
						"existing": "true",
					},
				}
				EventuallyCreation(func() error {
					cm.ResourceVersion = ""
					return k8sClient.Create(ctx, cm)
				}).Should(Succeed())
			}

			gtrA := newRawConfigMapGlobalTenantResource("gtr-collision-preexisting-a", map[string]string{
				"from": "a",
			})
			gtrA.Spec.TenantSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"energy": "solar"},
			}
			gtrA.Spec.Settings.Adopt = ptr.To(false)
			renameFirstRawConfigMap(gtrA, "gtr-shared-preexisting")

			gtrB := newRawConfigMapGlobalTenantResource("gtr-collision-preexisting-b", map[string]string{
				"from": "b",
			})
			gtrB.Spec.TenantSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"energy": "solar"},
			}
			gtrB.Spec.Settings.Adopt = ptr.To(false)
			renameFirstRawConfigMap(gtrB, "gtr-shared-preexisting")

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtrA) }).Should(Succeed())
			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtrB) }).Should(Succeed())

			expectGlobalTenantResourceFailed("gtr-collision-preexisting-a", "applying of")
			expectGlobalTenantResourceFailed("gtr-collision-preexisting-b", "applying of")

			for _, ns := range tenantANamespaces {
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name:      "gtr-shared-preexisting",
						Namespace: ns,
					}, cm)).To(Succeed())
					g.Expect(cm.Data).To(Equal(map[string]string{
						"existing": "true",
					}))
					g.Expect(cm.Labels).ToNot(HaveKey(managedByLabel))
					g.Expect(cm.Labels).ToNot(HaveKey(createdByLabel))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})

		It("fails when a second GlobalTenantResource targets an object already managed by another GlobalTenantResource without adoption", func() {
			gtrA := newRawConfigMapGlobalTenantResource("gtr-collision-managed-a", map[string]string{
				"owner": "first",
			})
			gtrA.Spec.TenantSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"energy": "solar"},
			}
			gtrA.Spec.Settings.Adopt = ptr.To(false)
			renameFirstRawConfigMap(gtrA, "gtr-shared-managed")

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtrA) }).Should(Succeed())
			expectGlobalTenantResourceReady("gtr-collision-managed-a")

			for _, ns := range tenantANamespaces {
				expectConfigMapData(ns, "gtr-shared-managed", map[string]string{"owner": "first"})
			}

			gtrB := newRawConfigMapGlobalTenantResource("gtr-collision-managed-b", map[string]string{
				"owner": "second",
			})
			gtrB.Spec.TenantSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"energy": "solar"},
			}
			gtrB.Spec.Settings.Adopt = ptr.To(false)
			renameFirstRawConfigMap(gtrB, "gtr-shared-managed")

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtrB) }).Should(Succeed())
			expectGlobalTenantResourceFailed("gtr-collision-managed-b", "applying of")

			for _, ns := range tenantANamespaces {
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name:      "gtr-shared-managed",
						Namespace: ns,
					}, cm)).To(Succeed())
					g.Expect(cm.Data).To(HaveKeyWithValue("owner", "first"))
					g.Expect(cm.Labels).To(HaveKeyWithValue(managedByLabel, meta.ValueControllerReplications))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})
	})

	Context("impersonation", func() {
		It("fails to apply raw items when the impersonated service account cannot create target resources", func() {
			saName := "gtr-no-create"
			ensureServiceAccount("capsule-system", saName)

			gtr := newRawConfigMapGlobalTenantResource("gtr-sa-no-create", map[string]string{
				"mode": "blocked",
			})
			gtr.Spec.ServiceAccount = &apimeta.NamespacedRFC1123ObjectReferenceWithNamespace{
				Name:      apimeta.RFC1123Name(saName),
				Namespace: apimeta.RFC1123SubdomainName("capsule-system"),
			}
			gtr.Spec.TenantSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"energy": "solar"},
			}
			renameFirstRawConfigMap(gtr, "gtr-blocked-create")

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

			expectGlobalTenantResourceFailed("gtr-sa-no-create", "applying of")

			for _, ns := range tenantANamespaces {
				expectConfigMapAbsent(ns, "gtr-blocked-create")
			}
		})

		It("fails to render generators when the impersonated service account cannot read context resources", func() {
			saName := "gtr-no-context-read"
			ensureServiceAccount("capsule-system", saName)

			sourceNs := "gtr-context-source"
			sourceNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: sourceNs},
			}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, sourceNamespace) }).Should(Succeed())
			defer ForceDeleteNamespace(ctx, sourceNs)

			sourceSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ctx-secret",
					Namespace: sourceNs,
					Labels: map[string]string{
						"pullsecret.company.com": "true",
					},
				},
				Type:       corev1.SecretTypeOpaque,
				StringData: map[string]string{".dockerconfigjson": "e30="},
			}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, sourceSecret) }).Should(Succeed())

			for _, ns := range tenantANamespaces {
				bindServiceAccountToConfigMapWriter("capsule-system", saName, ns)
			}

			gtr := &capsulev1beta2.GlobalTenantResource{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gtr-sa-no-context-read",
					Labels: map[string]string{
						"e2e.capsule.dev/test-suite": "true",
					},
				},
				Spec: capsulev1beta2.GlobalTenantResourceSpec{
					Scope: api.ResourceScopeNamespace,
					ServiceAccount: &apimeta.NamespacedRFC1123ObjectReferenceWithNamespace{
						Name:      apimeta.RFC1123Name(saName),
						Namespace: apimeta.RFC1123SubdomainName("capsule-system"),
					},
					TenantSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"energy": "solar"},
					},
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						ResyncPeriod:    metav1.Duration{Duration: 5 * time.Second},
						PruningOnDelete: ptr.To(true),
						Resources: []capsulev1beta2.ResourceSpec{{
							Context: &template.TemplateContext{
								Resources: []*template.TemplateResourceReference{{
									Index: "secrets",
									ResourceReference: template.ResourceReference{
										VersionKind: capruntime.VersionKind{
											APIVersion: "v1",
											Kind:       "Secret",
										},
										Namespace: sourceNs,
										Selector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"pullsecret.company.com": "true",
											},
										},
									},
								}},
							},
							Generators: []capsulev1beta2.TemplateItemSpec{{
								MissingKey: "error",
								Template: `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: gtr-blocked-context
data:
  count: '{{ if $.secrets }}{{ len $.secrets }}{{ else }}0{{ end }}'
`,
							}},
						}},
					},
				},
			}

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

			expectGlobalTenantResourceFailed("gtr-sa-no-context-read", "forbidden")

			for _, ns := range tenantANamespaces {
				expectConfigMapAbsent(ns, "gtr-blocked-context")
			}
		})

		It("fails to replicate namespacedItems when the impersonated service account cannot read source resources", func() {
			saName := "gtr-no-namespaceditem-read"
			ensureServiceAccount("capsule-system", saName)

			sourceNs := "gtr-source-items"
			sourceNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: sourceNs},
			}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, sourceNamespace) }).Should(Succeed())
			defer ForceDeleteNamespace(ctx, sourceNs)

			sourceSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "source-secret",
					Namespace: sourceNs,
					Labels: map[string]string{
						"replicate": "true",
					},
				},
				Type:       corev1.SecretTypeOpaque,
				StringData: map[string]string{"token": "abc"},
			}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, sourceSecret) }).Should(Succeed())

			for _, ns := range tenantANamespaces {
				bindServiceAccountToSecretWriter("capsule-system", saName, ns)
			}

			gtr := &capsulev1beta2.GlobalTenantResource{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gtr-sa-no-namespaceditem-read",
					Labels: map[string]string{
						"e2e.capsule.dev/test-suite": "true",
					},
				},
				Spec: capsulev1beta2.GlobalTenantResourceSpec{
					Scope: api.ResourceScopeNamespace,
					ServiceAccount: &apimeta.NamespacedRFC1123ObjectReferenceWithNamespace{
						Name:      apimeta.RFC1123Name(saName),
						Namespace: apimeta.RFC1123SubdomainName("capsule-system"),
					},
					TenantSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"energy": "solar"},
					},
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						ResyncPeriod:    metav1.Duration{Duration: 5 * time.Second},
						PruningOnDelete: ptr.To(true),
						Resources: []capsulev1beta2.ResourceSpec{{
							NamespacedItems: []template.ResourceReference{{
								VersionKind: capruntime.VersionKind{
									APIVersion: "v1",
									Kind:       "Secret",
								},
								Namespace: sourceNs,
								Selector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"replicate": "true",
									},
								},
							}},
						}},
					},
				},
			}

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

			expectGlobalTenantResourceFailed("gtr-sa-no-namespaceditem-read", "forbidden")

			for _, ns := range tenantANamespaces {
				expectSecretAbsent(ns, "source-secret")
			}
		})

		It("fails to prune replicated resources when the impersonated service account cannot delete them", func() {
			saCreate := "gtr-creator-ok"
			saNoDelete := "gtr-creator-no-delete"

			ensureServiceAccount("capsule-system", saCreate)
			ensureServiceAccount("capsule-system", saNoDelete)

			for _, ns := range tenantANamespaces {
				bindServiceAccountToConfigMapWriter("capsule-system", saCreate, ns)
				bindServiceAccountToConfigMapWriter("capsule-system", saNoDelete, ns)
			}

			gtr := newRawConfigMapGlobalTenantResource("gtr-sa-no-prune", map[string]string{
				"mode": "created",
			})
			gtr.Spec.ServiceAccount = &apimeta.NamespacedRFC1123ObjectReferenceWithNamespace{
				Name:      apimeta.RFC1123Name(saCreate),
				Namespace: apimeta.RFC1123SubdomainName("capsule-system"),
			}
			gtr.Spec.TenantSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"energy": "solar"},
			}
			gtr.Spec.PruningOnDelete = ptr.To(true)
			renameFirstRawConfigMap(gtr, "gtr-prune-protected")

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())
			expectGlobalTenantResourceReady("gtr-sa-no-prune")

			for _, ns := range tenantANamespaces {
				expectConfigMapData(ns, "gtr-prune-protected", map[string]string{"mode": "created"})
			}

			Eventually(func() error {
				current := &capsulev1beta2.GlobalTenantResource{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: gtr.Name}, current); err != nil {
					return err
				}
				current.Spec.ServiceAccount = &apimeta.NamespacedRFC1123ObjectReferenceWithNamespace{
					Name:      apimeta.RFC1123Name(saNoDelete),
					Namespace: apimeta.RFC1123SubdomainName("capsule-system"),
				}
				return k8sClient.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				current := &capsulev1beta2.GlobalTenantResource{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: gtr.Name}, current)).To(Succeed())
				g.Expect(current.Status.ServiceAccount).ToNot(BeNil())
				g.Expect(current.Status.ServiceAccount.Name).To(Equal(apimeta.RFC1123Name(saNoDelete)))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			Expect(k8sClient.Delete(ctx, gtr)).To(Succeed())

			for _, ns := range tenantANamespaces {
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "gtr-prune-protected", Namespace: ns}, cm)).To(Succeed())
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})
	})

	Context("adoption", func() {
		It("fails on preexisting objects when adoption is disabled", func() {
			for _, ns := range tenantANamespaces {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtr-adopt-me",
						Namespace: ns,
					},
					Data: map[string]string{
						"existing": "true",
					},
				}
				EventuallyCreation(func() error {
					cm.ResourceVersion = ""
					return k8sClient.Create(ctx, cm)
				}).Should(Succeed())
			}

			gtr := newRawConfigMapGlobalTenantResource("gtr-adoption-disabled", map[string]string{
				"mode": "new",
			})
			gtr.Spec.TenantSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"energy": "solar"},
			}
			gtr.Spec.Settings.Adopt = ptr.To(false)
			renameFirstRawConfigMap(gtr, "gtr-adopt-me")

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

			expectGlobalTenantResourceFailed("gtr-adoption-disabled", "applying of")

			for _, ns := range tenantANamespaces {
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "gtr-adopt-me", Namespace: ns}, cm)).To(Succeed())
					g.Expect(cm.Data).To(Equal(map[string]string{"existing": "true"}))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})

		It("adopts preexisting objects when adoption is enabled", func() {
			for _, ns := range tenantANamespaces {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtr-adopt-me-enabled",
						Namespace: ns,
					},
					Data: map[string]string{
						"existing": "true",
					},
				}
				EventuallyCreation(func() error {
					cm.ResourceVersion = ""
					return k8sClient.Create(ctx, cm)
				}).Should(Succeed())
			}

			gtr := newRawConfigMapGlobalTenantResource("gtr-adoption-enabled", map[string]string{
				"mode": "adopted",
				"foo":  "bar",
			})
			gtr.Spec.TenantSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"energy": "solar"},
			}
			gtr.Spec.Settings.Adopt = ptr.To(true)
			renameFirstRawConfigMap(gtr, "gtr-adopt-me-enabled")

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())
			expectGlobalTenantResourceReady("gtr-adoption-enabled")

			for _, ns := range tenantANamespaces {
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "gtr-adopt-me-enabled", Namespace: ns}, cm)).To(Succeed())
					g.Expect(cm.Data).To(HaveKeyWithValue("mode", "adopted"))
					g.Expect(cm.Data).To(HaveKeyWithValue("foo", "bar"))
					g.Expect(cm.Labels).To(HaveKeyWithValue(managedByLabel, meta.ValueControllerReplications))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})
	})

	Context("prune on selector drift", func() {
		It("prunes objects from tenants that stop matching tenantSelector", func() {
			gtr := newRawConfigMapGlobalTenantResource("gtr-tenant-selector-prune", map[string]string{
				"mode": "both",
			})
			gtr.Spec.TenantSelector = metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      "energy",
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{"solar", "lunar"},
				}},
			}
			renameFirstRawConfigMap(gtr, "gtr-tenant-selector-prune")

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

			for _, ns := range append(tenantANamespaces, tenantBNamespaces...) {
				expectConfigMapData(ns, "gtr-tenant-selector-prune", map[string]string{"mode": "both"})
			}

			Eventually(func() error {
				current := &capsulev1beta2.GlobalTenantResource{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: gtr.Name}, current); err != nil {
					return err
				}
				current.Spec.TenantSelector = metav1.LabelSelector{
					MatchLabels: map[string]string{"energy": "solar"},
				}
				return k8sClient.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			for _, ns := range tenantANamespaces {
				expectConfigMapData(ns, "gtr-tenant-selector-prune", map[string]string{"mode": "both"})
			}
			for _, ns := range tenantBNamespaces {
				expectConfigMapDeleted(ns, "gtr-tenant-selector-prune")
			}
		})

		It("prunes objects from namespaces that stop matching namespaceSelector", func() {
			for _, ns := range []string{"e2e-gtr-tenant-a-one", "e2e-gtr-tenant-a-two"} {
				Eventually(func() error {
					n := &corev1.Namespace{}
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: ns}, n); err != nil {
						return err
					}
					lbls := n.GetLabels()
					lbls["replicate"] = "true"
					n.SetLabels(lbls)
					return k8sClient.Update(ctx, n)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}

			gtr := newRawConfigMapGlobalTenantResource("gtr-namespace-selector-prune", map[string]string{
				"mode": "selected",
			})
			gtr.Spec.TenantSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"energy": "solar"},
			}
			gtr.Spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{"replicate": "true"},
			}
			renameFirstRawConfigMap(gtr, "gtr-namespace-selector-prune")

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

			expectConfigMapData("e2e-gtr-tenant-a-one", "gtr-namespace-selector-prune", map[string]string{"mode": "selected"})
			expectConfigMapData("e2e-gtr-tenant-a-two", "gtr-namespace-selector-prune", map[string]string{"mode": "selected"})

			Eventually(func() error {
				n := &corev1.Namespace{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-gtr-tenant-a-two"}, n); err != nil {
					return err
				}
				lbls := n.GetLabels()
				delete(lbls, "replicate")
				n.SetLabels(lbls)
				return k8sClient.Update(ctx, n)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			expectConfigMapData("e2e-gtr-tenant-a-one", "gtr-namespace-selector-prune", map[string]string{"mode": "selected"})
			expectConfigMapDeleted("e2e-gtr-tenant-a-two", "gtr-namespace-selector-prune")
		})
	})

	Context("service account resolution", func() {
		It("reflects the resolved service account in status", func() {
			gtr := newRawConfigMapGlobalTenantResource("gtr-sa-resolution", map[string]string{"mode": "default"})
			gtr.Spec.ServiceAccount = nil
			gtr.Spec.TenantSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"energy": "solar"},
			}

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

			Eventually(func(g Gomega) {
				current := &capsulev1beta2.GlobalTenantResource{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: gtr.Name}, current)).To(Succeed())
				g.Expect(current.Status.ServiceAccount).ToNot(BeNil())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
				configuration.Spec.Impersonation.GlobalDefaultServiceAccount = "default"
				configuration.Spec.Impersonation.GlobalDefaultServiceAccountNamespace = "capsule-system"
			})

			Eventually(func(g Gomega) {
				current := &capsulev1beta2.GlobalTenantResource{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: gtr.Name}, current)).To(Succeed())
				g.Expect(current.Status.ServiceAccount).ToNot(BeNil())
				g.Expect(current.Status.ServiceAccount.Name).To(Equal(apimeta.RFC1123Name("default")))
				g.Expect(current.Status.ServiceAccount.Namespace).To(Equal(apimeta.RFC1123SubdomainName("capsule-system")))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
	})

	Context("selection and fan-out", func() {
		It("applies raw items to all namespaces of the selected tenants", func() {
			gtr := newRawConfigMapGlobalTenantResource("gtr-apply-selected-tenants", map[string]string{
				"mode": "solar",
			})
			gtr.Spec.TenantSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"energy": "solar"},
			}
			renameFirstRawConfigMap(gtr, "gtr-shared-config")

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

			for _, ns := range tenantANamespaces {
				expectConfigMapData(ns, "gtr-shared-config", map[string]string{"mode": "solar"})
			}
			for _, ns := range tenantBNamespaces {
				expectConfigMapAbsent(ns, "gtr-shared-config")
			}
		})

		It("applies only to namespaces matching namespaceSelector within the selected tenants", func() {
			for _, ns := range []string{"e2e-gtr-tenant-a-one", "e2e-gtr-tenant-a-two"} {
				Eventually(func() error {
					n := &corev1.Namespace{}
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: ns}, n); err != nil {
						return err
					}
					labels := n.GetLabels()
					labels["replicate"] = "true"
					n.SetLabels(labels)
					return k8sClient.Update(ctx, n)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}

			gtr := newRawConfigMapGlobalTenantResource("gtr-tenant-and-namespace-selector", map[string]string{
				"mode": "filtered",
			})
			gtr.Spec.TenantSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"energy": "solar"},
			}
			gtr.Spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{"replicate": "true"},
			}
			renameFirstRawConfigMap(gtr, "gtr-filtered-config")

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

			expectConfigMapData("e2e-gtr-tenant-a-one", "gtr-filtered-config", map[string]string{"mode": "filtered"})
			expectConfigMapData("e2e-gtr-tenant-a-two", "gtr-filtered-config", map[string]string{"mode": "filtered"})
			expectConfigMapAbsent("e2e-gtr-tenant-a-system", "gtr-filtered-config")

			for _, ns := range tenantBNamespaces {
				expectConfigMapAbsent(ns, "gtr-filtered-config")
			}
		})
	})

	Context("apply lifecycle", func() {
		It("updates previously applied objects across all selected namespaces", func() {
			gtr := newRawConfigMapGlobalTenantResource("gtr-update", map[string]string{"mode": "before"})
			gtr.Spec.TenantSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"energy": "solar"},
			}
			renameFirstRawConfigMap(gtr, "gtr-update-config")

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

			for _, ns := range tenantANamespaces {
				expectConfigMapData(ns, "gtr-update-config", map[string]string{"mode": "before"})
			}

			Eventually(func() error {
				current := &capsulev1beta2.GlobalTenantResource{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: gtr.Name}, current); err != nil {
					return err
				}

				current.Spec.Resources[0].RawItems[0] = capsulev1beta2.RawExtension{
					RawExtension: runtime.RawExtension{
						Object: &corev1.ConfigMap{
							TypeMeta: metav1.TypeMeta{
								APIVersion: "v1",
								Kind:       "ConfigMap",
							},
							ObjectMeta: metav1.ObjectMeta{
								Name: "gtr-update-config",
							},
							Data: map[string]string{
								"mode": "after",
								"foo":  "bar",
							},
						},
					},
				}

				return k8sClient.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			for _, ns := range tenantANamespaces {
				expectConfigMapData(ns, "gtr-update-config", map[string]string{
					"mode": "after",
					"foo":  "bar",
				})
			}
		})

		It("prunes applied objects on delete when pruningOnDelete is enabled", func() {
			gtr := newRawConfigMapGlobalTenantResource("gtr-prune-enabled", map[string]string{"mode": "prune"})
			gtr.Spec.TenantSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"energy": "solar"},
			}
			gtr.Spec.PruningOnDelete = ptr.To(true)
			renameFirstRawConfigMap(gtr, "gtr-pruned")

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

			for _, ns := range tenantANamespaces {
				expectConfigMapData(ns, "gtr-pruned", map[string]string{"mode": "prune"})
			}

			Expect(k8sClient.Delete(ctx, gtr)).To(Succeed())

			for _, ns := range tenantANamespaces {
				expectConfigMapDeleted(ns, "gtr-pruned")
			}
		})

		It("keeps applied objects on delete when pruningOnDelete is disabled", func() {
			gtr := newRawConfigMapGlobalTenantResource("gtr-prune-disabled", map[string]string{"mode": "keep"})
			gtr.Spec.TenantSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"energy": "solar"},
			}
			gtr.Spec.PruningOnDelete = ptr.To(false)
			renameFirstRawConfigMap(gtr, "gtr-kept")

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

			for _, ns := range tenantANamespaces {
				expectConfigMapData(ns, "gtr-kept", map[string]string{"mode": "keep"})
			}

			Expect(k8sClient.Delete(ctx, gtr)).To(Succeed())

			for _, ns := range tenantANamespaces {
				expectConfigMapData(ns, "gtr-kept", map[string]string{"mode": "keep"})
			}

		})

	})

	Context("namespace target enforcement", func() {
		It("forces raw items into the iterated namespace even if metadata.namespace is set elsewhere", func() {
			gtr := &capsulev1beta2.GlobalTenantResource{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gtr-raw-target-namespace",
					Labels: map[string]string{
						"e2e.capsule.dev/test-suite": "true",
					},
				},
				Spec: capsulev1beta2.GlobalTenantResourceSpec{
					Scope: api.ResourceScopeNamespace,
					TenantSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"energy": "solar"},
					},
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						ResyncPeriod:    metav1.Duration{Duration: 5 * time.Second},
						PruningOnDelete: ptr.To(true),
						Resources: []capsulev1beta2.ResourceSpec{{
							RawItems: []capsulev1beta2.RawExtension{{
								RawExtension: runtime.RawExtension{
									Object: &corev1.ConfigMap{
										TypeMeta: metav1.TypeMeta{
											APIVersion: "v1",
											Kind:       "ConfigMap",
										},
										ObjectMeta: metav1.ObjectMeta{
											Name:      "gtr-raw-namespace-enforced",
											Namespace: "kube-system",
										},
										Data: map[string]string{
											"source": "raw",
										},
									},
								},
							}},
						}},
					},
				},
			}

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

			for _, ns := range tenantANamespaces {
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "gtr-raw-namespace-enforced", Namespace: ns}, cm)).To(Succeed())
					g.Expect(cm.Namespace).To(Equal(ns))
					g.Expect(cm.Data).To(HaveKeyWithValue("source", "raw"))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}

			expectConfigMapAbsent("kube-system", "gtr-raw-namespace-enforced")
		})
	})

	Context("raw and generator merge", func() {
		It("merges raw items and generators when they target the same object", func() {
			gtr := &capsulev1beta2.GlobalTenantResource{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gtr-raw-and-generator-same-object",
					Labels: map[string]string{
						"e2e.capsule.dev/test-suite": "true",
					},
				},
				Spec: capsulev1beta2.GlobalTenantResourceSpec{
					Scope: api.ResourceScopeNamespace,
					TenantSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"energy": "solar"},
					},
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						ResyncPeriod:    metav1.Duration{Duration: 5 * time.Second},
						PruningOnDelete: ptr.To(true),
						Resources: []capsulev1beta2.ResourceSpec{{
							RawItems: []capsulev1beta2.RawExtension{{
								RawExtension: runtime.RawExtension{
									Object: &corev1.ConfigMap{
										TypeMeta: metav1.TypeMeta{
											APIVersion: "v1",
											Kind:       "ConfigMap",
										},
										ObjectMeta: metav1.ObjectMeta{Name: "gtr-shared-merge"},
										Data: map[string]string{
											"static": "raw",
										},
									},
								},
							}},
							Generators: []capsulev1beta2.TemplateItemSpec{{
								MissingKey: "error",
								Template: `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: gtr-shared-merge
data:
  generated-{{ .namespace.metadata.name }}: "true"
`,
							}},
						}},
					},
				},
			}

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

			for _, ns := range tenantANamespaces {
				expectConfigMapData(ns, "gtr-shared-merge", map[string]string{
					"static":                        "raw",
					fmt.Sprintf("generated-%s", ns): "true",
				})
			}
		})

	})

	Context("context loading", func() {
		It("allows context loading from another namespace", func() {
			sourceNs := "gtr-shared-context"
			sourceNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: sourceNs},
			}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, sourceNamespace) }).Should(Succeed())
			defer ForceDeleteNamespace(ctx, sourceNs)

			for _, name := range []string{"ctx-1", "ctx-2"} {
				sec := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: sourceNs,
						Labels: map[string]string{
							"pullsecret.company.com": "true",
						},
					},
					Type:       corev1.SecretTypeOpaque,
					StringData: map[string]string{".dockerconfigjson": "e30="},
				}
				EventuallyCreation(func() error {
					sec.ResourceVersion = ""
					return k8sClient.Create(ctx, sec)
				}).Should(Succeed())
			}

			gtr := &capsulev1beta2.GlobalTenantResource{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gtr-context-cross-namespace",
					Labels: map[string]string{
						"e2e.capsule.dev/test-suite": "true",
					},
				},
				Spec: capsulev1beta2.GlobalTenantResourceSpec{
					Scope: api.ResourceScopeNamespace,
					TenantSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"energy": "solar"},
					},
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						ResyncPeriod:    metav1.Duration{Duration: 5 * time.Second},
						PruningOnDelete: ptr.To(true),
						Resources: []capsulev1beta2.ResourceSpec{{
							Context: &template.TemplateContext{
								Resources: []*template.TemplateResourceReference{{
									Index: "secrets",
									ResourceReference: template.ResourceReference{
										VersionKind: capruntime.VersionKind{
											APIVersion: "v1",
											Kind:       "Secret",
										},
										Namespace: sourceNs,
										Selector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"pullsecret.company.com": "true",
											},
										},
									},
								}},
							},
							Generators: []capsulev1beta2.TemplateItemSpec{{
								MissingKey: "error",
								Template: `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: gtr-context-count
data:
  count: '{{ if $.secrets }}{{ len $.secrets }}{{ else }}0{{ end }}'
`,
							}},
						}},
					},
				},
			}

			EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

			for _, ns := range tenantANamespaces {
				expectConfigMapData(ns, "gtr-context-count", map[string]string{"count": "2"})
			}
		})
	})
})

func newRawConfigMapGlobalTenantResource(name string, data map[string]string) *capsulev1beta2.GlobalTenantResource {
	return &capsulev1beta2.GlobalTenantResource{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"e2e.capsule.dev/test-suite": "true",
			},
		},
		Spec: capsulev1beta2.GlobalTenantResourceSpec{
			Scope: api.ResourceScopeNamespace,
			TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
				ResyncPeriod:    metav1.Duration{Duration: 5 * time.Second},
				PruningOnDelete: ptr.To(true),
				Resources: []capsulev1beta2.ResourceSpec{{
					RawItems: []capsulev1beta2.RawExtension{{
						RawExtension: runtime.RawExtension{
							Object: &corev1.ConfigMap{
								TypeMeta: metav1.TypeMeta{
									APIVersion: "v1",
									Kind:       "ConfigMap",
								},
								ObjectMeta: metav1.ObjectMeta{
									Name: "shared-config",
								},
								Data: data,
							},
						},
					}},
					AdditionalMetadata: &api.AdditionalMetadataSpec{
						Labels: map[string]string{
							"extra-label": "set-by-gtr",
						},
					},
				}},
			},
		},
	}
}

func renameFirstRawConfigMap(gtr *capsulev1beta2.GlobalTenantResource, name string) {
	gtr.Spec.Resources[0].RawItems[0] = capsulev1beta2.RawExtension{
		RawExtension: runtime.RawExtension{
			Object: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Data: gtr.Spec.Resources[0].RawItems[0].RawExtension.Object.(*corev1.ConfigMap).Data,
			},
		},
	}
}

func newRawConfigMapGlobalTenantResourceWithScope(name string, scope api.ResourceScope, data map[string]string) *capsulev1beta2.GlobalTenantResource {
	gtr := newRawConfigMapGlobalTenantResource(name, data)
	gtr.Spec.Scope = scope
	return gtr
}

func expectGlobalTenantResourceReady(name string) {
	Eventually(func(g Gomega) {
		current := &capsulev1beta2.GlobalTenantResource{}
		g.Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: name}, current)).To(Succeed())

		rdy := current.Status.Conditions.GetConditionByType(apimeta.ReadyCondition)
		g.Expect(rdy).ToNot(BeNil())
		g.Expect(rdy.Status).To(Equal(metav1.ConditionTrue))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func expectGlobalTenantResourceFailed(name, msgContains string) {
	Eventually(func(g Gomega) {
		current := &capsulev1beta2.GlobalTenantResource{}
		g.Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: name}, current)).To(Succeed())

		rdy := current.Status.Conditions.GetConditionByType(apimeta.ReadyCondition)
		g.Expect(rdy).ToNot(BeNil())
		g.Expect(rdy.Status).To(Equal(metav1.ConditionFalse))
		g.Expect(rdy.Message).To(ContainSubstring(msgContains))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}
