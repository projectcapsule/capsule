// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/resourcepermit"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
)

var _ = DescribeTable("ResourcePermit template apply conditions", Label("resource-permit", "resource-condition", "permit-condition", "requester"), func(global bool) {
	ctx := context.Background()
	prefix := "e2e-permit-condition-" + rand.String(6)
	var namespaces []string
	var owners []rbac.UserSpec
	for _, suffix := range []string{"-a", "-b"} {
		name := prefix + suffix
		owner := rbac.UserSpec{Name: name + "-owner", Kind: rbac.UserOwner}
		tenant := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{UserSpec: owner}}}}
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
		DeferCleanup(func() { EventuallyDeletion(tenant) })
		TenantReady(tenant, metav1.ConditionTrue, defaultTimeoutInterval)
		ns := NewNamespace(name, map[string]string{meta.TenantLabel: name})
		NamespaceCreation(ns, owner, defaultTimeoutInterval).Should(Succeed())
		DeferCleanup(func() { ForceDeleteNamespace(ctx, name) })
		NamespaceIsPartOfTenant(tenant, ns).Should(Succeed())
		namespaces = append(namespaces, name)
		owners = append(owners, owner)
	}
	selected, excluded := namespaces[0], namespaces[1]
	owner := impersonationClient(owners[0].Name, withDefaultGroups([]string{owners[0].Name}))
	ensureServiceAccount(selected, "condition-runner")
	bindServiceAccountToNamespacedResource(selected, "condition-runner", selected, []string{"configmaps"}, []string{"get", "list", "watch", "create", "update", "patch", "delete"})
	// Pruning checks namespace existence using the execution identity as well.
	bindServiceAccountToClusterResources(selected, "condition-runner", prefix, prefix, []rbacv1.PolicyRule{{
		APIGroups: []string{""}, Resources: []string{"namespaces"}, ResourceNames: []string{selected}, Verbs: []string{"get"},
	}})
	DeferCleanup(func() { EventuallyDeletion(&rbacv1.ClusterRole{Name: prefix}) })
	DeferCleanup(func() { EventuallyDeletion(&rbacv1.ClusterRoleBinding{Name: prefix}) })

	target := func(name string) runtime.RawExtension {
		return runtime.RawExtension{Object: &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: name, Data: map[string]string{"value": "rendered"}}}
	}
	resources := []apiruntime.ResourceTemplate{
		{Policy: apiruntime.ResourceTemplatePolicy{Condition: "object == null && now > timestamp('2000-01-01T00:00:00Z')"}, Targets: []runtime.RawExtension{target("conditional-target"), target("existing-remove")}},
		{Policy: apiruntime.ResourceTemplatePolicy{Condition: "false", Deletion: apiruntime.ResourceDeletionPolicyOrphan}, Targets: []runtime.RawExtension{target("skipped-target"), target("existing-orphan")}},
		{Targets: []runtime.RawExtension{target("unconditional-target")}},
	}
	approvals := resourcepermit.ApprovalSpec{Auto: true, Conditions: []string{fmt.Sprintf(
		`requestor.name == %q && %q in requestor.groups && requestor == requester && request.spec.requester.name == requester.name`, owners[0].Name, owners[0].Name,
	)}}
	var template client.Object = &capsulev1beta2.ResourcePermitTemplate{Name: prefix, Namespace: selected, Spec: capsulev1beta2.ResourcePermitTemplateSpec{
		Impersonation: &meta.LocalRFC1123ObjectReference{Name: "condition-runner"},
		Approvals:     approvals, DefaultDuration: &metav1.Duration{Duration: 5 * time.Minute}, Resources: resources,
	}}
	kind := capsulev1beta2.ResourcePermitTemplateKind
	if global {
		kind = capsulev1beta2.GlobalResourcePermitTemplateKind
		template = &capsulev1beta2.GlobalResourcePermitTemplate{Name: prefix, Spec: capsulev1beta2.GlobalResourcePermitTemplateSpec{
			Impersonation: resourcePermitServiceAccountReference(selected, "condition-runner"),
			Approvals:     approvals, DefaultDuration: &metav1.Duration{Duration: 5 * time.Minute}, Resources: resources,
			NamespaceSelectors: []selectors.NamespaceSelector{{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": selected}}}},
		}}
	}
	resourcePolicies := func(obj client.Object) []apiruntime.ResourceTemplate {
		return obj.(capsulev1beta2.ResourcePermitTemplateSource).TemplateData().Resources
	}
	By("rejecting invalid CEL on CREATE and UPDATE for this template kind")
	invalid := template.DeepCopyObject().(client.Object)
	resourcePolicies(invalid)[0].Policy.Condition = "42"
	Expect(k8sClient.Create(ctx, invalid, client.DryRunAll)).To(MatchError(ContainSubstring("spec.resources[0].policy.condition")))
	Expect(k8sClient.Create(ctx, template)).To(Succeed())
	DeferCleanup(func() { EventuallyDeletion(template) })
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(template), template)).To(Succeed())
		invalid := template.DeepCopyObject().(client.Object)
		resourcePolicies(invalid)[1].Policy.Condition = "object..invalid"
		g.Expect(k8sClient.Update(ctx, invalid, client.DryRunAll)).To(MatchError(ContainSubstring("spec.resources[1].policy.condition")))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(template), template)).To(Succeed())
	Expect(resourcePolicies(template)[1].Policy.Condition).To(Equal("false"))
	if global {
		expectGlobalResourcePermitTemplateNamespaces(ctx, template.GetName(), selected)
	}

	By("keeping the template and execution identity within the selected tenant")
	otherOwner := impersonationClient(owners[1].Name, withDefaultGroups([]string{owners[1].Name}))
	denied := &capsulev1beta2.ResourcePermit{Name: "wrong-tenant", Namespace: excluded, Spec: capsulev1beta2.ResourcePermitSpec{Template: capsulev1beta2.ResourcePermitTemplateReference{Kind: kind, Name: template.GetName()}}}
	err := otherOwner.Create(ctx, denied)
	if global {
		Expect(err).To(MatchError(ContainSubstring("is not available in namespace " + excluded)))
	} else {
		Expect(err).To(MatchError(ContainSubstring("template " + template.GetName() + " not found")))
	}
	Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(denied), &capsulev1beta2.ResourcePermit{}))).To(BeTrue())

	By("rejecting spoofed requester groups through the renamed CEL identity")
	unapproved := impersonationClient(owners[0].Name, withDefaultGroups([]string{"unapproved"}))
	spoofed := &capsulev1beta2.ResourcePermit{Name: "spoofed-requester", Namespace: selected, Spec: capsulev1beta2.ResourcePermitSpec{
		Template:  capsulev1beta2.ResourcePermitTemplateReference{Kind: kind, Name: template.GetName()},
		Requester: resourcepermit.AccessEntity{Name: owners[0].Name, Type: resourcepermit.AccessEntityTypeUser, Groups: []string{owners[0].Name}},
	}}
	err = unapproved.Create(ctx, spoofed)
	Expect(apierrors.IsForbidden(err)).To(BeTrue())
	Expect(err).To(MatchError(ContainSubstring("approval conditions not satisfied for template")))
	Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(spoofed), &capsulev1beta2.ResourcePermit{}))).To(BeTrue())

	existingVersions := map[string]string{}
	for _, name := range []string{"existing-remove", "existing-orphan"} {
		existing := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: name, Namespace: selected, Data: map[string]string{"value": "external"}}
		Expect(k8sClient.Patch(ctx, existing, client.Apply, client.FieldOwner("e2e-external-condition-owner"))).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(existing), existing)).To(Succeed())
		existingVersions[name] = existing.ResourceVersion
	}
	By("persisting the authenticated requester and using it in approval and lifecycle transitions")
	request := &capsulev1beta2.ResourcePermit{Name: "conditional-permit", Namespace: selected, Spec: capsulev1beta2.ResourcePermitSpec{
		Template:  capsulev1beta2.ResourcePermitTemplateReference{Kind: kind, Name: template.GetName()},
		Requester: resourcepermit.AccessEntity{Name: "spoofed", Groups: []string{"spoofed"}},
	}}
	Expect(owner.Create(ctx, request)).To(Succeed())
	DeferCleanup(func() { cleanupLifecycleResourcePermit(ctx, request) })
	active := waitForResourcePermitPhase(ctx, request, capsulev1beta2.ResourcePermitPhaseActive)
	Expect(active.Spec.Requester.Name).To(Equal(owners[0].Name))
	Expect(active.Spec.Requester.Type).To(Equal(resourcepermit.AccessEntityTypeUser))
	Expect(active.Spec.Requester.Groups).To(ContainElement(owners[0].Name))
	Expect(active.Spec.Requester.Groups).NotTo(ContainElement("spoofed"))
	Expect(active.Status.Transitions).NotTo(BeEmpty())
	Expect(active.Status.Transitions[0].Actor.Name).To(Equal(owners[0].Name))
	Expect(active.Status.Request.Approvals.Conditions).To(Equal(approvals.Conditions), "legacy expressions must remain usable without rewriting the approval snapshot")
	Expect(active.Status.ProcessedItems).To(HaveLen(5))
	for _, item := range active.Status.ProcessedItems {
		switch item.Name {
		case "conditional-target", "unconditional-target":
			Expect(item.LastApply.IsZero()).To(BeFalse())
			Expect(item.Created).To(BeTrue())
		default:
			Expect(item.Message).To(Equal(ssa.ConditionNotMet))
			Expect(item.LastApply.IsZero()).To(BeTrue())
			Expect(item.Created).To(BeFalse())
		}
	}
	for _, name := range []string{"conditional-target", "unconditional-target"} {
		expectConfigMapData(selected, name, map[string]string{"value": "rendered"})
	}

	By("keeping conditions and rendered targets frozen after template edits")
	snapshot := active.Status.Request.DeepCopy()
	Eventually(func() error {
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(template), template); err != nil {
			return err
		}
		resourcePolicies(template)[1].Policy.Condition = "true"
		return k8sClient.Update(ctx, template)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	Consistently(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(request), request)).To(Succeed())
		g.Expect(request.Status.Request).To(Equal(snapshot))
		g.Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: selected, Name: "skipped-target"}, &corev1.ConfigMap{}))).To(BeTrue())
		for _, name := range []string{"conditional-target", "unconditional-target", "skipped-target"} {
			g.Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: excluded, Name: name}, &corev1.ConfigMap{}))).To(BeTrue())
		}
	}, 5*time.Second, time.Second).Should(Succeed())

	By("pruning applied targets and making no writes to skipped targets on expiration")
	expireActiveResourcePermit(ctx, request)
	Eventually(func() bool {
		return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(request), &capsulev1beta2.ResourcePermit{}))
	}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	for _, name := range []string{"conditional-target", "unconditional-target", "skipped-target"} {
		expectConfigMapAbsent(selected, name)
	}
	for name, version := range existingVersions {
		actual := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: selected, Name: name}, actual)).To(Succeed())
		Expect(actual.ResourceVersion).To(Equal(version), "skipped target was written during apply or cleanup")
		Expect(actual.Data).To(Equal(map[string]string{"value": "external"}))
	}

	By("reporting runtime CEL errors during preflight without persisting any targets")
	Eventually(func() error {
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(template), template); err != nil {
			return err
		}
		resourcePolicies(template)[0].Policy.Condition = "object.data.missing == true"
		return k8sClient.Update(ctx, template)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	failed := &capsulev1beta2.ResourcePermit{Name: "condition-error", Namespace: selected, Spec: request.Spec}
	Expect(owner.Create(ctx, failed)).To(Succeed())
	DeferCleanup(func() { cleanupLifecycleResourcePermit(ctx, failed) })
	failed = waitForResourcePermitPhase(ctx, failed, capsulev1beta2.ResourcePermitPhaseFailed)
	Expect(failed.Status.Failure).NotTo(BeNil())
	Expect(failed.Status.Failure.Stage).To(Equal(capsulev1beta2.ResourcePermitFailureStagePreflight))
	Expect(failed.Status.Failure.Reason).To(Equal("ResourceDryRunFailed"))
	Expect(failed.Status.Failure.Message).To(ContainSubstring("ConditionEvaluationFailed"))
	for _, name := range []string{"conditional-target", "unconditional-target", "skipped-target"} {
		expectConfigMapAbsent(selected, name)
	}
},
	Entry("namespaced ResourcePermitTemplate", false),
	Entry("GlobalResourcePermitTemplate", true),
)
