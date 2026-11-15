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
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	admissionapi "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
	"github.com/projectcapsule/capsule/pkg/template"
)

const (
	watchSourceLabel = "projectcapsule.dev/watch-source"
	watchInputLabel  = "projectcapsule.dev/watch-input"
)

func watchNamespaces(prefix string) ([]rbac.UserSpec, []*corev1.Namespace) {
	GinkgoHelper()
	ctx := context.Background()
	owners := []rbac.UserSpec{{Kind: rbac.UserOwner, Name: prefix + "-alice"}, {Kind: rbac.UserOwner, Name: prefix + "-bob"}}
	var namespaces []*corev1.Namespace
	for i, owner := range owners {
		tenant := &capsulev1beta2.Tenant{Name: fmt.Sprintf("%s-%d", prefix, i), Labels: map[string]string{"env": "e2e", "projectcapsule.dev/watch-tenant": fmt.Sprint(i) + prefix}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{UserSpec: owner}}}}
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
		DeferCleanup(DeleteInBackground, tenant)
		TenantReadyTrue(tenant)
		count := 1
		if i == 0 {
			count = 2
		}
		for j := range count {
			ns := NewNamespace("", map[string]string{meta.NewTenantLabel: tenant.Name, "projectcapsule.dev/watch-profile": "excluded", "projectcapsule.dev/watch-case": prefix})
			if i == 0 && j == 0 {
				ns.Labels["projectcapsule.dev/watch-profile"] = "selected"
			}
			NamespaceCreation(ns, owner, defaultTimeoutInterval).Should(Succeed())
			DeferCleanup(DeleteNamespaceInBackground, ctx, ns.Name)
			TenantNamespaceReady(tenant, ns, uint(j+1))
			ensureServiceAccount(ns.Name, "default")
			bindServiceAccountToTenantResourceManager(ns.Name, "default", ns.Name)
			DeferCleanup(cleanupTenantResourcesWithDefaultServiceAccount, ctx, ns.Name)
			namespaces = append(namespaces, ns)
		}
	}
	return owners, namespaces
}

func watchRule(prefix string) capsulev1beta2.TenantResourceTrigger {
	return capsulev1beta2.TenantResourceTrigger{Name: "changes", Operations: []admissionv1.OperationType{admissionv1.OperationAll}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}, ObjectSelector: &metav1.LabelSelector{MatchLabels: map[string]string{watchSourceLabel: prefix}}, MatchConditions: []admissionv1.MatchCondition{{Name: "data-changed", Expression: "operation != 'UPDATE' || object.data != oldObject.data"}}}
}

func watchBody(prefix, sourceNamespace, output string) capsulev1beta2.TenantResourceCommonSpec {
	return capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, TriggerMinInterval: metav1.Duration{Duration: time.Second}, Triggers: []capsulev1beta2.TenantResourceTrigger{watchRule(prefix)}, Resources: []capsulev1beta2.ResourceSpec{{AdditionalMetadata: &api.AdditionalMetadataSpec{Labels: map[string]string{watchSourceLabel: prefix}}, Context: &template.TemplateContext{Resources: []*template.TemplateResourceReference{{Index: "inputs", APIVersion: "v1", Kind: "ConfigMap", Namespace: sourceNamespace, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{watchInputLabel: prefix}}}}}, Generators: []capsulev1beta2.TemplateItemSpec{{Template: fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
data:
  value: '{{ if .inputs }}{{ (index .inputs 0).data.value }}{{ else }}missing{{ end }}'
`, output)}}}}}
}

func watchToken(object client.Object) string {
	GinkgoHelper()
	switch target := object.(type) {
	case *capsulev1beta2.TenantResource:
		fresh := &capsulev1beta2.TenantResource{}
		Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(target), fresh)).To(Succeed())
		if fresh.Status.Trigger != nil {
			return fresh.Status.Trigger.ObservedRequest
		}
	case *capsulev1beta2.GlobalTenantResource:
		fresh := &capsulev1beta2.GlobalTenantResource{}
		Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(target), fresh)).To(Succeed())
		if fresh.Status.Trigger != nil {
			return fresh.Status.Trigger.ObservedRequest
		}
	}
	return ""
}

func waitWatchReady(object client.Object) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		var fresh client.Object
		if _, ok := object.(*capsulev1beta2.TenantResource); ok {
			fresh = &capsulev1beta2.TenantResource{}
		} else {
			fresh = &capsulev1beta2.GlobalTenantResource{}
		}
		g.Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(object), fresh)).To(Succeed())
		var status *capsulev1beta2.TenantResourceCommonStatus
		if target, ok := fresh.(*capsulev1beta2.TenantResource); ok {
			status = &target.Status.TenantResourceCommonStatus
		} else {
			status = &fresh.(*capsulev1beta2.GlobalTenantResource).Status.TenantResourceCommonStatus
		}
		g.Expect(status.ObservedGeneration).To(Equal(fresh.GetGeneration()))
		g.Expect(meta.IsStatusConditionTrue(status.Conditions, meta.ReadyCondition)).To(BeTrue(), fmt.Sprintf("conditions=%+v items=%+v", status.Conditions, status.ProcessedItems))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func changeWatchInput(actor client.Client, source *corev1.ConfigMap, value string) {
	GinkgoHelper()
	Eventually(func() error {
		fresh := &corev1.ConfigMap{}
		if err := actor.Get(context.Background(), client.ObjectKeyFromObject(source), fresh); err != nil {
			return err
		}
		fresh.Data["value"] = value
		return actor.Update(context.Background(), fresh)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

var _ = Describe("Replication resource watches", Label("replications", "replication-watches"), func() {
	It("observes persisted state with namespace profiles, CEL, managed exclusion and tenant isolation", func() {
		ctx := context.Background()
		prefix := "watch-profile-" + rand.String(6)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		source := &corev1.ConfigMap{Name: prefix + "-input", Namespace: namespaces[0].Name, Labels: map[string]string{watchSourceLabel: prefix, watchInputLabel: prefix}, Data: map[string]string{"value": "initial"}}
		Expect(actor.Create(ctx, source)).To(Succeed())
		local := &capsulev1beta2.TenantResource{Name: prefix, Namespace: namespaces[0].Name, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: watchBody(prefix, namespaces[0].Name, prefix+"-local")}}
		local.Spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespaces[0].Name}}
		Expect(actor.Create(ctx, local)).To(Succeed())
		waitWatchReady(local)
		other := &capsulev1beta2.TenantResource{Name: prefix, Namespace: namespaces[2].Name, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: watchBody(prefix, namespaces[2].Name, prefix+"-other")}}
		other.Spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespaces[2].Name}}
		bob := impersonationClient(owners[1].Name, withDefaultGroups(nil))
		Expect(bob.Create(ctx, other)).To(Succeed())
		waitWatchReady(other)
		global := &capsulev1beta2.GlobalTenantResource{Name: prefix, Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: "Namespace", TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/watch-tenant": "0" + prefix}}, TenantResourceCommonSpec: watchBody(prefix, namespaces[0].Name, prefix+"-global")}}
		global.Spec.Triggers[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/watch-case": prefix, "projectcapsule.dev/watch-profile": "selected"}}
		global.Spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespaces[0].Name}}
		Expect(k8sClient.Create(ctx, global)).To(Succeed())
		DeferCleanup(DeleteInBackground, global)
		waitWatchReady(global)
		By("waiting for the asynchronous watch to observe a real persisted change")
		Eventually(func() error {
			if watchToken(local) != "" && watchToken(global) != "" {
				return nil
			}
			fresh := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(source), fresh); err != nil {
				return err
			}
			fresh.Data["value"] = "boot-" + rand.String(6)
			if err := actor.Update(ctx, fresh); err != nil {
				return err
			}
			return fmt.Errorf("waiting for watch notification")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		changeWatchInput(actor, source, "persisted")
		expectConfigMapData(namespaces[0].Name, prefix+"-local", map[string]string{"value": "persisted"})
		expectConfigMapData(namespaces[0].Name, prefix+"-global", map[string]string{"value": "persisted"})
		Consistently(func() string { return watchToken(other) }, 3*time.Second, 200*time.Millisecond).Should(BeEmpty())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaces[1].Name, Name: prefix + "-global"}, &corev1.ConfigMap{}))).To(BeTrue())
		By("skipping dry runs, unrelated objects, metadata-only updates and excluded source namespaces")
		localToken, globalToken := watchToken(local), watchToken(global)
		dry := &corev1.ConfigMap{Name: prefix + "-dry", Namespace: namespaces[0].Name, Labels: map[string]string{watchSourceLabel: prefix}, Data: map[string]string{"value": "dry"}}
		Expect(actor.Create(ctx, dry, client.DryRunAll)).To(Succeed())
		Expect(apierrors.IsNotFound(actor.Get(ctx, client.ObjectKeyFromObject(dry), &corev1.ConfigMap{}))).To(BeTrue())
		Expect(actor.Create(ctx, &corev1.ConfigMap{Name: prefix + "-unrelated", Namespace: namespaces[0].Name})).To(Succeed())
		Expect(actor.Create(ctx, &corev1.ConfigMap{Name: prefix + "-excluded", Namespace: namespaces[1].Name, Labels: map[string]string{watchSourceLabel: prefix}, Data: map[string]string{"value": "excluded"}})).To(Succeed())
		Eventually(func() error {
			fresh := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(source), fresh); err != nil {
				return err
			}
			fresh.Annotations = map[string]string{"projectcapsule.dev/user-note": "changed"}
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Consistently(func() []string { return []string{watchToken(local), watchToken(global)} }, 3*time.Second, 200*time.Millisecond).Should(Equal([]string{localToken, globalToken}))
		By("ignoring a managed output even when its selector and changed data qualify")
		controller := impersonationClient(ControllerServiceAccountFull, nil)
		outputKey := client.ObjectKey{Name: prefix + "-local", Namespace: namespaces[0].Name}
		Eventually(func() error {
			fresh := &corev1.ConfigMap{}
			if err := controller.Get(ctx, outputKey, fresh); err != nil {
				return err
			}
			manager := ""
			prefix := meta.ReplicationFieldOwnerPrefix(local.Name, local.Namespace) + "/"
			for _, fields := range fresh.ManagedFields {
				if fields.Operation == metav1.ManagedFieldsOperationApply && strings.HasPrefix(fields.Manager, prefix) {
					manager = fields.Manager
					break
				}
			}
			Expect(manager).NotTo(BeEmpty(), "fixture must retain the real replication SSA owner")
			fresh.ManagedFields = nil
			fresh.APIVersion, fresh.Kind = "v1", "ConfigMap"
			fresh.Data["value"] = "controller-edit"
			return controller.Patch(ctx, fresh, client.Apply, client.FieldOwner(manager))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Consistently(func(g Gomega) {
			fresh := &corev1.ConfigMap{}
			g.Expect(controller.Get(ctx, outputKey, fresh)).To(Succeed())
			g.Expect(fresh.Data["value"]).To(Equal("controller-edit"))
			g.Expect(watchToken(local)).To(Equal(localToken))
		}, 3*time.Second, 200*time.Millisecond).Should(Succeed())
		By("observing delete state after the input has disappeared")
		Expect(actor.Delete(ctx, source)).To(Succeed())
		expectConfigMapData(namespaces[0].Name, prefix+"-local", map[string]string{"value": "missing"})
		expectConfigMapData(namespaces[0].Name, prefix+"-global", map[string]string{"value": "missing"})
		By("observing a same-name replacement without affecting another namespace profile or tenant")
		replacement := &corev1.ConfigMap{Name: source.Name, Namespace: source.Namespace, Labels: map[string]string{watchSourceLabel: prefix, watchInputLabel: prefix}, Data: map[string]string{"value": "replacement"}}
		Expect(actor.Create(ctx, replacement)).To(Succeed())
		Expect(replacement.UID).NotTo(Equal(source.UID))
		expectConfigMapData(namespaces[0].Name, prefix+"-local", map[string]string{"value": "replacement"})
		expectConfigMapData(namespaces[0].Name, prefix+"-global", map[string]string{"value": "replacement"})
		Consistently(func() string { return watchToken(other) }, 3*time.Second, 200*time.Millisecond).Should(BeEmpty())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaces[1].Name, Name: prefix + "-global"}, &corev1.ConfigMap{}))).To(BeTrue())
		By("retaining normal managed-resource enforcement")
		Eventually(func() error {
			fresh := &corev1.ConfigMap{}
			if err := actor.Get(ctx, outputKey, fresh); err != nil {
				return err
			}
			fresh.Data["value"] = "unauthorized"
			err := actor.Update(ctx, fresh)
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
		expectConfigMapData(namespaces[0].Name, prefix+"-local", map[string]string{"value": "replacement"})
		By("using updated source namespace labels without changing destination profiles")
		previousGlobal := watchToken(global)
		Eventually(func() error {
			fresh := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(namespaces[1]), fresh); err != nil {
				return err
			}
			fresh.Labels["projectcapsule.dev/watch-profile"] = "selected"
			return k8sClient.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func() error {
			if watchToken(global) != previousGlobal {
				return nil
			}
			fresh := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKey{Namespace: namespaces[1].Name, Name: prefix + "-excluded"}, fresh); err != nil {
				return err
			}
			fresh.Data = map[string]string{"value": rand.String(8)}
			if err := actor.Update(ctx, fresh); err != nil {
				return err
			}
			return fmt.Errorf("waiting for selected namespace event")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaces[1].Name, Name: prefix + "-global"}, &corev1.ConfigMap{}))).To(BeTrue())
		By("publishing no trigger admission registrations")
		configurations := &admissionv1.ValidatingWebhookConfigurationList{}
		Expect(k8sClient.List(ctx, configurations)).To(Succeed())
		for _, configuration := range configurations.Items {
			for _, hook := range configuration.Webhooks {
				Expect(strings.HasPrefix(hook.Name, "trigger-")).To(BeFalse())
			}
		}
	})

	It("reports invalid watch specifications through controller status", func() {
		ctx := context.Background()
		prefix := "watch-validation-" + rand.String(6)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		var repair *capsulev1beta2.TenantResource
		for _, test := range []struct {
			name   string
			change func(*capsulev1beta2.TenantResourceTrigger)
			reason string
		}{
			{"audience", func(trigger *capsulev1beta2.TenantResourceTrigger) {
				trigger.Audience = []rules.Audience{{Kind: rules.AudienceKindUser, Name: owners[0].Name}}
			}, "audience"},
			{"request", func(trigger *capsulev1beta2.TenantResourceTrigger) {
				trigger.MatchConditions[0].Expression = "request.userInfo.username == 'alice'"
			}, "undeclared"},
			{"wildcard", func(trigger *capsulev1beta2.TenantResourceTrigger) { trigger.Resources = []string{"*"} }, "explicit top-level"},
			{"subresource", func(trigger *capsulev1beta2.TenantResourceTrigger) { trigger.Resources = []string{"deployments/scale"} }, "explicit top-level"},
		} {
			spec := watchBody(prefix, namespaces[0].Name, prefix+"-output")
			spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespaces[0].Name}}
			test.change(&spec.Triggers[0])
			parent := &capsulev1beta2.TenantResource{Name: prefix + "-" + test.name, Namespace: namespaces[0].Name, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: spec}}
			err := actor.Create(ctx, parent)
			if test.name == "audience" {
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(test.reason))
				Expect(apierrors.IsNotFound(actor.Get(ctx, client.ObjectKeyFromObject(parent), &capsulev1beta2.TenantResource{}))).To(BeTrue())
				continue
			}
			Expect(err).To(Succeed())
			Eventually(func(g Gomega) {
				fresh := &capsulev1beta2.TenantResource{}
				g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh)).To(Succeed())
				g.Expect(fresh.Status.ObservedGeneration).To(Equal(fresh.Generation))
				condition := fresh.Status.Conditions.GetConditionByType(meta.ReadyCondition)
				g.Expect(condition).NotTo(BeNil())
				g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(condition.Message).To(ContainSubstring(test.reason))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaces[0].Name, Name: prefix + "-output"}, &corev1.ConfigMap{}))).To(BeTrue())
			// Simulate the dispatcher's pending notification without changing
			// the invalid specification or its generation.
			Eventually(func() error {
				fresh := &capsulev1beta2.TenantResource{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
					return err
				}
				if fresh.Annotations == nil {
					fresh.Annotations = map[string]string{}
				}
				fresh.Annotations[replication.TriggerAnnotation] = "invalid-" + test.name
				return k8sClient.Update(ctx, fresh)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			last := metav1.MicroTime{}
			Eventually(func() (bool, error) {
				fresh := &capsulev1beta2.TenantResource{}
				if err := actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
					return false, err
				}
				if fresh.Status.Trigger == nil {
					return false, nil
				}
				Expect(fresh.Status.Trigger.ObservedRequest).To(BeEmpty())
				if !last.Equal(&fresh.Status.Trigger.LastTriggeredAt) {
					last = fresh.Status.Trigger.LastTriggeredAt
					return false, nil
				}
				return true, nil
			}, defaultTimeoutInterval, 200*time.Millisecond).MustPassRepeatedly(15).Should(BeTrue(), "permanent validation errors must stop fast triggered attempts")
			repair = parent
		}
		By("resuming the selected namespace profile after correcting the specification")
		Eventually(func() error {
			fresh := &capsulev1beta2.TenantResource{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(repair), fresh); err != nil {
				return err
			}
			fresh.Spec.Triggers = []capsulev1beta2.TenantResourceTrigger{watchRule(prefix)}
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitWatchReady(repair)
		expectConfigMapData(namespaces[0].Name, prefix+"-output", map[string]string{"value": "missing"})
		for _, namespace := range namespaces[1:] {
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace.Name, Name: prefix + "-output"}, &corev1.ConfigMap{}))).To(BeTrue())
		}
	})

	It("combines namespace-restricted and unrestricted resource alternatives", func() {
		ctx := context.Background()
		prefix := "watch-alternatives-" + rand.String(6)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		bob := impersonationClient(owners[1].Name, withDefaultGroups(nil))
		restricted := watchRule(prefix)
		restricted.Name, restricted.Resources, restricted.MatchConditions = "restricted", []string{"secrets"}, nil
		restricted.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/watch-case": "excluded-" + prefix}}
		open := watchRule(prefix)
		open.Name, open.Operations = "unrestricted", []admissionv1.OperationType{admissionv1.Update}
		open.MatchConditions = []admissionv1.MatchCondition{{Name: "source-namespace", Expression: fmt.Sprintf("object.metadata.namespace == '%s'", namespaces[0].Name)}}
		common := capsulev1beta2.TenantResourceCommonSpec{Triggers: []capsulev1beta2.TenantResourceTrigger{restricted, open}, ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, TriggerMinInterval: metav1.Duration{Duration: time.Second}, Resources: []capsulev1beta2.ResourceSpec{}}
		target := &capsulev1beta2.GlobalTenantResource{Name: prefix, Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: "Namespace", TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/watch-tenant": "0" + prefix}}, TenantResourceCommonSpec: common}}
		Expect(k8sClient.Create(ctx, target)).To(Succeed())
		DeferCleanup(DeleteInBackground, target)
		waitWatchReady(target)
		onlyRestricted := &capsulev1beta2.GlobalTenantResource{Name: prefix + "-restricted", Spec: *target.Spec.DeepCopy()}
		onlyRestricted.Spec.Triggers = []capsulev1beta2.TenantResourceTrigger{restricted}
		Expect(k8sClient.Create(ctx, onlyRestricted)).To(Succeed())
		DeferCleanup(DeleteInBackground, onlyRestricted)
		waitWatchReady(onlyRestricted)
		source := &corev1.ConfigMap{Name: prefix, Namespace: namespaces[0].Name, Labels: map[string]string{watchSourceLabel: prefix}, Data: map[string]string{"value": "initial"}}
		Expect(actor.Create(ctx, source)).To(Succeed())
		Eventually(func() error {
			if watchToken(target) != "" {
				return nil
			}
			fresh := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(source), fresh); err != nil {
				return err
			}
			fresh.Data["value"] = rand.String(8)
			if err := actor.Update(ctx, fresh); err != nil {
				return err
			}
			return fmt.Errorf("waiting for unrestricted alternative")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		before := ""
		Eventually(func() bool {
			current := watchToken(target)
			if current == "" || current != before {
				before = current
				return false
			}
			return true
		}, defaultTimeoutInterval, 200*time.Millisecond).MustPassRepeatedly(20).Should(BeTrue())
		other := &corev1.ConfigMap{Name: prefix, Namespace: namespaces[2].Name, Labels: map[string]string{watchSourceLabel: prefix}, Data: map[string]string{"value": "other-tenant"}}
		Expect(bob.Create(ctx, other)).To(Succeed())
		changeWatchInput(bob, other, "other-tenant-update")
		Consistently(func() []string { return []string{watchToken(target), watchToken(onlyRestricted)} }, 3*time.Second, 200*time.Millisecond).Should(Equal([]string{before, ""}))
	})
	It("keeps parent bookkeeping quiet and applies cordon, interval and trigger changes", func() {
		ctx := context.Background()
		prefix := "watch-lifecycle-" + rand.String(6)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		bindWatchSubscriptionAccess(namespaces[0].Name, "default", []string{"get", "list", "watch"})

		trigger := watchRule(prefix)
		trigger.APIGroups = []string{capsulev1beta2.GroupVersion.Group}
		trigger.APIVersions = []string{"v1beta2"}
		trigger.Resources = []string{"tenantresources"}
		trigger.MatchConditions = nil
		spec := watchBody(prefix, namespaces[0].Name, prefix+"-output")
		spec.Triggers = []capsulev1beta2.TenantResourceTrigger{trigger}
		spec.Resources = []capsulev1beta2.ResourceSpec{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespaces[0].Name}}, RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"%s-output"},"data":{"value":"ready"}}`, prefix))}}}}
		parent := &capsulev1beta2.TenantResource{Name: prefix, Namespace: namespaces[0].Name, Labels: map[string]string{watchSourceLabel: prefix}, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: spec}}
		Expect(actor.Create(ctx, parent)).To(Succeed())
		waitWatchReady(parent)
		edit := func(change func(*capsulev1beta2.TenantResource)) {
			Eventually(func() error {
				fresh := &capsulev1beta2.TenantResource{}
				if err := actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
					return err
				}
				change(fresh)
				return actor.Update(ctx, fresh)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		userEvent := func(previous string) {
			// One user edit produces one eligible event. Repeated edits while
			// polling can leave legitimate work pending during the quiet check.
			edit(func(fresh *capsulev1beta2.TenantResource) {
				if fresh.Annotations == nil {
					fresh.Annotations = map[string]string{}
				}
				fresh.Annotations["projectcapsule.dev/user-event"] = rand.String(8)
			})
			Eventually(func(g Gomega) {
				fresh := &capsulev1beta2.TenantResource{}
				g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh)).To(Succeed())
				g.Expect(fresh.Status.Trigger).NotTo(BeNil())
				g.Expect(fresh.Status.Trigger.ObservedRequest).NotTo(BeEmpty())
				g.Expect(fresh.Status.Trigger.ObservedRequest).NotTo(Equal(previous))
				g.Expect(fresh.Status.Trigger.ObservedRequest).To(Equal(fresh.Annotations[replication.TriggerAnnotation]))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		By("establishing the informer baseline with persisted user changes")
		Eventually(func() error {
			if watchToken(parent) != "" {
				return nil
			}
			fresh := &capsulev1beta2.TenantResource{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
				return err
			}
			if fresh.Annotations == nil {
				fresh.Annotations = map[string]string{}
			}
			fresh.Annotations["projectcapsule.dev/bootstrap"] = rand.String(8)
			if err := actor.Update(ctx, fresh); err != nil {
				return err
			}
			return fmt.Errorf("waiting for informer baseline")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		By("draining bootstrap notifications before one-shot delivery")
		settled := ""
		Eventually(func() bool {
			fresh := &capsulev1beta2.TenantResource{}
			Expect(actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh)).To(Succeed())
			if fresh.Status.Trigger == nil {
				return false
			}
			current := fresh.Status.Trigger.ObservedRequest
			if current != settled {
				settled = current
				return false
			}
			return current != "" && current == fresh.Annotations[replication.TriggerAnnotation]
		}, defaultTimeoutInterval, 200*time.Millisecond).MustPassRepeatedly(20).Should(BeTrue())
		userEvent(settled)
		token := watchToken(parent)
		Consistently(func() string { return watchToken(parent) }, 4*time.Second, 200*time.Millisecond).Should(Equal(token), "internal notification/status patches must not feed back")
		By("observing a terminating source before its finalizer is removed")
		victim := &capsulev1beta2.TenantResource{Name: prefix + "-terminating", Namespace: namespaces[0].Name, Labels: map[string]string{watchSourceLabel: prefix}, Finalizers: []string{"projectcapsule.dev/watch-termination-test"}, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Cordoned: new(true), Resources: []capsulev1beta2.ResourceSpec{}}}}
		Expect(actor.Create(ctx, victim)).To(Succeed())
		DeferCleanup(func() {
			Eventually(func() error {
				fresh := &capsulev1beta2.TenantResource{}
				if err := actor.Get(ctx, client.ObjectKeyFromObject(victim), fresh); err != nil {
					return client.IgnoreNotFound(err)
				}
				fresh.Finalizers = slices.DeleteFunc(fresh.Finalizers, func(value string) bool { return value == "projectcapsule.dev/watch-termination-test" })
				return actor.Update(ctx, fresh)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
		Eventually(func() string { return watchToken(parent) }, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Equal(token))
		token = watchToken(parent)
		Consistently(func() string { return watchToken(parent) }, 3*time.Second, 200*time.Millisecond).Should(Equal(token))
		Expect(actor.Delete(ctx, victim)).To(Succeed())
		Eventually(func(g Gomega) {
			fresh := &capsulev1beta2.TenantResource{}
			g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(victim), fresh)).To(Succeed())
			g.Expect(fresh.DeletionTimestamp.IsZero()).To(BeFalse())
			g.Expect(fresh.Finalizers).To(ContainElement("projectcapsule.dev/watch-termination-test"))
			g.Expect(watchToken(parent)).NotTo(Equal(token))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("preserving ordinary spec reconciliation during a trigger cooldown")
		edit(func(fresh *capsulev1beta2.TenantResource) {
			fresh.Spec.TriggerMinInterval.Duration = 24 * time.Hour
			fresh.Spec.Resources[0].RawItems[0].Raw = []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"%s-output"},"data":{"value":"spec-updated"}}`, prefix))
		})
		waitWatchReady(parent)
		expectConfigMapData(namespaces[0].Name, prefix+"-output", map[string]string{"value": "spec-updated"})
		edit(func(fresh *capsulev1beta2.TenantResource) {
			fresh.Spec.TriggerMinInterval.Duration = time.Second
			fresh.Spec.Cordoned = new(true)
		})
		token = watchToken(parent)
		edit(func(fresh *capsulev1beta2.TenantResource) {
			if fresh.Annotations == nil {
				fresh.Annotations = map[string]string{}
			}
			fresh.Annotations["projectcapsule.dev/user-event"] = "cordoned"
		})
		Consistently(func() string { return watchToken(parent) }, 3*time.Second, 200*time.Millisecond).Should(Equal(token))
		edit(func(fresh *capsulev1beta2.TenantResource) { fresh.Spec.Cordoned = new(false) })
		waitWatchReady(parent)
		// Uncordoning the last subscriber starts a new informer. Ready reports
		// application, so probe persisted changes until its baseline is ready.
		Eventually(func() error {
			if watchToken(parent) != token {
				return nil
			}
			fresh := &capsulev1beta2.TenantResource{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
				return err
			}
			fresh.Annotations["projectcapsule.dev/user-event"] = rand.String(8)
			if err := actor.Update(ctx, fresh); err != nil {
				return err
			}
			return fmt.Errorf("waiting for the restarted watch")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			fresh := &capsulev1beta2.TenantResource{}
			g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh)).To(Succeed())
			g.Expect(fresh.Status.Trigger.ObservedRequest).To(Equal(fresh.Annotations[replication.TriggerAnnotation]))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		edit(func(fresh *capsulev1beta2.TenantResource) { fresh.Spec.Triggers = nil })
		waitWatchReady(parent)
		token = watchToken(parent)
		edit(func(fresh *capsulev1beta2.TenantResource) {
			fresh.Annotations["projectcapsule.dev/user-event"] = "unsubscribed"
		})
		Consistently(func() string { return watchToken(parent) }, 3*time.Second, 200*time.Millisecond).Should(Equal(token))
	})
	It("coalesces source bursts while unrelated subscriptions remain idle", Serial, func() {
		ctx := context.Background()
		prefix := "watch-load-" + rand.String(6)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		bob := impersonationClient(owners[1].Name, withDefaultGroups(nil))
		source := &corev1.ConfigMap{Name: prefix + "-input", Namespace: namespaces[0].Name, Labels: map[string]string{watchSourceLabel: prefix, watchInputLabel: prefix}, Data: map[string]string{"value": "initial"}}
		Expect(actor.Create(ctx, source)).To(Succeed())
		spec := watchBody(prefix, namespaces[0].Name, prefix+"-output")
		spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespaces[0].Name}}
		parent := &capsulev1beta2.TenantResource{Name: prefix, Namespace: namespaces[0].Name, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: spec}}
		Expect(actor.Create(ctx, parent)).To(Succeed())
		waitWatchReady(parent)
		// Bootstrap with the trigger retry path, then remove the subscription
		// before measuring the ordinary write baseline. Legacy initial errors
		// intentionally retain resyncPeriod and must not stall this fixture.
		Eventually(func() error {
			fresh := &capsulev1beta2.TenantResource{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
				return err
			}
			fresh.Spec.Triggers = nil
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitWatchReady(parent)
		var unrelated []*capsulev1beta2.TenantResource
		for i := range 64 {
			trigger := watchRule(prefix)
			trigger.Resources = []string{"secrets"}
			trigger.MatchConditions = nil
			other := &capsulev1beta2.TenantResource{Name: fmt.Sprintf("%s-unrelated-%d", prefix, i), Namespace: namespaces[2].Name, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Triggers: []capsulev1beta2.TenantResourceTrigger{trigger}, Resources: []capsulev1beta2.ResourceSpec{}}}}
			Expect(bob.Create(ctx, other)).To(Succeed())
			unrelated = append(unrelated, other)
		}
		for _, other := range unrelated {
			waitWatchReady(other)
		}
		initial := &capsulev1beta2.TenantResource{}
		Expect(actor.Get(ctx, client.ObjectKeyFromObject(parent), initial)).To(Succeed())
		observer, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		updates, err := observer.Watch(ctx, &capsulev1beta2.TenantResourceList{}, &client.ListOptions{Namespace: parent.Namespace, Raw: &metav1.ListOptions{FieldSelector: "metadata.name=" + parent.Name, ResourceVersion: initial.ResourceVersion}})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(updates.Stop)
		wave := func(name string) map[string]any {
			durations := make([]time.Duration, 0, 50)
			started := time.Now()
			for i := range 50 {
				before := time.Now()
				changeWatchInput(actor, source, fmt.Sprintf("%s-%d", name, i))
				durations = append(durations, time.Since(before))
			}
			slices.Sort(durations)
			return map[string]any{"writes": 50, "unrelatedSubscriptions": 64, "writeTime": time.Since(started).String(), "p50": durations[25].String(), "p95": durations[47].String()}
		}
		AddReportEntry("before-subscription", wave("baseline"))
		Eventually(func() error {
			fresh := &capsulev1beta2.TenantResource{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
				return err
			}
			fresh.Spec.Triggers = []capsulev1beta2.TenantResourceTrigger{watchRule(prefix)}
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitWatchReady(parent)
		Eventually(func() error {
			if watchToken(parent) != "" {
				return nil
			}
			fresh := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(source), fresh); err != nil {
				return err
			}
			fresh.Data["value"] = rand.String(8)
			if err := actor.Update(ctx, fresh); err != nil {
				return err
			}
			return fmt.Errorf("waiting for active watch")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		AddReportEntry("with-subscription", wave("watched"))
		expectConfigMapData(namespaces[0].Name, prefix+"-output", map[string]string{"value": "watched-49"})
		for _, other := range unrelated {
			Expect(watchToken(other)).To(BeEmpty())
		}
		By("preserving acknowledgement order across subscription edits and source bursts")
		final := &capsulev1beta2.TenantResource{}
		Expect(actor.Get(ctx, client.ObjectKeyFromObject(parent), final)).To(Succeed())
		requests := map[string]int{"": 0}
		remember := func(token string) {
			if _, ok := requests[token]; !ok {
				requests[token] = len(requests)
			}
		}
		initialAck := ""
		if initial.Status.Trigger != nil {
			initialAck = initial.Status.Trigger.ObservedRequest
		}
		remember(initialAck)
		remember(initial.Annotations[replication.TriggerAnnotation])
		acknowledged := requests[initialAck]
		Eventually(func() error {
			select {
			case update, ok := <-updates.ResultChan():
				if !ok || update.Type == watch.Error {
					return StopTrying("acknowledgement observer ended before the final state")
				}
				current, ok := update.Object.(*capsulev1beta2.TenantResource)
				if !ok || current.UID != initial.UID {
					return StopTrying("acknowledgement observer received an unrelated object")
				}
				remember(current.Annotations[replication.TriggerAnnotation])
				token := ""
				if current.Status.Trigger != nil {
					token = current.Status.Trigger.ObservedRequest
				}
				index, known := requests[token]
				if !known || index < acknowledged {
					return StopTrying("trigger acknowledgement regressed or referenced an unpublished request")
				}
				acknowledged = index
				if current.ResourceVersion == final.ResourceVersion {
					return nil
				}
			default:
			}
			return fmt.Errorf("waiting for acknowledgement observer to reach the final API snapshot")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})
	It("validates watches in controllers with policy conversion disabled", Label("config"), func() {
		ctx := context.Background()
		prefix := "watch-no-webhook-" + rand.String(6)
		owners, namespaces := watchNamespaces(prefix)
		original := &capsulev1beta2.CapsuleConfiguration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, original)).To(Succeed())
		DeferCleanup(func() {
			ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
				configuration.Spec.Admission.Mutating.Webhooks = original.Spec.Admission.Mutating.DeepCopy().Webhooks
			})
		})
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.Admission.Mutating.Webhooks = slices.DeleteFunc(configuration.Spec.Admission.Mutating.Webhooks, func(hook *admissionapi.MutatingWebhook) bool {
				return hook.Name == "replications.mutating.projectcapsule.dev"
			})
		})
		Eventually(func(g Gomega) {
			configurations := &admissionv1.MutatingWebhookConfigurationList{}
			g.Expect(k8sClient.List(ctx, configurations)).To(Succeed())
			for _, configuration := range configurations.Items {
				for _, hook := range configuration.Webhooks {
					g.Expect(hook.Name).NotTo(Equal("replications.mutating.projectcapsule.dev"))
				}
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		spec := watchBody(prefix, namespaces[0].Name, prefix+"-output")
		spec.Triggers[0].MatchConditions[0].Expression = "request.operation == 'CREATE'"
		parent := &capsulev1beta2.TenantResource{Name: prefix, Namespace: namespaces[0].Name, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: spec}}
		Expect(actor.Create(ctx, parent)).To(Succeed())
		Eventually(func(g Gomega) {
			fresh := &capsulev1beta2.TenantResource{}
			g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh)).To(Succeed())
			g.Expect(fresh.Status.ObservedGeneration).To(Equal(fresh.Generation))
			condition := fresh.Status.Conditions.GetConditionByType(meta.ReadyCondition)
			g.Expect(condition).NotTo(BeNil())
			g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(condition.Message).To(ContainSubstring("undeclared"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaces[0].Name, Name: prefix + "-output"}, &corev1.ConfigMap{}))).To(BeTrue())
		By("retaining validation when a new destination namespace is created")
		freshTenant := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: namespaces[0].Labels[meta.NewTenantLabel]}, freshTenant)).To(Succeed())
		destination := NewNamespace("", map[string]string{meta.NewTenantLabel: freshTenant.Name})
		NamespaceCreation(destination, owners[0], defaultTimeoutInterval).Should(Succeed())
		DeferCleanup(DeleteNamespaceInBackground, ctx, destination.Name)
		TenantNamespaceReady(freshTenant, destination, 3)
		bindServiceAccountToTenantResourceManager(namespaces[0].Name, "default", namespaces[1].Name)
		bindServiceAccountToTenantResourceManager(namespaces[0].Name, "default", destination.Name)
		Consistently(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: destination.Name, Name: prefix + "-output"}, &corev1.ConfigMap{}))
		}, 2*time.Second, 200*time.Millisecond).Should(BeTrue())

		other := &capsulev1beta2.TenantResource{Name: prefix, Namespace: namespaces[2].Name, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: watchBody(prefix, namespaces[2].Name, prefix+"-other")}}
		bob := impersonationClient(owners[1].Name, withDefaultGroups(nil))
		Expect(bob.Create(ctx, other)).To(Succeed())
		waitWatchReady(other)
		Eventually(func() error {
			fresh := &capsulev1beta2.TenantResource{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
				return err
			}
			fresh.Spec.Triggers[0].MatchConditions[0].Expression = "true"
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitWatchReady(parent)
		expectConfigMapData(namespaces[0].Name, prefix+"-output", map[string]string{"value": "missing"})
		expectConfigMapData(destination.Name, prefix+"-output", map[string]string{"value": "missing"})
	})
})
