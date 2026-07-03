// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	apimeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("Replication healthChecks", Ordered, Label("replications", "healthchecks"), func() {
	var (
		ctx           context.Context
		tnt           *capsulev1beta2.Tenant
		tenantOwner   rbac.UserSpec
		baseNamespace string
		namespaces    []string
	)

	// expectGlobalHealthy polls the GlobalTenantResource's Healthy condition.
	expectGlobalHealthy := func(name string, status metav1.ConditionStatus, msgContains string) {
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.GlobalTenantResource{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, current)).To(Succeed())

			cond := current.Status.Conditions.GetConditionByType(apimeta.HealthyCondition)
			g.Expect(cond).ToNot(BeNil())
			g.Expect(cond.Status).To(Equal(status))

			if msgContains != "" {
				g.Expect(cond.Message).To(ContainSubstring(msgContains))
			}
		}, 2*defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	expectTenantHealthy := func(namespace, name string, status metav1.ConditionStatus) {
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.TenantResource{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, current)).To(Succeed())

			cond := current.Status.Conditions.GetConditionByType(apimeta.HealthyCondition)
			g.Expect(cond).ToNot(BeNil())
			g.Expect(cond.Status).To(Equal(status))
		}, 2*defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	BeforeEach(func() {
		ctx = context.Background()
		tenantOwner = rbac.UserSpec{Name: "e2e-hc-owner", Kind: rbac.OwnerKind("User")}
		baseNamespace = "e2e-hc-system"
		namespaces = []string{"e2e-hc-one", "e2e-hc-two"}

		tnt = &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "e2e-hc-tenant",
				Labels: map[string]string{"hc": "true", "env": "e2e"},
			},
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{
					CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: tenantOwner},
				}},
			},
		}

		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""

			return k8sClient.Create(ctx, tnt)
		}).Should(Succeed())
		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)

		for _, ns := range append(append([]string{}, namespaces...), baseNamespace) {
			namespace := NewNamespace(ns, map[string]string{apimeta.TenantLabel: tnt.GetName()})
			NamespaceCreation(namespace, tenantOwner, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, namespace).Should(Succeed())
		}
	})

	AfterEach(func() {
		Eventually(func() error {
			list := &capsulev1beta2.GlobalTenantResourceList{}
			if err := k8sClient.List(ctx, list, client.MatchingLabels{"e2e.capsule.dev/test-suite": "true"}); err != nil {
				return err
			}

			for i := range list.Items {
				if err := k8sClient.Delete(ctx, &list.Items[i]); client.IgnoreNotFound(err) != nil {
					return err
				}
			}

			return nil
		}, "30s", "5s").Should(Succeed())

		for _, ns := range append([]string{baseNamespace}, namespaces...) {
			ForceDeleteNamespace(ctx, ns)
		}

		EventuallyDeletion(tnt)
	})

	// newConfigMapGTR builds a GlobalTenantResource replicating a ConfigMap to each
	// tenant namespace, with a health check driven by the ConfigMap's own data.
	newConfigMapGTR := func(name string, data map[string]string, checks []capsulev1beta2.HealthCheckSpec) *capsulev1beta2.GlobalTenantResource {
		return &capsulev1beta2.GlobalTenantResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: map[string]string{"e2e.capsule.dev/test-suite": "true"},
			},
			Spec: capsulev1beta2.GlobalTenantResourceSpec{
				Scope:          api.ResourceScopeNamespace,
				TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"hc": "true"}},
				TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
					ResyncPeriod:    metav1.Duration{Duration: 5 * time.Second},
					PruningOnDelete: ptr.To(true),
					HealthChecks:    checks,
					Resources: []capsulev1beta2.ResourceSpec{{
						RawItems: []capsulev1beta2.RawExtension{{
							RawExtension: runtime.RawExtension{
								Object: &corev1.ConfigMap{
									TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
									ObjectMeta: metav1.ObjectMeta{Name: name},
									Data:       data,
								},
							},
						}},
					}},
				},
			},
		}
	}

	It("reports Healthy via custom CEL expressions and flips to unhealthy", func() {
		checks := []capsulev1beta2.HealthCheckSpec{{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Current:    "data['ready'] == 'true'",
			Failed:     "data['ready'] == 'false'",
		}}

		gtr := newConfigMapGTR("gtr-hc-cel", map[string]string{"ready": "true"}, checks)
		EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

		expectGlobalHealthy("gtr-hc-cel", metav1.ConditionTrue, "healthy")

		// Drive the replicated objects unhealthy by flipping the source data.
		Eventually(func() error {
			current := &capsulev1beta2.GlobalTenantResource{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "gtr-hc-cel"}, current); err != nil {
				return err
			}

			current.Spec.Resources[0].RawItems[0] = capsulev1beta2.RawExtension{
				RawExtension: runtime.RawExtension{
					Object: &corev1.ConfigMap{
						TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
						ObjectMeta: metav1.ObjectMeta{Name: "gtr-hc-cel"},
						Data:       map[string]string{"ready": "false"},
					},
				},
			}

			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		expectGlobalHealthy("gtr-hc-cel", metav1.ConditionFalse, "unhealthy")
	})

	It("reports Healthy via the kstatus default for a Deployment", func() {
		deployment := &appsv1.Deployment{
			TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
			ObjectMeta: metav1.ObjectMeta{Name: "gtr-hc-deploy"},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To(int32(1)),
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "gtr-hc-deploy"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "gtr-hc-deploy"}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "pause",
							Image: "registry.k8s.io/pause:3.9",
						}},
					},
				},
			},
		}

		gtr := &capsulev1beta2.GlobalTenantResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "gtr-hc-deploy",
				Labels: map[string]string{"e2e.capsule.dev/test-suite": "true"},
			},
			Spec: capsulev1beta2.GlobalTenantResourceSpec{
				Scope:          api.ResourceScopeNamespace,
				TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"hc": "true"}},
				TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
					ResyncPeriod:    metav1.Duration{Duration: 5 * time.Second},
					PruningOnDelete: ptr.To(true),
					Resources: []capsulev1beta2.ResourceSpec{{
						RawItems: []capsulev1beta2.RawExtension{{
							RawExtension: runtime.RawExtension{Object: deployment},
						}},
					}},
				},
			},
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, gtr) }).Should(Succeed())

		expectGlobalHealthy("gtr-hc-deploy", metav1.ConditionTrue, "")
	})

	It("rejects a GlobalTenantResource with an invalid CEL expression at admission", func() {
		checks := []capsulev1beta2.HealthCheckSpec{{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Current:    "status.conditions[", // syntax error
		}}

		gtr := newConfigMapGTR("gtr-hc-invalid", map[string]string{"ready": "true"}, checks)

		err := k8sClient.Create(ctx, gtr)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid spec.healthChecks"))
	})

	It("reports Healthy on a namespaced TenantResource via custom CEL expressions", func() {
		checks := []capsulev1beta2.HealthCheckSpec{{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Current:    "data['ready'] == 'true'",
			Failed:     "data['ready'] == 'false'",
		}}

		tr := &capsulev1beta2.TenantResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tr-hc-cel",
				Namespace: baseNamespace,
				Labels:    map[string]string{"e2e.capsule.dev/test-suite": "true"},
			},
			Spec: capsulev1beta2.TenantResourceSpec{
				TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
					ResyncPeriod:    resyncPeriod,
					PruningOnDelete: ptr.To(true),
					HealthChecks:    checks,
					Resources: []capsulev1beta2.ResourceSpec{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{apimeta.TenantLabel: tnt.GetName()},
						},
						RawItems: []capsulev1beta2.RawExtension{{
							RawExtension: runtime.RawExtension{
								Object: &corev1.ConfigMap{
									TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
									ObjectMeta: metav1.ObjectMeta{Name: "tr-hc-cel"},
									Data:       map[string]string{"ready": "true"},
								},
							},
						}},
					}},
				},
			},
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, tr) }).Should(Succeed())

		expectTenantHealthy(baseNamespace, "tr-hc-cel", metav1.ConditionTrue)
	})
})
