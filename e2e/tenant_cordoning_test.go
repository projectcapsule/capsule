// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

var _ = Describe("cordoning a Tenant", Ordered, Label("tenant", "operations", "cordoning"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-cordoning",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-cordoning",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	patchNamespaceCordonedLabel := func(cs kubernetes.Interface, nsName string, value string) error {
		labels := map[string]any{
			meta.CordonedLabel: value,
		}

		if value == "" {
			labels[meta.CordonedLabel] = nil
		}

		patch, err := json.Marshal(map[string]any{
			"metadata": map[string]any{
				"labels": labels,
			},
		})
		if err != nil {
			return err
		}

		_, err = cs.CoreV1().Namespaces().Patch(
			context.TODO(),
			nsName,
			types.StrategicMergePatchType,
			patch,
			metav1.PatchOptions{},
		)

		return err
	}

	expectNamespaceExists := func(name string) {
		Eventually(func() error {
			current := &corev1.Namespace{}

			return k8sClient.Get(context.TODO(), types.NamespacedName{Name: name}, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	expectNamespaceDeleted := func(name string) {
		Eventually(func() bool {
			current := &corev1.Namespace{}
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: name}, current)

			return apierrors.IsNotFound(err)
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	}

	expectTenantCordonedCondition := func(expectedStatus metav1.ConditionStatus, expectedReason string) {
		Eventually(func(g Gomega) {
			t := &capsulev1beta2.Tenant{}
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)
			g.Expect(err).NotTo(HaveOccurred())

			condition := t.Status.Conditions.GetConditionByType(meta.CordonedCondition)
			g.Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

			g.Expect(condition.Type).To(Equal(meta.CordonedCondition))
			g.Expect(condition.Status).To(Equal(expectedStatus))
			g.Expect(condition.Reason).To(Equal(expectedReason))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	setTenantCordoned := func(cordoned bool) {
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, current); err != nil {
				return err
			}

			current.Spec.Cordoned = cordoned

			return k8sClient.Update(context.TODO(), current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	expectNamespaceCordonedLabel := func(nsName string, expected bool) {
		Eventually(func(g Gomega) {
			current := &corev1.Namespace{}
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: nsName}, current)
			g.Expect(err).NotTo(HaveOccurred())

			if expected {
				g.Expect(current.Labels).To(HaveKey(meta.CordonedLabel))
			} else {
				g.Expect(current.Labels).NotTo(HaveKey(meta.CordonedLabel))
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())

		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
	})

	JustAfterEach(func() {
		EventuallyDeletion(tnt)
	})

	It("should allow tenant owner to cordon a namespace by patching the cordoned label", func() {
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		By("patching the namespace cordoned label as tenant owner")
		Expect(patchNamespaceCordonedLabel(cs, ns.GetName(), meta.ValueTrue)).To(Succeed())
		expectNamespaceCordonedLabel(ns.GetName(), true)
	})

	It("should block namespace membership changes while Tenant is cordoned", func() {
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		existing := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		By("creating an initial namespace before cordoning")
		NamespaceCreation(existing, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, existing).Should(Succeed())

		By("cordoning the Tenant")
		setTenantCordoned(true)
		expectTenantCordonedCondition(metav1.ConditionTrue, meta.CordonedReason)
		expectNamespaceCordonedLabel(existing.GetName(), true)

		By("rejecting new namespace assignment to the cordoned Tenant")
		blocked := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		_, err := cs.CoreV1().Namespaces().Create(context.TODO(), blocked, metav1.CreateOptions{})
		Expect(err).To(HaveOccurred())

		By("rejecting deletion of an existing namespace while cordoned")
		err = cs.CoreV1().Namespaces().Delete(context.TODO(), existing.GetName(), metav1.DeleteOptions{})
		Expect(err).To(HaveOccurred())

		expectNamespaceExists(existing.GetName())

		By("uncordoning the Tenant allows namespace deletion again")
		setTenantCordoned(false)
		expectTenantCordonedCondition(metav1.ConditionFalse, meta.ActiveReason)
		expectNamespaceCordonedLabel(existing.GetName(), false)

		Expect(cs.CoreV1().Namespaces().Delete(context.TODO(), existing.GetName(), metav1.DeleteOptions{})).To(Succeed())
		expectNamespaceDeleted(existing.GetName())
	})

	It("should block content changes inside a namespace cordoned by tenant owner", func() {
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		By("cordoning the namespace directly")
		Expect(patchNamespaceCordonedLabel(cs, ns.GetName(), meta.ValueTrue)).To(Succeed())
		expectNamespaceCordonedLabel(ns.GetName(), true)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "blocked-create",
			},
			Spec: corev1.PodSpec{
				SecurityContext: nobodyPodSecurityContext(),
				Containers: []corev1.Container{
					{
						Name:            "container",
						Image:           "registry.k8s.io/pause:3.10",
						SecurityContext: restrictedContainerSecurityContext(),
					},
				},
			},
		}

		By("rejecting content creation inside the cordoned namespace")
		_, err := cs.CoreV1().Pods(ns.GetName()).Create(context.TODO(), pod, metav1.CreateOptions{})
		Expect(err).To(HaveOccurred())

		By("uncordoning the namespace allows content creation again")
		Expect(patchNamespaceCordonedLabel(cs, ns.GetName(), "")).To(Succeed())
		expectNamespaceCordonedLabel(ns.GetName(), false)

		_, err = cs.CoreV1().Pods(ns.GetName()).Create(context.TODO(), pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
	})

	It("should block or allow operations", func() {
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "container",
			},
			Spec: corev1.PodSpec{
				SecurityContext: nobodyPodSecurityContext(),
				Containers: []corev1.Container{
					{
						Name:            "container",
						Image:           "registry.k8s.io/pause:3.10",
						SecurityContext: restrictedContainerSecurityContext(),
					},
				},
			},
		}

		By("Verifying Tenant Status", func() {
			expectTenantCordonedCondition(metav1.ConditionFalse, meta.ActiveReason)
		})

		By("creating a Namespace and Pod", func() {
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

			EventuallyCreation(func() error {
				_, err := cs.CoreV1().Pods(ns.Name).Create(context.Background(), pod, metav1.CreateOptions{})

				if apierrors.IsAlreadyExists(err) {
					return nil
				}

				return err
			}).Should(Succeed())
		})

		By("cordoning the Tenant should add the cordoned Capsule label", func() {
			setTenantCordoned(true)
			expectNamespaceCordonedLabel(ns.GetName(), true)
		})

		By("Verifying Tenant Status after cordoning", func() {
			expectTenantCordonedCondition(metav1.ConditionTrue, meta.CordonedReason)
		})

		By("cordoning the Tenant deletion must be blocked", func() {
			setTenantCordoned(true)
			expectNamespaceCordonedLabel(ns.GetName(), true)

			Eventually(func() error {
				return cs.CoreV1().Pods(ns.Name).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})

		By("uncordoning the Tenant deletion must be allowed", func() {
			setTenantCordoned(false)
			expectNamespaceCordonedLabel(ns.GetName(), false)

			Eventually(func() error {
				err := cs.CoreV1().Pods(ns.Name).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
				if apierrors.IsNotFound(err) {
					return nil
				}

				return err
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("should not contain the cordoned Capsule label", func() {
			setTenantCordoned(false)
			expectNamespaceCordonedLabel(ns.GetName(), false)
		})

		By("Verifying Tenant Status after uncordoning", func() {
			expectTenantCordonedCondition(metav1.ConditionFalse, meta.ActiveReason)
		})
	})

})
