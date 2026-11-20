// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

func createHealthReviewReplication(global bool, name, namespace string, spec capsulev1beta2.TenantResourceCommonSpec, actor client.Client) client.Object {
	GinkgoHelper()
	var parent client.Object
	writer := actor
	if global {
		parent = &capsulev1beta2.GlobalTenantResource{Name: name, Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: api.ResourceScopeNamespace, TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/watch-tenant": "0" + name}}, ServiceAccount: &meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: "default", Namespace: meta.RFC1123SubdomainName(namespace)}, TenantResourceCommonSpec: spec}}
		writer = k8sClient
	} else {
		parent = &capsulev1beta2.TenantResource{Name: name, Namespace: namespace, Spec: capsulev1beta2.TenantResourceSpec{ServiceAccount: &meta.LocalRFC1123ObjectReference{Name: "default"}, TenantResourceCommonSpec: spec}}
	}
	Expect(writer.Create(context.Background(), parent)).To(Succeed())
	DeferCleanup(DeleteInBackground, parent)
	DeferCleanup(func() {
		if !CurrentSpecReport().Failed() {
			return
		}
		fresh := parent.DeepCopyObject().(client.Object)
		if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(parent), fresh); err == nil {
			payload, _ := json.MarshalIndent(fresh, "", "  ")
			fmt.Fprintf(GinkgoWriter, "\nReplication failure snapshot:\n%s\n", payload)
		}
	})
	return parent
}

var _ = Describe("Replication health regression cases", Label("tenant", "replications", "healthchecks", "health-review"), func() {
	DescribeTable("checks the final generation after composing a shared Deployment", func(global bool) {
		ctx := context.Background()
		prefix := "shared-health-" + rand.String(5)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		ns := namespaces[0].Name
		Expect(k8sClient.Create(ctx, &rbacv1.Role{Name: prefix, Namespace: ns, Rules: []rbacv1.PolicyRule{{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: []string{"get", "create", "update", "patch", "delete"}}}})).To(Succeed())
		Expect(k8sClient.Create(ctx, &rbacv1.RoleBinding{Name: prefix, Namespace: ns, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: prefix}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: "default", Namespace: ns}}})).To(Succeed())
		target := &appsv1.Deployment{Name: prefix, Namespace: ns, Spec: appsv1.DeploymentSpec{Replicas: new(int32(0)), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": prefix}}, Template: corev1.PodTemplateSpec{Labels: map[string]string{"app": prefix}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.10"}}}}}}
		Expect(actor.Create(ctx, target)).To(Succeed())
		serviceAccount := impersonationClient(serviceAccountUsername(ns, "default"), serviceAccountGroups(ns))
		Eventually(func() error { return serviceAccount.Get(ctx, client.ObjectKeyFromObject(target), &appsv1.Deployment{}) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(apierrors.IsForbidden(serviceAccount.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix}, &appsv1.Deployment{}))).To(BeTrue())
		initialGeneration := target.Generation
		spec := capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, HealthChecks: &capsulev1beta2.TenantResourceHealthChecks{Wait: true}}
		for _, body := range []string{`"minReadySeconds":1`, `"template":{"metadata":{"annotations":{"projectcapsule.dev/shared-health":"desired"}}}`} {
			spec.Resources = append(spec.Resources, capsulev1beta2.ResourceSpec{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": ns}},
				Policy:            &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(false), Force: true},
				RawItems:          []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":%q},"spec":{%s}}`, prefix, body))}},
			})
		}
		parent := createHealthReviewReplication(global, prefix, ns, spec, actor)
		status := expectReplicationHealth(parent, metav1.ConditionTrue, "Succeeded")
		Expect(actor.Get(ctx, client.ObjectKeyFromObject(target), target)).To(Succeed())
		Expect(target.Generation).To(BeNumerically(">=", initialGeneration+2), "both origins must change disjoint spec fields")
		Expect(status.ProcessedItems).To(HaveLen(2))
		Expect(status.ProcessedItems[0].Origin).NotTo(Equal(status.ProcessedItems[1].Origin))
		for _, item := range status.ProcessedItems {
			Expect(item.Health).NotTo(BeNil())
			Expect(item.Health.UID).To(Equal(target.UID))
			Expect(item.Health.Generation).To(Equal(target.Generation))
			Expect(item.Health.Digest).To(Equal(status.ProcessedItems[0].Health.Digest))
		}
		Expect(target.Spec.MinReadySeconds).To(Equal(int32(1)))
		Expect(target.Spec.Template.Annotations).To(HaveKeyWithValue("projectcapsule.dev/shared-health", "desired"))
		for _, namespace := range namespaces[1:] {
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace.Name, Name: prefix}, &appsv1.Deployment{}))).To(BeTrue())
		}
		Expect(apierrors.IsForbidden(actor.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix}, &appsv1.Deployment{}))).To(BeTrue())
	}, Entry("TenantResource", false), Entry("GlobalTenantResource", true))

	DescribeTable("restarts an incomplete health scan after a skipped generator disappears", func(global bool) {
		ctx := context.Background()
		prefix := "scan-health-" + rand.String(5)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		ns := namespaces[0].Name
		input := &corev1.ConfigMap{Name: prefix, Namespace: ns, Data: map[string]string{"value": "include"}, Labels: map[string]string{watchInputLabel: prefix}}
		Expect(actor.Create(ctx, input)).To(Succeed())
		spec := watchBody(prefix, ns, prefix+"-skipped")
		spec.Triggers = nil
		spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": ns}}
		spec.Resources[0].Policy = &apiruntime.ResourceReplicationPolicy{Condition: "false", Protect: new(false)}
		spec.Resources[0].Generators[0].Template = fmt.Sprintf(`{{ if eq (index .inputs 0).data.value "include" }}
apiVersion: v1
kind: ConfigMap
metadata:
  name: %s-skipped
{{ end }}`, prefix)
		spec.Resources = append(spec.Resources, capsulev1beta2.ResourceSpec{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": ns}}, Policy: &apiruntime.ResourceReplicationPolicy{Protect: new(false)}, RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q}}`, prefix+"-pending"))}}})
		spec.HealthChecks = &capsulev1beta2.TenantResourceHealthChecks{Wait: true, Expressions: []capsulev1beta2.TenantResourceHealthCheckExpression{{APIVersion: "v1", Kind: "ConfigMap", Current: `has(metadata.annotations) && 'projectcapsule.dev/health-state' in metadata.annotations && metadata.annotations['projectcapsule.dev/health-state'] == 'healthy'`}}}
		parent := createHealthReviewReplication(global, prefix, ns, spec, actor)
		pending := expectReplicationHealth(parent, metav1.ConditionUnknown, "HealthChecking")
		Expect(pending.ProcessedItems).To(HaveLen(2))
		deadline, revision := pending.Health.Deadline, pending.Health.Revision
		// Seed a valid checkpoint immediately after the skipped entry. The next
		// application must invalidate that position when the generator disappears.
		Eventually(func() error {
			fresh, status := replicationHealthObject(parent)
			if status.ProcessedItems[0].Health != nil {
				status.ProcessedItems[0], status.ProcessedItems[1] = status.ProcessedItems[1], status.ProcessedItems[0]
			}
			status.Health.Cursor = 1
			status.Health.Pending, status.Health.Failed, status.Health.Message = false, false, ""
			status.Health.NextCheckAt = metav1.NewTime(time.Now().Add(10 * time.Second))
			return k8sClient.Status().Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func() error {
			fresh := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(input), fresh); err != nil {
				return err
			}
			fresh.Data["value"] = "exclude"
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func() error {
			fresh := parent.DeepCopyObject().(client.Object)
			writer := actor
			if global {
				writer = k8sClient
			}
			if err := writer.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
				return err
			}
			annotations := fresh.GetAnnotations()
			if annotations == nil {
				annotations = map[string]string{}
			}
			annotations[meta.ReconcileAnnotation] = prefix
			fresh.SetAnnotations(annotations)
			return writer.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			fresh, status := replicationHealthObject(parent)
			g.Expect(fresh.GetAnnotations()).NotTo(HaveKey(meta.ReconcileAnnotation))
			g.Expect(status.ProcessedItems).To(HaveLen(1))
			g.Expect(status.Health.Revision).To(Equal(revision))
			g.Expect(status.Health.Deadline).To(Equal(deadline))
			g.Expect(status.Health.Cursor).To(BeZero())
			g.Expect(meta.IsStatusConditionTrue(status.Conditions, meta.ReadyCondition)).To(BeFalse())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Consistently(func() bool {
			_, status := replicationHealthObject(parent)
			return meta.IsStatusConditionTrue(status.Conditions, meta.HealthyCondition)
		}, 12*time.Second, defaultPollInterval).Should(BeFalse())
		Expect(apierrors.IsNotFound(actor.Get(ctx, client.ObjectKey{Namespace: ns, Name: prefix + "-skipped"}, &corev1.ConfigMap{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix + "-pending"}, &corev1.ConfigMap{}))).To(BeTrue())
		Expect(apierrors.IsForbidden(actor.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
	}, Entry("TenantResource", false), Entry("GlobalTenantResource", true))
})

var _ = Describe("Apply-only namespace profiles", Label("tenant", "replications", "replication-profiles"), func() {
	DescribeTable("replicates and prunes on namespace label changes without a periodic resync", func(explicitlyDisabled bool) {
		ctx := context.Background()
		prefix := "apply-profile-" + rand.String(5)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		ns := namespaces[0].Name
		bindServiceAccountToTenantResourceManager(ns, "default", namespaces[1].Name)
		spec := capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Resources: []capsulev1beta2.ResourceSpec{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/apply-profile": prefix}}, Policy: &apiruntime.ResourceReplicationPolicy{Protect: new(false)}, RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q}}`, prefix))}}}}}
		if explicitlyDisabled {
			spec.HealthChecks = &capsulev1beta2.TenantResourceHealthChecks{Wait: false}
		}
		parent := createHealthReviewReplication(false, prefix, ns, spec, actor)
		waitWatchReady(parent)
		label := func(namespace string, selected bool) {
			Eventually(func() error {
				fresh := &corev1.Namespace{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: namespace}, fresh); err != nil {
					return err
				}
				if selected {
					fresh.Labels["projectcapsule.dev/apply-profile"] = prefix
				} else {
					delete(fresh.Labels, "projectcapsule.dev/apply-profile")
				}
				return k8sClient.Update(ctx, fresh)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		label(namespaces[2].Name, true)
		label(namespaces[1].Name, true)
		key := client.ObjectKey{Namespace: namespaces[1].Name, Name: prefix}
		Eventually(func() error { return actor.Get(ctx, key, &corev1.ConfigMap{}) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		for _, namespace := range []*corev1.Namespace{namespaces[0], namespaces[2]} {
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace.Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
		}
		label(namespaces[1].Name, false)
		Eventually(func() bool { return apierrors.IsNotFound(actor.Get(ctx, key, &corev1.ConfigMap{})) }, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		Eventually(func(g Gomega) {
			fresh, status := replicationHealthObject(parent)
			g.Expect(status.ObservedGeneration).To(Equal(fresh.GetGeneration()))
			g.Expect(status.ProcessedItems).To(BeEmpty())
			g.Expect(status.Health).To(BeNil())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(apierrors.IsForbidden(actor.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
	}, Entry("health omitted", false), Entry("health explicitly disabled", true))
})
