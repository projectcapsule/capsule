// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"sort"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

var _ = Describe("when Tenant handles Node status", Label("tenant", "nodes", "status"), func() {
	const (
		e2eNodeLabelKey   = "capsule.clastix.io/e2e-node-status"
		e2eNodeLabelValue = "true"
	)

	tntNoRestrictions := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-node-status-no-restrictions",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "e2e-node-status-no-restrictions",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	tntWithSelector := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-node-status-with-selector",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "e2e-node-status-with-selector",
							Kind: "User",
						},
					},
				},
			},
			NodeSelector: map[string]string{
				e2eNodeLabelKey: e2eNodeLabelValue,
			},
		},
	}

	var (
		allNodeNames  []string
		primaryNode   string
		secondaryNode string
	)

	setNodeLabel := func(nodeName string, value *string) error {
		node := &corev1.Node{}
		if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: nodeName}, node); err != nil {
			return err
		}

		labels := node.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}

		if value == nil {
			delete(labels, e2eNodeLabelKey)
		} else {
			labels[e2eNodeLabelKey] = *value
		}

		node.SetLabels(labels)

		return k8sClient.Update(context.TODO(), node)
	}

	JustBeforeEach(func() {
		nodeList := &corev1.NodeList{}
		Expect(k8sClient.List(context.TODO(), nodeList)).To(Succeed())
		Expect(nodeList.Items).ToNot(BeEmpty())

		allNodeNames = allNodeNames[:0]
		for i := range nodeList.Items {
			allNodeNames = append(allNodeNames, nodeList.Items[i].GetName())
		}
		sort.Strings(allNodeNames)

		primaryNode = nodeList.Items[0].GetName()
		secondaryNode = ""
		if len(nodeList.Items) > 1 {
			secondaryNode = nodeList.Items[1].GetName()
		}

		for i := range nodeList.Items {
			name := nodeList.Items[i].GetName()
			EventuallyCreation(func() error {
				if name == primaryNode {
					return setNodeLabel(name, ptrTo(e2eNodeLabelValue))
				}

				return setNodeLabel(name, nil)
			}).Should(Succeed())
		}

		for _, tnt := range []*capsulev1beta2.Tenant{tntNoRestrictions, tntWithSelector} {
			EventuallyCreation(func() error {
				tnt.ResourceVersion = ""
				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
		}
	})

	JustAfterEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{tntNoRestrictions, tntWithSelector} {
			EventuallyCreation(func() error {
				return ignoreNotFound(k8sClient.Delete(context.TODO(), tnt))
			}).Should(Succeed())
		}

		nodeList := &corev1.NodeList{}
		Expect(k8sClient.List(context.TODO(), nodeList)).To(Succeed())

		for i := range nodeList.Items {
			name := nodeList.Items[i].GetName()
			EventuallyCreation(func() error {
				return setNodeLabel(name, nil)
			}).Should(Succeed())
		}
	})

	It("should reconcile status nodes on create and metadata update events", func() {
		By("verifying initial status nodes")
		Eventually(func() ([]string, error) {
			t := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tntNoRestrictions.GetName()}, t); err != nil {
				return nil, err
			}

			return t.Status.Nodes, nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(allNodeNames))

		Eventually(func() ([]string, error) {
			t := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tntWithSelector.GetName()}, t); err != nil {
				return nil, err
			}

			return t.Status.Nodes, nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Equal([]string{primaryNode}))

		By("updating node labels to trigger metadata-based status reconciliation")
		EventuallyCreation(func() error {
			return setNodeLabel(primaryNode, nil)
		}).Should(Succeed())

		expectedSelectorNodes := []string{}
		if secondaryNode != "" {
			EventuallyCreation(func() error {
				return setNodeLabel(secondaryNode, ptrTo(e2eNodeLabelValue))
			}).Should(Succeed())
			expectedSelectorNodes = append(expectedSelectorNodes, secondaryNode)
		}
		sort.Strings(expectedSelectorNodes)

		Eventually(func() ([]string, error) {
			t := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tntWithSelector.GetName()}, t); err != nil {
				return nil, err
			}

			return t.Status.Nodes, nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(expectedSelectorNodes))

		Eventually(func() ([]string, error) {
			t := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tntNoRestrictions.GetName()}, t); err != nil {
				return nil, err
			}

			return t.Status.Nodes, nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(allNodeNames))
	})
})

func ptrTo(s string) *string {
	return &s
}
