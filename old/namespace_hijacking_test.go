//go:build e2e

// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"math/rand"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("creating several Namespaces for a Tenant", func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "capsule-ns-attack-1",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "charlie",
					Kind: "User",
				},
				{
					Kind: "ServiceAccount",
					Name: "system:serviceaccount:attacker-system:attacker",
				},
			},
		},
	}

	kubeSystem := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kube-system",
		},
	}
	JustBeforeEach(func() {
		EventuallyCreation(func() (err error) {
			tnt.ResourceVersion = ""
			err = k8sClient.Create(context.TODO(), tnt)

			return
		}).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())

	})

	It("Can't hijack offlimits namespace", func() {
		tenant := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.Name}, tenant)).Should(Succeed())

		// Get the namespace
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: kubeSystem.GetName()}, kubeSystem)).Should(Succeed())

		for _, owner := range tnt.Spec.Owners {
			cs := ownerClient(owner)

			patch := []byte(fmt.Sprintf(`{"metadata":{"ownerReferences":[{"apiVersion":"%s/%s","kind":"Tenant","name":"%s","uid":"%s"}]}}`, capsulev1beta2.GroupVersion.Group, capsulev1beta2.GroupVersion.Version, tenant.GetName(), tenant.GetUID()))

			_, err := cs.CoreV1().Namespaces().Patch(context.TODO(), kubeSystem.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
			Expect(err).To(HaveOccurred())

		}
	})

	It("Owners can create and attempt to patch new namespaces but patches should not be applied", func() {
		for _, owner := range tnt.Spec.Owners {
			cs := ownerClient(owner)

			// Each owner creates a new namespace
			ns := NewNamespace("")
			NamespaceCreation(ns, owner, defaultTimeoutInterval).Should(Succeed())

			// Attempt to patch the owner references of the new namespace
			tenant := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.Name}, tenant)).Should(Succeed())

			randomUID := types.UID(fmt.Sprintf("%d", rand.Int()))
			randomName := fmt.Sprintf("random-tenant-%d", rand.Int())
			patch := []byte(fmt.Sprintf(`{"metadata":{"ownerReferences":[{"apiVersion":"%s/%s","kind":"Tenant","name":"%s","uid":"%s"}]}}`, capsulev1beta2.GroupVersion.Group, capsulev1beta2.GroupVersion.Version, randomName, randomUID))

			_, err := cs.CoreV1().Namespaces().Patch(context.TODO(), ns.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
			Expect(err).ToNot(HaveOccurred())

			retrievedNs := &corev1.Namespace{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.Name}, retrievedNs)).Should(Succeed())

			// Check if the namespace has an owner reference with the specific UID and name
			hasSpecificOwnerRef := false
			for _, ownerRef := range retrievedNs.OwnerReferences {
				if ownerRef.UID == randomUID && ownerRef.Name == randomName {
					hasSpecificOwnerRef = true
					break
				}
			}
			Expect(hasSpecificOwnerRef).To(BeFalse(), "Namespace should not have owner reference with UID %s and name %s", randomUID, randomName)

			hasOriginReference := false
			for _, ownerRef := range retrievedNs.OwnerReferences {
				if ownerRef.UID == tenant.GetUID() && ownerRef.Name == tenant.GetName() {
					hasOriginReference = true
					break
				}
			}
			Expect(hasOriginReference).To(BeTrue(), "Namespace should have origin reference", tenant.GetUID(), tenant.GetName())
		}
	})

})
