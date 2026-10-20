// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
		role := &rbacv1.ClusterRole{Name: name, Labels: map[string]string{"env": "e2e", "rbac.authorization.k8s.io/aggregate-to-admin": "true"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{group}, Resources: []string{"widgets"}, Verbs: []string{"create", "get", "list", "delete"}}}}
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
		TenantNamespaceReady(a, deniedNS, 1)
		TenantNamespaceReady(b, allowedNS, 1)
	})
})
