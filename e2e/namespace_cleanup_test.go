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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

var _ = Describe("namespace cleanup and provisioning", Label("namespace-cleanup", "termination", "rolebindings"), func() {
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
		cleanupRole := &rbacv1.ClusterRole{
			Name: name, Labels: map[string]string{"projectcapsule.dev/aggregate-to-controller": "true"},
			Rules: []rbacv1.PolicyRule{{APIGroups: []string{group}, Resources: []string{"cleanupitems"}, Verbs: []string{"get", "list", "delete", "patch"}}},
		}
		Expect(k8sClient.Create(ctx, cleanupRole)).To(Succeed())
		DeferCleanup(func() { EventuallyDeletion(cleanupRole) })
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
