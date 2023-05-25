//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

var _ = Describe("trying to escalate from a Tenant Namespace ServiceAccount", func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sa-privilege-escalation",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "mario",
					Kind: "User",
				},
			},
			NodeSelector: map[string]string{
				"kubernetes.io/os": "linux",
			},
		},
	}

	ns := NewNamespace("attack")

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
	})

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})

	It("should block Namespace changes", func() {
		role := rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ns-update-role",
				Namespace: ns.GetName(),
			},
			Rules: []rbacv1.PolicyRule{
				{
					Verbs:         []string{"update"},
					APIGroups:     []string{""},
					Resources:     []string{"namespaces"},
					ResourceNames: []string{ns.GetName()},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), &role)
		}).Should(Succeed())

		rolebinding := rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "attacker-rolebinding",
				Namespace: ns.GetName(),
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      "attacker",
					Namespace: ns.GetName(),
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     role.GetName(),
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), &rolebinding)
		}).Should(Succeed())

		c, err := config.GetConfig()
		Expect(err).ToNot(HaveOccurred())
		c.Impersonate.Groups = []string{"system:serviceaccounts"}
		c.Impersonate.UserName = fmt.Sprintf("system:serviceaccount:%s:%s", rolebinding.Subjects[0].Namespace, rolebinding.Subjects[0].Name)
		saClient, err := kubernetes.NewForConfig(c)
		Expect(err).ToNot(HaveOccurred())
		// Changing Owner Reference is forbidden
		Consistently(func() error {
			if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns); err != nil {
				return err
			}

			ns.OwnerReferences[0].UID = uuid.NewUUID()

			_, err = saClient.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})

			return err
		}, 10*time.Second, time.Second).ShouldNot(Succeed())
		// Removing Owner Reference is forbidden
		Consistently(func() error {
			if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns); err != nil {
				return err
			}

			ns.SetOwnerReferences(nil)

			_, err = saClient.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})

			return err
		}, 10*time.Second, time.Second).ShouldNot(Succeed())
		// Breaking nodeSelector is forbidden
		Consistently(func() error {
			if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns); err != nil {
				return err
			}

			ns.SetAnnotations(map[string]string{
				"scheduler.alpha.kubernetes.io/node-selector": "kubernetes.io/os=forbidden",
			})

			_, err = saClient.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})

			return err
		}, 10*time.Second, time.Second).ShouldNot(Succeed())
	})
})
