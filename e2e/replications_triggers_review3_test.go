// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	registrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

func triggerReviewNamespaces(prefix string) ([]rbac.UserSpec, []*corev1.Namespace) {
	GinkgoHelper()
	ctx := context.Background()
	owners := []rbac.UserSpec{{Kind: rbac.UserOwner, Name: prefix + "-alice"}, {Kind: rbac.UserOwner, Name: prefix + "-bob"}}
	namespaces := make([]*corev1.Namespace, 2)
	for i, owner := range owners {
		tenant := &capsulev1beta2.Tenant{Name: fmt.Sprintf("%s-%d", prefix, i), Labels: map[string]string{"env": "e2e", "projectcapsule.dev/trigger-review": prefix}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{UserSpec: owner}}}}
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
		DeferCleanup(DeleteInBackground, tenant)
		TenantReadyTrue(tenant)
		ns := NewNamespace(tenant.Name, map[string]string{meta.NewTenantLabel: tenant.Name, "projectcapsule.dev/trigger-review": prefix})
		NamespaceCreation(ns, owner, defaultTimeoutInterval).Should(Succeed())
		DeferCleanup(DeleteNamespaceInBackground, ctx, ns.Name)
		TenantNamespaceReady(tenant, ns, 1)
		ensureServiceAccount(ns.Name, "default")
		bindServiceAccountToTenantResourceManager(ns.Name, "default", ns.Name)
		DeferCleanup(cleanupTenantResourcesWithDefaultServiceAccount, ctx, ns.Name)
		namespaces[i] = ns
	}
	return owners, namespaces
}

func triggerHook(trigger capsulev1beta2.TenantResourceTrigger) *registrationv1.ValidatingWebhook {
	GinkgoHelper()
	list := &registrationv1.ValidatingWebhookConfigurationList{}
	Expect(k8sClient.List(context.Background(), list)).To(Succeed())
	prefix := replication.TriggerPath + replication.MatchID(&trigger) + "/"
	for _, config := range list.Items {
		for _, hook := range config.Webhooks {
			if hook.ClientConfig.Service != nil && hook.ClientConfig.Service.Path != nil && strings.HasPrefix(*hook.ClientConfig.Service.Path, prefix) {
				return hook.DeepCopy()
			}
		}
	}
	return nil
}

var _ = Describe("Replication trigger subresources and lifecycle", Label("replications", "replication-triggers", "trigger-review3"), func() {
	It("ignores a managed Deployment scale projection while accepting an unmanaged scale in the source tenant", func() {
		ctx := context.Background()
		prefix := "trigger-scale-" + rand.String(6)
		owners, namespaces := triggerReviewNamespaces(prefix)
		ns := namespaces[0].Name
		role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: prefix, Namespace: ns}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}}}}
		Expect(k8sClient.Create(ctx, role)).To(Succeed())
		Expect(k8sClient.Create(ctx, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: prefix, Namespace: ns}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: prefix}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: "default", Namespace: ns}}})).To(Succeed())
		trigger := capsulev1beta2.TenantResourceTrigger{Name: "scale", Operations: []registrationv1.OperationType{registrationv1.Update}, APIGroups: []string{"apps"}, APIVersions: []string{"v1"}, Resources: []string{"deployments/scale"}}
		raw := fmt.Sprintf(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"managed"},"spec":{"replicas":0,"selector":{"matchLabels":{"app":"%s-managed"}},"template":{"metadata":{"labels":{"app":"%s-managed"}},"spec":{"containers":[{"name":"workload","image":"registry.k8s.io/pause:3.10"}]}}}}`, prefix, prefix)
		parent := &capsulev1beta2.TenantResource{Name: prefix, Namespace: ns, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{trigger}, TriggerMinInterval: metav1.Duration{Duration: time.Second}, ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Resources: []capsulev1beta2.ResourceSpec{{RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(raw)}}}}}}}
		Expect(k8sClient.Create(ctx, parent)).To(Succeed())
		read := func() *capsulev1beta2.TenantResource {
			fresh := &capsulev1beta2.TenantResource{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), fresh)).To(Succeed())
			return fresh
		}
		Eventually(func() bool {
			fresh := read()
			return fresh.Status.ObservedGeneration == fresh.Generation && meta.IsStatusConditionTrue(fresh.Status.Conditions, meta.ReadyCondition)
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		waitForReplicationTrigger(trigger)
		owner := ownerClient(owners[0])
		// Use the administrator client for the managed target; source eligibility,
		// rather than the separate managed-object authorization policy, is under test.
		scale := func(actor rbac.UserSpec, namespace, name string) {
			actorClient := ownerClient(actor)
			Eventually(func() error {
				fresh, err := actorClient.AppsV1().Deployments(namespace).GetScale(ctx, name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				fresh.Spec.Replicas = 1
				_, err = actorClient.AppsV1().Deployments(namespace).UpdateScale(ctx, name, fresh, metav1.UpdateOptions{})
				return err
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		managed := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "managed"}, managed)).To(Succeed())
		Expect(managed.Labels[meta.NewManagedByCapsuleLabel]).To(Equal(meta.ValueControllerReplications))
		// An admin uses the suite's real credentials, without assuming its username.
		managedScale := func(replicas int32) {
			Eventually(func() error {
				fresh := &appsv1.Deployment{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: ns, Name: "managed"}, fresh); err != nil {
					return err
				}
				return k8sClient.SubResource("scale").Update(ctx, fresh, client.WithSubResourceBody(&autoscalingv1.Scale{Name: fresh.Name, Namespace: ns, ResourceVersion: fresh.ResourceVersion, Spec: autoscalingv1.ScaleSpec{Replicas: replicas}}))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		observeTriggerScaleLatency("managed", 40, func() { managedScale(1) })
		Consistently(func() string { return read().Annotations[replication.TriggerAnnotation] }, 3*time.Second, 200*time.Millisecond).Should(BeEmpty())
		// Restore the declared target before testing successful replication.
		managedScale(0)
		for _, sourceNs := range namespaces {
			deployment := &appsv1.Deployment{Name: "unmanaged", Namespace: sourceNs.Name, Spec: appsv1.DeploymentSpec{Replicas: new(int32(0)), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": prefix}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": prefix}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "workload", Image: "registry.k8s.io/pause:3.10"}}}}}}
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
		}
		scale(owners[1], namespaces[1].Name, "unmanaged")
		Consistently(func() string { return read().Annotations[replication.TriggerAnnotation] }, 3*time.Second, 200*time.Millisecond).Should(BeEmpty())
		_, err := owner.AppsV1().Deployments(namespaces[1].Name).GetScale(ctx, "unmanaged", metav1.GetOptions{})
		Expect(apierrors.IsForbidden(err)).To(BeTrue())
		observeTriggerScaleLatency("unmanaged", 40, func() { scale(owners[0], ns, "unmanaged") })
		Eventually(func(g Gomega) {
			fresh := read()
			g.Expect(fresh.Status.Trigger).NotTo(BeNil())
			g.Expect(fresh.Status.Trigger.ObservedRequest).NotTo(BeEmpty())
			g.Expect(fresh.Status.Trigger.ObservedRequest).To(Equal(fresh.Annotations[replication.TriggerAnnotation]))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	for _, global := range []bool{false, true} {
		It(fmt.Sprintf("retires deleting %s subscriptions before finalizers finish", map[bool]string{false: "local", true: "global"}[global]), func() {
			ctx := context.Background()
			prefix := "trigger-delete-" + rand.String(6)
			owners, namespaces := triggerReviewNamespaces(prefix)
			trigger := capsulev1beta2.TenantResourceTrigger{Name: "source", Operations: []registrationv1.OperationType{registrationv1.Create}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"secrets"}, MatchConditions: []registrationv1.MatchCondition{{Name: "run", Expression: fmt.Sprintf("request.name.startsWith('%s')", prefix)}}}
			if global {
				trigger.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/trigger-review": prefix}}
			}
			hold := "projectcapsule.dev/trigger-test-hold"
			var sources []client.Object
			for i, ns := range namespaces {
				spec := capsulev1beta2.TenantResourceCommonSpec{Resources: []capsulev1beta2.ResourceSpec{}, Triggers: []capsulev1beta2.TenantResourceTrigger{trigger}, ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}}
				var source client.Object = &capsulev1beta2.TenantResource{Name: prefix, Namespace: ns.Name, Finalizers: []string{hold}, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: spec}}
				if global {
					source = &capsulev1beta2.GlobalTenantResource{Name: fmt.Sprintf("%s-%d", prefix, i), Finalizers: []string{hold}, Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: api.ResourceScopeNamespace, TenantResourceCommonSpec: spec, TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/trigger-review": prefix}}}}
				}
				Expect(k8sClient.Create(ctx, source)).To(Succeed())
				sources = append(sources, source)
				DeferCleanup(func() {
					Eventually(func() error {
						fresh := source.DeepCopyObject().(client.Object)
						if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(source), fresh); err != nil {
							return client.IgnoreNotFound(err)
						}
						fresh.SetFinalizers(slices.DeleteFunc(fresh.GetFinalizers(), func(v string) bool { return v == hold }))
						return k8sClient.Update(ctx, fresh)
					}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
					DeleteInBackground(source)
				})
			}
			waitForReplicationTrigger(trigger)
			if !global {
				actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
				Expect(apierrors.IsForbidden(actor.Delete(ctx, sources[1].DeepCopyObject().(client.Object)))).To(BeTrue())
			}
			for i, source := range sources {
				current := source.DeepCopyObject().(client.Object)
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(source), current)).To(Succeed())
				before := current.GetGeneration()
				Expect(k8sClient.Delete(ctx, current)).To(Succeed())
				Eventually(func(g Gomega) {
					fresh := source.DeepCopyObject().(client.Object)
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(source), fresh)).To(Succeed())
					g.Expect(fresh.GetDeletionTimestamp().IsZero()).To(BeFalse())
					g.Expect(fresh.GetGeneration()).To(BeNumerically(">=", before))
					hook := triggerHook(trigger)
					if i == 1 {
						g.Expect(hook).To(BeNil())
						return
					}
					g.Expect(hook).NotTo(BeNil())
					if !global {
						g.Expect(hook.NamespaceSelector.MatchExpressions[0].Values).To(ConsistOf(namespaces[1].Name))
					}
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})
	}

	It("revalidates maximum trigger sets independently across tenants", func() {
		ctx := context.Background()
		prefix := "trigger-cel-" + rand.String(6)
		owners, namespaces := triggerReviewNamespaces(prefix)
		var parents []*capsulev1beta2.TenantResource
		for i, ns := range namespaces {
			parent := &capsulev1beta2.TenantResource{Name: prefix, Namespace: ns.Name, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Resources: []capsulev1beta2.ResourceSpec{}, TriggerMinInterval: metav1.Duration{Duration: time.Second}, ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}}}}
			// Keep the second maximum set validated but unpublished, so this
			// parallel-safe scenario does not exhaust the shared webhook budget.
			if i == 1 {
				parent.Spec.Cordoned = new(true)
			}
			for group := range 32 {
				trigger := capsulev1beta2.TenantResourceTrigger{Name: fmt.Sprintf("g%d", group), Operations: []registrationv1.OperationType{registrationv1.Create}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}}
				for condition := range 60 {
					trigger.MatchConditions = append(trigger.MatchConditions, registrationv1.MatchCondition{Name: fmt.Sprintf("c%d", condition), Expression: fmt.Sprintf("request.name != '%d-%d-%d'", i, group, condition)})
				}
				parent.Spec.Triggers = append(parent.Spec.Triggers, trigger)
			}
			actor := impersonationClient(owners[i].Name, withDefaultGroups(nil))
			Expect(actor.Create(ctx, parent)).To(Succeed())
			parents = append(parents, parent)
		}
		for pass := range 2 {
			for i, parent := range parents {
				actor := impersonationClient(owners[i].Name, withDefaultGroups(nil))
				Eventually(func() error {
					fresh := &capsulev1beta2.TenantResource{}
					if err := actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
						return err
					}
					fresh.Spec.ResyncPeriod.Duration = time.Duration(23+pass) * time.Hour
					return actor.Update(ctx, fresh)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				Eventually(func() bool {
					fresh := &capsulev1beta2.TenantResource{}
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), fresh)).To(Succeed())
					return fresh.Status.ObservedGeneration == fresh.Generation && meta.IsStatusConditionTrue(fresh.Status.Conditions, meta.ReadyCondition)
				}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
			}
		}
		waitForReplicationTrigger(parents[0].Spec.Triggers[0])
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		Expect(actor.Create(ctx, &corev1.ConfigMap{Name: prefix, Namespace: namespaces[0].Name})).To(Succeed())
		Eventually(func() bool {
			fresh := &capsulev1beta2.TenantResource{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parents[0]), fresh)).To(Succeed())
			return fresh.Status.Trigger != nil && fresh.Status.Trigger.ObservedRequest != "" && fresh.Status.Trigger.ObservedRequest == fresh.Annotations[replication.TriggerAnnotation]
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		Consistently(func() string {
			fresh := &capsulev1beta2.TenantResource{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parents[1]), fresh)).To(Succeed())
			return fresh.Annotations[replication.TriggerAnnotation]
		}, 3*time.Second, 200*time.Millisecond).Should(BeEmpty())
		Eventually(func() error {
			fresh := &capsulev1beta2.TenantResource{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(parents[0]), fresh); err != nil {
				return err
			}
			generation := fresh.Generation
			fresh.Spec.Triggers[0].MatchConditions[0].Expression = "object["
			err := actor.Update(ctx, fresh)
			if err == nil {
				return StopTrying("invalid CEL was persisted")
			}
			if apierrors.IsConflict(err) {
				return err
			}
			Expect(err.Error()).To(ContainSubstring("match"))
			check := &capsulev1beta2.TenantResource{}
			Expect(actor.Get(ctx, client.ObjectKeyFromObject(fresh), check)).To(Succeed())
			Expect(check.Generation).To(Equal(generation))
			Expect(check.Spec.Triggers[0].MatchConditions[0].Expression).NotTo(Equal("object["))
			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})
})

func observeTriggerScaleLatency(label string, count int, operation func()) {
	GinkgoHelper()
	samples := make([]time.Duration, 0, count)
	for range count {
		start := time.Now()
		operation()
		samples = append(samples, time.Since(start))
	}
	slices.Sort(samples)
	fmt.Fprintf(GinkgoWriter, "scale workload %s: operations=%d p50=%s p95=%s (fresh client read + scale update)\n", label, count, samples[count/2], samples[count*95/100])
}
