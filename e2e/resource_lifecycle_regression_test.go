// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/resourcepermit"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
)

func lifecycleTenantPair(ctx context.Context) ([]string, []client.Client) {
	prefix := "e2e-lifecycle-" + rand.String(6)
	var namespaces []string
	var actors []client.Client
	for _, suffix := range []string{"-a", "-b"} {
		name := prefix + suffix
		owner := rbac.UserSpec{Name: name + "-owner", Kind: rbac.UserOwner}
		tenant := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e", "lifecycle-test": name}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{UserSpec: owner}}}}
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
		DeferCleanup(func() { EventuallyDeletion(tenant) })
		TenantReady(tenant, metav1.ConditionTrue, defaultTimeoutInterval)
		ns := NewNamespace(name, map[string]string{meta.TenantLabel: name})
		NamespaceCreation(ns, owner, defaultTimeoutInterval).Should(Succeed())
		DeferCleanup(func() { ForceDeleteNamespace(ctx, name) })
		TenantNamespaceReady(tenant, ns, 1)
		namespaces = append(namespaces, name)
		actors = append(actors, impersonationClient(owner.Name, withDefaultGroups(nil)))
	}
	return namespaces, actors
}

var _ = Describe("Resource lifecycle provenance", Label("replications", "lifecycle-regression"), func() {
	DescribeTable("preserves adoption through policy changes and leaves replacement targets untouched", func(global bool) {
		ctx := context.Background()
		namespaces, actors := lifecycleTenantPair(ctx)
		for i, ns := range namespaces {
			seed := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: "adopted", Namespace: ns, Data: map[string]string{"external": "retained"}}
			Expect(actors[i].Patch(ctx, seed, client.Apply, client.FieldOwner("e2e-external"))).To(Succeed())
		}
		common := capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: metav1.Duration{Duration: time.Second}}
		for _, name := range []string{"adopted", "created"} {
			common.Resources = append(common.Resources, capsulev1beta2.ResourceSpec{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespaces[0]}},
				Policy:            &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(false)},
				RawItems:          []capsulev1beta2.RawExtension{{Object: &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: name, Data: map[string]string{"managed": "capsule"}}}},
			})
		}
		var parent client.Object
		var spec *capsulev1beta2.TenantResourceCommonSpec
		var status *capsulev1beta2.TenantResourceCommonStatus
		if global {
			obj := &capsulev1beta2.GlobalTenantResource{Name: namespaces[0], Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: api.ResourceScopeNamespace, TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"lifecycle-test": namespaces[0]}}, TenantResourceCommonSpec: common}}
			parent, spec, status = obj, &obj.Spec.TenantResourceCommonSpec, &obj.Status.TenantResourceCommonStatus
		} else {
			ensureServiceAccount(namespaces[0], "default")
			bindServiceAccountToTenantResourceManager(namespaces[0], "default", namespaces[0])
			obj := &capsulev1beta2.TenantResource{Name: "lifecycle", Namespace: namespaces[0], Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: common}}
			parent, spec, status = obj, &obj.Spec.TenantResourceCommonSpec, &obj.Status.TenantResourceCommonStatus
		}
		Expect(k8sClient.Create(ctx, parent)).To(Succeed())
		DeferCleanup(func() { EventuallyDeletion(parent) })
		waitItems := func(skipped bool) {
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent)).To(Succeed())
				g.Expect(status.ProcessedItems).To(HaveLen(2))
				for _, item := range status.ProcessedItems {
					g.Expect(item.Status).To(Equal(metav1.ConditionTrue), item.Message)
					g.Expect(item.LastApply.IsZero()).To(BeFalse())
					if skipped {
						g.Expect(item.Message).To(Equal(ssa.ConditionNotMet))
					}
					if item.Name == "adopted" {
						g.Expect(item.Created).To(BeFalse())
					}
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		waitItems(false)
		By("changing creation policy while both conditions are false")
		Eventually(func() error {
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent); err != nil {
				return err
			}
			for i := range spec.Resources {
				spec.Resources[i].Policy.Condition = "false"
				spec.Resources[i].Policy.Creation = apiruntime.ResourceCreationPolicyOwner
			}
			return k8sClient.Update(ctx, parent)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitItems(true)
		By("replacing the created object while application is skipped")
		created := &corev1.ConfigMap{Name: "created", Namespace: namespaces[0]}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(created), created)).To(Succeed())
		oldUID := created.UID
		Expect(actors[0].Delete(ctx, created)).To(Succeed())
		replacement := &corev1.ConfigMap{Name: created.Name, Namespace: created.Namespace, Data: map[string]string{"external": "replacement"}}
		Expect(actors[0].Create(ctx, replacement)).To(Succeed())
		Expect(replacement.UID).NotTo(Equal(oldUID))
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent)).To(Succeed())
			for _, item := range status.ProcessedItems {
				g.Expect(item.Created).To(BeFalse())
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		By("resuming the adopted object without promoting it to a created object")
		Eventually(func() error {
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent); err != nil {
				return err
			}
			spec.Resources[0].Policy.Condition = "true"
			spec.Resources[0].RawItems = []capsulev1beta2.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"adopted"},"data":{"managed":"resumed"}}`)}}
			return k8sClient.Update(ctx, parent)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectConfigMapData(namespaces[0], "adopted", map[string]string{"external": "retained", "managed": "resumed"})
		waitItems(false)
		By("cleaning up managed fields while retaining both external objects and the other tenant")
		Expect(k8sClient.Delete(ctx, parent)).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent))
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		expectConfigMapData(namespaces[0], "adopted", map[string]string{"external": "retained"})
		expectConfigMapData(namespaces[0], "created", replacement.Data)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(replacement), replacement)).To(Succeed())
		Expect(replacement.Labels).NotTo(HaveKey(meta.CreatedByCapsuleLabel))
		Expect(actors[0].Patch(ctx, replacement, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"annotations":{"owner":"allowed"}}}`)))).To(Succeed())
		expectConfigMapData(namespaces[1], "adopted", map[string]string{"external": "retained"})
		expectConfigMapAbsent(namespaces[1], "created")
	}, Entry("TenantResource", false), Entry("GlobalTenantResource", true))
})

var _ = Describe("ResourcePermit marker authentication", Label("resource-permit", "lifecycle-regression", "protection-identity"), func() {
	It("authenticates creation and adoption against the permit snapshot and isolates tenants", func() {
		ctx := context.Background()
		namespaces, actors := lifecycleTenantPair(ctx)
		for i, ns := range namespaces {
			ordinary := &corev1.ConfigMap{Name: "ordinary", Namespace: ns, Data: map[string]string{"external": "retained"}}
			Expect(actors[i].Create(ctx, ordinary)).To(Succeed())
			for marker, value := range map[string]string{meta.ResourcePermitProtectionLabel: meta.ValueTrue, meta.ProtectedByCapsuleLabel: meta.ValueControllerResourcePermit} {
				forged := &corev1.ConfigMap{Name: "forged", Namespace: ns, Labels: map[string]string{marker: value}, Annotations: map[string]string{meta.ResourcePermitServiceAccountAnnotation: ns + "-owner"}}
				Expect(actors[i].Create(ctx, forged)).To(MatchError(ContainSubstring("protected by a ResourcePermit")))
				expectConfigMapAbsent(ns, "forged")
				patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"labels": forged.Labels, "annotations": forged.Annotations}})
				Expect(err).NotTo(HaveOccurred())
				Expect(actors[i].Patch(ctx, ordinary, client.RawPatch(types.MergePatchType, patch))).To(MatchError(ContainSubstring("protected by a ResourcePermit")))
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(ordinary), ordinary)).To(Succeed())
				Expect(ordinary.Labels).NotTo(HaveKey(marker))
			}
		}
		By("using a real execution ServiceAccount to create and adopt protected targets")
		runnerName := namespaces[0] + "-runner"
		grantResourcePermitServiceAccount(namespaces[0], runnerName, namespaces[0], []string{"get", "create", "patch", "update", "delete"})
		runner := impersonationClient(serviceAccountUsername(namespaces[0], runnerName), serviceAccountGroups(namespaces[0]))
		template := &capsulev1beta2.GlobalResourcePermitTemplate{Name: namespaces[0], Spec: capsulev1beta2.GlobalResourcePermitTemplateSpec{Impersonation: resourcePermitServiceAccountReference(namespaces[0], runnerName), Approvals: resourcepermit.ApprovalSpec{Auto: true}}}
		for _, name := range []string{"created", "ordinary"} {
			template.Spec.Resources = append(template.Spec.Resources, apiruntime.ResourceTemplate{Policy: apiruntime.ResourceTemplatePolicy{Creation: apiruntime.ResourceCreationPolicyMerge}, Targets: []runtime.RawExtension{{Object: &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: name, Data: map[string]string{"managed": "permit"}}}}})
		}
		Expect(k8sClient.Create(ctx, template)).To(Succeed())
		DeferCleanup(func() { EventuallyDeletion(template) })
		permit := &capsulev1beta2.ResourcePermit{Name: "permit", Namespace: namespaces[0], Spec: capsulev1beta2.ResourcePermitSpec{Template: globalResourcePermitTemplateReference(template.Name)}}
		Expect(actors[0].Create(ctx, permit)).To(Succeed())
		waitForResourcePermitPhase(ctx, permit, capsulev1beta2.ResourcePermitPhaseActive)
		for _, name := range []string{"created", "ordinary"} {
			cm := &corev1.ConfigMap{Name: name, Namespace: namespaces[0]}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
			Expect(cm.Labels).To(HaveKeyWithValue(meta.ResourcePermitProtectionLabel, meta.ValueTrue))
			patch := client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"annotations":{"execution":"allowed"}}}`))
			Expect(actors[0].Patch(ctx, cm, patch)).To(MatchError(ContainSubstring("protected by a ResourcePermit")))
			Expect(runner.Patch(ctx, cm, patch)).To(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
			Expect(cm.Annotations).To(HaveKeyWithValue("execution", "allowed"))
		}
		By("measuring protected writes and denials with two live tenants")
		for _, scenario := range []struct {
			name    string
			actor   client.Client
			allowed bool
		}{{"allow", runner, true}, {"deny", actors[0], false}} {
			durations := make([]time.Duration, 0, 30)
			for i := range 30 {
				cm := &corev1.ConfigMap{Name: "created", Namespace: namespaces[0]}
				patch := client.RawPatch(types.MergePatchType, []byte(fmt.Sprintf(`{"metadata":{"annotations":{"latency-sample":"%d"}}}`, i)))
				start := time.Now()
				err := scenario.actor.Patch(ctx, cm, patch)
				durations = append(durations, time.Since(start))
				if scenario.allowed {
					Expect(err).NotTo(HaveOccurred())
				} else {
					Expect(err).To(MatchError(ContainSubstring("protected by a ResourcePermit")))
				}
			}
			slices.Sort(durations)
			fmt.Fprintf(GinkgoWriter, "ResourcePermit admission workload: tenants=2 case=%s requests=%d p50=%s p95=%s max=%s\n", scenario.name, len(durations), durations[15], durations[28], durations[29])
		}

		By("rejecting forged permit field ownership outside the frozen target set")
		foreign := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: "ordinary", Namespace: namespaces[1], Labels: map[string]string{meta.ResourcePermitProtectionLabel: meta.ValueTrue}, Annotations: map[string]string{meta.ResourcePermitServiceAccountAnnotation: serviceAccountUsername(namespaces[0], runnerName)}}
		// Grant only the fixture's cross-tenant write so the rejection tests Capsule admission, not RBAC.
		bindServiceAccountToNamespacedResource(namespaces[0], runnerName, namespaces[1], []string{"configmaps"}, []string{"get", "patch"})
		Expect(runner.Patch(ctx, foreign, client.Apply, client.FieldOwner(meta.ResourcePermitFieldOwner(permit)))).To(MatchError(ContainSubstring("protected by a ResourcePermit")))
		expectConfigMapData(namespaces[1], "ordinary", map[string]string{"external": "retained"})
		By("expiring the permit and releasing adopted fields before losing their authorization proof")
		expireActiveResourcePermit(ctx, permit)
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(permit), permit))
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		expectConfigMapAbsent(namespaces[0], "created")
		expectConfigMapData(namespaces[0], "ordinary", map[string]string{"external": "retained"})
		cm := &corev1.ConfigMap{Name: "ordinary", Namespace: namespaces[0]}
		Expect(actors[0].Patch(ctx, cm, client.RawPatch(types.MergePatchType, []byte(`{"data":{"owner":"allowed"}}`)))).To(Succeed())
	})
})
