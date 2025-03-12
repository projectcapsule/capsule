//go:build e2e

// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

var _ = Describe("creating a Service with user-specified labels and annotations", func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-user-metadata-forbidden",
		},
		Spec: capsulev1beta2.TenantSpec{
			ServiceOptions: &api.ServiceOptions{
				ForbiddenLabels: api.ForbiddenListSpec{
					Exact: []string{"foo", "bar"},
					Regex: "^gatsby-.*$",
				},
				ForbiddenAnnotations: api.ForbiddenListSpec{
					Exact: []string{"foo", "bar"},
					Regex: "^gatsby-.*$",
				},
			},
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "gatsby",
					Kind: "User",
				},
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})

	It("should allow", func() {
		By("specifying non-forbidden labels", func() {
			ns := NewNamespace("")
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			svc := NewService(types.NamespacedName{
				Namespace: ns.GetName(),
				Name:      "non-forbidden-labels",
			})
			svc.SetLabels(map[string]string{"bim": "baz"})
			ServiceCreation(svc, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		})
		By("specifying non-forbidden annotations", func() {
			ns := NewNamespace("")
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			svc := NewService(types.NamespacedName{
				Namespace: ns.GetName(),
				Name:      "non-forbidden-annotations",
			})
			svc.SetAnnotations(map[string]string{"bim": "baz"})
			ServiceCreation(svc, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		})
	})

	It("should fail when creating a Service", func() {
		By("specifying forbidden labels using exact match", func() {
			ns := NewNamespace("")
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			svc := NewService(types.NamespacedName{
				Namespace: ns.GetName(),
				Name:      "forbidden-labels-exact",
			})
			svc.SetLabels(map[string]string{"foo": "bar"})
			ServiceCreation(svc, tnt.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
		})
		By("specifying forbidden labels using regex match", func() {
			ns := NewNamespace("")
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			svc := NewService(types.NamespacedName{
				Namespace: ns.GetName(),
				Name:      "forbidden-labels-regex",
			})
			svc.SetLabels(map[string]string{"gatsby-foo": "bar"})
			ServiceCreation(svc, tnt.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
		})
		By("specifying forbidden annotations using exact match", func() {
			ns := NewNamespace("")
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			svc := NewService(types.NamespacedName{
				Namespace: ns.GetName(),
				Name:      "forbidden-annotations-exact",
			})
			svc.SetAnnotations(map[string]string{"foo": "bar"})
			ServiceCreation(svc, tnt.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
		})
		By("specifying forbidden annotations using regex match", func() {
			ns := NewNamespace("")
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			svc := NewService(types.NamespacedName{
				Namespace: ns.GetName(),
				Name:      "forbidden-annotations-regex",
			})
			svc.SetAnnotations(map[string]string{"gatsby-foo": "bar"})
			ServiceCreation(svc, tnt.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
		})
	})

	It("should fail when updating a Service", func() {
		cs := ownerClient(tnt.Spec.Owners[0])

		By("specifying forbidden labels using exact match", func() {
			ns := NewNamespace("")
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			svc := NewService(types.NamespacedName{
				Namespace: ns.GetName(),
				Name:      "forbidden-labels-exact-match",
			})
			ServiceCreation(svc, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			Consistently(func() error {
				svc, err := cs.CoreV1().Services(svc.Namespace).Get(context.Background(), svc.GetName(), metav1.GetOptions{})
				if err != nil {
					return nil
				}
				svc.SetLabels(map[string]string{"foo": "bar"})

				_, err = cs.CoreV1().Services(svc.Namespace).Update(context.Background(), svc, metav1.UpdateOptions{})

				return err
			}, 10*time.Second, time.Second).ShouldNot(Succeed())
		})
		By("specifying forbidden labels using regex match", func() {
			ns := NewNamespace("")
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			svc := NewService(types.NamespacedName{
				Namespace: ns.GetName(),
				Name:      "forbidden-labels-regex-match",
			})
			ServiceCreation(svc, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			Consistently(func() error {
				svc, err := cs.CoreV1().Services(svc.Namespace).Get(context.Background(), svc.GetName(), metav1.GetOptions{})
				if err != nil {
					return nil
				}

				svc.SetLabels(map[string]string{"gatsby-foo": "bar"})

				_, err = cs.CoreV1().Services(svc.Namespace).Update(context.Background(), svc, metav1.UpdateOptions{})

				return err
			}, 3*time.Second, time.Second).ShouldNot(Succeed())
		})
		By("specifying forbidden annotations using exact match", func() {
			ns := NewNamespace("")
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			svc := NewService(types.NamespacedName{
				Namespace: ns.GetName(),
				Name:      "forbidden-annotations-exact-match",
			})
			ServiceCreation(svc, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			Consistently(func() error {
				svc, err := cs.CoreV1().Services(svc.Namespace).Get(context.Background(), svc.GetName(), metav1.GetOptions{})
				if err != nil {
					return nil
				}

				svc.SetAnnotations(map[string]string{"foo": "bar"})

				_, err = cs.CoreV1().Services(svc.Namespace).Update(context.Background(), svc, metav1.UpdateOptions{})

				return err
			}, 10*time.Second, time.Second).ShouldNot(Succeed())
		})
		By("specifying forbidden annotations using regex match", func() {
			ns := NewNamespace("")
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			svc := NewService(types.NamespacedName{
				Namespace: ns.GetName(),
				Name:      "forbidden-annotations-regex-match",
			})
			ServiceCreation(svc, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			Consistently(func() error {
				svc, err := cs.CoreV1().Services(svc.Namespace).Get(context.Background(), svc.GetName(), metav1.GetOptions{})
				if err != nil {
					return nil
				}

				svc.SetAnnotations(map[string]string{"gatsby-foo": "bar"})

				_, err = cs.CoreV1().Services(svc.Namespace).Update(context.Background(), svc, metav1.UpdateOptions{})

				return err
			}, 10*time.Second, time.Second).ShouldNot(Succeed())
		})
	})
})
