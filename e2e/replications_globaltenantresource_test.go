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
	apimeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/template"
)

const (
	managedByLabel = "projectcapsule.dev/managed-by"
	createdByLabel = "projectcapsule.dev/created-by"
	resourcesLabel = "resources"
)

var _ = Describe("GlobalTenantResource", Label("replications", "global", "globaltenantresource"), Ordered, func() {
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

		tenantAOwner = rbac.UserSpec{Name: "solar-user", Kind: rbac.OwnerKind("User")}
		tenantBOwner = rbac.UserSpec{Name: "lunar-user", Kind: rbac.OwnerKind("User")}

		tenantANamespaces = []string{"gtr-a-one", "gtr-a-two", "gtr-a-system"}
		tenantBNamespaces = []string{"gtr-b-one", "gtr-b-two"}
		allNamespaces = append(append([]string{}, tenantANamespaces...), tenantBNamespaces...)

		tenantA = &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gtr-tenant-a",
				Labels: map[string]string{
					"energy": "solar",
					"group":  "alpha",
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
				Name: "gtr-tenant-b",
				Labels: map[string]string{
					"energy": "lunar",
					"group":  "beta",
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

		EventuallyCreation(func() error {
			tenantB.ResourceVersion = ""
			return k8sClient.Create(ctx, tenantB)
		}).Should(Succeed())

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
			cfg := &capsulev1beta2.CapsuleConfiguration{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: originConfig.Name}, cfg); err != nil {
				return err
			}
			cfg.Spec = originConfig.Spec
			return k8sClient.Update(ctx, cfg)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
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
				configuration.Spec.Impersonation.TenantDefaultServiceAccount = "default"
			})

			Eventually(func(g Gomega) {
				current := &capsulev1beta2.GlobalTenantResource{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: gtr.Name}, current)).To(Succeed())
				g.Expect(current.Status.ServiceAccount).ToNot(BeNil())
				g.Expect(current.Status.ServiceAccount.Name).To(Equal(apimeta.RFC1123Name("default")))
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
			for _, ns := range []string{"gtr-a-one", "gtr-a-two"} {
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

			expectConfigMapData("gtr-a-one", "gtr-filtered-config", map[string]string{"mode": "filtered"})
			expectConfigMapData("gtr-a-two", "gtr-filtered-config", map[string]string{"mode": "filtered"})
			expectConfigMapAbsent("gtr-a-system", "gtr-filtered-config")

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
			Expect(k8sClient.Delete(ctx, gtr)).To(Succeed())

			for _, ns := range tenantANamespaces {
				expectConfigMapData(ns, "gtr-kept", map[string]string{"mode": "keep"})
			}
		})
	})

	Context("namespace target enforcement", func() {
		It("forces raw items into the iterated namespace even if metadata.namespace is set elsewhere", func() {
			gtr := &capsulev1beta2.GlobalTenantResource{
				ObjectMeta: metav1.ObjectMeta{Name: "gtr-raw-target-namespace"},
				Spec: capsulev1beta2.GlobalTenantResourceSpec{
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
				ObjectMeta: metav1.ObjectMeta{Name: "gtr-raw-and-generator-same-object"},
				Spec: capsulev1beta2.GlobalTenantResourceSpec{
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
  generated-{{ namespace }}: "true"
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
				ObjectMeta: metav1.ObjectMeta{Name: "gtr-context-cross-namespace"},
				Spec: capsulev1beta2.GlobalTenantResourceSpec{
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
										APIVersion: "v1",
										Kind:       "Secret",
										Namespace:  sourceNs,
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
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: capsulev1beta2.GlobalTenantResourceSpec{
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
