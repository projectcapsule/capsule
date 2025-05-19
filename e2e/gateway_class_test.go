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
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

var _ = Describe("when Tenant handles Gateway classes", Label("gateway"), func() {
	authorized := &gatewayv1.GatewayClass{
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

	unauthorized := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "unauthorized-class",
			Labels: map[string]string{
				"env": "production55",
			},
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "projectcapsule.dev/customer-controller",
		},
	}

	tntWithoutLabel := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tnt-with-default-gateway-class-only",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: []capsulev1beta2.OwnerSpec{
				{
					Name: "gateway-without-label-selector",
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

	tntWithDefault := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tnt-with-default-gateway-class-and-label-selector",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: []capsulev1beta2.OwnerSpec{
				{
					Name: "gateway-default-and-label-selector",
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
					Name: "gateway-with-label-selector-only",
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
		for _, tnt := range []*capsulev1beta2.Tenant{tntWithDefault, tntWithoutLabel, tntWithLabel} {
			EventuallyCreation(func() error {
				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
		}
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), authorized)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), unauthorized)
		}).Should(Succeed())
	})

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), authorized)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), unauthorized)).Should(Succeed())
		for _, tnt := range []*capsulev1beta2.Tenant{tntWithDefault, tntWithoutLabel, tntWithLabel} {
			Eventually(func() error {
				return k8sClient.Delete(context.TODO(), tnt)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})

	It("should block or mutate Gateway", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("providing unauthorized class (block)", func() {
			Eventually(func() (err error) {
				g := &gatewayv1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "denied-gateway",
						Namespace: ns.GetName(),
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

		By("providing nonexistent gatewayClassName (mutate)", func() {
			g := &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mutated-gateway",
					Namespace: ns.GetName(),
				},
				Spec: gatewayv1.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{
							Name:     "http",
							Protocol: gatewayv1.HTTPProtocolType,
							Port:     80,
						},
					},
					GatewayClassName: gatewayv1.ObjectName("very-unauthorized-and-nonexistent-class"),
				},
			}
			Expect(k8sClient.Create(context.TODO(), g)).Should(Succeed())
			gw := &gatewayv1.Gateway{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: g.GetName(), Namespace: g.Namespace}, gw)).Should(Succeed())
			Expect(gw.Spec.GatewayClassName).Should(Equal(gatewayv1.ObjectName("customer-class")))
			return
		})
	})
	It("should allow enabled Gateway", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
	})
})
