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

var _ = Describe("namespace cleanup configuration", Serial, Label("config", "namespace-cleanup", "termination"), func() {
	It("holds foreign finalizers for the configured grace period and preserves other tenants", func() {
		ctx := context.Background()
		preserveNamespaceCleanup(ctx)

		// Long enough to tell apart from the default, which releases the
		// finalizers about 10 s after the namespace deletion.
		const gracePeriod = 45 * time.Second
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.NamespaceCleanup.GracePeriod = &metav1.Duration{Duration: gracePeriod}
		})

		By("rejecting a negative grace period and keeping the configured one")
		expectNamespaceCleanupInvalid(ctx, func(cleanup *capsulev1beta2.NamespaceCleanupConfiguration) {
			cleanup.GracePeriod = &metav1.Duration{Duration: -time.Second}
		}, "gracePeriod must not be negative")
		Expect(storedNamespaceCleanup(ctx).GracePeriod).To(Equal(&metav1.Duration{Duration: gracePeriod}))

		prefix := "e2e-cleanup-grace-" + rand.String(6)
		const hold = "e2e.projectcapsule.dev/release-hardware"
		nsA := cleanupTenantNamespace(ctx, prefix+"-a")
		nsB := cleanupTenantNamespace(ctx, prefix+"-b")
		// Stands for an object whose controller releases something outside the
		// namespace, e.g. a Cluster API machine powering off its host.
		held := heldConfigMap(ctx, nsA.Name, "held", hold)
		preserved := heldConfigMap(ctx, nsB.Name, "preserved", hold)

		By("deleting tenant A's namespace")
		Expect(k8sClient.Delete(ctx, nsA)).To(Succeed())
		terminating := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(nsA), terminating)).To(Succeed())
		Expect(terminating.DeletionTimestamp).NotTo(BeNil())
		deletedAt := terminating.DeletionTimestamp.Time

		By("holding the finalizer until the configured grace period has elapsed")
		holdFor := gracePeriod - cleanupDeadlineTolerance - time.Since(deletedAt)
		Expect(holdFor).To(BeNumerically(">", 2*configuration.DefaultNamespaceCleanupGracePeriod))
		expectFinalizerHeld(ctx, held, hold, holdFor)

		By("releasing it once the configured grace period has elapsed")
		expectFinalizerReleased(ctx, held, hold)
		expectNamespaceRemoved(ctx, nsA)

		By("leaving tenant B's namespace and its finalizer untouched")
		expectUntouched(ctx, nsB, preserved, hold)
	})

	It("applies a shortened grace period to namespaces already waiting and preserves other tenants", func() {
		ctx := context.Background()
		preserveNamespaceCleanup(ctx)

		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.NamespaceCleanup.GracePeriod = &metav1.Duration{Duration: 10 * time.Minute}
		})

		prefix := "e2e-cleanup-shorten-" + rand.String(6)
		const hold = "e2e.projectcapsule.dev/release-hardware"
		nsA := cleanupTenantNamespace(ctx, prefix+"-a")
		nsB := cleanupTenantNamespace(ctx, prefix+"-b")
		held := heldConfigMap(ctx, nsA.Name, "held", hold)
		preserved := heldConfigMap(ctx, nsB.Name, "preserved", hold)

		By("deleting tenant A's namespace under a ten-minute grace period")
		Expect(k8sClient.Delete(ctx, nsA)).To(Succeed())
		terminating := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(nsA), terminating)).To(Succeed())
		Expect(terminating.DeletionTimestamp).NotTo(BeNil())
		deletedAt := terminating.DeletionTimestamp.Time

		By("holding the finalizer past the default grace period")
		expectFinalizerHeld(ctx, held, hold, 2*configuration.DefaultNamespaceCleanupGracePeriod-time.Since(deletedAt))

		By("shortening the grace period while the namespace waits")
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.NamespaceCleanup.GracePeriod = &metav1.Duration{Duration: time.Second}
		})
		shortenedAt := time.Now()

		By("releasing it within one recheck period instead of after ten minutes")
		expectFinalizerReleased(ctx, held, hold)
		Expect(time.Since(shortenedAt)).To(BeNumerically("<=", namespaceCleanupGraceRecheckPeriod+namespaceCleanupRetryPeriod+cleanupDeadlineTolerance))
		expectNamespaceRemoved(ctx, nsA)

		By("leaving tenant B's namespace and its finalizer untouched")
		expectUntouched(ctx, nsB, preserved, hold)
	})

	It("retains listed finalizers until released or unlisted and preserves other tenants", func() {
		ctx := context.Background()
		preserveNamespaceCleanup(ctx)

		const retained, foreign = "e2e.projectcapsule.dev/retained", "e2e.projectcapsule.dev/foreign"
		// No grace period, so every cleanup pass runs while the listed finalizer is held.
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.NamespaceCleanup.GracePeriod = &metav1.Duration{}
			configuration.Spec.NamespaceCleanup.RetainedFinalizers = []string{retained}
		})

		By("rejecting a list with duplicates and keeping the configured one")
		expectNamespaceCleanupInvalid(ctx, func(cleanup *capsulev1beta2.NamespaceCleanupConfiguration) {
			cleanup.RetainedFinalizers = []string{retained, retained}
		}, "retainedFinalizers must not contain duplicates")
		Expect(storedNamespaceCleanup(ctx).RetainedFinalizers).To(Equal([]string{retained}))

		prefix := "e2e-cleanup-retain-" + rand.String(6)
		nsA := cleanupTenantNamespace(ctx, prefix+"-a")
		nsB := cleanupTenantNamespace(ctx, prefix+"-b")
		released := heldConfigMap(ctx, nsA.Name, "released", retained)
		unlisted := heldConfigMap(ctx, nsA.Name, "unlisted", retained)
		stripped := heldConfigMap(ctx, nsA.Name, "stripped", foreign)
		preserved := heldConfigMap(ctx, nsB.Name, "preserved", foreign, retained)

		By("deleting tenant A's namespace")
		Expect(k8sClient.Delete(ctx, nsA)).To(Succeed())

		By("removing the finalizer that is not listed")
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(stripped), &corev1.ConfigMap{}))
		}, defaultTerminationTimeoutInterval, defaultPollInterval).Should(BeTrue())

		expectHeldInTerminatingNamespace := func(objects ...*corev1.ConfigMap) {
			Consistently(func(g Gomega) {
				for _, obj := range objects {
					current := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), current)).To(Succeed())
					g.Expect(current.DeletionTimestamp).NotTo(BeNil())
					g.Expect(current.Finalizers).To(ConsistOf(retained))
				}
				namespace := &corev1.Namespace{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(nsA), namespace)).To(Succeed())
				g.Expect(namespace.DeletionTimestamp).NotTo(BeNil())
			}, 3*namespaceCleanupRetryPeriod, defaultPollInterval).Should(Succeed())
		}

		By("keeping the listed finalizers, and the namespace, across cleanup passes")
		expectHeldInTerminatingNamespace(released, unlisted)

		By("keeping the namespace while another object still holds a listed finalizer")
		releaseFinalizers(ctx, released)
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(released), &corev1.ConfigMap{}))
		}, defaultTerminationTimeoutInterval, defaultPollInterval).Should(BeTrue())
		expectHeldInTerminatingNamespace(unlisted)

		By("resuming the cleanup once the finalizer is no longer listed")
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.NamespaceCleanup.RetainedFinalizers = nil
		})
		unlistedAt := time.Now()
		expectFinalizerReleased(ctx, unlisted, retained)
		// The held namespace never left the cleanup loop: its next pass reads the
		// new configuration, without restarting the controller.
		Expect(time.Since(unlistedAt)).To(BeNumerically("<=", 2*namespaceCleanupRetryPeriod+cleanupDeadlineTolerance))
		expectNamespaceRemoved(ctx, nsA)

		By("leaving tenant B's namespace and its finalizers untouched")
		expectUntouched(ctx, nsB, preserved, foreign, retained)
	})
})

const (
	// Mirrors namespaceCleanupGraceRecheckPeriod in
	// internal/controllers/tenant/namespace_cleanup.go: a namespace within its
	// grace period is rechecked at least this often.
	namespaceCleanupGraceRecheckPeriod = time.Minute
	// deletionTimestamp keeps whole seconds; the rest covers the poll interval.
	cleanupDeadlineTolerance = 2 * time.Second
)

// expectFinalizerHeld asserts obj keeps finalizer for the whole duration.
func expectFinalizerHeld(ctx context.Context, obj *corev1.ConfigMap, finalizer string, duration time.Duration) {
	Consistently(func(g Gomega) {
		current := &corev1.ConfigMap{}
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), current)).To(Succeed())
		g.Expect(current.Finalizers).To(ContainElement(finalizer))
	}, duration, defaultPollInterval).Should(Succeed())
}

// expectFinalizerReleased waits until obj is gone or no longer has finalizer.
func expectFinalizerReleased(ctx context.Context, obj *corev1.ConfigMap, finalizer string) {
	Eventually(func(g Gomega) {
		current := &corev1.ConfigMap{}
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), current)
		if apierrors.IsNotFound(err) {
			return
		}
		g.Expect(err).To(Succeed())
		g.Expect(current.Finalizers).NotTo(ContainElement(finalizer))
	}, defaultTerminationTimeoutInterval, defaultPollInterval).Should(Succeed())
}

// preserveNamespaceCleanup restores spec.namespaceCleanup when the spec ends.
func preserveNamespaceCleanup(ctx context.Context) {
	original := &capsulev1beta2.CapsuleConfiguration{}
	Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, original)).To(Succeed())
	DeferCleanup(func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.NamespaceCleanup = original.Spec.NamespaceCleanup
		})
	})
}

func storedNamespaceCleanup(ctx context.Context) capsulev1beta2.NamespaceCleanupConfiguration {
	stored := &capsulev1beta2.CapsuleConfiguration{}
	Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, stored)).To(Succeed())

	return stored.Spec.NamespaceCleanup
}

// expectNamespaceCleanupInvalid expects the API server to reject the mutated
// spec.namespaceCleanup with message.
func expectNamespaceCleanupInvalid(ctx context.Context, mutate func(*capsulev1beta2.NamespaceCleanupConfiguration), message string) {
	Eventually(func() error {
		current := &capsulev1beta2.CapsuleConfiguration{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, current); err != nil {
			return err
		}
		mutate(&current.Spec.NamespaceCleanup)
		err := k8sClient.Update(ctx, current)
		switch {
		case err == nil:
			StopTrying("an invalid namespace cleanup configuration was accepted").Now()
		case apierrors.IsConflict(err):
			return err
		case !apierrors.IsInvalid(err):
			return StopTrying(fmt.Sprintf("expected an invalid error, got: %v", err))
		}
		Expect(err).To(MatchError(ContainSubstring(message)))
		return nil
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

// cleanupTenantNamespace creates a Tenant with one namespace, both removed in
// the background when the spec ends.
func cleanupTenantNamespace(ctx context.Context, name string) *corev1.Namespace {
	owner := rbac.UserSpec{Name: name + "-owner", Kind: rbac.UserOwner}
	tnt := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{UserSpec: owner}}}}
	Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
	DeferCleanup(func() { DeleteInBackground(tnt) })
	TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)

	ns := NewNamespace(name, map[string]string{meta.TenantLabel: name})
	NamespaceCreation(ns, owner, defaultTimeoutInterval).Should(Succeed())
	DeferCleanup(func() { DeleteNamespaceInBackground(ctx, ns.Name) })
	NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

	return ns
}

// heldConfigMap creates a ConfigMap holding finalizers, released when the spec
// ends as their controllers would, before its namespace is deleted.
func heldConfigMap(ctx context.Context, namespace, name string, finalizers ...string) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{Name: name, Namespace: namespace, Labels: map[string]string{"env": "e2e"}, Finalizers: finalizers}
	Expect(k8sClient.Create(ctx, cm)).To(Succeed())
	DeferCleanup(func() { releaseFinalizers(ctx, cm) })

	return cm
}

func releaseFinalizers(ctx context.Context, obj *corev1.ConfigMap) {
	Eventually(func() error {
		current := &corev1.ConfigMap{}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), current); err != nil {
			return client.IgnoreNotFound(err)
		}
		current.Finalizers = nil
		return k8sClient.Update(ctx, current)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func expectNamespaceRemoved(ctx context.Context, ns *corev1.Namespace) {
	Eventually(func() bool {
		return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(ns), &corev1.Namespace{}))
	}, defaultTerminationTimeoutInterval, defaultPollInterval).Should(BeTrue())
}

// expectUntouched asserts ns is not terminating and obj keeps exactly finalizers.
func expectUntouched(ctx context.Context, ns *corev1.Namespace, obj *corev1.ConfigMap, finalizers ...string) {
	namespace := &corev1.Namespace{}
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(ns), namespace)).To(Succeed())
	Expect(namespace.DeletionTimestamp).To(BeNil())

	current := &corev1.ConfigMap{}
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), current)).To(Succeed())
	Expect(current.DeletionTimestamp).To(BeNil())
	Expect(current.Finalizers).To(ConsistOf(finalizers))
}
