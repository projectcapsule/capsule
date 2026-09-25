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
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("ResourcePermit namespace cleanup", Label("resource-permit", "protection-termination"), func() {
	It("allows namespace-controller deletion without relaxing protection in another tenant", func() {
		ctx := context.Background()
		controller := impersonationClient(ControllerServiceAccountFull, nil)
		prefix := "e2e-permit-termination-" + rand.String(6)
		var namespaces []*corev1.Namespace
		var actors []client.Client
		var targets [][]*corev1.ConfigMap
		for _, suffix := range []string{"-a", "-b"} {
			name := prefix + suffix
			owner := rbac.UserSpec{Name: name + "-owner", Kind: rbac.UserOwner}
			tenant := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{UserSpec: owner}}}}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			DeferCleanup(func() { EventuallyDeletion(tenant) })
			TenantReady(tenant, metav1.ConditionTrue, defaultTimeoutInterval)
			ns := NewNamespace(name, map[string]string{meta.TenantLabel: name})
			NamespaceCreation(ns, owner, defaultTimeoutInterval).Should(Succeed())
			DeferCleanup(func() { ForceDeleteNamespace(ctx, name) })
			TenantNamespaceReady(tenant, ns, 1)
			namespaces = append(namespaces, ns)
			actor := impersonationClient(owner.Name, withDefaultGroups(nil))
			actors = append(actors, actor)
			var protected []*corev1.ConfigMap
			for marker, labels := range map[string]map[string]string{
				"legacy": {meta.ProtectedByCapsuleLabel: meta.ValueControllerResourcePermit},
				"permit": {meta.ResourcePermitProtectionLabel: meta.ValueTrue},
				"shared": {meta.ResourcePermitProtectionLabel: meta.ValueTrue, meta.ReplicationProtectionLabel: meta.ValueTrue},
			} {
				labels["env"] = "e2e"
				// No live permit can perform cleanup: Kubernetes must be able to
				// remove targets whose original execution identity is unavailable.
				cm := &corev1.ConfigMap{Name: marker, Namespace: name, Labels: labels,
					Annotations: map[string]string{meta.ResourcePermitServiceAccountAnnotation: "system:serviceaccount:" + name + ":missing"},
					Data:        map[string]string{"tenant": name},
				}
				Expect(controller.Create(ctx, cm)).To(Succeed())
				DeferCleanup(func() { Expect(client.IgnoreNotFound(controller.Delete(ctx, cm))).To(Succeed()) })
				protected = append(protected, cm)
			}
			targets = append(targets, protected)
		}
		assertProtected := func(index int) {
			for _, cm := range targets[index] {
				err := actors[index].Delete(ctx, cm, client.DryRunAll)
				Expect(apierrors.IsForbidden(err)).To(BeTrue())
				// Either protection webhook may reject a shared target first.
				// These fixtures have no live replication parent.
				Expect(err).To(MatchError(MatchRegexp("protected by a ResourcePermit|protected by a capsule replication; its managing parent is not yet available")))
				current := &corev1.ConfigMap{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), current)).To(Succeed())
				Expect(current.DeletionTimestamp).To(BeNil())
				Expect(current.Data).To(Equal(cm.Data))
			}
		}
		By("rejecting owner deletion while both tenants' namespaces are active")
		assertProtected(0)
		assertProtected(1)

		By("letting the namespace controller remove every protection-marker variant")
		Expect(k8sClient.Delete(ctx, namespaces[0])).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(namespaces[0]), &corev1.Namespace{}))
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		for _, cm := range targets[0] {
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), &corev1.ConfigMap{}))).To(BeTrue())
		}
		By("keeping the other tenant's resources protected and unchanged")
		assertProtected(1)
	})
})
