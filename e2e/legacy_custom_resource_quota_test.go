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
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	capsuletenant "github.com/projectcapsule/capsule/pkg/tenant"
)

var _ = Describe("legacy quota during namespace provisioning", Label("resourcequota", "namespace-rbac-priority"), func() {
	It("enforces quotas from the first authorized request and isolates reservations across tenants", func() {
		ctx := context.Background()
		Expect(apiextensionsv1.AddToScheme(k8sClient.Scheme())).To(Succeed())
		name := "e2e-early-quota-" + rand.String(6)
		group := name + ".test.capsule.dev"
		kgv := "widgets." + group + "_v1"
		crd := &apiextensionsv1.CustomResourceDefinition{Name: "widgets." + group, Labels: map[string]string{"env": "e2e"}, Spec: apiextensionsv1.CustomResourceDefinitionSpec{Group: group, Scope: apiextensionsv1.NamespaceScoped, Names: apiextensionsv1.CustomResourceDefinitionNames{Plural: "widgets", Singular: "widget", Kind: "Widget", ListKind: "WidgetList"}, Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{Name: "v1", Served: true, Storage: true, Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object"}}}}}}
		Expect(k8sClient.Create(ctx, crd)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, crd))).To(Succeed()) })
		// Kubernetes' built-in admin role needs this CRD's permissions aggregated
		// before a real Tenant owner can exercise its admission policy.
		role := &rbacv1.ClusterRole{Name: name, Labels: map[string]string{"env": "e2e", "rbac.authorization.k8s.io/aggregate-to-admin": "true"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{group}, Resources: []string{"widgets"}, Verbs: []string{"create", "get", "list", "update", "delete"}}}}
		Expect(k8sClient.Create(ctx, role)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, role))).To(Succeed()) })
		Eventually(func(g Gomega) {
			current := &apiextensionsv1.CustomResourceDefinition{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(crd), current)).To(Succeed())
			g.Expect(current.Status.Conditions).To(ContainElement(And(HaveField("Type", apiextensionsv1.Established), HaveField("Status", apiextensionsv1.ConditionTrue))))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		a := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Annotations: map[string]string{capsulev1beta2.LimitAnnotationForResource(kgv): "0"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: name}}}}
		b := &capsulev1beta2.Tenant{Name: name + "-other", Labels: map[string]string{"env": "e2e"}, Annotations: map[string]string{capsulev1beta2.LimitAnnotationForResource(kgv): "1"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: name + "-other"}}}}
		for _, tnt := range []*capsulev1beta2.Tenant{a, b} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
		createNamespace := func(tnt *capsulev1beta2.Tenant) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			return ns
		}
		// Do not wait for namespace membership or namespace Ready before the first
		// write: the fast permission queue may already have granted owner access.
		deniedNS, allowedNS := createNamespace(a), createNamespace(b)
		ownerDynamic := func(tnt *capsulev1beta2.Tenant) dynamic.Interface {
			config := rest.CopyConfig(cfg)
			config.Impersonate = rest.ImpersonationConfig{UserName: tnt.Spec.Owners[0].Name, Groups: withDefaultGroups(nil)}
			return dynamic.NewForConfigOrDie(config)
		}
		aClient, bClient := ownerDynamic(a), ownerDynamic(b)
		resource := schema.GroupVersionResource{Group: group, Version: "v1", Resource: "widgets"}
		object := func(name string) *unstructured.Unstructured {
			return &unstructured.Unstructured{Object: map[string]any{"apiVersion": group + "/v1", "kind": "Widget", "metadata": map[string]any{"name": name}}}
		}
		denial := "has reached quota limit of 0 items"
		Eventually(func() error {
			_, err := aClient.Resource(resource).Namespace(deniedNS.Name).Create(ctx, object("denied-first"), metav1.CreateOptions{})
			if err == nil {
				StopTrying("zero-quota resource was admitted before namespace provisioning completed").Now()
			}
			if strings.Contains(err.Error(), denial) {
				return nil
			}
			if !apierrors.IsForbidden(err) {
				StopTrying("unexpected admission failure").Wrap(err).Now()
			}
			return err // Retry only the RBAC propagation window, never a successful write.
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func() error {
			_, err := bClient.Resource(resource).Namespace(allowedNS.Name).Create(ctx, object("dry-run"), metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
			return err
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func() error {
			_, err := bClient.Resource(resource).Namespace(allowedNS.Name).Create(ctx, object("allowed-first"), metav1.CreateOptions{})
			return err
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		_, err := bClient.Resource(resource).Namespace(allowedNS.Name).Get(ctx, "allowed-first", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(bClient.Resource(resource).Namespace(allowedNS.Name).Delete(ctx, "allowed-first", metav1.DeleteOptions{DryRun: []string{metav1.DryRunAll}})).To(Succeed())
		_, err = bClient.Resource(resource).Namespace(allowedNS.Name).Create(ctx, object("denied-second"), metav1.CreateOptions{})
		Expect(err).To(MatchError(ContainSubstring("has reached quota limit of 1 items")))
		_, err = aClient.Resource(resource).Namespace(allowedNS.Name).Create(ctx, object("cross-tenant"), metav1.CreateOptions{})
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "tenant A must not reserve tenant B's quota: %v", err)
		admin := dynamic.NewForConfigOrDie(cfg)
		for _, entry := range []struct{ namespace, name string }{{deniedNS.Name, "denied-first"}, {allowedNS.Name, "denied-second"}, {allowedNS.Name, "cross-tenant"}, {allowedNS.Name, "dry-run"}} {
			_, err := admin.Resource(resource).Namespace(entry.namespace).Get(ctx, entry.name, metav1.GetOptions{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "denied resource persisted: %s/%s: %v", entry.namespace, entry.name, err)
		}
		Expect(bClient.Resource(resource).Namespace(allowedNS.Name).Delete(ctx, "allowed-first", metav1.DeleteOptions{})).To(Succeed())
		Eventually(func() error {
			_, err := bClient.Resource(resource).Namespace(allowedNS.Name).Create(ctx, object("after-delete"), metav1.CreateOptions{})
			return err
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			for _, entry := range []struct {
				tenant *capsulev1beta2.Tenant
				used   int64
			}{{a, 0}, {b, 1}} {
				current := &capsulev1beta2.Tenant{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(entry.tenant), current)).To(Succeed())
				used, err := capsulev1beta2.GetUsedResourceFromTenant(*current, kgv)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(used).To(Equal(entry.used), fmt.Sprintf("usage for %s", current.Name))
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("retaining pending admission reservations through background recounts")
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(b), current)).To(Succeed())
			state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(state.Pending[kgv]).To(BeEmpty(), "persisted objects must not be counted twice")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(bClient.Resource(resource).Namespace(allowedNS.Name).Delete(ctx, "after-delete", metav1.DeleteOptions{})).To(Succeed())
		Eventually(func(g Gomega) {
			_, err := admin.Resource(resource).Namespace(allowedNS.Name).Get(ctx, "after-delete", metav1.GetOptions{})
			g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		// Model a create accepted by another webhook replica whose object has
		// not yet committed. A real Tenant update drives its normal recount.
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(b), current)).To(Succeed())
			ns := &corev1.Namespace{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(allowedNS), ns)).To(Succeed())
			now := metav1.Now()
			expiry := metav1.NewTime(now.Add(2 * time.Minute))
			state, err := capsuletenant.ReadLegacyQuotaReservations(nil)
			g.Expect(err).NotTo(HaveOccurred())
			state.Pending[kgv] = []capsuletenant.LegacyQuotaReservation{{NamespaceUID: ns.UID, ID: "held-create", CreatedAt: now, UpdatedAt: now, ExpiresAt: &expiry, ObjectRef: capsulev1beta2.QuantityLedgerObjectRef{Namespace: ns.Name, Name: "held"}}}
			state.Namespaces[ns.Name] = ns.UID
			g.Expect(capsuletenant.WriteLegacyQuotaReservations(current.Annotations, state)).To(Succeed())
			current.Annotations[capsulev1beta2.UsedAnnotationForResource(kgv)] = "1"
			g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		// A completed recount prunes the namespace pin once membership is
		// published, while retaining the outstanding request reservation.
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(b), current)).To(Succeed())
			state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(state.Pending[kgv]).To(HaveLen(1))
			g.Expect(state.Namespaces).NotTo(HaveKey(allowedNS.Name))
			g.Expect(current.Annotations[capsulev1beta2.UsedAnnotationForResource(kgv)]).To(Equal("1"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		_, err = bClient.Resource(resource).Namespace(allowedNS.Name).Create(ctx, object("blocked-by-pending"), metav1.CreateOptions{})
		Expect(err).To(MatchError(ContainSubstring("has reached quota limit of 1 items")))
		_, err = admin.Resource(resource).Namespace(allowedNS.Name).Get(ctx, "blocked-by-pending", metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
		By("releasing abandoned reservations at expiry without blocking permission provisioning")
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(b), current)).To(Succeed())
			state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(state.Pending[kgv]).To(HaveLen(1))
			expired := metav1.NewTime(time.Now().Add(-time.Minute))
			state.Pending[kgv][0].ExpiresAt = &expired
			g.Expect(capsuletenant.WriteLegacyQuotaReservations(current.Annotations, state)).To(Succeed())
			g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(b), current)).To(Succeed())
			g.Expect(current.Annotations[capsulev1beta2.UsedAnnotationForResource(kgv)]).To(Equal("0"))
			state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(state.Pending).To(BeEmpty())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		_, err = bClient.Resource(resource).Namespace(allowedNS.Name).Create(ctx, object("after-expiry"), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, err = bClient.Resource(resource).Namespace(allowedNS.Name).Get(ctx, "after-expiry", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("counting generated names once so two objects fit a quota of two")
		Expect(bClient.Resource(resource).Namespace(allowedNS.Name).Delete(ctx, "after-expiry", metav1.DeleteOptions{})).To(Succeed())
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(b), current); err != nil {
				return err
			}
			current.Annotations[capsulev1beta2.LimitAnnotationForResource(kgv)] = "2"
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(b), current)).To(Succeed())
			g.Expect(current.Annotations[capsulev1beta2.UsedAnnotationForResource(kgv)]).To(Equal("0"))
			state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(state.Pending[kgv]).To(BeEmpty())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		generated := func() *unstructured.Unstructured { obj := object(""); obj.SetGenerateName("generated-"); return obj }
		firstGenerated, err := bClient.Resource(resource).Namespace(allowedNS.Name).Create(ctx, generated(), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(firstGenerated.GetName()).To(HavePrefix("generated-"))
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(b), current)).To(Succeed())
			state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(state.Pending[kgv]).To(BeEmpty(), "recount must match the persisted generated name")
			g.Expect(current.Annotations[capsulev1beta2.UsedAnnotationForResource(kgv)]).To(Equal("1"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		secondGenerated, err := bClient.Resource(resource).Namespace(allowedNS.Name).Create(ctx, generated(), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(secondGenerated.GetName()).NotTo(Equal(firstGenerated.GetName()))
		_, err = bClient.Resource(resource).Namespace(allowedNS.Name).Create(ctx, generated(), metav1.CreateOptions{})
		Expect(err).To(MatchError(ContainSubstring("has reached quota limit of 2 items")))
		_, err = aClient.Resource(resource).Namespace(allowedNS.Name).Create(ctx, generated(), metav1.CreateOptions{})
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "generated names must preserve tenant isolation: %v", err)
		persisted, err := bClient.Resource(resource).Namespace(allowedNS.Name).List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(persisted.Items).To(HaveLen(2), "denied generated-name requests must not persist")

		By("retaining a same-name CREATE's distinct UID through recount and deletion of the older object")
		Expect(bClient.Resource(resource).Namespace(allowedNS.Name).Delete(ctx, secondGenerated.GetName(), metav1.DeleteOptions{})).To(Succeed())
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(b), current)).To(Succeed())
			g.Expect(current.Annotations[capsulev1beta2.UsedAnnotationForResource(kgv)]).To(Equal("1"))
			state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(state.Pending[kgv]).To(BeEmpty())
			g.Expect(state.Deleting[kgv]).To(BeEmpty())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		// Validation runs before storage reports AlreadyExists. That failed
		// CREATE must retain its own reservation, not borrow the older UID.
		_, err = bClient.Resource(resource).Namespace(allowedNS.Name).Create(ctx, object(firstGenerated.GetName()), metav1.CreateOptions{})
		Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(), "%v", err)
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(b), current)).To(Succeed())
			state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(state.Pending[kgv]).To(HaveLen(1))
			g.Expect(state.Pending[kgv][0].ObjectRef.UID).NotTo(BeEmpty())
			g.Expect(state.Pending[kgv][0].ObjectRef.UID).NotTo(Equal(firstGenerated.GetUID()))
			g.Expect(current.Annotations[capsulev1beta2.UsedAnnotationForResource(kgv)]).To(Equal("2"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		// A pin on an already-published namespace is removed only by the
		// recount, providing an observable completion signal for this test.
		Eventually(func() error {
			current, ns := &capsulev1beta2.Tenant{}, &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(b), current); err != nil {
				return err
			}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(allowedNS), ns); err != nil {
				return err
			}
			state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			if err != nil {
				return err
			}
			state.Namespaces[ns.Name] = ns.UID
			if err := capsuletenant.WriteLegacyQuotaReservations(current.Annotations, state); err != nil {
				return err
			}
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(b), current)).To(Succeed())
			state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(state.Namespaces).NotTo(HaveKey(allowedNS.Name))
			g.Expect(state.Pending[kgv]).To(HaveLen(1))
			g.Expect(current.Annotations[capsulev1beta2.UsedAnnotationForResource(kgv)]).To(Equal("2"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(bClient.Resource(resource).Namespace(allowedNS.Name).Delete(ctx, firstGenerated.GetName(), metav1.DeleteOptions{})).To(Succeed())
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(b), current)).To(Succeed())
			state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(state.Pending[kgv]).To(HaveLen(1))
			g.Expect(state.Deleting[kgv]).To(BeEmpty())
			g.Expect(current.Annotations[capsulev1beta2.UsedAnnotationForResource(kgv)]).To(Equal("1"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		_, err = bClient.Resource(resource).Namespace(allowedNS.Name).Create(ctx, object("after-older-delete"), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, err = bClient.Resource(resource).Namespace(allowedNS.Name).Create(ctx, object("over-remaining-reservation"), metav1.CreateOptions{})
		Expect(err).To(MatchError(ContainSubstring("has reached quota limit of 2 items")))
		_, err = admin.Resource(resource).Namespace(allowedNS.Name).Get(ctx, "over-remaining-reservation", metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "denied object must not persist")
		currentA := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(a), currentA)).To(Succeed())
		Expect(currentA.Annotations[capsulev1beta2.UsedAnnotationForResource(kgv)]).To(Equal("0"))

		By("renewing a later DELETE attempt without growing per-object tracking")
		Expect(bClient.Resource(resource).Namespace(allowedNS.Name).Delete(ctx, "after-older-delete", metav1.DeleteOptions{})).To(Succeed())
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(b), current); err != nil {
				return err
			}
			state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			if err != nil {
				return err
			}
			for i := range state.Pending[kgv] {
				state.Pending[kgv][i].ExpiresAt = new(metav1.NewTime(time.Now().Add(-time.Minute)))
			}
			current.Annotations[capsulev1beta2.LimitAnnotationForResource(kgv)] = "1"
			if err := capsuletenant.WriteLegacyQuotaReservations(current.Annotations, state); err != nil {
				return err
			}
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectSettledUsage := func(used string) {
			Eventually(func(g Gomega) {
				current := &capsulev1beta2.Tenant{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(b), current)).To(Succeed())
				g.Expect(current.Annotations[capsulev1beta2.UsedAnnotationForResource(kgv)]).To(Equal(used))
				state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(state.Pending[kgv]).To(BeEmpty())
				g.Expect(state.Deleting[kgv]).To(BeEmpty())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		expectSettledUsage("0")
		retained := object("retry-delete")
		retained.SetFinalizers([]string{"e2e.projectcapsule.dev/hold"})
		retained, err = bClient.Resource(resource).Namespace(allowedNS.Name).Create(ctx, retained, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Eventually(func() error {
				current, err := admin.Resource(resource).Namespace(allowedNS.Name).Get(ctx, retained.GetName(), metav1.GetOptions{})
				if apierrors.IsNotFound(err) {
					return nil
				}
				if err != nil {
					return err
				}
				current.SetFinalizers(nil)
				_, err = admin.Resource(resource).Namespace(allowedNS.Name).Update(ctx, current, metav1.UpdateOptions{})
				return err
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
		expectSettledUsage("1")
		var priorAttempt metav1.Time
		Eventually(func() error {
			current, ns := &capsulev1beta2.Tenant{}, &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(b), current); err != nil {
				return err
			}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(allowedNS), ns); err != nil {
				return err
			}
			state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			if err != nil {
				return err
			}
			priorAttempt = metav1.Now()
			expires := metav1.NewTime(priorAttempt.Add(30 * time.Second))
			state.Deleting = map[string][]capsuletenant.LegacyQuotaDeletion{kgv: {{ID: "earlier-rejected-delete", NamespaceUID: ns.UID, CreatedAt: metav1.NewTime(priorAttempt.Add(-90 * time.Second)), ExpiresAt: &expires, ObjectRef: capsulev1beta2.QuantityLedgerObjectRef{Namespace: ns.Name, Name: retained.GetName(), UID: retained.GetUID()}}}}
			if err := capsuletenant.WriteLegacyQuotaReservations(current.Annotations, state); err != nil {
				return err
			}
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		_, err = bClient.Resource(resource).Namespace(allowedNS.Name).Create(ctx, object("before-delete-commits"), metav1.CreateOptions{})
		Expect(err).To(MatchError(ContainSubstring("has reached quota limit of 1 items")))
		_, err = admin.Resource(resource).Namespace(allowedNS.Name).Get(ctx, "before-delete-commits", metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "%v", err)
		Expect(bClient.Resource(resource).Namespace(allowedNS.Name).Delete(ctx, retained.GetName(), metav1.DeleteOptions{})).To(Succeed())
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(b), current)).To(Succeed())
			state, err := capsuletenant.ReadLegacyQuotaReservations(current.Annotations)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(state.Deleting[kgv]).To(HaveLen(1))
			g.Expect(state.Deleting[kgv][0].ID).NotTo(Equal("earlier-rejected-delete"))
			g.Expect(state.Deleting[kgv][0].ExpiresAt.After(priorAttempt.Add(119 * time.Second))).To(BeTrue())
			g.Expect(state.Deleting[kgv][0].ObjectRef.UID).To(Equal(retained.GetUID()))
			persisted, err := bClient.Resource(resource).Namespace(allowedNS.Name).Get(ctx, retained.GetName(), metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(persisted.GetDeletionTimestamp()).NotTo(BeNil())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func() error {
			current, err := bClient.Resource(resource).Namespace(allowedNS.Name).Get(ctx, retained.GetName(), metav1.GetOptions{})
			if err != nil {
				return err
			}
			current.SetFinalizers(nil)
			_, err = bClient.Resource(resource).Namespace(allowedNS.Name).Update(ctx, current, metav1.UpdateOptions{})
			return err
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectSettledUsage("0")

		By("recovering quota after DELETE when the complete annotation budget is full")
		_, err = bClient.Resource(resource).Namespace(allowedNS.Name).Create(ctx, object("full-metadata"), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		expectSettledUsage("1")
		const paddingKey = "e2e.projectcapsule.dev/padding"
		removePadding := func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(b), current); err != nil {
				return client.IgnoreNotFound(err)
			}
			if _, exists := current.Annotations[paddingKey]; !exists {
				return nil
			}
			delete(current.Annotations, paddingKey)
			return k8sClient.Update(ctx, current)
		}
		DeferCleanup(func() { Eventually(removePadding, defaultTimeoutInterval, defaultPollInterval).Should(Succeed()) })
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(b), current); err != nil {
				return err
			}
			delete(current.Annotations, paddingKey)
			size := len(paddingKey)
			for key, value := range current.Annotations {
				size += len(key) + len(value)
			}
			current.Annotations[paddingKey] = strings.Repeat("x", apivalidation.TotalAnnotationSizeLimitB-size)
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(bClient.Resource(resource).Namespace(allowedNS.Name).Delete(ctx, "full-metadata", metav1.DeleteOptions{})).To(Succeed())
		_, err = bClient.Resource(resource).Namespace(allowedNS.Name).Get(ctx, "full-metadata", metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "%v", err)
		expectSettledUsage("0")
		_, err = bClient.Resource(resource).Namespace(allowedNS.Name).Create(ctx, object("metadata-capacity"), metav1.CreateOptions{})
		Expect(err).To(MatchError(ContainSubstring("capacity exceeded")))
		_, err = admin.Resource(resource).Namespace(allowedNS.Name).Get(ctx, "metadata-capacity", metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "%v", err)
		Eventually(removePadding, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		_, err = bClient.Resource(resource).Namespace(allowedNS.Name).Create(ctx, object("after-metadata-recovery"), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		expectSettledUsage("1")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(a), currentA)).To(Succeed())
		Expect(currentA.Annotations[capsulev1beta2.UsedAnnotationForResource(kgv)]).To(Equal("0"))
		TenantNamespaceReady(a, deniedNS, 1)
		TenantNamespaceReady(b, allowedNS, 1)
	})
})
