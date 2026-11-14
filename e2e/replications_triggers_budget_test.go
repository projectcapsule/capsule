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
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

// Saturating the process-wide registration budget intentionally changes shared
// webhook state, so this scenario runs after ordinary parallel replication cases.
var _ = Describe("Replication trigger registration budget", Serial, Label("replications", "replication-triggers", "trigger-review4"), func() {
	It("keeps enforcement and lifecycle updates responsive beyond the CEL working set", func() {
		ctx := context.Background()
		prefix := "trigger-budget-" + rand.String(6)
		owners, namespaces := triggerReviewNamespaces(prefix)
		actors := []client.Client{impersonationClient(owners[0].Name, withDefaultGroups(nil)), impersonationClient(owners[1].Name, withDefaultGroups(nil))}
		var parents []*capsulev1beta2.TenantResource
		for i := range 5 {
			spec := capsulev1beta2.TenantResourceCommonSpec{TriggerMinInterval: metav1.Duration{Duration: time.Second}, ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Resources: []capsulev1beta2.ResourceSpec{{RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"budget-output-%d"},"data":{"value":"managed"}}`, i))}}}}}
			for group := range 32 {
				trigger := capsulev1beta2.TenantResourceTrigger{Name: fmt.Sprintf("g%d", group), Operations: []admissionv1.OperationType{admissionv1.Create}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}}
				for condition := range 60 {
					trigger.MatchConditions = append(trigger.MatchConditions, admissionv1.MatchCondition{Name: fmt.Sprintf("c%d", condition), Expression: fmt.Sprintf("request.name != '%s-%d-%d-%d'", prefix, i, group, condition)})
				}
				spec.Triggers = append(spec.Triggers, trigger)
			}
			parent := &capsulev1beta2.TenantResource{Name: fmt.Sprintf("%s-%d", prefix, i), Namespace: namespaces[i%2].Name, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: spec}}
			Expect(actors[i%2].Create(ctx, parent)).To(Succeed())
			parents = append(parents, parent)
		}
		waitReady := func(parent *capsulev1beta2.TenantResource) {
			Eventually(func(g Gomega) {
				fresh := &capsulev1beta2.TenantResource{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), fresh)).To(Succeed())
				g.Expect(fresh.Status.ObservedGeneration).To(Equal(fresh.Generation))
				g.Expect(meta.IsStatusConditionTrue(fresh.Status.Conditions, meta.ReadyCondition)).To(BeTrue(), fmt.Sprintf("status=%+v", fresh.Status))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		for _, parent := range parents {
			waitReady(parent)
		}
		By("publishing only a bounded subset of the 160 distinct native groups")
		var published *capsulev1beta2.TenantResource
		var selected capsulev1beta2.TenantResourceTrigger
		Eventually(func(g Gomega) {
			list := &admissionv1.ValidatingWebhookConfigurationList{}
			g.Expect(k8sClient.List(ctx, list)).To(Succeed())
			names := map[string]bool{}
			triggerCount := 0
			enforcement := false
			for _, cfg := range list.Items {
				for _, hook := range cfg.Webhooks {
					if strings.HasPrefix(hook.Name, "trigger-") {
						names[hook.Name] = true
						triggerCount++
					} else if strings.Contains(hook.Name, "replications.validating") {
						enforcement = true
					}
				}
			}
			g.Expect(triggerCount).To(BeNumerically(">", 0))
			g.Expect(triggerCount).To(BeNumerically("<=", 128))
			g.Expect(triggerCount).To(BeNumerically("<", 160))
			g.Expect(enforcement).To(BeTrue())
			published = nil
			for _, parent := range parents {
				if parent.Namespace != namespaces[0].Name {
					continue
				}
				for _, trigger := range parent.Spec.Triggers {
					if names["trigger-"+replication.MatchID(&trigger)[:40]+".projectcapsule.dev"] {
						published = parent
						selected = trigger
						break
					}
				}
				if published != nil {
					break
				}
			}
			g.Expect(published).NotTo(BeNil())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		By("preserving normal resource reconciliation through unrelated spec edits")
		for i, parent := range parents {
			Eventually(func() error {
				fresh := &capsulev1beta2.TenantResource{}
				if err := actors[i%2].Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
					return err
				}
				fresh.Spec.ResyncPeriod.Duration = 23 * time.Hour
				return actors[i%2].Update(ctx, fresh)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			waitReady(parent)
		}
		By("retaining managed-resource enforcement while triggers exceed the budget")
		Eventually(func() error {
			fresh := &corev1.ConfigMap{}
			key := client.ObjectKey{Name: "budget-output-0", Namespace: namespaces[0].Name}
			if err := actors[0].Get(ctx, key, fresh); err != nil {
				return err
			}
			fresh.Data["value"] = "unauthorized"
			err := actors[0].Update(ctx, fresh)
			if err == nil {
				return StopTrying("managed write unexpectedly succeeded")
			}
			if apierrors.IsConflict(err) {
				return err
			}
			Expect(apierrors.IsForbidden(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("is managed by a tenant capsule replication"))
			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "budget-output-0", Namespace: namespaces[0].Name}, cm)).To(Succeed())
		Expect(cm.Data["value"]).To(Equal("managed"))
		Expect(actors[0].Create(ctx, &corev1.ConfigMap{Name: prefix + "-source", Namespace: namespaces[0].Name})).To(Succeed())
		Eventually(func() bool {
			fresh := &capsulev1beta2.TenantResource{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(published), fresh)).To(Succeed())
			return fresh.Status.Trigger != nil && fresh.Status.Trigger.ObservedRequest != "" && fresh.Status.Trigger.ObservedRequest == fresh.Annotations[replication.TriggerAnnotation]
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		for _, parent := range parents {
			if parent.Namespace == namespaces[1].Name {
				Consistently(func() string {
					fresh := &capsulev1beta2.TenantResource{}
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), fresh)).To(Succeed())
					return fresh.Annotations[replication.TriggerAnnotation]
				}, 2*time.Second, 200*time.Millisecond).Should(BeEmpty())
			}
		}
		By("retiring and reactivating subscriptions on cordon and trigger removal")
		edit := func(change func(*capsulev1beta2.TenantResourceCommonSpec)) {
			Eventually(func() error {
				fresh := &capsulev1beta2.TenantResource{}
				if err := actors[0].Get(ctx, client.ObjectKeyFromObject(published), fresh); err != nil {
					return err
				}
				change(&fresh.Spec.TenantResourceCommonSpec)
				return actors[0].Update(ctx, fresh)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		edit(func(s *capsulev1beta2.TenantResourceCommonSpec) { s.Cordoned = new(true) })
		Eventually(func() bool { return triggerHook(selected) == nil }, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		edit(func(s *capsulev1beta2.TenantResourceCommonSpec) { s.Cordoned = new(false) })
		waitForReplicationTrigger(selected)
		edit(func(s *capsulev1beta2.TenantResourceCommonSpec) { s.Triggers = nil })
		Eventually(func() bool { return triggerHook(selected) == nil }, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		By("still rejecting invalid CEL even for an over-budget specification")
		parent := parents[1]
		Eventually(func() error {
			fresh := &capsulev1beta2.TenantResource{}
			if err := actors[1].Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
				return err
			}
			generation := fresh.Generation
			previousExpression := fresh.Spec.Triggers[31].MatchConditions[59].Expression
			fresh.Spec.Triggers[31].MatchConditions[59].Expression = "object["
			err := actors[1].Update(ctx, fresh)
			if err == nil {
				return StopTrying("invalid over-budget specification was accepted")
			}
			if apierrors.IsConflict(err) {
				return err
			}
			Expect(err.Error()).To(ContainSubstring("invalid admission match condition"))
			check := &capsulev1beta2.TenantResource{}
			Expect(actors[1].Get(ctx, client.ObjectKeyFromObject(parent), check)).To(Succeed())
			Expect(check.Spec.Triggers[31].MatchConditions[59].Expression).To(Equal(previousExpression))
			Expect(check.Generation).To(Equal(generation))
			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})
})
