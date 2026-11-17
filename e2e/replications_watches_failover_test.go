// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

func readyWatchControllerPods(ctx context.Context) []*corev1.Pod {
	GinkgoHelper()
	pods := &corev1.PodList{}
	Expect(k8sClient.List(ctx, pods, client.InNamespace(ControllerNamespace))).To(Succeed())
	var ready []*corev1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		manager, running := false, false
		for _, container := range pod.Spec.Containers {
			manager = manager || container.Name == "manager"
		}
		for _, condition := range pod.Status.Conditions {
			running = running || (condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue)
		}
		if manager && running && pod.DeletionTimestamp.IsZero() {
			ready = append(ready, pod)
		}
	}
	return ready
}

var _ = Describe("Replication watch leader handoff", Serial, Label("config", "replications", "replication-watches", "watch-failover"), func() {
	It("recovers a persisted pending token and cooldown across actual elected-pod loss", func() {
		ctx := context.Background()
		prefix := "watch-failover-" + rand.String(6)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		source := &corev1.ConfigMap{Name: prefix + "-input", Namespace: namespaces[0].Name, Labels: map[string]string{watchSourceLabel: prefix, watchInputLabel: prefix}, Data: map[string]string{"value": "initial"}}
		Expect(actor.Create(ctx, source)).To(Succeed())
		gate := &capsulev1beta2.TenantResource{Name: prefix + "-gate", Namespace: source.Namespace, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Resources: []capsulev1beta2.ResourceSpec{}, ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}}}}
		Expect(actor.Create(ctx, gate)).To(Succeed())
		waitWatchReady(gate)
		spec := watchBody(prefix, source.Namespace, prefix+"-output")
		spec.DependsOn = []meta.LocalRFC1123ObjectReference{{Name: meta.RFC1123Name(gate.Name)}}
		spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": source.Namespace}}
		parent := &capsulev1beta2.TenantResource{Name: prefix, Namespace: source.Namespace, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: spec}}
		Expect(actor.Create(ctx, parent)).To(Succeed())
		waitWatchReady(parent)
		expectConfigMapData(source.Namespace, prefix+"-output", map[string]string{"value": "initial"})
		// Complete the initial ownership handshake with the normal short retry
		// interval, then establish the long persisted cooldown under test.
		Eventually(func() error {
			fresh := &capsulev1beta2.TenantResource{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
				return err
			}
			fresh.Spec.TriggerMinInterval.Duration = 5 * time.Minute
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitWatchReady(parent)
		probeSpec := watchBody(prefix, source.Namespace, prefix+"-probe")
		probeSpec.Resources = []capsulev1beta2.ResourceSpec{}
		probe := &capsulev1beta2.TenantResource{Name: prefix + "-probe", Namespace: source.Namespace, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: probeSpec}}
		Expect(actor.Create(ctx, probe)).To(Succeed())
		waitWatchReady(probe)
		bob := impersonationClient(owners[1].Name, withDefaultGroups(nil))
		Expect(bob.Create(ctx, &corev1.ConfigMap{Name: prefix, Namespace: namespaces[2].Name, Labels: map[string]string{watchSourceLabel: prefix}, Data: map[string]string{"value": "other-tenant-private"}})).To(Succeed())
		other := &capsulev1beta2.TenantResource{Name: prefix, Namespace: namespaces[2].Name, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: probeSpec}}
		Expect(bob.Create(ctx, other)).To(Succeed())
		waitWatchReady(other)
		By("persisting a real watch notification while dependency application is blocked")
		Eventually(func() error {
			fresh := &capsulev1beta2.TenantResource{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(gate), fresh); err != nil {
				return err
			}
			trigger := watchRule(prefix)
			trigger.MatchConditions[0].Expression = "request.operation == 'CREATE'"
			fresh.Spec.Triggers = []capsulev1beta2.TenantResourceTrigger{trigger}
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			fresh := &capsulev1beta2.TenantResource{}
			g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(gate), fresh)).To(Succeed())
			condition := fresh.Status.Conditions.GetConditionByType(meta.ReadyCondition)
			g.Expect(fresh.Status.ObservedGeneration).To(Equal(fresh.Generation))
			g.Expect(condition).NotTo(BeNil())
			g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(condition.Message).To(ContainSubstring("undeclared"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		pending := ""
		Eventually(func() error {
			fresh := &capsulev1beta2.TenantResource{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
				return err
			}
			pending = fresh.Annotations[replication.TriggerAnnotation]
			if pending != "" && fresh.Status.Trigger != nil && fresh.Status.Trigger.ObservedRequest != pending {
				return nil
			}
			input := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(source), input); err != nil {
				return err
			}
			input.Data["value"] = "pending-" + rand.String(6)
			if err := actor.Update(ctx, input); err != nil {
				return err
			}
			return fmt.Errorf("waiting for persisted unacknowledged notification")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		lease := &coordinationv1.Lease{}
		leaseKey := client.ObjectKey{Namespace: ControllerNamespace, Name: "42c733ea.clastix.capsule.io"}
		Expect(k8sClient.Get(ctx, leaseKey, lease)).To(Succeed())
		Expect(lease.Spec.HolderIdentity).NotTo(BeNil())
		previousHolder := *lease.Spec.HolderIdentity
		Eventually(func() int { return len(readyWatchControllerPods(ctx)) }, defaultTimeoutInterval, defaultPollInterval).Should(BeNumerically(">=", 2))
		var leader *corev1.Pod
		for _, pod := range readyWatchControllerPods(ctx) {
			if strings.HasPrefix(previousHolder, pod.Name+"_") {
				leader = pod
			}
		}
		Expect(leader).NotTo(BeNil(), "lease holder must identify a ready controller pod")
		DeferCleanup(func() {
			Eventually(func() int { return len(readyWatchControllerPods(ctx)) }, defaultTimeoutInterval, defaultPollInterval).Should(BeNumerically(">=", 2))
		})
		By("losing the elected pod without losing writes or ordinary managed protection")
		handoffStarted := time.Now()
		Expect(k8sClient.Delete(ctx, leader, &client.DeleteOptions{GracePeriodSeconds: new(int64(0))})).To(Succeed())
		changeWatchInput(actor, source, "while-electing")
		Eventually(func() error {
			output := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKey{Namespace: source.Namespace, Name: prefix + "-output"}, output); err != nil {
				return err
			}
			output.Data["value"] = "unauthorized"
			err := actor.Update(ctx, output)
			if err == nil {
				return StopTrying("managed output write succeeded during failover")
			}
			if apierrors.IsConflict(err) {
				return err
			}
			Expect(apierrors.IsForbidden(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("is managed by a tenant capsule replication"))
			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		deadline := defaultTimeoutInterval
		if lease.Spec.LeaseDurationSeconds != nil {
			deadline += time.Duration(*lease.Spec.LeaseDurationSeconds) * time.Second
		}
		Eventually(func(g Gomega) {
			fresh := &coordinationv1.Lease{}
			g.Expect(k8sClient.Get(ctx, leaseKey, fresh)).To(Succeed())
			g.Expect(fresh.Spec.HolderIdentity).NotTo(BeNil())
			g.Expect(*fresh.Spec.HolderIdentity).NotTo(BeEmpty())
			g.Expect(*fresh.Spec.HolderIdentity).NotTo(Equal(previousHolder))
		}, deadline, defaultPollInterval).Should(Succeed())
		Expect(k8sClient.Get(ctx, leaseKey, lease)).To(Succeed())
		AddReportEntry("leader-handoff", map[string]any{"previous": previousHolder, "current": *lease.Spec.HolderIdentity, "duration": time.Since(handoffStarted)})
		fresh := &capsulev1beta2.TenantResource{}
		Expect(actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh)).To(Succeed())
		Expect(fresh.Annotations[replication.TriggerAnnotation]).To(Equal(pending))
		Expect(fresh.Status.Trigger).NotTo(BeNil())
		Expect(fresh.Status.Trigger.ObservedRequest).NotTo(Equal(pending))
		By("acknowledging that same token after dependency recovery on the new leader")
		Eventually(func() error {
			fresh := &capsulev1beta2.TenantResource{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(gate), fresh); err != nil {
				return err
			}
			fresh.Spec.Triggers = nil
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitWatchReady(gate)
		waitWatchReady(parent)
		expectConfigMapData(source.Namespace, prefix+"-output", map[string]string{"value": "while-electing"})
		Eventually(func(g Gomega) {
			fresh := &capsulev1beta2.TenantResource{}
			g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh)).To(Succeed())
			g.Expect(fresh.Status.Trigger).NotTo(BeNil())
			g.Expect(fresh.Status.Trigger.ObservedRequest).To(Equal(pending))
			g.Expect(fresh.Annotations[replication.TriggerAnnotation]).To(Equal(pending))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		By("proving new watches are active while the persisted long cooldown still prevents another token")
		Eventually(func(g Gomega) {
			fresh := &capsulev1beta2.TenantResource{}
			g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(probe), fresh)).To(Succeed())
			observed := ""
			if fresh.Status.Trigger != nil {
				observed = fresh.Status.Trigger.ObservedRequest
			}
			g.Expect(observed).To(Equal(fresh.Annotations[replication.TriggerAnnotation]))
		}, defaultTimeoutInterval, defaultPollInterval).MustPassRepeatedly(5).Should(Succeed())
		previousProbe := watchToken(probe)
		Eventually(func() error {
			if watchToken(probe) != previousProbe {
				return nil
			}
			input := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(source), input); err != nil {
				return err
			}
			input.Data["value"] = "new-leader-" + rand.String(6)
			if err := actor.Update(ctx, input); err != nil {
				return err
			}
			return fmt.Errorf("waiting for a real notification from the new watch leader")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Consistently(func() string {
			fresh := &capsulev1beta2.TenantResource{}
			Expect(actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh)).To(Succeed())
			Expect(fresh.Status.Trigger.ObservedRequest).To(Equal(pending))
			return fresh.Annotations[replication.TriggerAnnotation]
		}, 3*time.Second, 200*time.Millisecond).Should(Equal(pending))
		Expect(watchToken(other)).To(BeEmpty())
		for _, namespace := range namespaces[1:] {
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace.Name, Name: prefix + "-output"}, &corev1.ConfigMap{}))).To(BeTrue())
		}
		Expect(apierrors.IsForbidden(actor.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
	})
})
