// Copyright 2020-2026 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("metadata mutating webhook", Ordered, Label("tenant", "metadata"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-metadata-webhook",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-metadata-webhook",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	ns := NewNamespace("")

	BeforeAll(func() {
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceReady(tnt, ns, 1)
	})

	AfterAll(func() {
		EventuallyDeletion(tnt)
	})

	It("adds the tenant labels to namespaced resources", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "e2e-metadata-webhook",
				Namespace: ns.GetName(),
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), cm)
		}).Should(Succeed())

		Expect(cm.GetLabels()).To(HaveKeyWithValue(meta.NewTenantLabel, tnt.GetName()))
		Expect(cm.GetLabels()).To(HaveKeyWithValue(meta.ManagedByCapsuleLabel, tnt.GetName()))
	})

	It("does not mutate LocalSubjectAccessReviews", func() {
		lsar := &authorizationv1.LocalSubjectAccessReview{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns.GetName(),
			},
			Spec: authorizationv1.SubjectAccessReviewSpec{
				User: tnt.Spec.Owners[0].Name,
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: ns.GetName(),
					Verb:      "get",
					Resource:  "pods",
				},
			},
		}

		// The API server rejects LocalSubjectAccessReviews with any metadata besides the namespace,
		// so a mutated review fails here rather than returning labels.
		res, err := clusterAdminClient().AuthorizationV1().LocalSubjectAccessReviews(ns.GetName()).Create(context.TODO(), lsar, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.GetLabels()).NotTo(HaveKey(meta.NewTenantLabel))
		Expect(res.GetLabels()).NotTo(HaveKey(meta.ManagedByCapsuleLabel))
		Expect(res.Status.Allowed).To(BeTrue())
	})
})
