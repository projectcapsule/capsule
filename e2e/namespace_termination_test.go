// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("terminating namespace with guardrails", Label("namespace", "termination"), func() {
	ctx := context.TODO()

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-termination",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "gatsby",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	var (
		nsName string
		podKey types.NamespacedName
	)

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(ctx, tnt)
		}).Should(Succeed())

		// Create namespace as tenant owner
		ns := NewNamespace("")
		nsName = ns.GetName()

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())

		// Create a pod with a finalizer so the namespace can't complete deletion
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "finalizer-pod",
				Namespace:  nsName,
				Finalizers: []string{"e2e.capsule.io/block-delete"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "pause",
						Image:   "registry.k8s.io/pause:3.9",
						Command: []string{"/pause"},
					},
				},
			},
		}
		podKey = types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())
	})

	JustAfterEach(func() {
		EventuallyDeletion(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: podKey.Name, Namespace: podKey.Namespace}})
		EventuallyDeletion(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}})
		EventuallyDeletion(tnt)
	})

	It("keeps managed rolebindings during namespace termination and cleans up after finalizer removal", func() {
		By("deleting the namespace (it should get stuck terminating due to pod finalizer)", func() {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
			Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
		})

		By("verifying namespace is terminating", func() {
			Eventually(func(g Gomega) {
				ns := &corev1.Namespace{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(ns.DeletionTimestamp).ToNot(BeNil(), "namespace should have deletionTimestamp")
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("while namespace is terminating, verify rolebindings are still present", func() {
			Consistently(func(g Gomega) {
				// Namespace likely still exists during this window
				ns := &corev1.Namespace{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns)
				g.Expect(err).ToNot(HaveOccurred())

				VerifyTenantRoleBindings(tnt)
			}, 10*time.Second, defaultPollInterval).Should(Succeed())
		})

		By("while namespace is terminating, verify tenant still exists and has controller finalizer", func() {
			Consistently(func(g Gomega) {
				cur := &capsulev1beta2.Tenant{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: tnt.GetName()}, cur)).To(Succeed())
				g.Expect(controllerutil.ContainsFinalizer(cur, meta.ControllerFinalizer)).To(BeTrue(),
					"tenant should still have controller finalizer while a namespace is terminating",
				)
			}, 10*time.Second, defaultPollInterval).Should(Succeed())
		})

		By("removing the pod finalizer to unblock namespace deletion", func() {
			Eventually(func(g Gomega) error {
				p := &corev1.Pod{}
				if err := k8sClient.Get(ctx, podKey, p); err != nil {
					// If it's already gone, we're done
					if apierrors.IsNotFound(err) {
						return nil
					}
					return err
				}

				// Already cleared
				if len(p.Finalizers) == 0 {
					return nil
				}

				p.Finalizers = nil
				return k8sClient.Update(ctx, p)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("verifying the pod is eventually deleted", func() {
			Eventually(func() bool {
				p := &corev1.Pod{}
				err := k8sClient.Get(ctx, podKey, p)
				return apierrors.IsNotFound(err)
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue(),
				"expected pod %s/%s to be deleted", podKey.Namespace, podKey.Name,
			)
		})

		By("verifying the namespace is eventually deleted", func() {
			Eventually(func() bool {
				ns := &corev1.Namespace{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns)
				return apierrors.IsNotFound(err)
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue(),
				"expected namespace %q to be deleted", nsName,
			)
		})

		By("verifying tenant still exists (and finalizer cleanup depending on policy)", func() {
			Eventually(func(g Gomega) {
				cur := &capsulev1beta2.Tenant{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: tnt.GetName()}, cur)
				g.Expect(err).ToNot(HaveOccurred())

				g.Expect(cur.Status.Size).Should(Equal(uint(0)))
				g.Expect(controllerutil.ContainsFinalizer(cur, meta.ControllerFinalizer)).To(BeFalse())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
	})
})
