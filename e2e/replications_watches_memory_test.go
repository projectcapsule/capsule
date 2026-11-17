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

var _ = Describe("Replication watch snapshot memory", Serial, Label("replications", "replication-watches", "watch-memory"), func() {
	It("recovers evicted old state without inventing events or crossing tenant profiles", func() {
		ctx := context.Background()
		prefix := "watch-memory-" + rand.String(6)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		source := &corev1.ConfigMap{Name: prefix + "-input", Namespace: namespaces[0].Name, Labels: map[string]string{watchSourceLabel: prefix, watchInputLabel: prefix}, Data: map[string]string{"value": "initial"}}
		marker := &corev1.ConfigMap{Name: prefix + "-marker", Namespace: source.Namespace, Labels: map[string]string{watchSourceLabel: prefix}, Data: map[string]string{"value": "initial"}}
		Expect(actor.Create(ctx, source)).To(Succeed())
		Expect(actor.Create(ctx, marker)).To(Succeed())
		var parents []*capsulev1beta2.TenantResource
		for index := range 8 {
			name := fmt.Sprintf("%s-%d", prefix, index)
			ensureServiceAccount(source.Namespace, name)
			bindServiceAccountToTenantResourceManager(source.Namespace, name, source.Namespace)
			spec := watchBody(prefix, source.Namespace, name)
			spec.Triggers[0].Operations = []admissionv1.OperationType{admissionv1.Update}
			if index == 0 {
				spec.ResyncPeriod = metav1.Duration{Duration: 3 * time.Second}
				spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": source.Namespace}}
			} else {
				spec.Resources = []capsulev1beta2.ResourceSpec{}
			}
			parent := &capsulev1beta2.TenantResource{Name: name, Namespace: source.Namespace, Spec: capsulev1beta2.TenantResourceSpec{ServiceAccount: &meta.LocalRFC1123ObjectReference{Name: meta.RFC1123Name(name)}, TenantResourceCommonSpec: spec}}
			Expect(actor.Create(ctx, parent)).To(Succeed())
			parents = append(parents, parent)
			waitWatchReady(parent)
		}
		other := &capsulev1beta2.TenantResource{Name: prefix, Namespace: namespaces[2].Name, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{watchRule(prefix)}, Resources: []capsulev1beta2.ResourceSpec{}, ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}}}}
		bob := impersonationClient(owners[1].Name, withDefaultGroups(nil))
		Expect(bob.Create(ctx, other)).To(Succeed())
		waitWatchReady(other)
		barrier := func(previous []string) []string {
			GinkgoHelper()
			Eventually(func() error {
				ready := true
				for i, parent := range parents {
					fresh := &capsulev1beta2.TenantResource{}
					if err := actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
						return err
					}
					ready = ready && fresh.Status.Trigger != nil && fresh.Status.Trigger.ObservedRequest != "" && fresh.Status.Trigger.ObservedRequest == fresh.Annotations[replication.TriggerAnnotation]
					if len(previous) > 0 {
						ready = ready && fresh.Status.Trigger != nil && fresh.Status.Trigger.ObservedRequest != previous[i]
					}
				}
				if ready {
					return nil
				}
				fresh := &corev1.ConfigMap{}
				if err := actor.Get(ctx, client.ObjectKeyFromObject(marker), fresh); err != nil {
					return err
				}
				fresh.Data["value"] = rand.String(8)
				if err := actor.Update(ctx, fresh); err != nil {
					return err
				}
				return fmt.Errorf("waiting for all watch identities to pass the event barrier")
			}, defaultTimeoutInterval, 200*time.Millisecond).MustPassRepeatedly(10).Should(Succeed())
			tokens := make([]string, len(parents))
			for i, parent := range parents {
				tokens[i] = watchToken(parent)
			}
			return tokens
		}
		initial := barrier(nil)
		By("evicting cold input snapshots independently in eight authorized watch identities")
		for i := range 96 {
			object := &corev1.ConfigMap{Name: fmt.Sprintf("%s-fill-%03d", prefix, i), Namespace: source.Namespace, Data: map[string]string{"value": strings.Repeat("x", 32*1024)}}
			Expect(actor.Create(ctx, object)).To(Succeed())
		}
		baseline := barrier(initial)
		By("skipping a cold UPDATE while periodic reconciliation recovers the selected namespace profile")
		changeWatchInput(actor, source, "cold-update")
		expectConfigMapData(source.Namespace, parents[0].Name, map[string]string{"value": "cold-update"})
		Consistently(func() []string {
			tokens := make([]string, len(parents))
			for i, parent := range parents {
				tokens[i] = watchToken(parent)
			}
			return tokens
		}, 3*time.Second, 200*time.Millisecond).Should(Equal(baseline))
		By("matching real old/new state after the evicted object becomes warm again")
		for _, parent := range parents {
			Eventually(func() error {
				fresh := &capsulev1beta2.TenantResource{}
				if err := actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
					return err
				}
				fresh.Spec.Triggers[0].MatchConditions = append(fresh.Spec.Triggers[0].MatchConditions[:1], capsulev1beta2.TenantResourceTriggerCondition{Name: "real-old-state", Expression: fmt.Sprintf("object.metadata.name != '%s' || oldObject.data.value == 'cold-update'", source.Name)})
				return actor.Update(ctx, fresh)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			waitWatchReady(parent)
		}
		// Ready reports resource application. A marker event must also pass the
		// new trigger generations before the one-shot old/new-state assertion.
		baseline = barrier(baseline)
		changeWatchInput(actor, source, "warm")
		Eventually(func(g Gomega) {
			for i, parent := range parents {
				g.Expect(watchToken(parent)).NotTo(Equal(baseline[i]))
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectConfigMapData(source.Namespace, parents[0].Name, map[string]string{"value": "warm"})
		By("recovering oversized inputs through periodic reconciliation without retaining or notifying their watch payloads")
		beforeReset := make([]string, len(parents))
		for i, parent := range parents {
			beforeReset[i] = watchToken(parent)
			Eventually(func() error {
				fresh := &capsulev1beta2.TenantResource{}
				if err := actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
					return err
				}
				fresh.Spec.Triggers[0].MatchConditions = fresh.Spec.Triggers[0].MatchConditions[:1]
				return actor.Update(ctx, fresh)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			waitWatchReady(parent)
		}
		warm := barrier(beforeReset)
		Eventually(func() error {
			fresh := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(source), fresh); err != nil {
				return err
			}
			fresh.Data["value"], fresh.Data["padding"] = "large-fallback", strings.Repeat("x", 512*1024)
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectConfigMapData(source.Namespace, parents[0].Name, map[string]string{"value": "large-fallback"})
		Consistently(func() []string {
			tokens := make([]string, len(parents))
			for i, parent := range parents {
				tokens[i] = watchToken(parent)
			}
			return tokens
		}, 3*time.Second, 200*time.Millisecond).Should(Equal(warm))
		Expect(watchToken(other)).To(BeEmpty())
		for _, ns := range namespaces[1:] {
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: parents[0].Name}, &corev1.ConfigMap{}))).To(BeTrue())
		}
		for _, parent := range parents {
			reader := impersonationClient(serviceAccountUsername(source.Namespace, parent.Name), serviceAccountGroups(source.Namespace))
			Expect(apierrors.IsForbidden(reader.List(ctx, &corev1.ConfigMapList{}, client.InNamespace(namespaces[2].Name)))).To(BeTrue())
		}
	})
})
