// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	registrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

var _ = Describe("Replication trigger review regressions", Label("replications", "replication-triggers", "trigger-review"), func() {
	It("does not feed controller bookkeeping back into a self-subscription", func() {
		ctx := context.Background()
		prefix := "trigger-self-" + rand.String(6)
		owner := rbac.UserSpec{Kind: rbac.UserOwner, Name: prefix + "-owner"}
		tnt := &capsulev1beta2.Tenant{Name: prefix, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{UserSpec: owner}}}}
		Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
		DeferCleanup(DeleteInBackground, tnt)
		TenantReadyTrue(tnt)
		ns := NewNamespace(prefix, map[string]string{meta.NewTenantLabel: tnt.Name})
		NamespaceCreation(ns, owner, defaultTimeoutInterval).Should(Succeed())
		DeferCleanup(DeleteNamespaceInBackground, ctx, ns.Name)
		TenantNamespaceReady(tnt, ns, 1)
		ensureServiceAccount(ns.Name, "default")
		bindServiceAccountToTenantResourceManager(ns.Name, "default", ns.Name)
		label := "projectcapsule.dev/trigger-test"
		trigger := capsulev1beta2.TenantResourceTrigger{Name: "self", Operations: []registrationv1.OperationType{registrationv1.Update}, APIGroups: []string{capsulev1beta2.GroupVersion.Group}, APIVersions: []string{"v1beta2"}, Resources: []string{"tenantresources"}, ObjectSelector: &metav1.LabelSelector{MatchLabels: map[string]string{label: prefix}}}
		parent := &capsulev1beta2.TenantResource{Name: prefix, Namespace: ns.Name, Labels: map[string]string{label: prefix}, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, TriggerMinInterval: metav1.Duration{Duration: time.Second}, Triggers: []capsulev1beta2.TenantResourceTrigger{trigger}, Resources: []capsulev1beta2.ResourceSpec{{RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"self-output"},"data":{"value":"initial"}}`)}}}}}}}
		Expect(k8sClient.Create(ctx, parent)).To(Succeed())
		DeferCleanup(cleanupTenantResourcesWithDefaultServiceAccount, ctx, ns.Name)
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
		Consistently(func() string { return read().Annotations[replication.TriggerAnnotation] }, 2*time.Second, 200*time.Millisecond).Should(BeEmpty())
		deadline := read().Status.NextResyncAt
		Expect(deadline).NotTo(BeNil())
		actor := impersonationClient(owner.Name, withDefaultGroups(nil))
		By("allowing a genuine user spec update to trigger reconciliation")
		Eventually(func() error {
			fresh := &capsulev1beta2.TenantResource{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
				return err
			}
			fresh.Spec.Resources[0].RawItems[0].Raw = []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"self-output"},"data":{"value":"changed"}}`)
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			cm := &corev1.ConfigMap{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "self-output", Namespace: ns.Name}, cm)).To(Succeed())
			g.Expect(cm.Data["value"]).To(Equal("changed"))
			fresh := read()
			g.Expect(fresh.Status.Trigger).NotTo(BeNil())
			g.Expect(fresh.Status.Trigger.ObservedRequest).To(Equal(fresh.Annotations[replication.TriggerAnnotation]))
			g.Expect(fresh.Status.Trigger.ObservedRequest).NotTo(BeEmpty())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(read().Status.NextResyncAt.Time.After(deadline.Time)).To(BeFalse(), "trigger must not postpone the outstanding periodic deadline")
		token := read().Status.Trigger.ObservedRequest
		Consistently(func() string { return read().Annotations[replication.TriggerAnnotation] }, 4*time.Second, 200*time.Millisecond).Should(Equal(token))
		By("keeping later user metadata changes eligible after an internal patch")
		generation := read().Generation
		Eventually(func() error {
			fresh := &capsulev1beta2.TenantResource{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
				return err
			}
			if fresh.Annotations == nil {
				fresh.Annotations = map[string]string{}
			}
			fresh.Annotations["projectcapsule.dev/trigger-test-user"] = "changed"
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			fresh := read()
			g.Expect(fresh.Generation).To(Equal(generation))
			g.Expect(fresh.Status.Trigger.ObservedRequest).NotTo(Equal(token))
			g.Expect(fresh.Status.Trigger.ObservedRequest).To(Equal(fresh.Annotations[replication.TriggerAnnotation]))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		token = read().Status.Trigger.ObservedRequest
		Consistently(func() string { return read().Annotations[replication.TriggerAnnotation] }, 4*time.Second, 200*time.Millisecond).Should(Equal(token))
	})

	It("keeps objects in tenants with management-like names eligible and isolated", func() {
		ctx := context.Background()
		prefix := "trigger-names-" + rand.String(6)
		var parents []*capsulev1beta2.TenantResource
		var owners []rbac.UserSpec
		namespaces := []*corev1.Namespace{}
		trigger := capsulev1beta2.TenantResourceTrigger{Name: "secrets", Operations: []registrationv1.OperationType{registrationv1.Create}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"secrets"}, ObjectSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/trigger-test": prefix}}}
		for _, name := range []string{"controller", "resources", "replications"} {
			owner := rbac.UserSpec{Kind: rbac.UserOwner, Name: prefix + "-" + name}
			owners = append(owners, owner)
			tnt := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{UserSpec: owner}}}}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(DeleteInBackground, tnt)
			TenantReadyTrue(tnt)
			ns := NewNamespace(prefix+"-"+name, map[string]string{meta.NewTenantLabel: name})
			namespaces = append(namespaces, ns)
			NamespaceCreation(ns, owner, defaultTimeoutInterval).Should(Succeed())
			DeferCleanup(DeleteNamespaceInBackground, ctx, ns.Name)
			TenantNamespaceReady(tnt, ns, 1)
			ensureServiceAccount(ns.Name, "default")
			bindServiceAccountToTenantResourceManager(ns.Name, "default", ns.Name)
			parent := &capsulev1beta2.TenantResource{Name: prefix, Namespace: ns.Name, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Triggers: []capsulev1beta2.TenantResourceTrigger{trigger}, Resources: []capsulev1beta2.ResourceSpec{{RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"name-output"}}`)}}}}}}}
			Expect(k8sClient.Create(ctx, parent)).To(Succeed())
			parents = append(parents, parent)
			DeferCleanup(cleanupTenantResourcesWithDefaultServiceAccount, ctx, ns.Name)
			Eventually(func() bool {
				fresh := &capsulev1beta2.TenantResource{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), fresh)).To(Succeed())
				return fresh.Status.ObservedGeneration == fresh.Generation && meta.IsStatusConditionTrue(fresh.Status.Conditions, meta.ReadyCondition)
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		}
		waitForReplicationTrigger(trigger)
		for i, ns := range namespaces {
			owner := ownerClient(owners[i])
			secret, err := owner.CoreV1().Secrets(ns.Name).Create(ctx, &corev1.Secret{Name: "source", Labels: map[string]string{"projectcapsule.dev/trigger-test": prefix}}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(secret.Labels[meta.ManagedByCapsuleLabel]).To(Equal([]string{"controller", "resources", "replications"}[i]))
			Eventually(func() bool {
				fresh := &capsulev1beta2.TenantResource{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parents[i]), fresh)).To(Succeed())
				return fresh.Status.Trigger != nil && fresh.Status.Trigger.ObservedRequest != ""
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
			for _, other := range parents[i+1:] {
				Consistently(func() *capsulev1beta2.TenantResourceTriggerStatus {
					fresh := &capsulev1beta2.TenantResource{}
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(other), fresh)).To(Succeed())
					return fresh.Status.Trigger
				}, time.Second, 200*time.Millisecond).Should(BeNil())
			}
		}
	})
})

func waitForReplicationTrigger(trigger capsulev1beta2.TenantResourceTrigger) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		list := &registrationv1.ValidatingWebhookConfigurationList{}
		g.Expect(k8sClient.List(context.Background(), list)).To(Succeed())
		found := false
		for _, config := range list.Items {
			for _, hook := range config.Webhooks {
				if hook.ClientConfig.Service != nil && hook.ClientConfig.Service.Path != nil && strings.HasPrefix(*hook.ClientConfig.Service.Path, replication.TriggerPath+replication.MatchID(&trigger)+"/") {
					g.Expect(hook.FailurePolicy).NotTo(BeNil())
					g.Expect(*hook.FailurePolicy).To(Equal(registrationv1.Ignore))
					found = true
				}
			}
		}
		g.Expect(found).To(BeTrue())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}
