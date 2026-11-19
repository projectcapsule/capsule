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
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

// Serial because this exercises a shared background-controller outage.
var _ = Describe("Dedicated replication controller", Serial, Label("config", "replications", "replication-controller"), func() {
	It("keeps restarted workers live but unready until legacy replica templates are retired", Label("replication-handoff"), func() {
		ctx := context.Background()
		prefix := "legacy-handoff-" + rand.String(5)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		ns := namespaces[0].Name
		input := &corev1.ConfigMap{Name: prefix, Namespace: ns, Labels: map[string]string{watchInputLabel: prefix}, Data: map[string]string{"value": "initial"}}
		Expect(actor.Create(ctx, input)).To(Succeed())
		spec := watchBody(prefix, ns, prefix+"-output")
		spec.Triggers = nil
		spec.ResyncPeriod = metav1.Duration{Duration: time.Second}
		spec.Resources[0].Policy = &apiruntime.ResourceReplicationPolicy{Protect: new(false)}
		spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": ns}}
		parent := createHealthReviewReplication(false, prefix, ns, spec, actor)
		waitWatchReady(parent)
		outputKey := client.ObjectKey{Namespace: ns, Name: prefix + "-output"}
		Eventually(func(g Gomega) {
			output := &corev1.ConfigMap{}
			g.Expect(actor.Get(ctx, outputKey, output)).To(Succeed())
			g.Expect(output.Data["value"]).To(Equal("initial"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		deployments := &appsv1.DeploymentList{}
		Expect(k8sClient.List(ctx, deployments, client.InNamespace(ControllerNamespace))).To(Succeed())
		var worker *appsv1.Deployment
		for i := range deployments.Items {
			for _, container := range deployments.Items[i].Spec.Template.Spec.Containers {
				if container.Name == "manager" && len(container.Args) > 0 && container.Args[0] == "replications" {
					worker = deployments.Items[i].DeepCopy()
				}
			}
		}
		Expect(worker).NotTo(BeNil())
		originalReplicas := *worker.Spec.Replicas
		legacyLabels := map[string]string{"projectcapsule.dev/legacy-handoff": prefix, "app.kubernetes.io/instance": worker.Labels["app.kubernetes.io/instance"]}
		legacySpec := worker.Spec.Template.Spec.DeepCopy()
		for i := range legacySpec.Containers {
			if legacySpec.Containers[i].Name == "manager" {
				legacySpec.Containers[i].Args = legacySpec.Containers[i].Args[1:]
			}
		}
		legacy := &appsv1.ReplicaSet{Name: prefix, Namespace: ControllerNamespace, Labels: legacyLabels, Spec: appsv1.ReplicaSetSpec{Replicas: new(int32(1)), Selector: &metav1.LabelSelector{MatchLabels: legacyLabels}, Template: corev1.PodTemplateSpec{Labels: legacyLabels, Spec: *legacySpec}}}
		scale := func(obj client.Object, replicas int32) {
			Eventually(func() error {
				fresh := obj.DeepCopyObject().(client.Object)
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), fresh); err != nil {
					return err
				}
				switch typed := fresh.(type) {
				case *appsv1.Deployment:
					typed.Spec.Replicas = new(replicas)
				case *appsv1.ReplicaSet:
					typed.Spec.Replicas = new(replicas)
				}
				return k8sClient.Update(ctx, fresh)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		Expect(k8sClient.Create(ctx, legacy)).To(Succeed())
		retireLegacy := func() {
			scale(legacy, 0)
			pods := &corev1.PodList{}
			Expect(k8sClient.List(ctx, pods, client.InNamespace(ControllerNamespace), client.MatchingLabels(legacyLabels))).To(Succeed())
			for i := range pods.Items {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &pods.Items[i]))).To(Succeed())
			}
		}
		DeferCleanup(func() {
			retireLegacy()
			scale(worker, originalReplicas)
			DeleteInBackground(legacy)
			Eventually(func() int { return len(readyWatchControllerPods(ctx)) }, defaultTimeoutInterval, defaultPollInterval).Should(BeNumerically(">=", originalReplicas))
		})
		scale(worker, 0)
		Eventually(func(g Gomega) {
			pods := &corev1.PodList{}
			g.Expect(k8sClient.List(ctx, pods, client.InNamespace(ControllerNamespace), client.MatchingLabels(worker.Spec.Selector.MatchLabels))).To(Succeed())
			g.Expect(pods.Items).To(BeEmpty())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		scale(worker, originalReplicas)
		cs := clusterAdminClient()
		Eventually(func(g Gomega) {
			pods := &corev1.PodList{}
			g.Expect(k8sClient.List(ctx, pods, client.InNamespace(ControllerNamespace), client.MatchingLabels(worker.Spec.Selector.MatchLabels))).To(Succeed())
			g.Expect(pods.Items).To(HaveLen(int(originalReplicas)))
			for _, pod := range pods.Items {
				g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning))
				for _, condition := range pod.Status.Conditions {
					if condition.Type == corev1.PodReady {
						g.Expect(condition.Status).To(Equal(corev1.ConditionFalse))
					}
				}
				response, err := cs.CoreV1().Pods(ControllerNamespace).ProxyGet("http", pod.Name, "10080", "healthz", nil).DoRaw(ctx)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(string(response)).To(Equal("ok"))
				_, err = cs.CoreV1().Pods(ControllerNamespace).ProxyGet("http", pod.Name, "10080", "readyz", nil).DoRaw(ctx)
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring("replication-handoff"))
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(actor.Create(ctx, &corev1.ConfigMap{Name: prefix + "-admission", Namespace: ns})).To(Succeed())
		Expect(apierrors.IsForbidden(actor.Create(ctx, &corev1.ConfigMap{Name: prefix, Namespace: namespaces[2].Name}))).To(BeTrue())
		Eventually(func() error {
			fresh := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(input), fresh); err != nil {
				return err
			}
			fresh.Data["value"] = "after-handoff"
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func() error {
			fresh := &capsulev1beta2.TenantResource{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
				return err
			}
			if fresh.Annotations == nil {
				fresh.Annotations = map[string]string{}
			}
			fresh.Annotations[meta.ReconcileAnnotation] = prefix
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Consistently(func(g Gomega) {
			output := &corev1.ConfigMap{}
			g.Expect(actor.Get(ctx, outputKey, output)).To(Succeed())
			g.Expect(output.Data["value"]).To(Equal("initial"))
		}, 3*time.Second, defaultPollInterval).Should(Succeed())
		retireLegacy()
		Eventually(func(g Gomega) {
			output := &corev1.ConfigMap{}
			g.Expect(actor.Get(ctx, outputKey, output)).To(Succeed())
			g.Expect(output.Data["value"]).To(Equal("after-handoff"))
			g.Expect(len(readyWatchControllerPods(ctx))).To(BeNumerically(">=", originalReplicas))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

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
