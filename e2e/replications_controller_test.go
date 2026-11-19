// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

// Serial because this exercises a shared background-controller outage.
var _ = Describe("Dedicated replication controller", Serial, Label("config", "replications", "replication-controller"), func() {
	It("preserves admission and tenant profiles while replication workers are unavailable", func() {
		ctx := context.Background()
		prefix := "replication-controller-" + rand.String(6)
		deployments := &appsv1.DeploymentList{}
		Expect(k8sClient.List(ctx, deployments, client.InNamespace(ControllerNamespace))).To(Succeed())
		var dedicated *appsv1.Deployment
		for i := range deployments.Items {
			d := &deployments.Items[i]
			for _, container := range d.Spec.Template.Spec.Containers {
				if container.Name == "manager" && len(container.Args) > 0 && container.Args[0] == "replications" {
					dedicated = d.DeepCopy()
				}
			}
		}
		Expect(dedicated).NotTo(BeNil(), "the chart must deploy the replications subcommand separately")
		key := client.ObjectKeyFromObject(dedicated)
		originalReplicas := *dedicated.Spec.Replicas
		selector := dedicated.Spec.Selector.MatchLabels
		scale := func(replicas int32) {
			Eventually(func() error {
				fresh := &appsv1.Deployment{}
				if err := k8sClient.Get(ctx, key, fresh); err != nil {
					return err
				}
				fresh.Spec.Replicas = new(replicas)
				return k8sClient.Update(ctx, fresh)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		ready := func() {
			Eventually(func(g Gomega) {
				fresh := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, key, fresh)).To(Succeed())
				g.Expect(fresh.Status.ObservedGeneration).To(BeNumerically(">=", fresh.Generation))
				g.Expect(fresh.Status.ReadyReplicas).To(Equal(originalReplicas))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		DeferCleanup(func() { scale(originalReplicas); ready() })
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		source := &corev1.ConfigMap{Name: prefix, Namespace: namespaces[0].Name, Labels: map[string]string{watchSourceLabel: prefix, watchInputLabel: prefix}, Data: map[string]string{"value": "initial"}}
		Expect(actor.Create(ctx, source)).To(Succeed())
		local := &capsulev1beta2.TenantResource{Name: prefix, Namespace: source.Namespace, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: watchBody(prefix, source.Namespace, prefix+"-local")}}
		local.Spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": source.Namespace}}
		Expect(actor.Create(ctx, local)).To(Succeed())
		global := &capsulev1beta2.GlobalTenantResource{Name: prefix, Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: "Namespace", TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/watch-tenant": "0" + prefix}}, TenantResourceCommonSpec: watchBody(prefix, source.Namespace, prefix+"-global")}}
		global.Spec.Resources[0].NamespaceSelector = local.Spec.Resources[0].NamespaceSelector.DeepCopy()
		Expect(k8sClient.Create(ctx, global)).To(Succeed())
		DeferCleanup(DeleteInBackground, global)
		waitWatchReady(local)
		waitWatchReady(global)
		for _, output := range []string{"-local", "-global"} {
			expectConfigMapData(source.Namespace, prefix+output, map[string]string{"value": "initial"})
		}

		// Leadership and application readiness precede watch preparation. Require
		// a fresh, acknowledged event on both parents after every handoff.
		converge := func(value string) {
			beforeLocal, beforeGlobal := watchToken(local), watchToken(global)
			Eventually(func(g Gomega) {
				input := &corev1.ConfigMap{}
				g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(source), input)).To(Succeed())
				input.Data["value"] = value
				input.Data["marker"] = rand.String(8)
				g.Expect(actor.Update(ctx, input)).To(Succeed())
				for _, output := range []string{"-local", "-global"} {
					cm := &corev1.ConfigMap{}
					g.Expect(actor.Get(ctx, client.ObjectKey{Namespace: source.Namespace, Name: prefix + output}, cm)).To(Succeed())
					g.Expect(cm.Data["value"]).To(Equal(value))
				}
				l := &capsulev1beta2.TenantResource{}
				r := &capsulev1beta2.GlobalTenantResource{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(local), l)).To(Succeed())
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(global), r)).To(Succeed())
				for i, status := range []*capsulev1beta2.TenantResourceCommonStatus{&l.Status.TenantResourceCommonStatus, &r.Status.TenantResourceCommonStatus} {
					object := []client.Object{l, r}[i]
					token := object.GetAnnotations()[replication.TriggerAnnotation]
					g.Expect(token).NotTo(BeEmpty())
					g.Expect(token).NotTo(Equal([]string{beforeLocal, beforeGlobal}[i]))
					g.Expect(status.ObservedGeneration).To(Equal(object.GetGeneration()))
					g.Expect(status.Trigger).NotTo(BeNil())
					g.Expect(status.Trigger.ObservedRequest).To(Equal(token))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}

		converge("running")
		By("keeping admission and managed protection available with no replication pods")
		scale(0)
		Eventually(func() int {
			pods := &corev1.PodList{}
			Expect(k8sClient.List(ctx, pods, client.InNamespace(ControllerNamespace), client.MatchingLabels(selector))).To(Succeed())
			return len(pods.Items)
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeZero())
		changeWatchInput(actor, source, "during-outage")
		Eventually(func() error {
			output := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKey{Namespace: source.Namespace, Name: prefix + "-local"}, output); err != nil {
				return err
			}
			output.Data["value"] = "unauthorized"
			err := actor.Update(ctx, output)
			if err == nil {
				return StopTrying("managed output write succeeded")
			}
			if apierrors.IsConflict(err) {
				return err
			}
			Expect(apierrors.IsForbidden(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("is managed by a tenant capsule replication"))
			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectConfigMapData(source.Namespace, prefix+"-local", map[string]string{"value": "running"})
		pending := &capsulev1beta2.TenantResource{Name: prefix + "-pending", Namespace: source.Namespace, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: watchBody(prefix, source.Namespace, prefix+"-pending")}}
		pending.Spec.Resources[0].NamespaceSelector = local.Spec.Resources[0].NamespaceSelector.DeepCopy()
		Expect(actor.Create(ctx, pending)).To(Succeed())
		Consistently(func() int64 {
			fresh := &capsulev1beta2.TenantResource{}
			Expect(actor.Get(ctx, client.ObjectKeyFromObject(pending), fresh)).To(Succeed())
			return fresh.Status.ObservedGeneration
		}, time.Second, 200*time.Millisecond).Should(BeZero())
		Expect(apierrors.IsNotFound(actor.Get(ctx, client.ObjectKey{Namespace: source.Namespace, Name: prefix + "-pending"}, &corev1.ConfigMap{}))).To(BeTrue())
		Expect(apierrors.IsForbidden(actor.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: "private"}, &corev1.ConfigMap{}))).To(BeTrue())
		for _, ns := range namespaces[1:] {
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: prefix + "-global"}, &corev1.ConfigMap{}))).To(BeTrue())
		}
		By("reconciling both existing and newly admitted resources after recovery")
		scale(originalReplicas)
		ready()
		converge("during-outage")
		waitWatchReady(pending)
		expectConfigMapData(source.Namespace, prefix+"-pending", map[string]string{"value": "during-outage"})
		waitWatchReady(local)
		waitWatchReady(global)
	})
})
