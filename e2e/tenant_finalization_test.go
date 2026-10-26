// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("Tenant finalization", Label("tenant", "termination", "tenant-finalization"), func() {
	DescribeTable("cleans blocked resources and finishes namespace deletion after the owning Tenant disappears", func(recreate bool) {
		ctx := context.Background()
		admin := clusterAdminClient()
		name := "e2e-orphan-" + rand.String(6)
		tenantA := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Name: name, Kind: rbac.UserOwner}}}}
		tenantB := &capsulev1beta2.Tenant{Name: name + "-other", Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Name: name + "-other", Kind: rbac.UserOwner}}}}
		for _, tnt := range []*capsulev1beta2.Tenant{tenantA, tenantB} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(func() { EventuallyDeletion(tnt) })
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
		foreign := NewNamespace(name+"-foreign", map[string]string{meta.TenantLabel: tenantB.Name})
		NamespaceCreation(foreign, tenantB.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceReady(tenantB, foreign, 1)
		held := NewNamespace(name+"-held", map[string]string{meta.TenantLabel: tenantA.Name})
		hold := corev1.FinalizerName("e2e.projectcapsule.dev/hold-namespace")
		metadataHold := "e2e.projectcapsule.dev/hold-namespace-metadata"
		held.Finalizers = []string{metadataHold}
		held.Spec.Finalizers = []corev1.FinalizerName{corev1.FinalizerKubernetes, hold}
		NamespaceCreation(held, tenantA.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceReady(tenantA, held, 1)
		actor := impersonationClient(tenantA.Spec.Owners[0].Name, withDefaultGroups(nil))
		blocked := &corev1.ConfigMap{Name: "blocked", Namespace: held.Name,
			Finalizers: []string{"e2e.projectcapsule.dev/hold-content"}, Data: map[string]string{"tenant": tenantA.Name},
		}
		Expect(actor.Create(ctx, blocked)).To(Succeed())
		preserved := &corev1.ConfigMap{Name: "blocked", Namespace: foreign.Name, Data: map[string]string{"tenant": tenantB.Name}}
		Expect(impersonationClient(tenantB.Spec.Owners[0].Name, withDefaultGroups(nil)).Create(ctx, preserved)).To(Succeed())
		err := actor.Delete(ctx, preserved)
		Expect(apierrors.IsForbidden(err)).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring(`cannot delete resource "configmaps"`)))
		release := func() {
			Eventually(func() error {
				current, err := admin.CoreV1().Namespaces().Get(ctx, held.Name, metav1.GetOptions{})
				if apierrors.IsNotFound(err) {
					return nil
				}
				if err != nil {
					return err
				}
				if controllerutil.RemoveFinalizer(current, metadataHold) {
					if err := k8sClient.Update(ctx, current); err != nil {
						return err
					}
				}
				remaining := []corev1.FinalizerName{}
				for _, finalizer := range current.Spec.Finalizers {
					if finalizer != hold {
						remaining = append(remaining, finalizer)
					}
				}
				current.Spec.Finalizers = remaining
				_, err = admin.CoreV1().Namespaces().Finalize(ctx, current, metav1.UpdateOptions{})
				return err
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		DeferCleanup(release)
		Expect(k8sClient.Delete(ctx, tenantA)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(held), held)).To(Succeed())
			g.Expect(held.DeletionTimestamp).NotTo(BeNil())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("reproducing the state of a namespace that persisted after its Tenant's final scan")
		// Admission and namespace persistence are separate from Tenant finalization.
		// Deliberately remove the deleting Tenant's finalizer as administrator to
		// reproduce that ordering deterministically, without timing-dependent sleeps
		// or changing webhook configuration shared with parallel tests.
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tenantA), current); err != nil {
				return client.IgnoreNotFound(err)
			}
			controllerutil.RemoveFinalizer(current, meta.ControllerFinalizer)
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenantA), &capsulev1beta2.Tenant{}))
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		var replacement *capsulev1beta2.Tenant
		var replacementNamespace *corev1.Namespace
		var replacementContent *corev1.ConfigMap
		if recreate {
			By("recreating the Tenant name with a different UID")
			replacement = &capsulev1beta2.Tenant{Name: tenantA.Name, Labels: map[string]string{"env": "e2e"}, Spec: tenantA.Spec}
			Expect(k8sClient.Create(ctx, replacement)).To(Succeed())
			DeferCleanup(func() { EventuallyDeletion(replacement) })
			Expect(replacement.UID).NotTo(Equal(tenantA.UID))
			TenantReady(replacement, metav1.ConditionTrue, defaultTimeoutInterval)
			replacementNamespace = NewNamespace(name+"-replacement", map[string]string{meta.TenantLabel: replacement.Name})
			NamespaceCreation(replacementNamespace, replacement.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			DeferCleanup(func() { ForceDeleteNamespace(ctx, replacementNamespace.Name) })
			TenantNamespaceReady(replacement, replacementNamespace, 1)
			replacementContent = &corev1.ConfigMap{Name: blocked.Name, Namespace: replacementNamespace.Name,
				Finalizers: append([]string(nil), blocked.Finalizers...), Data: map[string]string{"tenant": "replacement"},
			}
			Expect(actor.Create(ctx, replacementContent)).To(Succeed())
		}

		By("rejecting tenant reassignment through the finalize subresource")
		Eventually(func() error {
			current, err := admin.CoreV1().Namespaces().Get(ctx, held.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			current.Labels[meta.TenantLabel] = tenantB.Name
			current.OwnerReferences = []metav1.OwnerReference{{APIVersion: capsulev1beta2.GroupVersion.String(), Kind: "Tenant", Name: tenantB.Name, UID: tenantB.UID}}
			_, err = admin.CoreV1().Namespaces().Finalize(ctx, current, metav1.UpdateOptions{})
			if err == nil {
				Fail("namespace ownership changed through the finalize subresource")
			}
			return err
		}, defaultTimeoutInterval, defaultPollInterval).Should(MatchError(ContainSubstring("namespace tenant ownership can not change during termination")))
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(held), held)).To(Succeed())
		Expect(held.Labels[meta.TenantLabel]).To(Equal(tenantA.Name))
		Expect(held.OwnerReferences).To(ContainElement(HaveField("UID", tenantA.UID)))
		Expect(held.Spec.Finalizers).To(ContainElement(hold))
		Expect(held.Finalizers).To(ContainElement(metadataHold))

		By("clearing blocked content without its former Tenant")
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(blocked), &corev1.ConfigMap{}))
		}, defaultTerminationTimeoutInterval, defaultPollInterval).Should(BeTrue())
		By("letting the namespace controller update status without its former Tenant")
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(held), held)).To(Succeed())
			g.Expect(held.Status.Phase).To(Equal(corev1.NamespaceTerminating))
			g.Expect(held.Spec.Finalizers).NotTo(ContainElement(corev1.FinalizerKubernetes))
		}, defaultTerminationTimeoutInterval, defaultPollInterval).Should(Succeed())
		release()
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(held), &corev1.Namespace{}))
		}, defaultTerminationTimeoutInterval, defaultPollInterval).Should(BeTrue())
		if replacement != nil {
			current := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(replacement), current)).To(Succeed())
			Expect(current.UID).To(Equal(replacement.UID))
			Expect(current.DeletionTimestamp).To(BeNil())
			currentContent := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(replacementContent), currentContent)).To(Succeed())
			Expect(currentContent.UID).To(Equal(replacementContent.UID))
			Expect(currentContent.Data).To(Equal(replacementContent.Data))
			Expect(currentContent.Finalizers).To(Equal(replacementContent.Finalizers))
			Expect(currentContent.DeletionTimestamp).To(BeNil())
		}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenantB), tenantB)).To(Succeed())
		Expect(tenantB.DeletionTimestamp).To(BeNil())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreign), foreign)).To(Succeed())
		Expect(foreign.DeletionTimestamp).To(BeNil())
		currentContent := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(preserved), currentContent)).To(Succeed())
		Expect(currentContent.UID).To(Equal(preserved.UID))
		Expect(currentContent.Data).To(Equal(preserved.Data))
		Expect(currentContent.DeletionTimestamp).To(BeNil())
	}, Entry("missing Tenant", false), Entry("recreated Tenant", true))

	It("retains an owner until its namespaces disappear even when namespace status is missing", func() {
		ctx := context.Background()
		admin := clusterAdminClient()
		name := "e2e-finalize-" + rand.String(6)
		tenantA := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Name: name, Kind: rbac.UserOwner}}}}
		tenantB := &capsulev1beta2.Tenant{Name: name + "-other", Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Name: name + "-other", Kind: rbac.UserOwner}}}}
		By("protecting empty Tenants in the CREATE response before any reconciliation")
		for _, tnt := range []*capsulev1beta2.Tenant{tenantA, tenantB} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(func() { EventuallyDeletion(tnt) })
			Expect(tnt.Finalizers).To(ContainElement(meta.ControllerFinalizer))
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
		held := NewNamespace(name+"-held", map[string]string{meta.TenantLabel: tenantA.Name})
		// A namespace-level spec finalizer holds the namespace itself after all
		// content is gone; Capsule's namespaced resource cleanup cannot remove it.
		hold := corev1.FinalizerName("e2e.projectcapsule.dev/hold-namespace")
		held.Spec.Finalizers = []corev1.FinalizerName{corev1.FinalizerKubernetes, hold}
		NamespaceCreation(held, tenantA.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		release := func() {
			Eventually(func() error {
				current, err := admin.CoreV1().Namespaces().Get(ctx, held.Name, metav1.GetOptions{})
				if apierrors.IsNotFound(err) {
					return nil
				}
				if err != nil {
					return err
				}
				remaining := []corev1.FinalizerName{}
				for _, finalizer := range current.Spec.Finalizers {
					if finalizer != hold {
						remaining = append(remaining, finalizer)
					}
				}
				current.Spec.Finalizers = remaining
				_, err = admin.CoreV1().Namespaces().Finalize(ctx, current, metav1.UpdateOptions{})
				return err
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		DeferCleanup(release)
		foreign := NewNamespace(name+"-foreign", map[string]string{meta.TenantLabel: tenantB.Name})
		NamespaceCreation(foreign, tenantB.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceReady(tenantA, held, 1)
		TenantNamespaceReady(tenantB, foreign, 1)

		By("deleting Tenant A while retaining its namespace")
		Expect(k8sClient.Delete(ctx, tenantA)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(held), held)).To(Succeed())
			g.Expect(held.DeletionTimestamp).NotTo(BeNil())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("recovering an incomplete status projection using actual namespace ownership")
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tenantA), current); err != nil {
				return err
			}
			current.Status.Spaces = nil
			current.Status.Namespaces = nil
			current.Status.Size = 0
			return k8sClient.Status().Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenantA), current)).To(Succeed())
			g.Expect(current.Finalizers).To(ContainElement(meta.ControllerFinalizer))
			g.Expect(current.Status.Spaces).To(HaveLen(1))
			g.Expect(current.Status.Spaces[0].UID).To(Equal(held.UID))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Consistently(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenantA), current)).To(Succeed())
			g.Expect(current.Finalizers).To(ContainElement(meta.ControllerFinalizer))
		}, 5*time.Second, defaultPollInterval).Should(Succeed())

		By("rejecting new namespace assignment to the terminating Tenant")
		late := NewNamespace(name+"-late", map[string]string{meta.TenantLabel: tenantA.Name})
		_, err := ownerClient(tenantA.Spec.Owners[0].UserSpec).CoreV1().Namespaces().Create(ctx, late, metav1.CreateOptions{})
		Expect(err).To(MatchError(ContainSubstring("tenant is terminating and does not accept new namespaces")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(late), &corev1.Namespace{}))).To(BeTrue())

		By("preserving Tenant B and its namespace during Tenant A cleanup")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenantB), tenantB)).To(Succeed())
		Expect(tenantB.DeletionTimestamp).To(BeNil())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreign), foreign)).To(Succeed())
		Expect(foreign.DeletionTimestamp).To(BeNil())

		By("releasing the Tenant only after its last namespace has disappeared")
		release()
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(held), &corev1.Namespace{}))
		}, defaultTerminationTimeoutInterval, defaultPollInterval).Should(BeTrue())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenantA), &capsulev1beta2.Tenant{}))
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	})
})
