//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/api"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

var _ = Describe("modifying node labels and annotations", func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-node-user-metadata-forbidden",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
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
		EventuallyCreation(func() error {
			return ModifyNode(func(node *corev1.Node) error {
				annotations := node.GetAnnotations()

				delete(annotations, "bim")
				delete(annotations, "foo")
				delete(annotations, "gatsby-foo")

				node.SetAnnotations(annotations)

				labels := node.GetLabels()

				delete(labels, "bim")
				delete(labels, "foo")
				delete(labels, "gatsby-foo")

				node.SetLabels(labels)

				return k8sClient.Update(context.Background(), node)
			})
		}).Should(Succeed())
	})

	It("should allow", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.NodeMetadata = &capsulev1beta2.NodeMetadata{
				ForbiddenLabels: api.ForbiddenListSpec{
					Exact: []string{"foo", "bar"},
					Regex: "^gatsby-.*$",
				},
				ForbiddenAnnotations: api.ForbiddenListSpec{
					Exact: []string{"foo", "bar"},
					Regex: "^gatsby-.*$",
				},
			}
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
