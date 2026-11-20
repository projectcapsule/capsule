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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

var _ = Describe("Replication health inventory consistency", Label("tenant", "replications", "healthchecks", "health-inventory"), func() {
	DescribeTable("reapplies inventory that no longer matches its health checkpoint", func(global bool) {
		ctx := context.Background()
		prefix := "health-checkpoint-" + rand.String(5)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		ns := namespaces[0].Name
		spec := capsulev1beta2.TenantResourceCommonSpec{
			ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour},
			HealthChecks: &capsulev1beta2.TenantResourceHealthChecks{Wait: true, Expressions: []capsulev1beta2.TenantResourceHealthCheckExpression{{APIVersion: "v1", Kind: "ConfigMap", Current: "false"}}},
			Resources:    []capsulev1beta2.ResourceSpec{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": ns}}, Policy: &apiruntime.ResourceReplicationPolicy{Protect: new(false)}, RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q}}`, prefix))}}}},
		}
		parent := createHealthReviewReplication(global, prefix, ns, spec, actor)
		expectReplicationHealth(parent, metav1.ConditionUnknown, "HealthChecking")
		var previousRequest metav1.MicroTime
		// Reproduce the status left by a late, apply-only namespace publication:
		// keep the checkpoint, but replace its pinned inventory with an unchecked item.
		Eventually(func() error {
			fresh, status := replicationHealthObject(parent)
			previousRequest = status.Health.RequestedAt
			status.ProcessedItems[0].Health = nil
			status.Health.Cursor = 0
			status.Health.NextCheckAt = metav1.Now()
			return k8sClient.Status().Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Consistently(func(g Gomega) {
			_, status := replicationHealthObject(parent)
			g.Expect(meta.IsStatusConditionTrue(status.Conditions, meta.HealthyCondition)).To(BeFalse(), "an unchecked target cannot complete health")
			g.Expect(meta.IsStatusConditionTrue(status.Conditions, meta.ReadyCondition)).To(BeFalse())
		}, 6*time.Second, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			fresh, status := replicationHealthObject(parent)
			g.Expect(status.ProcessedItems).To(HaveLen(1))
			g.Expect(status.ProcessedItems[0].Health).NotTo(BeNil())
			g.Expect(status.Health.Applying).To(BeFalse())
			g.Expect(status.Health.RequestedAt).NotTo(Equal(previousRequest), "a full apply must run without a manual request or periodic resync")
			g.Expect(status.Health.ObservedGeneration).To(Equal(fresh.GetGeneration()))
			g.Expect(status.Conditions.GetConditionByType(meta.HealthyCondition).Status).To(Equal(metav1.ConditionUnknown))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
		Expect(apierrors.IsForbidden(actor.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
	}, Entry("TenantResource", false), Entry("GlobalTenantResource", true))

	It("pins a cluster target composed across logical namespace scopes to its final revision", func() {
		ctx := context.Background()
		prefix := "health-cluster-" + rand.String(5)
		_, namespaces := watchNamespaces(prefix)
		ns := namespaces[0].Name
		group := prefix + ".projectcapsule.dev"
		crd := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinition", "metadata": map[string]any{"name": "healthtargets." + group, "labels": map[string]any{"env": "e2e"}},
			"spec": map[string]any{"group": group, "scope": "Cluster", "names": map[string]any{"plural": "healthtargets", "singular": "healthtarget", "kind": "HealthTarget"}, "versions": []any{map[string]any{"name": "v1", "served": true, "storage": true, "schema": map[string]any{"openAPIV3Schema": map[string]any{"type": "object", "properties": map[string]any{"spec": map[string]any{"type": "object", "properties": map[string]any{"first": map[string]any{"type": "boolean"}, "second": map[string]any{"type": "boolean"}}}}}}}}},
		}}
		Expect(k8sClient.Create(ctx, crd)).To(Succeed())
		DeferCleanup(DeleteInBackground, crd)
		target := &unstructured.Unstructured{Object: map[string]any{"apiVersion": group + "/v1", "kind": "HealthTarget", "metadata": map[string]any{"name": prefix}, "spec": map[string]any{"first": false, "second": false}}}
		Eventually(func() error { return k8sClient.Create(ctx, target) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		DeferCleanup(DeleteInBackground, target)
		initialGeneration := target.GetGeneration()
		role := &rbacv1.ClusterRole{Name: prefix, Rules: []rbacv1.PolicyRule{{APIGroups: []string{group}, Resources: []string{"healthtargets"}, ResourceNames: []string{prefix}, Verbs: []string{"get", "patch", "update", "delete"}}}}
		binding := &rbacv1.ClusterRoleBinding{Name: prefix, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: prefix}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: "default", Namespace: ns}}}
		Expect(k8sClient.Create(ctx, role)).To(Succeed())
		DeferCleanup(DeleteInBackground, role)
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())
		DeferCleanup(DeleteInBackground, binding)
		sa := impersonationClient(serviceAccountUsername(ns, "default"), serviceAccountGroups(ns))
		Eventually(func() error { return sa.Get(ctx, client.ObjectKeyFromObject(target), target) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		otherSA := impersonationClient(serviceAccountUsername(namespaces[2].Name, "default"), serviceAccountGroups(namespaces[2].Name))
		Expect(apierrors.IsForbidden(otherSA.Get(ctx, client.ObjectKeyFromObject(target), target.DeepCopy()))).To(BeTrue())
		spec := capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, HealthChecks: &capsulev1beta2.TenantResourceHealthChecks{Wait: true, Expressions: []capsulev1beta2.TenantResourceHealthCheckExpression{{APIVersion: group + "/v1", Kind: "HealthTarget", Current: "spec.first && spec.second"}}}}
		for i, field := range []string{"first", "second"} {
			spec.Resources = append(spec.Resources, capsulev1beta2.ResourceSpec{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespaces[i].Name}}, Policy: &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyMerge, Force: true, Protect: new(false)}, RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":%q,"kind":"HealthTarget","metadata":{"name":%q},"spec":{%q:true}}`, group+"/v1", prefix, field))}}})
		}
		parent := createHealthReviewReplication(true, prefix, ns, spec, k8sClient)
		// Pruning needs this fixture's cluster RBAC and CRD until the source
		// finalizer completes; background deletion would race their cleanup.
		DeferCleanup(EventuallyDeletion, parent)
		status := expectReplicationHealth(parent, metav1.ConditionTrue, "Succeeded")
		Expect(sa.Get(ctx, client.ObjectKeyFromObject(target), target)).To(Succeed())
		Expect(target.GetGeneration()).To(BeNumerically(">=", initialGeneration+2))
		Expect(target.GetNamespace()).To(BeEmpty())
		Expect(status.ProcessedItems).To(HaveLen(2))
		Expect(status.ProcessedItems[0].Namespace).NotTo(Equal(status.ProcessedItems[1].Namespace))
		for _, item := range status.ProcessedItems {
			Expect(item.ClusterScoped).To(BeTrue())
			Expect(item.Tenant).To(Equal(prefix + "-0"))
			Expect(item.Health.UID).To(Equal(target.GetUID()))
			Expect(item.Health.Generation).To(Equal(target.GetGeneration()))
			Expect(item.Health.Digest).To(Equal(status.ProcessedItems[0].Health.Digest))
		}
		Expect(apierrors.IsForbidden(otherSA.Get(ctx, client.ObjectKeyFromObject(target), target.DeepCopy()))).To(BeTrue())
	})
})
