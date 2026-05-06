package e2e

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	apimeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
	"github.com/projectcapsule/capsule/pkg/template"
)

var (
	resyncPeriod = metav1.Duration{Duration: 10 * time.Second}
)

var _ = Describe("TenantResource SSA", Label("replications", "namespace", "tenantresource"), Ordered, func() {
	var (
		ctx                   context.Context
		tnt                   *capsulev1beta2.Tenant
		baseNamespace         string
		targetNamespaces      []string
		tenantOwner           rbac.UserSpec
		additionalBindingUser rbac.UserSpec
		sharedSourceSecret    *corev1.Secret
		contextSecretOne      *corev1.Secret
		contextSecretTwo      *corev1.Secret
	)

	originalConfig := &capsulev1beta2.CapsuleConfiguration{}

	BeforeEach(func() {
		ctx = context.Background()
		baseNamespace = "solar-system"
		targetNamespaces = []string{"solar-one", "solar-two", "solar-three"}
		tenantOwner = rbac.UserSpec{Name: "solar-user", Kind: rbac.OwnerKind("User")}
		additionalBindingUser = rbac.UserSpec{Name: "bob", Kind: rbac.OwnerKind("User")}

		Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: defaultConfigurationName}, originalConfig)).To(Succeed())

		tnt = &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{Name: "tenantresource-ssa"},
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{
					CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: tenantOwner},
				}},
				AdditionalRoleBindings: []rbac.AdditionalRoleBindingsSpec{{
					ClusterRoleName: "admin",
					Subjects:        []rbacv1.Subject{{Kind: "User", Name: additionalBindingUser.Name}},
				}},
			},
		}

		sharedSourceSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "seed-secret",
				Namespace: baseNamespace,
				Labels: map[string]string{
					"replicate": "true",
					"source":    "static",
				},
			},
			Type:       corev1.SecretTypeOpaque,
			StringData: map[string]string{"seed": "base"},
		}

		contextSecretOne = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pull-secret-one",
				Namespace: "solar-one",
				Labels:    map[string]string{"pullsecret.company.com": "true"},
			},
			Type:       corev1.SecretTypeOpaque,
			StringData: map[string]string{".dockerconfigjson": "e30="},
		}
		contextSecretTwo = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pull-secret-two",
				Namespace: "solar-one",
				Labels:    map[string]string{"pullsecret.company.com": "true"},
			},
			Type:       corev1.SecretTypeOpaque,
			StringData: map[string]string{".dockerconfigjson": "e30="},
		}

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, originalConfig)).To(Succeed())

		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(ctx, tnt)
		}).Should(Succeed())

		for _, ns := range append(append([]string{}, targetNamespaces...), baseNamespace) {
			namespace := NewNamespace(ns, map[string]string{apimeta.TenantLabel: tnt.GetName()})
			NamespaceCreation(namespace, tenantOwner, defaultTimeoutInterval).Should(Succeed())
		}
	})

	AfterEach(func() {
		ignoreNotFound(k8sClient.Delete(ctx, sharedSourceSecret))
		ignoreNotFound(k8sClient.Delete(ctx, contextSecretOne))
		ignoreNotFound(k8sClient.Delete(ctx, contextSecretTwo))

		for _, ns := range append([]string{baseNamespace}, targetNamespaces...) {
			ForceDeleteNamespace(ctx, ns)
		}

		EventuallyDeletion(tnt)

		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec = originalConfig.Spec
		})
	})

	It("aligns objects created by the legacy resource label implementation", func() {
		By("creating legacy-labelled objects in all target namespaces")
		for _, ns := range targetNamespaces {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "legacy-aligned-config",
					Namespace: ns,
					Labels: map[string]string{
						"capsule.clastix.io/resources": "0",
						apimeta.TenantLabel:            tnt.GetName(),
					},
				},
				Data: map[string]string{
					"legacy": "true",
				},
			}
			EventuallyCreation(func() error {
				cm.ResourceVersion = ""
				return k8sClient.Create(ctx, cm)
			}).Should(Succeed())
		}

		tr := newRawConfigMapTenantResource(baseNamespace, "legacy-alignment", map[string]string{
			"mode": "new-controller",
			"foo":  "bar",
		})
		tr.Spec.Resources[0].RawItems[0] = capsulev1beta2.RawExtension{
			RawExtension: runtime.RawExtension{
				Object: &corev1.ConfigMap{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "ConfigMap",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "legacy-aligned-config",
					},
					Data: map[string]string{
						"mode": "new-controller",
						"foo":  "bar",
					},
				},
			},
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())
		expectTenantResourceReady(baseNamespace, tr.Name)

		By("verifying the object was aligned to the new implementation")
		for _, ns := range targetNamespaces {
			Eventually(func(g Gomega) {
				cm := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      "legacy-aligned-config",
					Namespace: ns,
				}, cm)).To(Succeed())

				g.Expect(cm.Data).To(HaveKeyWithValue("mode", "new-controller"))
				g.Expect(cm.Data).To(HaveKeyWithValue("foo", "bar"))

				// legacy marker still present from the old object
				g.Expect(cm.Labels).To(HaveKeyWithValue(apimeta.TenantLabel, tnt.GetName()))

				// new implementation metadata
				g.Expect(cm.Labels).To(HaveKeyWithValue(apimeta.CreatedByCapsuleLabel, apimeta.ValueControllerReplications))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})

	Context("impersonation", func() {
		It("reflects the resolved service account in status", func() {
			tr := newRawConfigMapTenantResource(baseNamespace, "sa-resolution", map[string]string{"mode": "default-controller"})
			tr.Spec.ServiceAccount = nil

			By("creating the TenantResource without an explicit ServiceAccount")
			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())

			By("defaulting to the controller service account")
			expectResolvedServiceAccount(baseNamespace, tr.Name, "capsule", "capsule-system")

			By("configuring a tenant default service account")
			ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
				configuration.Spec.Impersonation.TenantDefaultServiceAccount = "default"
			})
			expectResolvedServiceAccount(baseNamespace, tr.Name, "default", baseNamespace)

			By("overriding with an explicit service account on the TenantResource")
			Eventually(func() error {
				current := &capsulev1beta2.TenantResource{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: tr.Name, Namespace: baseNamespace}, current); err != nil {
					return err
				}
				current.Spec.ServiceAccount = &apimeta.LocalRFC1123ObjectReference{Name: apimeta.RFC1123Name("custom-account")}
				return k8sClient.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			expectResolvedServiceAccount(baseNamespace, tr.Name, "custom-account", baseNamespace)

			By("removing the explicit override again")
			Eventually(func() error {
				current := &capsulev1beta2.TenantResource{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: tr.Name, Namespace: baseNamespace}, current); err != nil {
					return err
				}
				current.Spec.ServiceAccount = nil
				return k8sClient.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			expectResolvedServiceAccount(baseNamespace, tr.Name, "default", baseNamespace)
		})

		It("fails to apply raw items when the impersonated service account cannot create target resources", func() {
			saName := "restricted-creator"
			ensureServiceAccount(baseNamespace, saName)

			// Intentionally do not grant create/update/patch on configmaps in tenant namespaces.

			tr := newRawConfigMapTenantResource(baseNamespace, "sa-no-create", map[string]string{
				"mode": "blocked",
			})
			tr.Spec.ServiceAccount = &apimeta.LocalRFC1123ObjectReference{
				Name: apimeta.RFC1123Name(saName),
			}
			renameFirstTenantResourceRawConfigMap(tr, "blocked-create-config")

			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())

			expectResolvedServiceAccount(baseNamespace, tr.Name, saName, baseNamespace)
			expectTenantResourceFailed(baseNamespace, tr.Name, "applying of")

			for _, ns := range targetNamespaces {
				expectConfigMapAbsent(ns, "blocked-create-config")
			}
		})

		It("fails to render generators when the impersonated service account cannot read context resources", func() {
			saName := "restricted-context-reader"
			ensureServiceAccount(baseNamespace, saName)

			// Create context source secret.
			sec := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ctx-secret",
					Namespace: "solar-one",
					Labels: map[string]string{
						"pullsecret.company.com": "true",
					},
				},
				Type:       corev1.SecretTypeOpaque,
				StringData: map[string]string{".dockerconfigjson": "e30="},
			}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, sec) }).Should(Succeed())

			// Grant write on ConfigMaps in target namespaces if you want to isolate the failure to context loading.
			for _, ns := range targetNamespaces {
				bindServiceAccountToConfigMapWriter(baseNamespace, saName, ns)
			}
			// But do NOT grant get/list on secrets in solar-one.

			tr := &capsulev1beta2.TenantResource{
				ObjectMeta: metav1.ObjectMeta{Name: "sa-no-context-read", Namespace: baseNamespace},
				Spec: capsulev1beta2.TenantResourceSpec{
					ServiceAccount: &apimeta.LocalRFC1123ObjectReference{
						Name: apimeta.RFC1123Name(saName),
					},
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						ResyncPeriod:    resyncPeriod,
						PruningOnDelete: ptr.To(true),
						Resources: []capsulev1beta2.ResourceSpec{{
							Context: &template.TemplateContext{
								Resources: []*template.TemplateResourceReference{{
									Index: "secrets",
									ResourceReference: template.ResourceReference{
										APIVersion: "v1",
										Kind:       "Secret",
										Namespace:  "solar-one",
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
  name: blocked-context
data:
  count: '{{ if $.secrets }}{{ len $.secrets }}{{ else }}0{{ end }}'
`,
							}},
						}},
					},
				},
			}

			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())

			expectResolvedServiceAccount(baseNamespace, tr.Name, saName, baseNamespace)
			expectTenantResourceFailed(baseNamespace, tr.Name, "forbidden")

			for _, ns := range targetNamespaces {
				expectConfigMapAbsent(ns, "blocked-context")
			}
		})

		It("fails to prune replicated resources when the impersonated service account cannot delete them", func() {
			saCreate := "creator-ok"
			saNoDelete := "creator-no-delete"

			ensureServiceAccount(baseNamespace, saCreate)
			ensureServiceAccount(baseNamespace, saNoDelete)

			// Phase 1: creator can fully reconcile objects in every targeted namespace.
			for _, ns := range append(targetNamespaces, baseNamespace) {
				bindServiceAccountToConfigMapWriter(baseNamespace, saCreate, ns)
			}

			// Phase 2 SA can still read/apply, but has no delete verb.
			for _, ns := range append(targetNamespaces, baseNamespace) {
				bindServiceAccountToConfigMapWriter(baseNamespace, saNoDelete, ns)
			}

			tr := newRawConfigMapTenantResource(baseNamespace, "sa-no-prune", map[string]string{
				"mode": "created",
			})
			tr.Spec.ServiceAccount = &apimeta.LocalRFC1123ObjectReference{
				Name: apimeta.RFC1123Name(saCreate),
			}
			tr.Spec.PruningOnDelete = ptr.To(true)
			renameFirstTenantResourceRawConfigMap(tr, "prune-protected")

			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())
			expectTenantResourceReady(baseNamespace, tr.Name)

			for _, ns := range append(targetNamespaces, baseNamespace) {
				expectConfigMapData(ns, "prune-protected", map[string]string{"mode": "created"})
			}

			// Switch to the SA that cannot delete.
			Eventually(func() error {
				current := &capsulev1beta2.TenantResource{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: tr.Name, Namespace: baseNamespace}, current); err != nil {
					return err
				}
				current.Spec.ServiceAccount = &apimeta.LocalRFC1123ObjectReference{
					Name: apimeta.RFC1123Name(saNoDelete),
				}
				return k8sClient.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				current := &capsulev1beta2.TenantResource{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: tr.Name, Namespace: baseNamespace}, current)).To(Succeed())
				g.Expect(current.Status.ServiceAccount).ToNot(BeNil())
				g.Expect(current.Status.ServiceAccount.Name).To(Equal(apimeta.RFC1123Name(saNoDelete)))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			Expect(k8sClient.Delete(ctx, tr)).To(Succeed())

			// Objects should remain because prune cannot delete them.
			for _, ns := range append(targetNamespaces, baseNamespace) {
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "prune-protected", Namespace: ns}, cm)).To(Succeed())
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})
		It("fails to replicate namespacedItems when the impersonated service account cannot read source resources", func() {
			saName := "restricted-source-reader"
			ensureServiceAccount(baseNamespace, saName)

			source := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "source-secret",
					Namespace: baseNamespace,
					Labels: map[string]string{
						"replicate": "true",
					},
				},
				Type:       corev1.SecretTypeOpaque,
				StringData: map[string]string{"token": "abc"},
			}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, source) }).Should(Succeed())

			// Allow write into targets only, but do not grant read on source namespace secrets.
			for _, ns := range targetNamespaces {
				bindServiceAccountToSecretWriter(baseNamespace, saName, ns)
			}

			tr := &capsulev1beta2.TenantResource{
				ObjectMeta: metav1.ObjectMeta{Name: "sa-no-namespaceditem-read", Namespace: baseNamespace},
				Spec: capsulev1beta2.TenantResourceSpec{
					ServiceAccount: &apimeta.LocalRFC1123ObjectReference{
						Name: apimeta.RFC1123Name(saName),
					},
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						ResyncPeriod:    resyncPeriod,
						PruningOnDelete: ptr.To(true),
						Resources: []capsulev1beta2.ResourceSpec{{
							NamespacedItems: []template.ResourceReference{{
								APIVersion: "v1",
								Kind:       "Secret",
								Namespace:  baseNamespace,
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

			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())

			expectResolvedServiceAccount(baseNamespace, tr.Name, saName, baseNamespace)
			expectTenantResourceFailed(baseNamespace, tr.Name, "forbidden")

			for _, ns := range targetNamespaces {
				expectSecretAbsent(ns, "source-secret")
			}
		})

	})

	Context("advanced TenantResource ownership and namespace behavior", func() {
		It("allows multiple TenantResources to adopt and co-manage the same preexisting object with non-conflicting fields", func() {
			By("creating the preexisting object in all target namespaces")
			for _, ns := range targetNamespaces {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "shared-adopted-config",
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

			trA := newRawConfigMapTenantResource(baseNamespace, "adopt-shared-a", map[string]string{
				"foo": "one",
			})
			trA.Spec.Resources[0].RawItems[0] = capsulev1beta2.RawExtension{
				RawExtension: runtime.RawExtension{
					Object: &corev1.ConfigMap{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "v1",
							Kind:       "ConfigMap",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: "shared-adopted-config",
						},
						Data: map[string]string{
							"foo": "one",
						},
					},
				},
			}
			trA.Spec.Settings.Adopt = ptr.To(true)

			trB := newRawConfigMapTenantResource(baseNamespace, "adopt-shared-b", map[string]string{
				"bar": "two",
			})
			trB.Spec.Resources[0].RawItems[0] = capsulev1beta2.RawExtension{
				RawExtension: runtime.RawExtension{
					Object: &corev1.ConfigMap{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "v1",
							Kind:       "ConfigMap",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: "shared-adopted-config",
						},
						Data: map[string]string{
							"bar": "two",
						},
					},
				},
			}
			trB.Spec.Settings.Adopt = ptr.To(true)

			By("creating both TenantResources")
			EventuallyCreation(func() error { return k8sClient.Create(ctx, trA) }).Should(Succeed())
			EventuallyCreation(func() error { return k8sClient.Create(ctx, trB) }).Should(Succeed())

			expectTenantResourceReady(baseNamespace, trA.Name)
			expectTenantResourceReady(baseNamespace, trB.Name)

			By("verifying the final object contains the merged fields and is adopted, not created")
			for _, ns := range targetNamespaces {
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "shared-adopted-config", Namespace: ns}, cm)).To(Succeed())
					g.Expect(cm.Data).To(HaveKeyWithValue("existing", "true"))
					g.Expect(cm.Data).To(HaveKeyWithValue("foo", "one"))
					g.Expect(cm.Data).To(HaveKeyWithValue("bar", "two"))
					g.Expect(cm.Labels).To(HaveKeyWithValue(apimeta.NewManagedByCapsuleLabel, apimeta.ValueControllerReplications))
					g.Expect(cm.Labels).ToNot(HaveKey(apimeta.CreatedByCapsuleLabel))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})

		It("fails when multiple TenantResources target the same preexisting object without adoption", func() {
			By("creating the preexisting object in all target namespaces")
			for _, ns := range targetNamespaces {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "shared-no-adopt-config",
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

			trA := newRawConfigMapTenantResource(baseNamespace, "no-adopt-shared-a", map[string]string{
				"foo": "one",
			})
			trA.Spec.Resources[0].RawItems[0] = capsulev1beta2.RawExtension{
				RawExtension: runtime.RawExtension{
					Object: &corev1.ConfigMap{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "v1",
							Kind:       "ConfigMap",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: "shared-no-adopt-config",
						},
						Data: map[string]string{
							"foo": "one",
						},
					},
				},
			}
			trA.Spec.Settings.Adopt = ptr.To(false)

			trB := newRawConfigMapTenantResource(baseNamespace, "no-adopt-shared-b", map[string]string{
				"bar": "two",
			})
			trB.Spec.Resources[0].RawItems[0] = capsulev1beta2.RawExtension{
				RawExtension: runtime.RawExtension{
					Object: &corev1.ConfigMap{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "v1",
							Kind:       "ConfigMap",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: "shared-no-adopt-config",
						},
						Data: map[string]string{
							"bar": "two",
						},
					},
				},
			}
			trB.Spec.Settings.Adopt = ptr.To(false)

			By("creating both TenantResources")
			EventuallyCreation(func() error { return k8sClient.Create(ctx, trA) }).Should(Succeed())
			EventuallyCreation(func() error { return k8sClient.Create(ctx, trB) }).Should(Succeed())

			expectTenantResourceFailed(baseNamespace, trA.Name, "applying of 3 resources failed")
			expectTenantResourceFailed(baseNamespace, trB.Name, "applying of 3 resources failed")

			By("verifying the preexisting object remains unchanged")
			for _, ns := range targetNamespaces {
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "shared-no-adopt-config", Namespace: ns}, cm)).To(Succeed())
					g.Expect(cm.Data).To(Equal(map[string]string{
						"existing": "true",
					}))
					g.Expect(cm.Labels).ToNot(HaveKey(apimeta.NewManagedByCapsuleLabel))
					g.Expect(cm.Labels).ToNot(HaveKey(apimeta.CreatedByCapsuleLabel))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})

		It("forces rawItems into the current iterating tenant namespace regardless of metadata.namespace", func() {
			tr := &capsulev1beta2.TenantResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "raw-target-namespace",
					Namespace: baseNamespace,
				},
				Spec: capsulev1beta2.TenantResourceSpec{
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						PruningOnDelete: ptr.To(true),
						ResyncPeriod:    resyncPeriod,
						Resources: []capsulev1beta2.ResourceSpec{{
							RawItems: []capsulev1beta2.RawExtension{{
								RawExtension: runtime.RawExtension{
									Object: &corev1.ConfigMap{
										TypeMeta: metav1.TypeMeta{
											APIVersion: "v1",
											Kind:       "ConfigMap",
										},
										ObjectMeta: metav1.ObjectMeta{
											Name:      "raw-namespace-enforced",
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

			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())
			expectTenantResourceReady(baseNamespace, tr.Name)

			By("verifying the ConfigMap exists in each tenant namespace")
			for _, ns := range targetNamespaces {
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "raw-namespace-enforced", Namespace: ns}, cm)).To(Succeed())
					g.Expect(cm.Namespace).To(Equal(ns))
					g.Expect(cm.Data).To(HaveKeyWithValue("source", "raw"))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}

			By("verifying the ConfigMap was not created in the foreign namespace")
			expectConfigMapAbsent("kube-system", "raw-namespace-enforced")
		})

		It("merges rawItems and generators when they target the same object", func() {
			tr := &capsulev1beta2.TenantResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "raw-and-generator-same-object",
					Namespace: baseNamespace,
				},
				Spec: capsulev1beta2.TenantResourceSpec{
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						PruningOnDelete: ptr.To(true),
						ResyncPeriod:    resyncPeriod,
						Resources: []capsulev1beta2.ResourceSpec{{
							RawItems: []capsulev1beta2.RawExtension{{
								RawExtension: runtime.RawExtension{
									Object: &corev1.ConfigMap{
										TypeMeta: metav1.TypeMeta{
											APIVersion: "v1",
											Kind:       "ConfigMap",
										},
										ObjectMeta: metav1.ObjectMeta{
											Name: "raw-generated-shared",
										},
										Data: map[string]string{
											"static": "raw",
										},
									},
								},
							}},
							Generators: []capsulev1beta2.TemplateItemSpec{{
								MissingKey: "zero",
								Template: `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: raw-generated-shared
data:
  generated-{{ $.namespace.metadata.name }}: "true"
`,
							}},
						}},
					},
				},
			}

			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())
			expectTenantResourceReady(baseNamespace, tr.Name)

			By("verifying the object contains both raw and generated data in every target namespace")
			for _, ns := range targetNamespaces {
				expectConfigMapData(ns, "raw-generated-shared", map[string]string{
					"static":                        "raw",
					fmt.Sprintf("generated-%s", ns): "true",
				})
			}
		})
	})

	Context("apply lifecycle with prune enabled", func() {
		It("applies, updates and prunes raw items", func() {
			tr := newRawConfigMapTenantResource(baseNamespace, "raw-prune-enabled", map[string]string{
				"mode": "before",
				"foo":  "one",
			})
			tr.Spec.PruningOnDelete = ptr.To(true)

			By("creating the TenantResource")
			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())
			expectTenantResourceReady(baseNamespace, tr.Name)

			By("verifying the created ConfigMaps and status entries")
			for _, ns := range targetNamespaces {
				expectConfigMapData(ns, "shared-config", map[string]string{"mode": "before", "foo": "one"})
				expectManagedLabelsOnConfigMap(ns, "shared-config", true)
				expectProcessedItemStatus(baseNamespace, tr.Name, configMapRID(tnt.Name, ns, "shared-config", "0/raw-0"), metav1.ConditionTrue, true, "")
			}

			By("updating the applied data")
			Eventually(func() error {
				current := &capsulev1beta2.TenantResource{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: tr.Name, Namespace: baseNamespace}, current); err != nil {
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
								Name: "shared-config",
							},
							Data: map[string]string{
								"mode": "after",
								"foo":  "two",
								"bar":  "three",
							},
						},
					},
				}

				return k8sClient.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			for _, ns := range targetNamespaces {
				expectConfigMapData(ns, "shared-config", map[string]string{"mode": "after", "foo": "two", "bar": "three"})
				expectProcessedItemApplied(baseNamespace, tr.Name, configMapRID(tnt.Name, ns, "shared-config", "0/raw-0"))
			}

			By("deleting the TenantResource and pruning created objects")
			Expect(k8sClient.Delete(ctx, tr)).To(Succeed())
			for _, ns := range targetNamespaces {
				expectConfigMapDeleted(ns, "shared-config")
			}
		})
	})

	Context("apply lifecycle with prune disabled", func() {
		It("applies, updates and keeps objects while removing managed ownership", func() {
			tr := newRawConfigMapTenantResource(baseNamespace, "raw-prune-disabled", map[string]string{"mode": "keep"})
			tr.Spec.PruningOnDelete = ptr.To(false)

			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())
			expectTenantResourceReady(baseNamespace, tr.Name)

			for _, ns := range targetNamespaces {
				expectConfigMapData(ns, "shared-config", map[string]string{"mode": "keep"})
				expectManagedLabelsOnConfigMap(ns, "shared-config", true)
			}

			Expect(k8sClient.Delete(ctx, tr)).To(Succeed())

			By("verifying the ConfigMaps remain but are no longer managed")
			for _, ns := range targetNamespaces {
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "shared-config", Namespace: ns}, cm)).To(Succeed())
					g.Expect(cm.Labels).To(HaveKeyWithValue(apimeta.CreatedByCapsuleLabel, apimeta.ValueControllerReplications))
					g.Expect(cm.Labels).ToNot(HaveKey(apimeta.NewManagedByCapsuleLabel))
					g.Expect(cm.Data).To(HaveKeyWithValue("mode", "keep"))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})
	})

	Context("adoption", func() {
		It("fails without adopt and succeeds with adopt", func() {
			preexisting := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "adopt-me", Namespace: "solar-one"},
				Data:       map[string]string{"existing": "true"},
			}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, preexisting) }).Should(Succeed())

			withoutAdopt := newGeneratorConfigMapTenantResource(baseNamespace, "adopt-disabled", `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: adopt-me
data:
  source: generator
  namespace: "{{ $.namespace.metadata.name }}"
`)
			withoutAdopt.Spec.PruningOnDelete = ptr.To(true)
			withoutAdopt.Spec.Settings.Adopt = ptr.To(false)

			EventuallyCreation(func() error { return k8sClient.Create(ctx, withoutAdopt) }).Should(Succeed())
			expectTenantResourceFailed(baseNamespace, withoutAdopt.Name, "applying of")
			expectProcessedItemStatus(baseNamespace, withoutAdopt.Name, configMapRID(tnt.Name, "solar-one", "adopt-me", "0/generator-0-0"), metav1.ConditionFalse, false, "cannot be adopted")

			By("recreating a second resource with adoption enabled")
			withAdopt := newGeneratorConfigMapTenantResource(baseNamespace, "adopt-enabled", `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: adopt-me
data:
  source: generator
  namespace: "{{ $.namespace.metadata.name }}"
`)
			withAdopt.Spec.PruningOnDelete = ptr.To(true)
			withAdopt.Spec.Settings.Adopt = ptr.To(true)

			EventuallyCreation(func() error { return k8sClient.Create(ctx, withAdopt) }).Should(Succeed())
			expectTenantResourceReady(baseNamespace, withAdopt.Name)
			expectConfigMapData("solar-one", "adopt-me", map[string]string{"source": "generator", "namespace": "solar-one"})

			Eventually(func(g Gomega) {
				cm := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "adopt-me", Namespace: "solar-one"}, cm)).To(Succeed())
				g.Expect(cm.Labels).To(HaveKeyWithValue(apimeta.NewManagedByCapsuleLabel, apimeta.ValueControllerReplications))
				g.Expect(cm.Labels).ToNot(HaveKey(apimeta.CreatedByCapsuleLabel))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			expectProcessedItemStatus(baseNamespace, withAdopt.Name, configMapRID(tnt.Name, "solar-one", "adopt-me", "0/generator-0-0"), metav1.ConditionTrue, false, "")
		})
	})

	Context("same object within one TenantResource", func() {
		It("merges non-conflicting fields from generator and raw item", func() {
			tr := &capsulev1beta2.TenantResource{
				ObjectMeta: metav1.ObjectMeta{Name: "same-object-merge", Namespace: baseNamespace},
				Spec: capsulev1beta2.TenantResourceSpec{
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						PruningOnDelete: ptr.To(true),
						ResyncPeriod:    resyncPeriod,
						Resources: []capsulev1beta2.ResourceSpec{{
							Generators: []capsulev1beta2.TemplateItemSpec{{
								MissingKey: "error",
								Template: `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: common-config
data:
  generated-{{ $.namespace.metadata.name }}: from-generator
`,
							}},
							RawItems: []capsulev1beta2.RawExtension{{RawExtension: runtime.RawExtension{Object: &corev1.ConfigMap{
								TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
								ObjectMeta: metav1.ObjectMeta{Name: "common-config"},
								Data:       map[string]string{"additional-data": "raw"},
							}}}},
						}},
					},
				},
			}

			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())
			expectTenantResourceReady(baseNamespace, tr.Name)

			for _, ns := range targetNamespaces {
				expectConfigMapData(ns, "common-config", map[string]string{
					fmt.Sprintf("generated-%s", ns): "from-generator",
					"additional-data":               "raw",
				})
			}
		})
	})

	Context("generators and template context", func() {
		It("renders generator templates with tenant and namespace data", func() {
			tr := newGeneratorConfigMapTenantResource(baseNamespace, "generator-template", `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: generated-{{ $.namespace.metadata.name }}
data:
  tenant: "{{ $.tenant.metadata.name }}"
  namespace: "{{ $.namespace.metadata.name }}"
`)

			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())
			expectTenantResourceReady(baseNamespace, tr.Name)

			for _, ns := range targetNamespaces {
				expectConfigMapData(ns, fmt.Sprintf("generated-%s", ns), map[string]string{
					"tenant":    tnt.Name,
					"namespace": ns,
				})
				expectProcessedItemStatus(baseNamespace, tr.Name, configMapRID(tnt.Name, ns, fmt.Sprintf("generated-%s", ns), "0/generator-0-0"), metav1.ConditionTrue, true, "")
			}
		})

		It("places generated objects into the current tenant namespace even when the template sets metadata.namespace to a foreign namespace", func() {
			tr := &capsulev1beta2.TenantResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "generator-enforce-target-namespace",
					Namespace: baseNamespace,
				},
				Spec: capsulev1beta2.TenantResourceSpec{
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						PruningOnDelete: ptr.To(true),
						ResyncPeriod:    resyncPeriod,
						Resources: []capsulev1beta2.ResourceSpec{{
							Generators: []capsulev1beta2.TemplateItemSpec{{
								MissingKey: "error",
								Template: `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: generated-namespace-locked
  namespace: kube-system
data:
  source: generator
  renderedFor: "{{ $.namespace.metadata.name }}"
`,
							}},
						}},
					},
				},
			}

			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())
			expectTenantResourceReady(baseNamespace, tr.Name)

			By("verifying the generated object is created in each tenant namespace, not in the foreign namespace")
			for _, ns := range targetNamespaces {
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name:      "generated-namespace-locked",
						Namespace: ns,
					}, cm)).To(Succeed())
					g.Expect(cm.Namespace).To(Equal(ns))
					g.Expect(cm.Data).To(HaveKeyWithValue("source", "generator"))
					g.Expect(cm.Data).To(HaveKeyWithValue("renderedFor", ns))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}

			Consistently(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "generated-namespace-locked",
					Namespace: "kube-system",
				}, &corev1.ConfigMap{})
			}, 5*time.Second, defaultPollInterval).Should(HaveOccurred())
		})

		It("loads context from the explicitly referenced namespace for every rendered namespace", func() {
			EventuallyCreation(func() error { return k8sClient.Create(ctx, contextSecretOne) }).Should(Succeed())
			EventuallyCreation(func() error { return k8sClient.Create(ctx, contextSecretTwo) }).Should(Succeed())

			tr := &capsulev1beta2.TenantResource{
				ObjectMeta: metav1.ObjectMeta{Name: "context-loading-fixed-namespace", Namespace: baseNamespace},
				Spec: capsulev1beta2.TenantResourceSpec{
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						PruningOnDelete: ptr.To(true),
						ResyncPeriod:    resyncPeriod,
						Resources: []capsulev1beta2.ResourceSpec{{
							Context: &template.TemplateContext{
								Resources: []*template.TemplateResourceReference{{
									Index: "secrets",
									ResourceReference: template.ResourceReference{
										APIVersion: "v1",
										Kind:       "Secret",
										Namespace:  "solar-one",
										Selector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"pullsecret.company.com": "true",
											},
										},
									},
								}},
							},
							Generators: []capsulev1beta2.TemplateItemSpec{{
								MissingKey: "zero",
								Template: `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: show-context
data:
  count: '{{ if $.secrets }}{{ len $.secrets }}{{ else }}0{{ end }}'
`,
							}},
						}},
					},
				},
			}

			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())
			expectTenantResourceReady(baseNamespace, tr.Name)

			expectConfigMapData("solar-one", "show-context", map[string]string{"count": "2"})
			expectConfigMapData("solar-two", "show-context", map[string]string{"count": "2"})
			expectConfigMapData("solar-three", "show-context", map[string]string{"count": "2"})
		})

		It("loads context from the current iterating namespace", func() {
			EventuallyCreation(func() error { return k8sClient.Create(ctx, contextSecretOne) }).Should(Succeed())
			EventuallyCreation(func() error { return k8sClient.Create(ctx, contextSecretTwo) }).Should(Succeed())

			tr := &capsulev1beta2.TenantResource{
				ObjectMeta: metav1.ObjectMeta{Name: "context-loading-variable-namespace", Namespace: baseNamespace},
				Spec: capsulev1beta2.TenantResourceSpec{
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						PruningOnDelete: ptr.To(true),
						ResyncPeriod:    resyncPeriod,
						Resources: []capsulev1beta2.ResourceSpec{{
							Context: &template.TemplateContext{
								Resources: []*template.TemplateResourceReference{{
									Index: "secrets",
									ResourceReference: template.ResourceReference{
										APIVersion: "v1",
										Kind:       "Secret",
										Namespace:  "{{ namespace }}",
										Selector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"pullsecret.company.com": "true",
											},
										},
									},
								}},
							},
							Generators: []capsulev1beta2.TemplateItemSpec{{
								MissingKey: "zero",
								Template: `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: show-context
data:
  count: '{{ if $.secrets }}{{ len $.secrets }}{{ else }}0{{ end }}'
`,
							}},
						}},
					},
				},
			}

			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())
			expectTenantResourceReady(baseNamespace, tr.Name)

			expectConfigMapData("solar-one", "show-context", map[string]string{"count": "2"})
			expectConfigMapData("solar-two", "show-context", map[string]string{"count": "0"})
			expectConfigMapData("solar-three", "show-context", map[string]string{"count": "0"})
		})

		It("loads context per rendered namespace", func() {
			EventuallyCreation(func() error { return k8sClient.Create(ctx, contextSecretOne) }).Should(Succeed())
			EventuallyCreation(func() error { return k8sClient.Create(ctx, contextSecretTwo) }).Should(Succeed())

			solarTwoSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pull-secret-solar-two",
					Namespace: "solar-two",
					Labels: map[string]string{
						"pullsecret.company.com": "true",
					},
				},
				Type:       corev1.SecretTypeOpaque,
				StringData: map[string]string{".dockerconfigjson": "e30="},
			}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, solarTwoSecret) }).Should(Succeed())

			tr := &capsulev1beta2.TenantResource{
				ObjectMeta: metav1.ObjectMeta{Name: "context-variable-namespace", Namespace: baseNamespace},
				Spec: capsulev1beta2.TenantResourceSpec{
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						PruningOnDelete: ptr.To(true),
						ResyncPeriod:    resyncPeriod,
						Resources: []capsulev1beta2.ResourceSpec{{
							Context: &template.TemplateContext{
								Resources: []*template.TemplateResourceReference{{
									Index: "secrets",
									ResourceReference: template.ResourceReference{
										APIVersion: "v1",
										Kind:       "Secret",
										Namespace:  "{{.namespace}}",
										Selector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"pullsecret.company.com": "true",
											},
										},
									},
								}},
							},
							Generators: []capsulev1beta2.TemplateItemSpec{{
								MissingKey: "zero",
								Template: `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: show-context
data:
  count: '{{ if $.secrets }}{{ len $.secrets }}{{ else }}0{{ end }}'
`,
							}},
						}},
					},
				},
			}

			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())
			expectTenantResourceReady(baseNamespace, tr.Name)

			expectConfigMapData("solar-one", "show-context", map[string]string{"count": "2"})
			expectConfigMapData("solar-two", "show-context", map[string]string{"count": "1"})
			expectConfigMapData("solar-three", "show-context", map[string]string{"count": "0"})
		})

		It("fails when context tries to load from a namespace outside the tenant", func() {
			foreignSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foreign-pull-secret",
					Namespace: "kube-system",
					Labels: map[string]string{
						"pullsecret.company.com": "true",
					},
				},
				Type:       corev1.SecretTypeOpaque,
				StringData: map[string]string{".dockerconfigjson": "e30="},
			}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, foreignSecret) }).Should(Succeed())
			defer ignoreNotFound(k8sClient.Delete(ctx, foreignSecret))

			tr := &capsulev1beta2.TenantResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "context-forbidden-namespace",
					Namespace: baseNamespace,
				},
				Spec: capsulev1beta2.TenantResourceSpec{
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						PruningOnDelete: ptr.To(true),
						ResyncPeriod:    resyncPeriod,
						Resources: []capsulev1beta2.ResourceSpec{{
							Context: &template.TemplateContext{
								Resources: []*template.TemplateResourceReference{{
									Index: "secrets",
									ResourceReference: template.ResourceReference{
										APIVersion: "v1",
										Kind:       "Secret",
										Namespace:  "kube-system",
										Selector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"pullsecret.company.com": "true",
											},
										},
									},
								}},
							},
							Generators: []capsulev1beta2.TemplateItemSpec{{
								MissingKey: "zero",
								Template: `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: forbidden-context
data:
  count: '{{ if $.secrets }}{{ len $.secrets }}{{ else }}0{{ end }}'
`,
							}},
						}},
					},
				},
			}

			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())

			Eventually(func(g Gomega) {
				current := &capsulev1beta2.TenantResource{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: tr.Name, Namespace: tr.Namespace}, current)).To(Succeed())

				rdy := current.Status.Conditions.GetConditionByType(apimeta.ReadyCondition)
				g.Expect(rdy).ToNot(BeNil())
				g.Expect(rdy.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(rdy.Message).To(ContainSubstring("cross-namespace selection is not allowed"))
				g.Expect(rdy.Message).To(ContainSubstring("kube-system"))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			for _, ns := range targetNamespaces {
				Consistently(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: "forbidden-context", Namespace: ns}, &corev1.ConfigMap{})
				}, 5*time.Second, defaultPollInterval).Should(HaveOccurred())
			}
		})

		It("fails when namespacedItems tries to load from a namespace outside the tenant", func() {
			foreignSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foreign-source-secret",
					Namespace: "kube-system",
					Labels: map[string]string{
						"pullsecret.company.com": "true",
					},
				},
				Type:       corev1.SecretTypeOpaque,
				StringData: map[string]string{"token": "forbidden"},
			}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, foreignSecret) }).Should(Succeed())
			defer ignoreNotFound(k8sClient.Delete(ctx, foreignSecret))

			tr := &capsulev1beta2.TenantResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "namespaceditems-forbidden-namespace",
					Namespace: baseNamespace,
				},
				Spec: capsulev1beta2.TenantResourceSpec{
					ServiceAccount: &apimeta.LocalRFC1123ObjectReference{Name: apimeta.RFC1123Name("replicator")},
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						PruningOnDelete: ptr.To(true),
						ResyncPeriod:    resyncPeriod,
						Resources: []capsulev1beta2.ResourceSpec{{
							NamespacedItems: []template.ResourceReference{{
								APIVersion: "v1",
								Kind:       "Secret",
								Namespace:  "kube-system",
								Selector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"pullsecret.company.com": "true",
									},
								},
							}},
						}},
					},
				},
			}

			EnsureServiceAccount(ctx, k8sClient, tr.Spec.ServiceAccount.Name.String(), baseNamespace)
			EnsureRoleAndBindingForNamespaces(ctx, k8sClient, tr.Spec.ServiceAccount.Name.String(), baseNamespace, append(targetNamespaces, baseNamespace))

			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())

			Eventually(func(g Gomega) {
				current := &capsulev1beta2.TenantResource{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: tr.Name, Namespace: tr.Namespace}, current)).To(Succeed())

				rdy := current.Status.Conditions.GetConditionByType(apimeta.ReadyCondition)
				g.Expect(rdy).ToNot(BeNil())
				g.Expect(rdy.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(rdy.Message).To(ContainSubstring("cross-namespace selection is not allowed"))
				g.Expect(rdy.Message).To(ContainSubstring("kube-system"))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			for _, ns := range targetNamespaces {
				Consistently(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: "foreign-source-secret", Namespace: ns}, &corev1.Secret{})
				}, 5*time.Second, defaultPollInterval).Should(HaveOccurred())
			}
		})

		It("fails when a templated namespace resolves to a forbidden namespace", func() {
			foreignSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "templated-foreign-secret",
					Namespace: "kube-system",
					Labels: map[string]string{
						"pullsecret.company.com": "true",
					},
				},
				Type:       corev1.SecretTypeOpaque,
				StringData: map[string]string{"token": "forbidden"},
			}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, foreignSecret) }).Should(Succeed())
			defer ignoreNotFound(k8sClient.Delete(ctx, foreignSecret))

			tr := &capsulev1beta2.TenantResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "templated-forbidden-namespace",
					Namespace: baseNamespace,
				},
				Spec: capsulev1beta2.TenantResourceSpec{
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						PruningOnDelete: ptr.To(true),
						ResyncPeriod:    resyncPeriod,
						Resources: []capsulev1beta2.ResourceSpec{{
							Context: &template.TemplateContext{
								Resources: []*template.TemplateResourceReference{{
									Index: "secrets",
									ResourceReference: template.ResourceReference{
										APIVersion: "v1",
										Kind:       "Secret",
										Namespace:  "{{ forbiddenNamespace }}",
										Selector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"pullsecret.company.com": "true",
											},
										},
									},
								}},
							},
							Generators: []capsulev1beta2.TemplateItemSpec{{
								MissingKey: "zero",
								Template: `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: templated-forbidden-context
data:
  count: '{{ if $.secrets }}{{ len $.secrets }}{{ else }}0{{ end }}'
`,
							}},
						}},
					},
				},
			}

			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())

			Eventually(func(g Gomega) {
				current := &capsulev1beta2.TenantResource{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: tr.Name, Namespace: tr.Namespace}, current)).To(Succeed())

				rdy := current.Status.Conditions.GetConditionByType(apimeta.ReadyCondition)
				g.Expect(rdy).ToNot(BeNil())
				g.Expect(rdy.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(rdy.Message).To(ContainSubstring("cross-namespace selection is not allowed"))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		It("places rawItems into the current tenant namespace even when metadata.namespace is set to a foreign namespace", func() {
			tr := &capsulev1beta2.TenantResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rawitems-enforce-target-namespace",
					Namespace: baseNamespace,
				},
				Spec: capsulev1beta2.TenantResourceSpec{
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						PruningOnDelete: ptr.To(true),
						ResyncPeriod:    resyncPeriod,
						Resources: []capsulev1beta2.ResourceSpec{{
							RawItems: []capsulev1beta2.RawExtension{{
								RawExtension: runtime.RawExtension{
									Object: &corev1.ConfigMap{
										TypeMeta: metav1.TypeMeta{
											APIVersion: "v1",
											Kind:       "ConfigMap",
										},
										ObjectMeta: metav1.ObjectMeta{
											Name:      "raw-namespace-locked",
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

			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())
			expectTenantResourceReady(baseNamespace, tr.Name)

			By("verifying the object is created in each tenant namespace, not in the foreign namespace")
			for _, ns := range targetNamespaces {
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name:      "raw-namespace-locked",
						Namespace: ns,
					}, cm)).To(Succeed())
					g.Expect(cm.Namespace).To(Equal(ns))
					g.Expect(cm.Data).To(HaveKeyWithValue("source", "raw"))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}

			Consistently(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "raw-namespace-locked",
					Namespace: "kube-system",
				}, &corev1.ConfigMap{})
			}, 5*time.Second, defaultPollInterval).Should(HaveOccurred())
		})

	})

	Context("multiple TenantResources targeting the same object", func() {
		It("allows non-conflicting ownership without force", func() {
			first := newGeneratorConfigMapTenantResource(baseNamespace, "same-object-no-force-a", `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: owned-together
data:
  from-a: one
`)
			second := newRawConfigMapTenantResource(baseNamespace, "same-object-no-force-b", map[string]string{"from-b": "two"})
			second.Spec.Resources[0].RawItems[0].RawExtension.Object.(*corev1.ConfigMap).Name = "owned-together"

			EventuallyCreation(func() error { return k8sClient.Create(ctx, first) }).Should(Succeed())
			EventuallyCreation(func() error { return k8sClient.Create(ctx, second) }).Should(Succeed())

			expectTenantResourceReady(baseNamespace, first.Name)
			expectTenantResourceReady(baseNamespace, second.Name)

			for _, ns := range targetNamespaces {
				expectConfigMapData(ns, "owned-together", map[string]string{"from-a": "one", "from-b": "two"})
			}
		})

		It("fails on conflicting ownership without force", func() {
			first := newRawConfigMapTenantResource(baseNamespace, "same-object-conflict-a", map[string]string{"shared": "one"})
			first.Spec.Resources[0].RawItems[0].RawExtension.Object.(*corev1.ConfigMap).Name = "force-target"

			second := newRawConfigMapTenantResource(baseNamespace, "same-object-conflict-b", map[string]string{"shared": "two"})
			second.Spec.Resources[0].RawItems[0].RawExtension.Object.(*corev1.ConfigMap).Name = "force-target"
			second.Spec.Settings.Force = ptr.To(false)

			EventuallyCreation(func() error { return k8sClient.Create(ctx, first) }).Should(Succeed())
			expectTenantResourceReady(baseNamespace, first.Name)

			EventuallyCreation(func() error { return k8sClient.Create(ctx, second) }).Should(Succeed())
			expectTenantResourceFailed(baseNamespace, second.Name, "applying of")

			for _, ns := range targetNamespaces {
				expectConfigMapData(ns, "force-target", map[string]string{"shared": "one"})
			}
		})

		It("wins conflicting ownership with force", func() {
			first := newRawConfigMapTenantResource(baseNamespace, "same-object-force-a", map[string]string{"shared": "one"})
			first.Spec.Resources[0].RawItems[0].RawExtension.Object.(*corev1.ConfigMap).Name = "forced-target"

			second := newRawConfigMapTenantResource(baseNamespace, "same-object-force-b", map[string]string{"shared": "two"})
			second.Spec.Resources[0].RawItems[0].RawExtension.Object.(*corev1.ConfigMap).Name = "forced-target"
			second.Spec.Settings.Force = ptr.To(true)

			EventuallyCreation(func() error { return k8sClient.Create(ctx, first) }).Should(Succeed())
			expectTenantResourceReady(baseNamespace, first.Name)

			EventuallyCreation(func() error { return k8sClient.Create(ctx, second) }).Should(Succeed())
			expectTenantResourceReady(baseNamespace, second.Name)

			for _, ns := range targetNamespaces {
				expectConfigMapData(ns, "forced-target", map[string]string{"shared": "two"})
			}
		})
	})

	Context("namespaced item replication", func() {
		It("replicates source objects and strips selector labels to avoid loops", func() {
			EventuallyCreation(func() error { return k8sClient.Create(ctx, sharedSourceSecret) }).Should(Succeed())

			tr := &capsulev1beta2.TenantResource{
				ObjectMeta: metav1.ObjectMeta{Name: "selector-replication", Namespace: baseNamespace},
				Spec: capsulev1beta2.TenantResourceSpec{
					ServiceAccount: &apimeta.LocalRFC1123ObjectReference{Name: apimeta.RFC1123Name("replicator")},
					TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
						PruningOnDelete: ptr.To(true),
						ResyncPeriod:    resyncPeriod,
						Resources: []capsulev1beta2.ResourceSpec{{
							NamespacedItems: []template.ResourceReference{{
								APIVersion: "v1",
								Kind:       "Secret",
								Namespace:  baseNamespace,
								Selector: &metav1.LabelSelector{MatchLabels: map[string]string{
									"replicate": "true",
								}},
							}},
						}},
					},
				},
			}

			EnsureServiceAccount(ctx, k8sClient, tr.Spec.ServiceAccount.Name.String(), baseNamespace)
			EnsureRoleAndBindingForNamespaces(ctx, k8sClient, tr.Spec.ServiceAccount.Name.String(), baseNamespace, append(targetNamespaces, baseNamespace))

			EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())
			expectTenantResourceReady(baseNamespace, tr.Name)

			for _, ns := range targetNamespaces {
				Eventually(func(g Gomega) {
					sec := &corev1.Secret{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: sharedSourceSecret.Name, Namespace: ns}, sec)).To(Succeed())
					g.Expect(sec.Labels).ToNot(HaveKey("replicate"))
					g.Expect(sec.Labels).To(HaveKeyWithValue("source", "static"))
					g.Expect(sec.Labels).To(HaveKeyWithValue(apimeta.CreatedByCapsuleLabel, apimeta.ValueControllerReplications))
					g.Expect(sec.Labels).To(HaveKeyWithValue(apimeta.NewManagedByCapsuleLabel, apimeta.ValueControllerReplications))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})
	})
})

func newRawConfigMapTenantResource(namespace, name string, data map[string]string) *capsulev1beta2.TenantResource {
	return &capsulev1beta2.TenantResource{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: capsulev1beta2.TenantResourceSpec{
			TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
				ResyncPeriod:    resyncPeriod,
				PruningOnDelete: ptr.To(true),
				Resources: []capsulev1beta2.ResourceSpec{{
					RawItems: []capsulev1beta2.RawExtension{{RawExtension: runtime.RawExtension{Object: &corev1.ConfigMap{
						TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
						ObjectMeta: metav1.ObjectMeta{Name: "shared-config"},
						Data:       data,
					}}}},
					AdditionalMetadata: &api.AdditionalMetadataSpec{Labels: map[string]string{"extra-label": "set-by-tr"}},
				}},
			},
		},
	}
}

func newGeneratorConfigMapTenantResource(namespace, name, tpl string) *capsulev1beta2.TenantResource {
	return &capsulev1beta2.TenantResource{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: capsulev1beta2.TenantResourceSpec{
			TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
				ResyncPeriod:    resyncPeriod,
				PruningOnDelete: ptr.To(true),
				Resources: []capsulev1beta2.ResourceSpec{{
					Generators: []capsulev1beta2.TemplateItemSpec{{MissingKey: "zero", Template: tpl}},
				}},
			},
		},
	}
}

func getTenantResource(namespace, name string) *capsulev1beta2.TenantResource {
	tr := &capsulev1beta2.TenantResource{}
	Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, tr)).To(Succeed())
	return tr
}

func expectTenantResourceReady(namespace, name string) {
	Eventually(func(g Gomega) {
		tr := &capsulev1beta2.TenantResource{}
		g.Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, tr)).To(Succeed())
		rdy := tr.Status.Conditions.GetConditionByType(apimeta.ReadyCondition)
		g.Expect(rdy).ToNot(BeNil())
		g.Expect(rdy.Status).To(Equal(metav1.ConditionTrue))
		g.Expect(tr.Status.Size).To(BeNumerically(">", 0))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func expectTenantResourceFailed(namespace, name, contains string) {
	Eventually(func(g Gomega) {
		tr := &capsulev1beta2.TenantResource{}
		g.Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, tr)).To(Succeed())
		rdy := tr.Status.Conditions.GetConditionByType(apimeta.ReadyCondition)
		g.Expect(rdy).ToNot(BeNil())
		g.Expect(rdy.Status).To(Equal(metav1.ConditionFalse))
		g.Expect(strings.ToLower(rdy.Message)).To(ContainSubstring(strings.ToLower(contains)))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func expectResolvedServiceAccount(namespace, name, saName, saNamespace string) {
	Eventually(func(g Gomega) {
		tr := &capsulev1beta2.TenantResource{}
		g.Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, tr)).To(Succeed())
		g.Expect(tr.Status.ServiceAccount).ToNot(BeNil())
		g.Expect(tr.Status.ServiceAccount.Name).To(Equal(apimeta.RFC1123Name(saName)))
		g.Expect(tr.Status.ServiceAccount.Namespace).To(Equal(apimeta.RFC1123SubdomainName(saNamespace)))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func expectConfigMapData(namespace, name string, expected map[string]string) {
	Eventually(func(g Gomega) {
		cm := &corev1.ConfigMap{}
		g.Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, cm)).To(Succeed())
		for k, v := range expected {
			g.Expect(cm.Data).To(HaveKeyWithValue(k, v))
		}
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func expectConfigMapDeleted(namespace, name string) {
	Eventually(func() error {
		cm := &corev1.ConfigMap{}
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, cm)
		return client.IgnoreNotFound(err)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

	Consistently(func() bool {
		cm := &corev1.ConfigMap{}
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, cm)
		return client.IgnoreNotFound(err) == nil
	}, 3*time.Second, defaultPollInterval).Should(BeTrue())
}

func expectManagedLabelsOnConfigMap(namespace, name string, created bool) {
	Eventually(func(g Gomega) {
		cm := &corev1.ConfigMap{}
		g.Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, cm)).To(Succeed())
		g.Expect(cm.Labels).To(HaveKeyWithValue(apimeta.NewManagedByCapsuleLabel, apimeta.ValueControllerReplications))
		if created {
			g.Expect(cm.Labels).To(HaveKeyWithValue(apimeta.CreatedByCapsuleLabel, apimeta.ValueControllerReplications))
		}
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func expectProcessedItemApplied(namespace, trName string, rid gvk.ResourceID) {
	Eventually(func(g Gomega) {
		tr := getTenantResource(namespace, trName)
		item := tr.Status.ProcessedItems.GetItem(rid)
		g.Expect(item).ToNot(BeNil())
		g.Expect(item.LastApply).ToNot(BeNil())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func expectProcessedItemStatus(namespace, trName string, rid gvk.ResourceID, cond metav1.ConditionStatus, created bool, msgContains string) {
	Eventually(func(g Gomega) {
		tr := getTenantResource(namespace, trName)
		item := tr.Status.ProcessedItems.GetItem(rid)
		g.Expect(item).ToNot(BeNil(), "processed item %+v not found", rid)
		g.Expect(item.ObjectReferenceStatusCondition.Status).To(Equal(cond))
		g.Expect(item.ObjectReferenceStatusCondition.Type).To(Equal(apimeta.ReadyCondition))
		g.Expect(item.ObjectReferenceStatusCondition.Created).To(Equal(created))
		if msgContains != "" {
			g.Expect(strings.ToLower(item.ObjectReferenceStatusCondition.Message)).To(ContainSubstring(strings.ToLower(msgContains)))
		}
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func configMapRID(tenant, namespace, name, origin string) gvk.ResourceID {
	return gvk.ResourceID{
		Version:   "v1",
		Kind:      "ConfigMap",
		Name:      name,
		Namespace: namespace,
		TenantResourceIDWithOrigin: gvk.TenantResourceIDWithOrigin{
			Origin:           origin,
			TenantResourceID: gvk.TenantResourceID{Tenant: tenant},
		},
	}
}

func expectConfigMapAbsent(namespace, name string) {
	Consistently(func() error {
		return k8sClient.Get(context.Background(), types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		}, &corev1.ConfigMap{})
	}, 5*time.Second, defaultPollInterval).Should(HaveOccurred())
}

func expectSecretAbsent(namespace, name string) {
	Consistently(func() error {
		return k8sClient.Get(context.Background(), types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		}, &corev1.Secret{})
	}, 5*time.Second, defaultPollInterval).Should(HaveOccurred())
}

func renameFirstTenantResourceRawConfigMap(tr *capsulev1beta2.TenantResource, name string) {
	tr.Spec.Resources[0].RawItems[0] = capsulev1beta2.RawExtension{
		RawExtension: runtime.RawExtension{
			Object: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
				Data: tr.Spec.Resources[0].RawItems[0].RawExtension.Object.(*corev1.ConfigMap).Data,
			},
		},
	}
}

func bindServiceAccountToNamespacedResource(
	saNamespace, saName, targetNamespace string,
	resources, verbs []string,
) {
	ctx := context.Background()

	resourceKey := strings.Join(resources, "-")
	roleName := fmt.Sprintf("sa-%s-%s-%s", saName, resourceKey, targetNamespace)
	roleBindingName := fmt.Sprintf("sa-%s-%s-%s-binding", saName, resourceKey, targetNamespace)

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: targetNamespace,
		},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{""},
			Resources: resources,
			Verbs:     verbs,
		}},
	}

	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleBindingName,
			Namespace: targetNamespace,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      saName,
			Namespace: saNamespace,
		}},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     roleName,
		},
	}

	Eventually(func() error {
		current := &rbacv1.Role{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: roleName, Namespace: targetNamespace}, current)
		if apierrors.IsNotFound(err) {
			return k8sClient.Create(ctx, role)
		}
		if err != nil {
			return err
		}

		current.Rules = role.Rules
		return k8sClient.Update(ctx, current)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

	Eventually(func() error {
		current := &rbacv1.RoleBinding{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: roleBindingName, Namespace: targetNamespace}, current)
		if apierrors.IsNotFound(err) {
			return k8sClient.Create(ctx, roleBinding)
		}
		if err != nil {
			return err
		}

		current.Subjects = roleBinding.Subjects
		current.RoleRef = roleBinding.RoleRef
		return k8sClient.Update(ctx, current)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func bindServiceAccountToSecretWriter(saNamespace, saName, targetNamespace string) {
	bindServiceAccountToNamespacedResource(
		saNamespace,
		saName,
		targetNamespace,
		[]string{"secrets"},
		[]string{"get", "list", "watch", "create", "update", "patch"},
	)
}

func bindServiceAccountToSecretReader(saNamespace, saName, targetNamespace string) {
	bindServiceAccountToNamespacedResource(
		saNamespace,
		saName,
		targetNamespace,
		[]string{"secrets"},
		[]string{"get", "list", "watch"},
	)
}

func bindServiceAccountToConfigMapWriter(saNamespace, saName, targetNamespace string) {
	bindServiceAccountToNamespacedResource(
		saNamespace,
		saName,
		targetNamespace,
		[]string{"configmaps"},
		[]string{"get", "list", "watch", "create", "update", "patch"},
	)
}

func bindServiceAccountToConfigMapDeleter(saNamespace, saName, targetNamespace string) {
	bindServiceAccountToNamespacedResource(
		saNamespace,
		saName,
		targetNamespace,
		[]string{"configmaps"},
		[]string{"get", "list", "watch", "delete"},
	)
}

func ensureServiceAccount(namespace, name string) {
	ctx := context.Background()

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	Eventually(func() error {
		current := &corev1.ServiceAccount{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, current)
		if apierrors.IsNotFound(err) {
			return k8sClient.Create(ctx, sa)
		}
		return err
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}
