// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
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
)

var _ = Describe("ResourcePermit namespace cleanup", Label("resource-permit", "protection-termination"), func() {
	It("allows deletion in a terminating namespace while preserving protection in another tenant", func() {
		ctx := context.Background()
		prefix := "e2e-permit-termination-" + rand.String(6)
		var namespaces []string
		var actors []client.Client
		for _, suffix := range []string{"-a", "-b"} {
			name := prefix + suffix
			owner := rbac.UserSpec{Name: name + "-owner", Kind: rbac.UserOwner}
			tnt := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{UserSpec: owner}}}}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(func() { EventuallyDeletion(tnt) })
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			ns := NewNamespace(name, map[string]string{meta.TenantLabel: name, "e2e-permit-termination": prefix})
			NamespaceCreation(ns, owner, defaultTimeoutInterval).Should(Succeed())
			DeferCleanup(func() { ForceDeleteNamespace(ctx, name) })
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
			namespaces = append(namespaces, name)
			actors = append(actors, impersonationClient(owner.Name, withDefaultGroups(nil)))
		}
		template := &capsulev1beta2.GlobalResourcePermitTemplate{Name: prefix, Spec: capsulev1beta2.GlobalResourcePermitTemplateSpec{
			Impersonation:      resourcePermitServiceAccountReference(ControllerNamespace, ControllerServiceAccount),
			Approvals:          resourcepermit.ApprovalSpec{Auto: true},
			NamespaceSelectors: []selectors.NamespaceSelector{{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"e2e-permit-termination": prefix}}}},
			Resources: []apiruntime.ResourceTemplate{{Targets: []runtime.RawExtension{{Object: &corev1.ConfigMap{
				APIVersion: "v1", Kind: "ConfigMap", Name: "protected", Finalizers: []string{"e2e.projectcapsule.dev/hold"}, Data: map[string]string{"value": "managed"},
			}}}}},
		}}
		Expect(k8sClient.Create(ctx, template)).To(Succeed())
		DeferCleanup(func() { EventuallyDeletion(template) })
		expectGlobalResourcePermitTemplateNamespaces(ctx, template.Name, namespaces...)
		controller := impersonationClient(ControllerServiceAccountFull, serviceAccountGroups(ControllerNamespace))
		clearFinalizer := func(namespace string) {
			Eventually(func() error {
				target := &corev1.ConfigMap{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "protected"}, target); err != nil {
					return client.IgnoreNotFound(err)
				}
				before := target.DeepCopy()
				target.Finalizers = nil
				return controller.Patch(ctx, target, client.MergeFrom(before))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		for i, namespace := range namespaces {
			permit := &capsulev1beta2.ResourcePermit{Name: "cleanup", Namespace: namespace, Spec: capsulev1beta2.ResourcePermitSpec{Template: globalResourcePermitTemplateReference(template.Name)}}
			Expect(actors[i].Create(ctx, permit)).To(Succeed())
			DeferCleanup(func() { cleanupLifecycleResourcePermit(ctx, permit) })
			DeferCleanup(func() { clearFinalizer(namespace) })
			waitForResourcePermitPhase(ctx, permit, capsulev1beta2.ResourcePermitPhaseActive)
			Expect(actors[i].Delete(ctx, &corev1.ConfigMap{Name: "protected", Namespace: namespace}, client.DryRunAll)).To(MatchError(ContainSubstring("resources protected by a ResourcePermit")))
		}

		By("allowing namespace cleanup without permitting content updates")
		release := holdNamespaceTerminating(ctx, namespaces[0])
		DeferCleanup(release)
		Expect(actors[0].Delete(ctx, &corev1.ConfigMap{Name: "protected", Namespace: namespaces[0]}, client.DryRunAll)).To(Succeed())
		target := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaces[0], Name: "protected"}, target)).To(Succeed())
		target.Data["value"] = "unauthorized"
		Expect(actors[0].Update(ctx, target)).To(MatchError(ContainSubstring("resources protected by a ResourcePermit")))
		expectConfigMapData(namespaces[0], target.Name, map[string]string{"value": "managed"})
		Expect(actors[1].Delete(ctx, &corev1.ConfigMap{Name: "protected", Namespace: namespaces[1]}, client.DryRunAll)).To(MatchError(ContainSubstring("resources protected by a ResourcePermit")))
		expectConfigMapData(namespaces[1], "protected", map[string]string{"value": "managed"})
		clearFinalizer(namespaces[0])
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaces[0], Name: "protected"}, &corev1.ConfigMap{}))
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	})
})
