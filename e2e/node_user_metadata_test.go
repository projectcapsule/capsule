//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	"fmt"
	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	"github.com/clastix/capsule/pkg/webhook/utils"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("modifying node labels and annotations", func() {
	tnt := &capsulev1beta1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-node-user-metadata-forbidden",
		},
		Spec: capsulev1beta1.TenantSpec{
			Owners: capsulev1beta1.OwnerListSpec{
				{
					Name: "gatsby",
					Kind: "User",
				},
			},
		},
	}

	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-modifier",
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"nodes"},
				Verbs:     []string{"patch", "update", "get", "list"},
			},
		},
	}

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-modifier",
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     "node-modifier",
			APIGroup: rbacv1.GroupName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     rbacv1.UserKind,
				APIGroup: rbacv1.GroupName,
				Name:     "gatsby",
			},
		},
	}

	JustBeforeEach(func() {
		version := GetKubernetesVersion()
		nodeWebhookSupported, _ := utils.NodeWebhookSupported(version)

		if !nodeWebhookSupported {
			Skip(fmt.Sprintf("Node webhook is disabled for current version %s", version.String()))
		}

		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			cr.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), cr)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			crb.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), crb)
		}).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), crb)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), cr)).Should(Succeed())
	})

	It("should allow", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1alpha1.CapsuleConfiguration) {
			protected := map[string]string{
				capsulev1alpha1.ForbiddenNodeLabelsAnnotation:            "foo,bar",
				capsulev1alpha1.ForbiddenNodeLabelsRegexpAnnotation:      "^gatsby-.*$",
				capsulev1alpha1.ForbiddenNodeAnnotationsAnnotation:       "foo,bar",
				capsulev1alpha1.ForbiddenNodeAnnotationsRegexpAnnotation: "^gatsby-.*$",
			}
			configuration.SetAnnotations(protected)
		})
		By("adding non-forbidden labels", func() {
			EventuallyCreation(func() error {
				return ModifyNode(func(node *corev1.Node) error {
					node.Labels["bim"] = "baz"
					cs := ownerClient(tnt.Spec.Owners[0])

					_, err := cs.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
					return err
				})
			}).Should(Succeed())
		})
		By("modifying non-forbidden labels", func() {
			EventuallyCreation(func() error {
				return ModifyNode(func(node *corev1.Node) error {
					node.Labels["bim"] = "bom"
					cs := ownerClient(tnt.Spec.Owners[0])

					_, err := cs.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
					return err
				})
			}).Should(Succeed())
		})
		By("adding non-forbidden annotations", func() {
			EventuallyCreation(func() error {
				return ModifyNode(func(node *corev1.Node) error {
					node.Annotations["bim"] = "baz"
					cs := ownerClient(tnt.Spec.Owners[0])

					_, err := cs.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
					return err
				})
			}).Should(Succeed())
		})
		By("modifying non-forbidden annotations", func() {
			EventuallyCreation(func() error {
				return ModifyNode(func(node *corev1.Node) error {
					node.Annotations["bim"] = "bom"
					cs := ownerClient(tnt.Spec.Owners[0])

					_, err := cs.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
					return err
				})
			}).Should(Succeed())
		})
	})
	It("should fail", func() {
		Expect(ModifyNode(func(node *corev1.Node) error {
			node.Labels["foo"] = "bar"
			node.Labels["gatsby-foo"] = "bar"
			node.Annotations["foo"] = "bar"
			node.Annotations["gatsby-foo"] = "bar"
			return k8sClient.Update(context.Background(), node)
		})).Should(Succeed())

		By("adding forbidden labels using exact match", func() {
			EventuallyCreation(func() error {
				return ModifyNode(func(node *corev1.Node) error {
					node.Labels["bar"] = "baz"
					cs := ownerClient(tnt.Spec.Owners[0])

					_, err := cs.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
					return err
				})
			}).ShouldNot(Succeed())
		})
		By("adding forbidden labels using regex match", func() {
			EventuallyCreation(func() error {
				return ModifyNode(func(node *corev1.Node) error {
					node.Labels["gatsby-foo"] = "baz"
					cs := ownerClient(tnt.Spec.Owners[0])

					_, err := cs.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
					return err
				})
			}).ShouldNot(Succeed())
		})
		By("modifying forbidden labels", func() {
			EventuallyCreation(func() error {
				return ModifyNode(func(node *corev1.Node) error {
					node.Labels["foo"] = "baz"
					cs := ownerClient(tnt.Spec.Owners[0])

					_, err := cs.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
					return err
				})
			}).ShouldNot(Succeed())
		})
		By("adding forbidden annotations using exact match", func() {
			EventuallyCreation(func() error {
				return ModifyNode(func(node *corev1.Node) error {
					node.Annotations["bar"] = "baz"
					cs := ownerClient(tnt.Spec.Owners[0])

					_, err := cs.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
					return err
				})
			}).ShouldNot(Succeed())
		})
		By("adding forbidden annotations using regex match", func() {
			EventuallyCreation(func() error {
				return ModifyNode(func(node *corev1.Node) error {
					node.Annotations["gatsby-foo"] = "baz"
					cs := ownerClient(tnt.Spec.Owners[0])

					_, err := cs.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
					return err
				})
			}).ShouldNot(Succeed())
		})
		By("modifying forbidden annotations", func() {
			EventuallyCreation(func() error {
				return ModifyNode(func(node *corev1.Node) error {
					node.Annotations["foo"] = "baz"
					cs := ownerClient(tnt.Spec.Owners[0])

					_, err := cs.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
					return err
				})
			}).ShouldNot(Succeed())
		})
	})
})
