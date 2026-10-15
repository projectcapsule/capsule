// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"maps"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("TenantResource template environment sandbox", Label("tenant", "replications", "tenantresource", "template-sandbox"), func() {
	DescribeTable("rejects environment access while preserving scoped rendering and recovery", func(function string) {
		ctx := context.Background()
		prefix := "e2e-template-sandbox-" + rand.String(6)
		selector := map[string]string{"e2e.projectcapsule.dev/template-sandbox": prefix}
		var tenants []*capsulev1beta2.Tenant
		var actors []client.Client
		var namespaces []string
		DeferCleanup(func() {
			cleanupTenantResourcesWithDefaultServiceAccount(ctx, namespaces...)
			for _, namespace := range namespaces {
				ForceDeleteNamespace(ctx, namespace)
			}
			for _, tenant := range tenants {
				EventuallyDeletion(tenant)
			}
		})

		By("creating two tenants with distinct owners and matching namespace labels")
		for _, suffix := range []string{"-a", "-b"} {
			name := prefix + suffix
			owner := rbac.UserSpec{Name: name + "-owner", Kind: rbac.UserOwner}
			tenant := &capsulev1beta2.Tenant{
				Name: name, Labels: map[string]string{"env": "e2e"},
				Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{UserSpec: owner}}},
			}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			tenants = append(tenants, tenant)
			TenantReady(tenant, metav1.ConditionTrue, defaultTimeoutInterval)
			actors = append(actors, impersonationClient(owner.Name, withDefaultGroups(nil)))
			for i, namespaceSuffix := range []string{"-target", "-source", "-excluded"} {
				if suffix == "-b" && i > 0 {
					break
				}
				labels := map[string]string{meta.TenantLabel: name}
				if i == 0 {
					maps.Copy(labels, selector)
				}
				namespace := NewNamespace(name+namespaceSuffix, labels)
				NamespaceCreation(namespace, owner, defaultTimeoutInterval).Should(Succeed())
				namespaces = append(namespaces, namespace.Name)
				TenantNamespaceReady(tenant, namespace, uint(i+1))
			}
		}
		targetA, sourceA, excludedA, targetB := namespaces[0], namespaces[1], namespaces[2], namespaces[3]
		for _, pair := range [][2]string{{sourceA, targetA}, {targetB, targetB}} {
			ensureServiceAccount(pair[0], "default")
			bindServiceAccountToTenantResourceManager(pair[0], "default", pair[1])
		}

		safeTemplate := func(name, value string) string {
			return fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
data:
  tenant: "{{ .tenant.metadata.name }}"
  namespace: "{{ .namespace.metadata.name }}"
  value: '{{ %q | upper }}'
`, name, value)
		}
		newParent := func(namespace, name, tpl string) *capsulev1beta2.TenantResource {
			parent := newGeneratorConfigMapTenantResource(namespace, name, tpl)
			parent.Labels = map[string]string{"env": "e2e"}
			parent.Spec.ServiceAccount = &meta.LocalRFC1123ObjectReference{Name: "default"}
			parent.Spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{MatchLabels: selector}
			return parent
		}
		assertAbsent := func(namespace, name string) {
			err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &corev1.ConfigMap{})
			if err == nil {
				Fail(fmt.Sprintf("unexpected template output %s/%s", namespace, name))
			}
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected missing template output, got %v", err)
		}

		By("rendering safe context and functions independently in both tenants")
		for i, pair := range [][2]string{{sourceA, targetA}, {targetB, targetB}} {
			parent := newParent(pair[0], "safe-template", safeTemplate("safe-output", "safe"))
			Expect(actors[i].Create(ctx, parent)).To(Succeed())
			expectTenantResourceProcessedNamespaces(pair[0], parent.Name, "safe-output", []string{pair[1]})
			expectConfigMapData(pair[1], "safe-output", map[string]string{
				"tenant": tenants[i].Name, "namespace": pair[1], "value": "SAFE",
			})
		}
		assertAbsent(sourceA, "safe-output")
		assertAbsent(excludedA, "safe-output")

		By("rejecting the prohibited function before any generated object is applied")
		argument := "CAPSULE_TEMPLATE_SANDBOX_TEST"
		if function != "env" {
			argument = "$" + argument
		}
		blockedTemplate := fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: blocked-output
data:
  value: '{{ %s %q }}'
`, function, argument)
		blocked := newParent(sourceA, "blocked-template", blockedTemplate)
		Expect(actors[0].Create(ctx, blocked)).To(Succeed())
		Eventually(func(g Gomega) {
			assertAbsent(targetA, "blocked-output")
			current := &capsulev1beta2.TenantResource{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(blocked), current)).To(Succeed())
			g.Expect(current.Status.ObservedGeneration).To(Equal(current.Generation))
			ready := current.Status.Conditions.GetConditionByType(meta.ReadyCondition)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(ready.Reason).To(Equal(meta.FailedReason))
			g.Expect(ready.Message).To(ContainSubstring(fmt.Sprintf(`function %q not defined`, function)))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Consistently(func() error {
			for _, namespace := range namespaces {
				assertAbsent(namespace, "blocked-output")
			}
			return nil
		}, 5*time.Second, defaultPollInterval).Should(Succeed())

		By("recovering after an owner replaces the template with a safe expression")
		Eventually(func() error {
			current := &capsulev1beta2.TenantResource{}
			if err := actors[0].Get(ctx, client.ObjectKeyFromObject(blocked), current); err != nil {
				return err
			}
			current.Spec.Resources[0].Generators[0].Template = safeTemplate("blocked-output", "repaired")
			return actors[0].Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectTenantResourceProcessedNamespaces(sourceA, blocked.Name, "blocked-output", []string{targetA})
		expectConfigMapData(targetA, "blocked-output", map[string]string{
			"tenant": tenants[0].Name, "namespace": targetA, "value": "REPAIRED",
		})
		for _, namespace := range []string{sourceA, excludedA, targetB} {
			assertAbsent(namespace, "blocked-output")
		}
		for i, namespace := range []string{targetA, targetB} {
			expectConfigMapData(namespace, "safe-output", map[string]string{
				"tenant": tenants[i].Name, "namespace": namespace, "value": "SAFE",
			})
		}
	}, Entry("env", "env"), Entry("expandenv", "expandenv"), Entry("expandEnv", "expandEnv"))
})
