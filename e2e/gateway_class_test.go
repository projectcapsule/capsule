// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/utils"
)

func gatewayAdmissionClient(user string) client.Client {
	// Keep the tenant-owner username visible to admission while granting the
	// authorization needed for Gateway API resources.
	return impersonationClient(user, []string{"system:masters"})
}

var _ = Describe("when Tenant handles Gateway classes", Ordered, Label("tenant", "classes", "gatewayclass"), func() {
	authorized := &gatewayv1.GatewayClass{
		Name: "customer-class",
		Labels: map[string]string{
			"env": "production",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "projectcapsule.dev/customer-controller",
		},
	}

	exact := &gatewayv1.GatewayClass{
		Name: "legacy",
		Labels: map[string]string{
			"env": "e2e",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "projectcapsule.dev/customer-controller",
		},
	}

	exactU := &gatewayv1.GatewayClass{
		Name: "legacy-2",
		Labels: map[string]string{
			"env": "e2e",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "projectcapsule.dev/customer-controller",
		},
	}

	unauthorized := &gatewayv1.GatewayClass{
		Name: "unauthorized-class",
		Labels: map[string]string{
			"env": "production55",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "projectcapsule.dev/customer-controller",
		},
	}

	tntWithDefault := &capsulev1beta2.Tenant{
		Name: "e2e-gateway-default-and-label-selector",
		Labels: map[string]string{
			"env": "e2e",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: []rbac.OwnerSpec{
				{
					Name: "gateway-default-and-label-selector",
					Kind: "User",
				},
			},
			GatewayOptions: capsulev1beta2.GatewayOptions{
				AllowedClasses: &api.DefaultAllowedListSpec{
					Default: "customer-class",
					Exact:   []string{"legacy-2"},
					MatchLabels: map[string]string{
						"env": "production",
					},
				},
			},
		},
	}

	tntWithoutDefault := &capsulev1beta2.Tenant{
		Name: "e2e-gateway-label-selector-only",
		Labels: map[string]string{
			"env": "e2e",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: []rbac.OwnerSpec{
				{
					Name: "gateway-with-label-selector-only",
					Kind: "User",
				},
			},
			GatewayOptions: capsulev1beta2.GatewayOptions{
				AllowedClasses: &api.DefaultAllowedListSpec{
					Exact: []string{"legacy"},
					MatchLabels: map[string]string{
						"env": "production",
					},
				},
			},
		},
	}

	tntNoRestrictions := &capsulev1beta2.Tenant{
		Name: "e2e-gateway-no-restrictions",
		Spec: capsulev1beta2.TenantSpec{
			Owners: []rbac.OwnerSpec{
				{
					Name: "e2e-gateway-no-restrictions",
					Kind: "User",
				},
			},
		},
	}

	JustBeforeEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{tntWithDefault, tntWithoutDefault, tntNoRestrictions} {
			tnt.ResourceVersion = ""
			EventuallyCreation(func() error {
				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}

		if err := k8sClient.List(context.Background(), &gatewayv1.GatewayClassList{}); err != nil {
			if utils.IsUnsupportedAPI(err) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

		utilruntime.Must(gatewayv1.Install(scheme.Scheme))

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
			EventuallyDeletion(tnt)
		}

		if err := k8sClient.List(context.Background(), &gatewayv1.GatewayClassList{}); err != nil {
			if utils.IsUnsupportedAPI(err) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

		Eventually(func() (err error) {
			req, _ := labels.NewRequirement("env", selection.Exists, nil)

			return k8sClient.DeleteAllOf(context.TODO(), &gatewayv1.GatewayClass{}, &client.DeleteAllOfOptions{
				LabelSelector: labels.NewSelector().Add(*req),
			})
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})
	It("should allow all classes", func() {
		if err := k8sClient.List(context.Background(), &gatewayv1.GatewayClassList{}); err != nil {
			if utils.IsUnsupportedAPI(err) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

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

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tntNoRestrictions.GetName(),
		})
		NamespaceCreation(ns, tntNoRestrictions.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tntNoRestrictions, ns).Should(Succeed())

		admissionClient := gatewayAdmissionClient(tntNoRestrictions.Spec.Owners[0].UserSpec.Name)

		By("providing any storageclass", func() {
			for _, class := range []*gatewayv1.GatewayClass{authorized, unauthorized, exact, exactU} {
				c := class.GetName()
				Eventually(func() (err error) {
					g := &gatewayv1.Gateway{
						Name:      class.GetName() + "-gateway",
						Namespace: ns.GetName(),
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

					err = admissionClient.Create(context.TODO(), g)
					return
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})

		By("providing nonexistent gatewayClassName", func() {
			Eventually(func() (err error) {
				g := &gatewayv1.Gateway{
					Name:      "nonexistent-gateway",
					Namespace: ns.GetName(),
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
				err = admissionClient.Create(context.TODO(), g)
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("Verify Status (Deletion)", func() {
			for _, crd := range []*gatewayv1.GatewayClass{authorized} {
				EventuallyDeletion(crd)
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
		if err := k8sClient.List(context.Background(), &gatewayv1.GatewayClassList{}); err != nil {
			if utils.IsUnsupportedAPI(err) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

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

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tntWithDefault.GetName(),
		})
		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tntWithDefault, ns).Should(Succeed())

		admissionClient := gatewayAdmissionClient(tntWithDefault.Spec.Owners[0].UserSpec.Name)

		By("providing unauthorized gatewayClassName", func() {
			Eventually(func() (err error) {
				g := &gatewayv1.Gateway{
					Name:      "denied-gateway",
					Namespace: ns.GetName(),
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
				err = admissionClient.Create(context.TODO(), g)
				return
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})

		By("providing nonexistent gatewayClassName", func() {
			Eventually(func() (err error) {
				g := &gatewayv1.Gateway{
					Name:      "nonexistent-gateway",
					Namespace: ns.GetName(),
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
				err = admissionClient.Create(context.TODO(), g)
				return
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})

		By("Verify Status (Deletion)", func() {
			for _, crd := range []*gatewayv1.GatewayClass{authorized} {
				EventuallyDeletion(crd)
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
				EventuallyDeletion(crd)
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
		if err := k8sClient.List(context.Background(), &gatewayv1.GatewayClassList{}); err != nil {
			if utils.IsUnsupportedAPI(err) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

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

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tntWithDefault.GetName(),
		})
		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tntWithDefault, ns).Should(Succeed())

		admissionClient := gatewayAdmissionClient(tntWithDefault.Spec.Owners[0].UserSpec.Name)

		By("providing authorized class", func() {
			Eventually(func() (err error) {
				g := &gatewayv1.Gateway{
					Name:      "authorized-gateway",
					Namespace: ns.GetName(),
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
				err = admissionClient.Create(context.TODO(), g)
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("providing authorized class (exact)", func() {
			Eventually(func() (err error) {
				g := &gatewayv1.Gateway{
					Name:      "authorized-gateway-exact",
					Namespace: ns.GetName(),
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
				err = admissionClient.Create(context.TODO(), g)
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("providing no gatewayClassName", func() {
			g := &gatewayv1.Gateway{
				Name:      "mutated-gateway",
				Namespace: ns.GetName(),
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
			Expect(admissionClient.Create(context.TODO(), g)).Should(Succeed())
			gw := &gatewayv1.Gateway{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: g.GetName(), Namespace: g.Namespace}, gw)).Should(Succeed())
			Expect(gw.Spec.GatewayClassName).Should(Equal(gatewayv1.ObjectName("customer-class")))

			return
		})
	})
	It("should fail on invalid configuration", func() {
		if err := k8sClient.List(context.Background(), &gatewayv1.GatewayClassList{}); err != nil {
			if utils.IsUnsupportedAPI(err) {
				Skip(fmt.Sprintf("Running test due to unsupported API kind: %s", err.Error()))
			}
		}

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

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tntWithoutDefault.GetName(),
		})
		NamespaceCreation(ns, tntWithoutDefault.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tntWithoutDefault, ns).Should(Succeed())

		admissionClient := gatewayAdmissionClient(tntWithoutDefault.Spec.Owners[0].UserSpec.Name)

		By("providing empty GatewayClassName", func() {
			Eventually(func() (err error) {
				g := &gatewayv1.Gateway{
					Name:      "empty-gateway",
					Namespace: ns.GetName(),
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
				err = admissionClient.Create(context.TODO(), g)
				return
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})

		By("Verify Status (Deletion)", func() {
			for _, crd := range []*gatewayv1.GatewayClass{authorized} {
				EventuallyDeletion(crd)
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
				EventuallyDeletion(crd)
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
