// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

var _ = Describe("verify scalability", Label("scalability"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-scalability",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "gatsby",
							Kind: "User",
						},
					},
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

	It("verify lifecycle (scalability)", func() {
		const amount = 50
		const podsPerNamespace int32 = 1

		getTenant := func() *capsulev1beta2.Tenant {
			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)).To(Succeed())
			return t
		}

		waitSize := func(expected uint) {
			Eventually(func() uint {
				return getTenant().Status.Size
			}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(expected))
		}

		waitInstancePresent := func(ns *corev1.Namespace) {
			Eventually(func() error {
				t := getTenant()
				inst := t.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{
					Name: ns.GetName(),
					UID:  ns.GetUID(),
				})
				if inst == nil {
					return fmt.Errorf("instance not found for ns=%q uid=%q", ns.GetName(), ns.GetUID())
				}

				condition := inst.Conditions.GetConditionByType(meta.ReadyCondition)
				Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")
				if inst == nil {
					return fmt.Errorf("instance not found for ns=%q uid=%q", ns.GetName(), ns.GetUID())
				}

				if inst.Name != ns.GetName() {
					return fmt.Errorf("instance.Name=%q, want %q", inst.Name, ns.GetName())
				}

				cond := inst.Conditions.GetConditionByType(meta.ReadyCondition)
				if cond == nil {
					return fmt.Errorf("missing %q condition", meta.ReadyCondition)
				}
				if cond.Type != meta.ReadyCondition {
					return fmt.Errorf("cond.Type=%q, want %q", cond.Type, meta.ReadyCondition)
				}
				if cond.Status != metav1.ConditionTrue {
					return fmt.Errorf("cond.Status=%q, want %q", cond.Status, metav1.ConditionTrue)
				}
				if cond.Reason != meta.SucceededReason {
					return fmt.Errorf("cond.Reason=%q, want %q", cond.Reason, meta.SucceededReason)
				}

				return nil
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}

		waitInstanceAbsent := func(ns *corev1.Namespace) {
			Eventually(func() bool {
				t := getTenant()
				inst := t.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{
					Name: ns.GetName(),
					UID:  ns.GetUID(),
				})
				return inst == nil
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		}

		// --- Scale up: create N namespaces and verify Tenant status each time ---
		namespaces := make([]*corev1.Namespace, 0, amount)
		for i := 0; i < amount; i++ {
			ns := NewNamespace(fmt.Sprintf("scale-%d", i))
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			// Expect size bumped to i+1 and instance present
			waitSize(uint(i + 1))
			waitInstancePresent(ns)

			// --- NEW: create low-impact traffic pods in the namespace ---
			dep := newTrafficDeployment(ns.GetName(), podsPerNamespace)
			EventuallyCreation(func() error {
				return k8sClient.Create(context.TODO(), dep)
			}).Should(Succeed())

			// Wait until pods are actually scheduled & ready
			waitDeploymentReady(context.TODO(), ns.GetName(), dep.Name, podsPerNamespace)

			namespaces = append(namespaces, ns)
		}

		// --- Scale down: delete N namespaces and verify Tenant status each time ---
		for i := 0; i < amount; i++ {
			ns := namespaces[i]
			Expect(k8sClient.Delete(context.TODO(), ns)).To(Succeed())

			// Expect size decremented and instance absent
			waitSize(uint(amount - i - 1))
			waitInstanceAbsent(ns)
		}

	})

})

func newTrafficDeployment(ns string, replicas int32) *appsv1.Deployment {
	labels := map[string]string{"app": "traffic-pause"}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "traffic-pause",
			Namespace: ns,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					// pause container keeps footprint tiny
					Containers: []corev1.Container{
						{
							Name:  "pause",
							Image: "registry.k8s.io/pause:3.9",
							// No resources specified => requests/limits default to zero.
							// Resources: corev1.ResourceRequirements{},
						},
					},
				},
			},
		},
	}
}

func scaleDeployment(ctx context.Context, ns, name string, replicas int32) {
	Eventually(func() error {
		d := &appsv1.Deployment{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, d); err != nil {
			return err
		}
		*d.Spec.Replicas = replicas
		return k8sClient.Update(ctx, d)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

	waitDeploymentReady(ctx, ns, name, replicas)
}

func waitDeploymentReady(ctx context.Context, ns, name string, replicas int32) {
	Eventually(func() (int32, error) {
		d := &appsv1.Deployment{}
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, d)
		if err != nil {
			return 0, err
		}
		return d.Status.ReadyReplicas, nil
	}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(replicas))
}
