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
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiRuntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

var _ = Describe("namespace cleanup and provisioning", Label("namespace-cleanup", "termination", "rolebindings"), func() {
	It("completes finalizer updates while preserving metadata enforcement and tenant isolation", Label("termination-metadata"), func() {
		ctx := context.Background()
		name := "e2e-finalizer-metadata-" + rand.String(6)
		const blockedLabel = "e2e.projectcapsule.dev/blocked"
		tenantA := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{{Name: name, Kind: rbac.UserOwner}},
			Rules: []*rules.NamespaceRuleBodyTenant{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "guarded"}},
				NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
					Action: rules.ActionTypeDeny,
					Metadata: []rules.MetadataRule{{APIGroups: []string{"v1"}, Kinds: []string{"ConfigMap"},
						Labels: map[string]rules.MetadataValueRule{blockedLabel: {Values: []apiRuntime.ExpressionMatch{{Exact: []string{"true"}}}}},
					}},
				}},
			}},
		}}
		tenantB := &capsulev1beta2.Tenant{Name: name + "-other", Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{{Name: name + "-other", Kind: rbac.UserOwner}},
		}}
		for _, tnt := range []*capsulev1beta2.Tenant{tenantA, tenantB} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(func() { EventuallyDeletion(tnt) })
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
		createNamespace := func(tnt *capsulev1beta2.Tenant, suffix string, labels map[string]string, size uint) *corev1.Namespace {
			labels[meta.TenantLabel] = tnt.Name
			ns := NewNamespace(name+suffix, labels)
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			DeferCleanup(func() { ForceDeleteNamespace(ctx, ns.Name) })
			TenantNamespaceReady(tnt, ns, size)
			return ns
		}
		selected := createNamespace(tenantA, "-selected", map[string]string{"profile": "guarded"}, 1)
		plain := createNamespace(tenantA, "-plain", map[string]string{}, 2)
		foreign := createNamespace(tenantB, "-foreign", map[string]string{}, 1)
		actor := impersonationClient(tenantA.Spec.Owners[0].Name, withDefaultGroups(nil))
		otherActor := impersonationClient(tenantB.Spec.Owners[0].Name, withDefaultGroups(nil))
		create := func(c client.Client, ns *corev1.Namespace, blocked bool) *corev1.ConfigMap {
			labels := map[string]string{"env": "e2e"}
			if blocked {
				labels[blockedLabel] = "true"
			}
			cm := &corev1.ConfigMap{Name: "held", Namespace: ns.Name, Labels: labels,
				Finalizers: []string{"e2e.projectcapsule.dev/hold-object"}, Data: map[string]string{"tenant": ns.Name},
			}
			Expect(c.Create(ctx, cm)).To(Succeed())
			return cm
		}
		held := create(actor, selected, false)
		unselected := create(actor, plain, true)
		preserved := create(otherActor, foreign, true)

		By("maintaining namespace ownership labels on ordinary updates")
		Eventually(func() error {
			current := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(unselected), current); err != nil {
				return err
			}
			current.Labels[meta.NewTenantLabel], current.Labels[meta.ManagedByCapsuleLabel] = tenantB.Name, tenantB.Name
			return actor.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &corev1.ConfigMap{}
			g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(unselected), current)).To(Succeed())
			g.Expect(current.Labels[meta.NewTenantLabel]).To(Equal(tenantA.Name))
			g.Expect(current.Labels[meta.ManagedByCapsuleLabel]).To(Equal(tenantA.Name))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("enforcing selected namespace rules during finalizer removal")
		Expect(actor.Delete(ctx, held)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(held), held)).To(Succeed())
			g.Expect(held.DeletionTimestamp).NotTo(BeNil())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		var rejected error
		Eventually(func() error {
			current := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(held), current); err != nil {
				return err
			}
			current.Finalizers = nil
			current.Labels[blockedLabel] = "true"
			rejected = actor.Update(ctx, current)
			if rejected == nil {
				Fail("metadata enforcement was bypassed while removing a finalizer")
			}
			return rejected
		}, defaultTimeoutInterval, defaultPollInterval).Should(MatchError(ContainSubstring(`metadata label "true" at metadata.labels["e2e.projectcapsule.dev/blocked"] is denied by namespace rule`)))
		Expect(apierrors.IsForbidden(rejected)).To(BeTrue())
		Expect(actor.Get(ctx, client.ObjectKeyFromObject(held), held)).To(Succeed())
		Expect(held.Labels).NotTo(HaveKey(blockedLabel))
		Expect(held.Finalizers).To(ContainElement("e2e.projectcapsule.dev/hold-object"))

		By("completing an authorized finalizer-only update")
		Eventually(func() error {
			current := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(held), current); err != nil {
				return err
			}
			current.Finalizers = nil
			return actor.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(actor.Get(ctx, client.ObjectKeyFromObject(held), &corev1.ConfigMap{}))
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())

		By("preserving the other Tenant's resources and access boundaries")
		err := actor.Delete(ctx, preserved)
		Expect(apierrors.IsForbidden(err)).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring(`cannot delete resource "configmaps"`)))
		current := &corev1.ConfigMap{}
		Expect(otherActor.Get(ctx, client.ObjectKeyFromObject(preserved), current)).To(Succeed())
		Expect(current.UID).To(Equal(preserved.UID))
		Expect(current.Data).To(Equal(preserved.Data))
		Expect(current.Labels[meta.NewTenantLabel]).To(Equal(tenantB.Name))
		Expect(current.Finalizers).To(Equal(preserved.Finalizers))
		Expect(current.DeletionTimestamp).To(BeNil())
	})

	It("cleans a batch of namespaces before releasing the Tenant and preserves another tenant", Label("termination-performance"), func() {
		ctx := context.Background()
		prefix := "e2e-termination-batch-" + rand.String(6)
		var tenants []*capsulev1beta2.Tenant
		for _, suffix := range []string{"-a", "-b"} {
			tnt := &capsulev1beta2.Tenant{Name: prefix + suffix, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{Name: prefix + suffix, Kind: rbac.UserOwner}},
			}}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(func() { EventuallyDeletion(tnt) })
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			tenants = append(tenants, tnt)
		}
		createNamespace := func(tnt *capsulev1beta2.Tenant, name string, size uint) *corev1.Namespace {
			ns := NewNamespace(name, map[string]string{meta.TenantLabel: tnt.Name})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			DeferCleanup(func() { ForceDeleteNamespace(ctx, ns.Name) })
			TenantNamespaceReady(tnt, ns, size)
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(ns), ns)).To(Succeed())
			return ns
		}
		foreign := createNamespace(tenants[1], prefix+"-foreign", 1)
		preserved := &corev1.ConfigMap{Name: "preserved", Namespace: foreign.Name, Data: map[string]string{"tenant": tenants[1].Name}}
		Expect(k8sClient.Create(ctx, preserved)).To(Succeed())
		actor := impersonationClient(tenants[0].Spec.Owners[0].Name, withDefaultGroups(nil))
		err := actor.Delete(ctx, preserved)
		Expect(apierrors.IsForbidden(err)).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring(`cannot delete resource "configmaps"`)))

		const hold = "e2e.projectcapsule.dev/hold-batch"
		var namespaces []*corev1.Namespace
		for i := range 4 {
			ns := createNamespace(tenants[0], fmt.Sprintf("%s-%d", prefix, i), uint(i+1))
			Eventually(func() error {
				current := &corev1.Namespace{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ns), current); err != nil {
					return err
				}
				if !controllerutil.AddFinalizer(current, hold) {
					return nil
				}
				return k8sClient.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			for j := range 64 {
				cm := &corev1.ConfigMap{Name: fmt.Sprintf("held-%d", j), Namespace: ns.Name,
					Labels: map[string]string{"env": "e2e", "termination-batch": prefix}, Finalizers: []string{"e2e.projectcapsule.dev/hold-content"},
					Data: map[string]string{"payload": strings.Repeat("x", 16*1024)},
				}
				Expect(actor.Create(ctx, cm)).To(Succeed())
			}
			namespaces = append(namespaces, ns)
		}
		By("deleting the Tenant with four populated namespaces")
		started := time.Now()
		Expect(k8sClient.Delete(ctx, tenants[0])).To(Succeed())
		Eventually(func(g Gomega) {
			for _, ns := range namespaces {
				list := &corev1.ConfigMapList{}
				g.Expect(k8sClient.List(ctx, list, client.InNamespace(ns.Name), client.MatchingLabels{"termination-batch": prefix})).To(Succeed())
				g.Expect(list.Items).To(BeEmpty())
			}
		}, defaultTerminationTimeoutInterval, defaultPollInterval).Should(Succeed())
		elapsed := time.Since(started)
		GinkgoWriter.Printf("Termination batch: namespaces=4 objects=256 payload=16KiB cleanup=%s\n", elapsed)
		AddReportEntry("termination-batch-cleanup", elapsed.String())

		By("retaining the Tenant until every namespace actually disappears")
		currentTenant := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), currentTenant)).To(Succeed())
		Expect(currentTenant.Finalizers).To(ContainElement(meta.ControllerFinalizer))
		for _, ns := range namespaces {
			current := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(ns), current)).To(Succeed())
			Expect(current.DeletionTimestamp).NotTo(BeNil())
			Expect(current.Finalizers).To(ContainElement(hold))
			Eventually(func() error {
				current := &corev1.Namespace{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ns), current); err != nil {
					return client.IgnoreNotFound(err)
				}
				if !controllerutil.RemoveFinalizer(current, hold) {
					return nil
				}
				return k8sClient.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		Eventually(func(g Gomega) {
			for _, ns := range namespaces {
				g.Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(ns), &corev1.Namespace{}))).To(BeTrue())
			}
			g.Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), &capsulev1beta2.Tenant{}))).To(BeTrue())
		}, defaultTerminationTimeoutInterval, defaultPollInterval).Should(Succeed())
		GinkgoWriter.Printf("Termination batch: Tenant and namespaces removed after %s\n", time.Since(started))

		By("preserving the other Tenant and its namespace contents")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[1]), currentTenant)).To(Succeed())
		Expect(currentTenant.DeletionTimestamp).To(BeNil())
		currentNamespace := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreign), currentNamespace)).To(Succeed())
		Expect(currentNamespace.UID).To(Equal(foreign.UID))
		Expect(currentNamespace.DeletionTimestamp).To(BeNil())
		currentMap := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(preserved), currentMap)).To(Succeed())
		Expect(currentMap.UID).To(Equal(preserved.UID))
		Expect(currentMap.Data).To(Equal(preserved.Data))
		Expect(currentMap.DeletionTimestamp).To(BeNil())
	})

	It("preserves namespace profiles and tenant isolation while namespaces are repeatedly recreated", func() {
		ctx := context.Background()
		name := "e2e-cleanup-" + rand.String(6)
		readerName := name + "-reader"
		By("registering a namespaced API supporting deletion and finalizer patches")
		Expect(apiextensionsv1.AddToScheme(k8sClient.Scheme())).To(Succeed())
		group := name + ".example.com"
		crd := &apiextensionsv1.CustomResourceDefinition{
			Name: "cleanupitems." + group,
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Group: group, Scope: apiextensionsv1.NamespaceScoped,
				Names: apiextensionsv1.CustomResourceDefinitionNames{Plural: "cleanupitems", Singular: "cleanupitem", Kind: "CleanupItem", ListKind: "CleanupItemList"},
				Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
					Name: "v1", Served: true, Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object"}},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, crd)).To(Succeed())
		DeferCleanup(func() { EventuallyDeletion(crd) })
		newCustomResource := func(namespace string) *unstructured.Unstructured {
			return &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": group + "/v1", "kind": "CleanupItem",
				"metadata": map[string]any{
					"name": "held", "namespace": namespace,
					"finalizers": []any{"e2e.projectcapsule.dev/cleanup"},
				},
			}}
		}
		tenantA := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{{Name: name, Kind: rbac.UserOwner}},
			Rules: []*rules.NamespaceRuleBodyTenant{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "reader"}},
				Permissions: rules.NamespaceRulePermissionBody{Bindings: []rbac.AdditionalRoleBindingsSpec{{
					ClusterRoleName: "view", Subjects: []rbacv1.Subject{{Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: readerName}},
				}}},
			}},
		}}
		tenantB := &capsulev1beta2.Tenant{Name: name + "-other", Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{{Name: name + "-other", Kind: rbac.UserOwner}},
		}}
		for _, tnt := range []*capsulev1beta2.Tenant{tenantA, tenantB} {
			EventuallyCreation(func() error { return k8sClient.Create(ctx, tnt) }).Should(Succeed())
			DeferCleanup(func() { EventuallyDeletion(tnt) })
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
		createNamespace := func(tnt *capsulev1beta2.Tenant, nsName, profile string) *corev1.Namespace {
			ns := NewNamespace(nsName, map[string]string{meta.TenantLabel: tnt.Name, "profile": profile})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(ns), ns)).To(Succeed())
			return ns
		}
		foreign := createNamespace(tenantB, name+"-foreign", "reader")
		unselected := createNamespace(tenantA, name+"-plain", "plain")
		selected := createNamespace(tenantA, name+"-selected", "reader")
		TenantNamespaceReady(tenantA, selected, 2)
		TenantNamespaceReady(tenantB, foreign, 1)
		By("checking cleanup permissions from the standalone strict controller role")
		// The suite normally aggregates admin into the controller. Use a separate
		// identity bound only to its base role so aggregation cannot hide missing verbs.
		bindings := &rbacv1.ClusterRoleBindingList{}
		Expect(k8sClient.List(ctx, bindings)).To(Succeed())
		var controllerRole string
		for _, binding := range bindings.Items {
			if !strings.HasSuffix(binding.RoleRef.Name, ":controller") {
				continue
			}
			for _, subject := range binding.Subjects {
				if subject.Kind == rbacv1.ServiceAccountKind && subject.Name == ControllerServiceAccount && subject.Namespace == ControllerNamespace {
					controllerRole = binding.RoleRef.Name
				}
			}
		}
		Expect(controllerRole).NotTo(BeEmpty(), "the e2e installation must use strict or minimal RBAC")
		baseRole := &rbacv1.ClusterRole{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: controllerRole}, baseRole)).To(Succeed())
		Expect(baseRole.AggregationRule).To(BeNil())
		cleanupIdentity := name + "-cleanup-rbac"
		cleanupBinding := &rbacv1.ClusterRoleBinding{
			Name:     cleanupIdentity,
			RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: controllerRole},
			Subjects: []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: cleanupIdentity}},
		}
		Expect(k8sClient.Create(ctx, cleanupBinding)).To(Succeed())
		DeferCleanup(func() { EventuallyDeletion(cleanupBinding) })
		Eventually(func(g Gomega) {
			for _, resource := range []struct{ group, name string }{{"", "configmaps"}, {"", "secrets"}, {group, "cleanupitems"}} {
				for _, verb := range []string{"get", "list", "delete", "patch", "create", "update"} {
					review := &authorizationv1.SubjectAccessReview{Spec: authorizationv1.SubjectAccessReviewSpec{
						User: cleanupIdentity, Groups: []string{"system:authenticated"},
						ResourceAttributes: &authorizationv1.ResourceAttributes{Namespace: selected.Name, Group: resource.group, Resource: resource.name, Verb: verb},
					}}
					g.Expect(k8sClient.Create(ctx, review)).To(Succeed())
					g.Expect(review.Status.EvaluationError).To(BeEmpty())
					g.Expect(review.Status.Allowed).To(Equal(verb != "create" && verb != "update"), "%s on %s/%s: %+v", verb, resource.group, resource.name, review.Status)
				}
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		reader := impersonationClientSet(readerName, []string{"system:authenticated"})
		owner := ownerClient(tenantA.Spec.Owners[0].UserSpec)
		assertProfiles := func() {
			Eventually(func() error {
				_, err := reader.CoreV1().ConfigMaps(selected.Name).List(ctx, metav1.ListOptions{})
				return err
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			for _, ns := range []*corev1.Namespace{unselected, foreign} {
				_, err := reader.CoreV1().ConfigMaps(ns.Name).List(ctx, metav1.ListOptions{})
				Expect(apierrors.IsForbidden(err)).To(BeTrue(), "profile reader must not access %s: %v", ns.Name, err)
			}
			_, err := owner.CoreV1().ConfigMaps(foreign.Name).Create(ctx, &corev1.ConfigMap{Name: "unauthorized"}, metav1.CreateOptions{})
			Expect(apierrors.IsForbidden(err)).To(BeTrue(), "tenant A owner must not write into tenant B: %v", err)
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: foreign.Name, Name: "unauthorized"}, &corev1.ConfigMap{}))).To(BeTrue())
		}
		assertProfiles()
		foreignObject := &corev1.ConfigMap{Name: "preserved", Namespace: foreign.Name, Finalizers: []string{"e2e.projectcapsule.dev/preserve"}}
		Expect(k8sClient.Create(ctx, foreignObject)).To(Succeed())
		foreignSecret := &corev1.Secret{Name: "preserved", Namespace: foreign.Name, Finalizers: []string{"e2e.projectcapsule.dev/preserve"}}
		Expect(k8sClient.Create(ctx, foreignSecret)).To(Succeed())
		foreignCustomResource := newCustomResource(foreign.Name)
		EventuallyCreation(func() error { return k8sClient.Create(ctx, foreignCustomResource) }).Should(Succeed())
		var previousUID string
		for cycle := 0; cycle < 3; cycle++ {
			By(fmt.Sprintf("recreating a namespace, cycle %d", cycle+1))
			churn := createNamespace(tenantA, name+"-churn", "reader")
			Expect(string(churn.UID)).NotTo(Equal(previousUID))
			previousUID = string(churn.UID)
			TenantNamespaceReady(tenantA, churn, uint(3+cycle))
			held := &corev1.ConfigMap{Name: "held", Namespace: churn.Name, Finalizers: []string{"e2e.projectcapsule.dev/cleanup"}}
			EventuallyCreation(func() error {
				_, err := owner.CoreV1().ConfigMaps(churn.Name).Create(ctx, held, metav1.CreateOptions{})
				return err
			}).Should(Succeed())
			heldSecret := &corev1.Secret{Name: "held", Namespace: churn.Name, Finalizers: []string{"e2e.projectcapsule.dev/cleanup"}}
			Expect(k8sClient.Create(ctx, heldSecret)).To(Succeed())
			heldCustomResource := newCustomResource(churn.Name)
			EventuallyCreation(func() error { return k8sClient.Create(ctx, heldCustomResource) }).Should(Succeed())
			release := holdNamespaceTerminating(ctx, churn.Name)
			DeferCleanup(release)
			start := time.Now()
			fresh := createNamespace(tenantA, fmt.Sprintf("%s-fresh-%d", name, cycle), "plain")
			EventuallyCreation(func() error {
				_, err := owner.CoreV1().ConfigMaps(fresh.Name).Create(ctx, &corev1.ConfigMap{Name: "owner-write"}, metav1.CreateOptions{})
				return err
			}).Should(Succeed())
			GinkgoWriter.Printf("Namespace %s owner permissions usable after %s while %s is held terminating\n", fresh.Name, time.Since(start), churn.Name)
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(held), &corev1.ConfigMap{}))
			}, defaultTerminationTimeoutInterval, defaultPollInterval).Should(BeTrue(), "cleanup must remove the resource finalizer")
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(heldSecret), &corev1.Secret{}))
			}, defaultTerminationTimeoutInterval, defaultPollInterval).Should(BeTrue(), "cleanup must remove Secret finalizers without aggregated admin privileges")
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(heldCustomResource), newCustomResource(churn.Name)))
			}, defaultTerminationTimeoutInterval, defaultPollInterval).Should(BeTrue(), "discovery must retain custom APIs supporting all cleanup verbs")
			current := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(churn), current)).To(Succeed())
			Expect(current.DeletionTimestamp).NotTo(BeNil(), "namespace must still be held while new permissions are installed")
			assertProfiles()
			preserved := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreignObject), preserved)).To(Succeed())
			Expect(preserved.UID).To(Equal(foreignObject.UID))
			Expect(preserved.Finalizers).To(Equal(foreignObject.Finalizers))
			Expect(preserved.DeletionTimestamp).To(BeNil())
			preservedSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreignSecret), preservedSecret)).To(Succeed())
			Expect(preservedSecret.UID).To(Equal(foreignSecret.UID))
			Expect(preservedSecret.Finalizers).To(Equal(foreignSecret.Finalizers))
			Expect(preservedSecret.DeletionTimestamp).To(BeNil())
			preservedCustomResource := newCustomResource(foreign.Name)
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(foreignCustomResource), preservedCustomResource)).To(Succeed())
			Expect(preservedCustomResource.GetUID()).To(Equal(foreignCustomResource.GetUID()))
			Expect(preservedCustomResource.GetFinalizers()).To(Equal(foreignCustomResource.GetFinalizers()))
			Expect(preservedCustomResource.GetDeletionTimestamp()).To(BeNil())
			release()
			EventuallyDeletion(churn)
		}
		By("updating the selected namespace profile after cleanup")
		Eventually(func() error {
			current := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(selected), current); err != nil {
				return err
			}
			current.Labels["profile"] = "plain"
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func() bool {
			_, err := reader.CoreV1().ConfigMaps(selected.Name).List(ctx, metav1.ListOptions{})
			return apierrors.IsForbidden(err)
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	})
})
