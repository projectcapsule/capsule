// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

var _ = Describe("Replication watch preparation capacity", Serial, Label("replications", "replication-watches", "watch-capacity"), func() {
	It("continues periodic application and validation when a watch subscription exceeds preparation capacity", func() {
		ctx := context.Background()
		prefix := "watch-capacity-" + rand.String(6)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		source := &corev1.ConfigMap{Name: prefix, Namespace: namespaces[0].Name, Labels: map[string]string{watchSourceLabel: prefix, watchInputLabel: prefix}, Data: map[string]string{"value": "initial"}}
		Expect(actor.Create(ctx, source)).To(Succeed())
		var parents []*capsulev1beta2.TenantResource
		for index := range 5 {
			name := fmt.Sprintf("%s-%02d", prefix, index)
			spec := watchBody(prefix, namespaces[0].Name, name)
			if index == 4 {
				spec.ResyncPeriod = metav1.Duration{Duration: 3 * time.Second}
			}
			spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespaces[0].Name}}
			spec.Triggers = nil
			for triggerIndex := range 32 {
				trigger := watchRule(prefix)
				trigger.Name = fmt.Sprintf("events-%d", triggerIndex)
				trigger.MatchConditions = nil
				for condition := range 60 {
					trigger.MatchConditions = append(trigger.MatchConditions, admissionv1.MatchCondition{Name: fmt.Sprintf("condition-%d", condition), Expression: "true"})
				}
				spec.Triggers = append(spec.Triggers, trigger)
			}
			parent := &capsulev1beta2.TenantResource{Name: name, Namespace: namespaces[0].Name, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: spec}}
			Expect(actor.Create(ctx, parent)).To(Succeed())
			parents = append(parents, parent)
			waitWatchReady(parent)
			expectConfigMapData(parent.Namespace, parent.Name, map[string]string{"value": "initial"})
		}
		By("notifying prepared subscriptions while the fifth remains omitted")
		Eventually(func() error {
			ready := true
			for _, parent := range parents[:4] {
				ready = ready && watchToken(parent) != ""
			}
			if ready {
				return nil
			}
			fresh := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(source), fresh); err != nil {
				return err
			}
			fresh.Data["value"] = "event-" + rand.String(6)
			if err := actor.Update(ctx, fresh); err != nil {
				return err
			}
			return fmt.Errorf("waiting for prepared subscriptions")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		omitted := parents[4]
		Consistently(func() string { return watchToken(omitted) }, 3*time.Second, 200*time.Millisecond).Should(BeEmpty())
		By("recovering source changes through periodic application without a watch notification")
		changeWatchInput(actor, source, "periodic")
		expectConfigMapData(omitted.Namespace, omitted.Name, map[string]string{"value": "periodic"})
		Expect(watchToken(omitted)).To(BeEmpty())
		By("rejecting invalid CEL in the omitted source's new generation")
		Eventually(func() error {
			fresh := &capsulev1beta2.TenantResource{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(omitted), fresh); err != nil {
				return err
			}
			fresh.Spec.Triggers[31].MatchConditions[59].Expression = "request.operation == 'UPDATE'"
			fresh.Spec.Resources[0].Generators[0].Template = fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: %s\ndata:\n  value: blocked\n", omitted.Name)
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			fresh := &capsulev1beta2.TenantResource{}
			g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(omitted), fresh)).To(Succeed())
			g.Expect(fresh.Status.ObservedGeneration).To(Equal(fresh.Generation))
			condition := fresh.Status.Conditions.GetConditionByType(meta.ReadyCondition)
			g.Expect(condition).NotTo(BeNil())
			g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(condition.Message).To(ContainSubstring("request"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Consistently(func(g Gomega) {
			output := &corev1.ConfigMap{}
			g.Expect(actor.Get(ctx, client.ObjectKey{Namespace: omitted.Namespace, Name: omitted.Name}, output)).To(Succeed())
			g.Expect(output.Data["value"]).NotTo(Equal("blocked"))
		}, 3*time.Second, 200*time.Millisecond).Should(Succeed())
		By("resuming application after correction without requiring watch capacity")
		Eventually(func() error {
			fresh := &capsulev1beta2.TenantResource{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(omitted), fresh); err != nil {
				return err
			}
			fresh.Spec.Triggers[31].MatchConditions[59].Expression = "true"
			fresh.Spec.Resources[0].Generators[0].Template = fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: %s\ndata:\n  value: recovered\n", omitted.Name)
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitWatchReady(omitted)
		expectConfigMapData(omitted.Namespace, omitted.Name, map[string]string{"value": "recovered"})
		Expect(watchToken(omitted)).To(BeEmpty())
		for _, namespace := range namespaces[1:] {
			for _, parent := range parents {
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace.Name, Name: parent.Name}, &corev1.ConfigMap{}))).To(BeTrue())
			}
		}
	})
})
