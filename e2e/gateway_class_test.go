// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

var _ = Describe("when Tenant handles Gateway classes", Label("tenant", "classes", "gateway"), func() {
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

	exact := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "legacy",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "projectcapsule.dev/customer-controller",
		},
	}

	exactU := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "legacy-2",
			Labels: map[string]string{
				"env": "e2e",
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

	tntWithDefault := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-gateway-default-and-label-selector",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: []api.OwnerSpec{
				{
					UserSpec: api.UserSpec{
						Name: "gateway-default-and-label-selector",
						Kind: "User",
					},
				},
			},
			GatewayOptions: capsulev1beta2.GatewayOptions{
				AllowedClasses: &api.DefaultAllowedListSpec{
					Default: "customer-class",
					SelectorAllowedListSpec: api.SelectorAllowedListSpec{
						AllowedListSpec: api.AllowedListSpec{
							Exact: []string{"legacy-2"},
						},
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

	tntWithoutDefault := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-gateway-label-selector-only",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: []api.OwnerSpec{
				{
					UserSpec: api.UserSpec{
						Name: "gateway-with-label-selector-only",
						Kind: "User",
					},
				},
			},
			GatewayOptions: capsulev1beta2.GatewayOptions{
				AllowedClasses: &api.DefaultAllowedListSpec{
					SelectorAllowedListSpec: api.SelectorAllowedListSpec{
						AllowedListSpec: api.AllowedListSpec{
							Exact: []string{"legacy"},
						},
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

	tntNoRestrictions := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-gateway-no-restrictions",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: []api.OwnerSpec{
				{
					UserSpec: api.UserSpec{
						Name: "e2e-gateway-no-restrictions",
						Kind: "User",
					},
				},
			},
		},
	}

	JustBeforeEach(func() {
		utilruntime.Must(gatewayv1.Install(scheme.Scheme))
		for _, tnt := range []*capsulev1beta2.Tenant{tntWithDefault, tntWithoutDefault, tntNoRestrictions} {
			tnt.ResourceVersion = ""
			EventuallyCreation(func() error {
				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
		}
		for _, crd := range []*gatewayv1.GatewayClass{authorized, unauthorized, exact, exactU} {
			Eventually(func() error {
				crd.ResourceVersion = ""
				return k8sClient.Create(context.TODO(), crd)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})
	JustAfterEach(func() {
		utilruntime.Must(gatewayv1.Install(scheme.Scheme))
		for _, tnt := range []*capsulev1beta2.Tenant{tntWithDefault, tntWithoutDefault, tntNoRestrictions} {
			EventuallyCreation(func() error {
				return ignoreNotFound(k8sClient.Delete(context.TODO(), tnt))
			}).Should(Succeed())
		}

		Eventually(func() (err error) {
			req, _ := labels.NewRequirement("env", selection.Exists, nil)

			return k8sClient.DeleteAllOf(context.TODO(), &gatewayv1.GatewayClass{}, &client.DeleteAllOfOptions{
				ListOptions: client.ListOptions{
					LabelSelector: labels.NewSelector().Add(*req),
				},
			})
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})
	It("should allow all classes", func() {
		By("Verify Status (Creation)", func() {
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntNoRestrictions.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.GatewayClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf(exact.GetName(), exactU.GetName(), authorized.GetName(), unauthorized.GetName()))
		})

		ns := NewNamespace("")
		NamespaceCreation(ns, tntNoRestrictions.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntNoRestrictions, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("providing any storageclass", func() {
			for _, class := range []*gatewayv1.GatewayClass{authorized, unauthorized, exact, exactU} {
				c := class.GetName()
				Eventually(func() (err error) {
					g := &gatewayv1.Gateway{
						ObjectMeta: metav1.ObjectMeta{
							Name:      class.GetName() + "-gateway",
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
							GatewayClassName: gatewayv1.ObjectName(c),
						},
					}

					err = k8sClient.Create(context.TODO(), g)
					return
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})

		By("providing nonexistent gatewayClassName", func() {
			Eventually(func() (err error) {
				g := &gatewayv1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "nonexistent-gateway",
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
				err = k8sClient.Create(context.TODO(), g)
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("Verify Status (Deletion)", func() {
			for _, crd := range []*gatewayv1.GatewayClass{authorized} {
				Expect(ignoreNotFound(k8sClient.Delete(context.TODO(), crd))).To(Succeed())
			}
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntNoRestrictions.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.GatewayClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf(exact.GetName(), exactU.GetName(), unauthorized.GetName()))
		})

	})

	It("should block Gateway", func() {
		By("Verify Status (Creation)", func() {
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}

				// return the error so Eventually will retry until it’s nil
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntWithDefault.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.GatewayClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf(exactU.GetName(), authorized.GetName()))
		})

		ns := NewNamespace("")
		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("providing unauthorized gatewayClassName", func() {
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

		By("providing nonexistent gatewayClassName", func() {
			Eventually(func() (err error) {
				g := &gatewayv1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "nonexistent-gateway",
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
				err = k8sClient.Create(context.TODO(), g)
				return
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})

		By("Verify Status (Deletion)", func() {
			for _, crd := range []*gatewayv1.GatewayClass{authorized} {
				Expect(ignoreNotFound(k8sClient.Delete(context.TODO(), crd))).To(Succeed())
			}
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}

				// return the error so Eventually will retry until it’s nil
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntWithDefault.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.GatewayClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf(exactU.GetName()))

			for _, crd := range []*gatewayv1.GatewayClass{exactU} {
				Expect(ignoreNotFound(k8sClient.Delete(context.TODO(), crd))).To(Succeed())
			}
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}

				// return the error so Eventually will retry until it’s nil
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntWithDefault.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.GatewayClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf())
		})

	})
	It("should allow Gateway", func() {
		By("Verify Status (Creation)", func() {
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}

				// return the error so Eventually will retry until it’s nil
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntWithDefault.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.GatewayClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf(exactU.GetName(), authorized.GetName()))
		})

		ns := NewNamespace("")
		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
		By("providing authorized class", func() {
			Eventually(func() (err error) {
				g := &gatewayv1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "authorized-gateway",
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
						GatewayClassName: gatewayv1.ObjectName("customer-class"),
					},
				}
				err = k8sClient.Create(context.TODO(), g)
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("providing authorized class (exact)", func() {
			Eventually(func() (err error) {
				g := &gatewayv1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "authorized-gateway-exact",
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
						GatewayClassName: gatewayv1.ObjectName("legacy-2"),
					},
				}
				err = k8sClient.Create(context.TODO(), g)
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("providing no gatewayClassName", func() {
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
				},
			}
			Expect(k8sClient.Create(context.TODO(), g)).Should(Succeed())
			gw := &gatewayv1.Gateway{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: g.GetName(), Namespace: g.Namespace}, gw)).Should(Succeed())
			Expect(gw.Spec.GatewayClassName).Should(Equal(gatewayv1.ObjectName("customer-class")))

			return
		})
	})
	It("should fail on invalid configuration", func() {
		By("Verify Status (Creation)", func() {
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}

				// return the error so Eventually will retry until it’s nil
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntWithoutDefault.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.GatewayClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf(exact.GetName(), authorized.GetName()))
		})

		ns := NewNamespace("")
		NamespaceCreation(ns, tntWithoutDefault.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithoutDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
		By("providing empty GatewayClassName", func() {
			Eventually(func() (err error) {
				g := &gatewayv1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "empty-gateway",
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
					},
				}
				err = k8sClient.Create(context.TODO(), g)
				return
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})

		By("Verify Status (Deletion)", func() {
			for _, crd := range []*gatewayv1.GatewayClass{authorized} {
				Expect(ignoreNotFound(k8sClient.Delete(context.TODO(), crd))).To(Succeed())
			}
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}

				// return the error so Eventually will retry until it’s nil
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntWithoutDefault.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.GatewayClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf(exact.GetName()))

			for _, crd := range []*gatewayv1.GatewayClass{exact} {
				Expect(ignoreNotFound(k8sClient.Delete(context.TODO(), crd))).To(Succeed())
			}
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}

				// return the error so Eventually will retry until it’s nil
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntWithoutDefault.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.GatewayClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf())
		})
	})
})
