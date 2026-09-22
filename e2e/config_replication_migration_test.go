// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

// Serial because seeding pre-upgrade objects temporarily changes webhook configuration.
var _ = Describe("Replication settings migration", Serial,
	Label("config", "replications", "replication-migration"), func() {
		It("migrates stored legacy resources on update without overriding explicit policies or crossing tenants", func() {
			ctx := context.Background()
			const seedLabel = "e2e.projectcapsule.dev/legacy-replication"
			const hookName = "replications.mutating.projectcapsule.dev"
			original := &capsulev1beta2.CapsuleConfiguration{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, original)).To(Succeed())
			Expect(original.Spec.Admission.Mutating).NotTo(BeNil())
			DeferCleanup(func() {
				ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
					configuration.Spec.Admission.Mutating = original.Spec.Admission.Mutating.DeepCopy()
				})
			})
			By("excluding only labeled migration fixtures from policy conversion while seeding old objects")
			ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
				found := false
				for _, hook := range configuration.Spec.Admission.Mutating.Webhooks {
					if hook.Name != hookName {
						continue
					}
					found = true
					if hook.ObjectSelector == nil {
						hook.ObjectSelector = &metav1.LabelSelector{}
					}
					hook.ObjectSelector.MatchExpressions = append(hook.ObjectSelector.MatchExpressions, metav1.LabelSelectorRequirement{Key: seedLabel, Operator: metav1.LabelSelectorOpDoesNotExist})
				}
				Expect(found).To(BeTrue(), "replication conversion webhook must be installed")
			})
			Eventually(func(g Gomega) {
				webhook := &admissionregistrationv1.MutatingWebhookConfiguration{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: string(original.Spec.Admission.Mutating.Name)}, webhook)).To(Succeed())
				found := false
				for _, hook := range webhook.Webhooks {
					if hook.Name == hookName {
						found = true
						g.Expect(hook.ObjectSelector).NotTo(BeNil())
						g.Expect(hook.ObjectSelector.MatchExpressions).To(ContainElement(metav1.LabelSelectorRequirement{Key: seedLabel, Operator: metav1.LabelSelectorOpDoesNotExist}))
					}
				}
				g.Expect(found).To(BeTrue())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			owner := rbac.UserSpec{Name: "e2e-migration-owner", Kind: rbac.OwnerKind("User")}
			for _, name := range []string{"e2e-migration-selected", "e2e-migration-excluded"} {
				tenant := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e", "e2e.projectcapsule.dev/migration": name}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{UserSpec: owner}}}}
				Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
				DeferCleanup(func() { EventuallyDeletion(tenant) })
				TenantReady(tenant, metav1.ConditionTrue, defaultTimeoutInterval)
				namespace := NewNamespace(name, map[string]string{meta.TenantLabel: name, "e2e.projectcapsule.dev/migration-target": "true"})
				NamespaceCreation(namespace, owner, defaultTimeoutInterval).Should(Succeed())
				DeferCleanup(func() { ForceDeleteNamespace(ctx, name) })
				NamespaceIsPartOfTenant(tenant, namespace).Should(Succeed())
			}
			const target = "e2e-migration-selected"
			const excluded = "e2e-migration-excluded"
			ensureServiceAccount(target, "default")
			bindServiceAccountToTenantResourceManager(target, "default", target)

			for _, global := range []bool{false, true} {
				for _, adopt := range []bool{false, true} {
					name := fmt.Sprintf("migration-global-%t-adopt-%t", global, adopt)
					explicit := &apiruntime.ResourceTemplatePolicy{Creation: apiruntime.ResourceCreationPolicyOwner, Force: false, Protect: new(false), Deletion: apiruntime.ResourceDeletionPolicyRemove}
					block := func(suffix string, policy *apiruntime.ResourceTemplatePolicy) capsulev1beta2.ResourceSpec {
						return capsulev1beta2.ResourceSpec{
							Policy:            policy,
							NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"e2e.projectcapsule.dev/migration-target": "true"}},
							RawItems:          []capsulev1beta2.RawExtension{{Object: &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: name + suffix, Data: map[string]string{"mode": "replicated"}}}},
						}
					}
					common := capsulev1beta2.TenantResourceCommonSpec{
						Cordoned: new(true), ResyncPeriod: resyncPeriod,
						Settings:        capsulev1beta2.TenantResourceCommonSpecSettings{Adopt: new(adopt), Force: new(adopt)},
						PruningOnDelete: new(!adopt),
						Resources:       []capsulev1beta2.ResourceSpec{block("-existing", nil), block("-explicit", explicit), block("-created", nil)},
					}
					var parent client.Object
					var spec *capsulev1beta2.TenantResourceCommonSpec
					var status *capsulev1beta2.TenantResourceCommonStatus
					if global {
						resource := &capsulev1beta2.GlobalTenantResource{Name: name, Labels: map[string]string{seedLabel: "true"}, Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: api.ResourceScopeNamespace, TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"e2e.projectcapsule.dev/migration": target}}, TenantResourceCommonSpec: common}}
						parent, spec, status = resource, &resource.Spec.TenantResourceCommonSpec, &resource.Status.TenantResourceCommonStatus
					} else {
						resource := &capsulev1beta2.TenantResource{Name: name, Namespace: target, Labels: map[string]string{seedLabel: "true"}, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: common}}
						parent, spec, status = resource, &resource.Spec.TenantResourceCommonSpec, &resource.Status.TenantResourceCommonStatus
					}
					key := client.ObjectKeyFromObject(parent)
					By("persisting " + name + " with legacy settings and missing policies")
					Eventually(func(g Gomega) {
						probe := parent.DeepCopyObject().(client.Object)
						g.Expect(k8sClient.Create(ctx, probe, client.DryRunAll)).To(Succeed())
						if resource, ok := probe.(*capsulev1beta2.GlobalTenantResource); ok {
							g.Expect(resource.Spec.Resources[0].Policy).To(BeNil())
						} else {
							g.Expect(probe.(*capsulev1beta2.TenantResource).Spec.Resources[0].Policy).To(BeNil())
						}
					}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
					Expect(k8sClient.Create(ctx, parent)).To(Succeed())
					DeferCleanup(func() {
						Eventually(func() error {
							if err := k8sClient.Get(ctx, key, parent); err != nil {
								return client.IgnoreNotFound(err)
							}
							spec.Cordoned = new(false)
							spec.Resources = []capsulev1beta2.ResourceSpec{}
							if err := k8sClient.Update(ctx, parent); err != nil {
								return err
							}
							return client.IgnoreNotFound(k8sClient.Delete(ctx, parent))
						}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
					})
					Expect(k8sClient.Get(ctx, key, parent)).To(Succeed())
					Expect(spec.Resources[0].Policy).To(BeNil())
					Expect(spec.Resources[2].Policy).To(BeNil())
					seed := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: name + "-existing", Namespace: target, Data: map[string]string{"mode": "external", "outside": "retained"}}
					Expect(k8sClient.Patch(ctx, seed, client.Apply, client.FieldOwner("e2e-migration-external"))).To(Succeed())
					By("migrating " + name + " through UPDATE admission")
					Eventually(func() error {
						if err := k8sClient.Get(ctx, key, parent); err != nil {
							return err
						}
						labels := parent.GetLabels()
						delete(labels, seedLabel)
						parent.SetLabels(labels)
						spec.Cordoned = new(false)
						return k8sClient.Update(ctx, parent)
					}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
					Expect(k8sClient.Get(ctx, key, parent)).To(Succeed())
					want := &apiruntime.ResourceTemplatePolicy{Creation: apiruntime.ResourceCreationPolicyOwner, Force: adopt, Protect: new(true), Deletion: apiruntime.ResourceDeletionPolicyRemove}
					if adopt {
						want.Creation, want.Deletion = apiruntime.ResourceCreationPolicyMerge, apiruntime.ResourceDeletionPolicyOrphan
					}
					Expect(spec.Resources[0].Policy).To(Equal(want))
					Expect(spec.Resources[2].Policy).To(Equal(want))
					Expect(spec.Resources[1].Policy).To(Equal(explicit))
					Expect(spec.Settings).To(Equal(common.Settings))
					Expect(spec.PruningOnDelete).To(Equal(common.PruningOnDelete))
					By("verifying migrated policy behavior and tenant isolation")
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, key, parent)).To(Succeed())
						g.Expect(status.ProcessedItems).To(HaveLen(3))
						for _, item := range status.ProcessedItems {
							g.Expect(item.Namespace).To(Equal(target))
							g.Expect(item.Tenant).To(Equal(target))
							if item.Name == name+"-existing" && !adopt {
								g.Expect(item.Status).To(Equal(metav1.ConditionFalse))
								g.Expect(item.Message).To(ContainSubstring("cannot be adopted"))
							} else {
								g.Expect(item.Status).To(Equal(metav1.ConditionTrue))
							}
						}
					}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
					mode := "external"
					if adopt {
						mode = "replicated"
					}
					expectConfigMapData(target, name+"-existing", map[string]string{"mode": mode, "outside": "retained"})
					expectConfigMapData(target, name+"-created", map[string]string{"mode": "replicated"})
					expectConfigMapAbsent(excluded, name+"-existing")
					expectConfigMapAbsent(excluded, name+"-created")
					before := spec.DeepCopy()
					Expect(k8sClient.Patch(ctx, parent, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"annotations":{"e2e-migration":"revisited"}}}`)))).To(Succeed())
					Expect(k8sClient.Get(ctx, key, parent)).To(Succeed())
					Expect(spec).To(Equal(before), "readmitting a migrated object must be idempotent")
					err := k8sClient.Patch(ctx, parent, client.RawPatch(types.JSONPatchType, []byte(`[{"op":"replace","path":"/spec/resources/0/policy/creation","value":"Invalid"}]`)), client.DryRunAll)
					Expect(apierrors.IsInvalid(err)).To(BeTrue(), fmt.Sprint(err))
					Expect(k8sClient.Delete(ctx, parent)).To(Succeed())
					Eventually(func() bool { return apierrors.IsNotFound(k8sClient.Get(ctx, key, parent)) }, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
					expectConfigMapAbsent(target, name+"-explicit")
					if adopt {
						expectReplicationPolicyOrphan(target, name+"-existing")
						expectReplicationPolicyOrphan(target, name+"-created")
					} else {
						expectConfigMapAbsent(target, name+"-created")
					}
				}
			}
		})
	})
