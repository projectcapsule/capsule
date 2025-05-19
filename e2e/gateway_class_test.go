// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/projectcapsule/capsule/pkg/api"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

var _ = Describe("when Tenant handles Gateway classes", func() {
	crd := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "customer-class",
			Labels: map[string]string{
				"env": "production",
			},
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "projectcapsule.dev/customer-controller",
		},
	}

	tntWithDefault := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tnt-with-default-gateway-class-only",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: []capsulev1beta2.OwnerSpec{
				{
					Name: "gateway-selector",
					Kind: "User",
				},
			},
			GatewayOptions: capsulev1beta2.GatewayOptions{
				AllowedClasses: &api.SelectionListWithDefaultSpec{
					Default: "customer-class",
				},
			},
		},
	}

	tntWithDefaultAndLabel := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tnt-with-default-gateway-class-and-label-selector",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: []capsulev1beta2.OwnerSpec{
				{
					Name: "gateway-selector",
					Kind: "User",
				},
			},
			GatewayOptions: capsulev1beta2.GatewayOptions{
				AllowedClasses: &api.SelectionListWithDefaultSpec{
					Default: "customer-class",
					SelectionListWithSpec: api.SelectionListWithSpec{
						LabelSelector: v1.LabelSelector{
							MatchLabels: map[string]string{
								"env": "production",
							},
						},
					},
				},
			},
		},
	}

	tntWithLabel := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tnt-with-label-selector-only",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: []capsulev1beta2.OwnerSpec{
				{
					Name: "gateway-selector",
					Kind: "User",
				},
			},
			GatewayOptions: capsulev1beta2.GatewayOptions{
				AllowedClasses: &api.SelectionListWithDefaultSpec{
					SelectionListWithSpec: api.SelectionListWithSpec{
						LabelSelector: v1.LabelSelector{
							MatchLabels: map[string]string{
								"env": "production",
							},
						},
					},
				},
			},
		},
	}

	JustBeforeEach(func() {
		utilruntime.Must(gatewayv1.Install(scheme.Scheme))
		for _, tnt := range []*capsulev1beta2.Tenant{tntWithDefaultAndLabel, tntWithLabel, tntWithDefault} {
			EventuallyCreation(func() error {
				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
		}
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), crd)
		}).Should(Succeed())

	})

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), crd)).Should(Succeed())
		for _, tnt := range []*capsulev1beta2.Tenant{tntWithDefaultAndLabel, tntWithLabel, tntWithDefault} {
			Eventually(func() error {
				return k8sClient.Delete(context.TODO(), tnt)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})

	It("should block a non allowed class", func() {
		nsForDefaultAndLabel := NewNamespace("nsfordefaultandlabel")
		NamespaceCreation(nsForDefaultAndLabel, tntWithDefaultAndLabel.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithDefaultAndLabel, defaultTimeoutInterval).Should(ContainElement(nsForDefaultAndLabel.GetName()))

		By("using non-existent gatewayClassName", func() {
			Eventually(func() (err error) {
				g := &gatewayv1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "denied-gateway",
						Namespace: nsForDefaultAndLabel.GetName(),
					},
					Spec: gatewayv1.GatewaySpec{
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
								Port:     80,
							},
						},
						GatewayClassName: gatewayv1.ObjectName("unauthorized-class"),
					},
				}
				err = k8sClient.Create(context.TODO(), g)
				return
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
	})
})
