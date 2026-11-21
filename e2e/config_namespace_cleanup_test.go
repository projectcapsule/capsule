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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

var _ = Describe("namespace cleanup grace period", Serial, Label("config", "namespace-cleanup", "termination"), func() {
	It("holds foreign finalizers for the configured grace period and preserves other tenants", func() {
		ctx := context.Background()
		original := &capsulev1beta2.CapsuleConfiguration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, original)).To(Succeed())
		DeferCleanup(func() {
			ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
				configuration.Spec.NamespaceCleanup = original.Spec.NamespaceCleanup
			})
		})

		// Long enough to tell apart from the default, which releases the
		// finalizers about 10 s after the namespace deletion.
		const gracePeriod = 45 * time.Second
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.NamespaceCleanup.GracePeriod = &metav1.Duration{Duration: gracePeriod}
		})

		By("rejecting a negative grace period and keeping the configured one")
		Eventually(func() error {
			current := &capsulev1beta2.CapsuleConfiguration{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, current); err != nil {
				return err
			}
			current.Spec.NamespaceCleanup.GracePeriod = &metav1.Duration{Duration: -time.Second}
			err := k8sClient.Update(ctx, current)
			switch {
			case err == nil:
				StopTrying("a negative namespace cleanup grace period was accepted").Now()
			case apierrors.IsConflict(err):
				return err
			case !apierrors.IsInvalid(err):
				return StopTrying(fmt.Sprintf("expected an invalid error, got: %v", err))
			}
			Expect(err).To(MatchError(ContainSubstring("gracePeriod must not be negative")))
			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		stored := &capsulev1beta2.CapsuleConfiguration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, stored)).To(Succeed())
		Expect(stored.Spec.NamespaceCleanup.GracePeriod).NotTo(BeNil())
		Expect(stored.Spec.NamespaceCleanup.GracePeriod.Duration).To(Equal(gracePeriod))

		prefix := "e2e-cleanup-grace-" + rand.String(6)
		const hold = "e2e.projectcapsule.dev/release-hardware"
		var namespaces []*corev1.Namespace
		var held []*corev1.ConfigMap
		for _, suffix := range []string{"-a", "-b"} {
			name := prefix + suffix
			owner := rbac.UserSpec{Name: name + "-owner", Kind: rbac.UserOwner}
			tnt := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{UserSpec: owner}}}}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(func() { DeleteInBackground(tnt) })
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)

			ns := NewNamespace(name, map[string]string{meta.TenantLabel: name})
			NamespaceCreation(ns, owner, defaultTimeoutInterval).Should(Succeed())
			DeferCleanup(func() { DeleteNamespaceInBackground(ctx, ns.Name) })
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

			// Stands for an object whose controller releases something outside
			// the namespace, e.g. a Cluster API machine powering off its host.
			cm := &corev1.ConfigMap{Name: "held", Namespace: ns.Name, Labels: map[string]string{"env": "e2e"}, Finalizers: []string{hold}}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())
			namespaces = append(namespaces, ns)
			held = append(held, cm)
		}
		// Tenant B's object keeps its finalizer: release it so its namespace can go.
		DeferCleanup(func() {
			Eventually(func() error {
				current := &corev1.ConfigMap{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(held[1]), current); err != nil {
					return client.IgnoreNotFound(err)
				}
				current.Finalizers = nil
				return k8sClient.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("deleting tenant A's namespace")
		Expect(k8sClient.Delete(ctx, namespaces[0])).To(Succeed())
		terminating := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(namespaces[0]), terminating)).To(Succeed())
		Expect(terminating.DeletionTimestamp).NotTo(BeNil())
		deletedAt := terminating.DeletionTimestamp.Time

		By("holding the finalizer well past the default grace period")
		holdFor := gracePeriod - 15*time.Second - time.Since(deletedAt)
		Expect(holdFor).To(BeNumerically(">", 2*configuration.DefaultNamespaceCleanupGracePeriod))
		Consistently(func(g Gomega) {
			current := &corev1.ConfigMap{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(held[0]), current)).To(Succeed())
			g.Expect(current.Finalizers).To(ContainElement(hold))
		}, holdFor, defaultPollInterval).Should(Succeed())

		By("releasing it once the configured grace period has elapsed")
		Eventually(func(g Gomega) {
			current := &corev1.ConfigMap{}
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(held[0]), current)
			if apierrors.IsNotFound(err) {
				return
			}
			g.Expect(err).To(Succeed())
			g.Expect(current.Finalizers).NotTo(ContainElement(hold))
		}, defaultTerminationTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(namespaces[0]), &corev1.Namespace{}))
		}, defaultTerminationTimeoutInterval, defaultPollInterval).Should(BeTrue())

		By("leaving tenant B's namespace and its finalizer untouched")
		current := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(namespaces[1]), current)).To(Succeed())
		Expect(current.DeletionTimestamp).To(BeNil())
		preserved := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(held[1]), preserved)).To(Succeed())
		Expect(preserved.DeletionTimestamp).To(BeNil())
		Expect(preserved.Finalizers).To(ConsistOf(hold))
	})
})
