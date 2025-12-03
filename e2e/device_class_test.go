// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	resources "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("when Tenant handles Device classes", Label("tenant", "classes", "device"), func() {
	erm := "nvidia.com/gpu"
	authorized := &resources.DeviceClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gpu.example.com",
			Labels: map[string]string{
				"env": "authorized",
			},
		},
		Spec: resources.DeviceClassSpec{
			Selectors: []resources.DeviceSelector{
				{
					CEL: &resources.CELDeviceSelector{
						Expression: "device.driver == 'gpu.example.com' && device.attributes['gpu.example.com'].type == 'gpu'",
					},
				},
			},
			ExtendedResourceName: &erm,
		},
	}
	authorized2 := &resources.DeviceClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gpu2.example.com",
			Labels: map[string]string{
				"env": "authorized",
			},
		},
		Spec: resources.DeviceClassSpec{
			Selectors: []resources.DeviceSelector{
				{
					CEL: &resources.CELDeviceSelector{
						Expression: "device.driver == 'gpu.example.com' && device.attributes['gpu.example.com'].type == 'gpu'",
					},
				},
			},
			ExtendedResourceName: &erm,
		},
	}
	unauthorized := &resources.DeviceClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gpu3.example.com",
			Labels: map[string]string{
				"env": "unauthorized",
			},
		},
		Spec: resources.DeviceClassSpec{
			Selectors: []resources.DeviceSelector{
				{
					CEL: &resources.CELDeviceSelector{
						Expression: "device.driver == 'gpu.example.com' && device.attributes['gpu.example.com'].type == 'gpu'",
					},
				},
			},
			ExtendedResourceName: &erm,
		},
	}

	tntWithAuthorized := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-authorized-deviceclass",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: []api.OwnerSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "authorized-deviceclass",
							Kind: "User",
						},
					},
				},
			},
			DeviceClasses: &api.SelectorAllowedListSpec{
				LabelSelector: v1.LabelSelector{
					MatchLabels: map[string]string{
						"env": "authorized",
					},
				},
			},
		},
	}
	tntWithUnauthorized := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-unauthorized-deviceclass",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: []api.OwnerSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "unauthorized-deviceclass",
							Kind: "User",
						},
					},
				},
			},
			DeviceClasses: &api.SelectorAllowedListSpec{
				LabelSelector: v1.LabelSelector{
					MatchLabels: map[string]string{
						"env": "production",
					},
				},
			},
		},
	}

	JustBeforeEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{tntWithAuthorized, tntWithUnauthorized} {
			tnt.ResourceVersion = ""
			EventuallyCreation(func() error {
				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
		}
		for _, crd := range []*resources.DeviceClass{authorized, authorized2, unauthorized} {
			crd.ResourceVersion = ""
			EventuallyCreation(func() error {
				return k8sClient.Create(context.TODO(), crd)
			}).Should(Succeed())
		}
	})
	JustAfterEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{tntWithAuthorized, tntWithUnauthorized} {
			EventuallyCreation(func() error {
				return ignoreNotFound(k8sClient.Delete(context.TODO(), tnt))
			}).Should(Succeed())
		}

		Eventually(func() (err error) {
			req, _ := labels.NewRequirement("env", selection.Exists, nil)

			return k8sClient.DeleteAllOf(context.TODO(), &resources.DeviceClass{}, &client.DeleteAllOfOptions{
				ListOptions: client.ListOptions{
					LabelSelector: labels.NewSelector().Add(*req),
				},
			})
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})
	It("ResourceClaims", func() {
		By("Verify Status (Creation)", func() {
			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntWithAuthorized.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.DeviceClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				Should(ConsistOf(authorized.GetName(), authorized2.GetName()))
		})

		ns := NewNamespace("")
		NamespaceCreation(ns, tntWithAuthorized.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithAuthorized, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("providing authorized device class", func() {
			for _, class := range []*resources.DeviceClass{authorized} {
				Eventually(func() (err error) {
					g := &resources.ResourceClaim{
						ObjectMeta: metav1.ObjectMeta{
							Name:      class.GetName() + "-resource-claim",
							Namespace: ns.GetName(),
						},
						Spec: resources.ResourceClaimSpec{
							Devices: resources.DeviceClaim{
								Requests: []resources.DeviceRequest{
									{
										Name: "authorized-device-class-resource-claim",
										Exactly: &resources.ExactDeviceRequest{
											DeviceClassName: "gpu.example.com",
											Selectors: []resources.DeviceSelector{
												{
													CEL: &resources.CELDeviceSelector{
														Expression: "device.driver == 'gpu.example.com' && device.attributes['gpu.example.com'].type == 'gpu'",
													},
												},
											},
										},
									},
								},
							},
						},
					}

					err = k8sClient.Create(context.TODO(), g)
					return
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})

		By("providing unauthorized device class", func() {
			for _, class := range []*resources.DeviceClass{unauthorized} {
				Eventually(func() (err error) {
					g := &resources.ResourceClaim{
						ObjectMeta: metav1.ObjectMeta{
							Name:      class.GetName() + "-resource-claim",
							Namespace: ns.GetName(),
						},
						Spec: resources.ResourceClaimSpec{
							Devices: resources.DeviceClaim{
								Requests: []resources.DeviceRequest{
									{
										Name: "unauthorized-device-class-resource-claim",
										Exactly: &resources.ExactDeviceRequest{
											DeviceClassName: "gpu3.example.com",
											Selectors: []resources.DeviceSelector{
												{
													CEL: &resources.CELDeviceSelector{
														Expression: "device.driver == 'gpu.example.com' && device.attributes['gpu.example.com'].type == 'gpu'",
													},
												},
											},
										},
									},
								},
							},
						},
					}

					err = k8sClient.Create(context.TODO(), g)
					return
				}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
			}
		})

		By("providing non-existent device class", func() {
			for _, class := range []*resources.DeviceClass{unauthorized} {
				Eventually(func() (err error) {
					g := &resources.ResourceClaim{
						ObjectMeta: metav1.ObjectMeta{
							Name:      class.GetName() + "-resource-claim",
							Namespace: ns.GetName(),
						},
						Spec: resources.ResourceClaimSpec{
							Devices: resources.DeviceClaim{
								Requests: []resources.DeviceRequest{
									{
										Name: "missing-device-class-resource-claim",
										Exactly: &resources.ExactDeviceRequest{
											DeviceClassName: "gpu53.example.com",
											Selectors: []resources.DeviceSelector{
												{
													CEL: &resources.CELDeviceSelector{
														Expression: "device.driver == 'gpu.example.com' && device.attributes['gpu.example.com'].type == 'gpu'",
													},
												},
											},
										},
									},
								},
							},
						},
					}

					err = k8sClient.Create(context.TODO(), g)
					return
				}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
			}
		})

		By("Verify Status (Deletion)", func() {
			for _, class := range []*resources.DeviceClass{authorized} {
				Expect(ignoreNotFound(k8sClient.Delete(context.TODO(), class))).To(Succeed())
			}

			Eventually(func() ([]string, error) {
				t := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tntWithAuthorized.GetName()},
					t,
				); err != nil {
					return nil, err
				}

				return t.Status.Classes.DeviceClasses, nil
			}, defaultTimeoutInterval, defaultPollInterval).
				ShouldNot(ConsistOf(authorized.GetName(), authorized2.GetName()))
		})
	})
	It("ResourceClaimTemplates", func() {

		ns := NewNamespace("")
		NamespaceCreation(ns, tntWithAuthorized.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithAuthorized, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("providing authorized device class", func() {
			for _, class := range []*resources.DeviceClass{authorized} {
				Eventually(func() (err error) {
					g := &resources.ResourceClaimTemplate{
						ObjectMeta: metav1.ObjectMeta{
							Name:      class.GetName() + "-resource-claim",
							Namespace: ns.GetName(),
						},
						Spec: resources.ResourceClaimTemplateSpec{
							Spec: resources.ResourceClaimSpec{
								Devices: resources.DeviceClaim{
									Requests: []resources.DeviceRequest{
										{
											Name: "authorized-device-class-resource-claim",
											Exactly: &resources.ExactDeviceRequest{
												DeviceClassName: "gpu.example.com",
												Selectors: []resources.DeviceSelector{
													{
														CEL: &resources.CELDeviceSelector{
															Expression: "device.driver == 'gpu.example.com' && device.attributes['gpu.example.com'].type == 'gpu'",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					}

					err = k8sClient.Create(context.TODO(), g)
					return
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})

		By("providing unauthorized device class", func() {
			for _, class := range []*resources.DeviceClass{unauthorized} {
				Eventually(func() (err error) {
					g := &resources.ResourceClaimTemplate{
						ObjectMeta: metav1.ObjectMeta{
							Name:      class.GetName() + "-resource-claim",
							Namespace: ns.GetName(),
						},
						Spec: resources.ResourceClaimTemplateSpec{
							Spec: resources.ResourceClaimSpec{
								Devices: resources.DeviceClaim{
									Requests: []resources.DeviceRequest{
										{
											Name: "unauthorized-device-class-resource-claim",
											Exactly: &resources.ExactDeviceRequest{
												DeviceClassName: "gpu3.example.com",
												Selectors: []resources.DeviceSelector{
													{
														CEL: &resources.CELDeviceSelector{
															Expression: "device.driver == 'gpu.example.com' && device.attributes['gpu.example.com'].type == 'gpu'",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					}

					err = k8sClient.Create(context.TODO(), g)
					return
				}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
			}
		})

		By("providing both authorized and unauthorized device classes", func() {
			for _, class := range []*resources.DeviceClass{unauthorized} {
				Eventually(func() (err error) {
					g := &resources.ResourceClaimTemplate{
						ObjectMeta: metav1.ObjectMeta{
							Name:      class.GetName() + "-resource-claim",
							Namespace: ns.GetName(),
						},
						Spec: resources.ResourceClaimTemplateSpec{
							Spec: resources.ResourceClaimSpec{
								Devices: resources.DeviceClaim{
									Requests: []resources.DeviceRequest{
										{
											Name: "unauthorized-device-class-resource-claim",
											Exactly: &resources.ExactDeviceRequest{
												DeviceClassName: "gpu3.example.com",
												Selectors: []resources.DeviceSelector{
													{
														CEL: &resources.CELDeviceSelector{
															Expression: "device.driver == 'gpu.example.com' && device.attributes['gpu.example.com'].type == 'gpu'",
														},
													},
												},
											},
										},
										{
											Name: "authorized-device-class-resource-claim",
											Exactly: &resources.ExactDeviceRequest{
												DeviceClassName: "gpu.example.com",
												Selectors: []resources.DeviceSelector{
													{
														CEL: &resources.CELDeviceSelector{
															Expression: "device.driver == 'gpu.example.com' && device.attributes['gpu.example.com'].type == 'gpu'",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					}

					err = k8sClient.Create(context.TODO(), g)
					return
				}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
			}
		})

		By("providing authorized and missing device classes", func() {
			for _, class := range []*resources.DeviceClass{unauthorized} {
				Eventually(func() (err error) {
					g := &resources.ResourceClaimTemplate{
						ObjectMeta: metav1.ObjectMeta{
							Name:      class.GetName() + "-resource-claim",
							Namespace: ns.GetName(),
						},
						Spec: resources.ResourceClaimTemplateSpec{
							Spec: resources.ResourceClaimSpec{
								Devices: resources.DeviceClaim{
									Requests: []resources.DeviceRequest{
										{
											Name: "missing-device-class-resource-claim",
											Exactly: &resources.ExactDeviceRequest{
												DeviceClassName: "gpu63.example.com",
												Selectors: []resources.DeviceSelector{
													{
														CEL: &resources.CELDeviceSelector{
															Expression: "device.driver == 'gpu.example.com' && device.attributes['gpu.example.com'].type == 'gpu'",
														},
													},
												},
											},
										},
										{
											Name: "authorized-device-class-resource-claim",
											Exactly: &resources.ExactDeviceRequest{
												DeviceClassName: "gpu.example.com",
												Selectors: []resources.DeviceSelector{
													{
														CEL: &resources.CELDeviceSelector{
															Expression: "device.driver == 'gpu.example.com' && device.attributes['gpu.example.com'].type == 'gpu'",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					}

					err = k8sClient.Create(context.TODO(), g)
					return
				}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
			}
		})

		By("providing two authorized device classes", func() {
			for _, class := range []*resources.DeviceClass{unauthorized} {
				Eventually(func() (err error) {
					g := &resources.ResourceClaimTemplate{
						ObjectMeta: metav1.ObjectMeta{
							Name:      class.GetName() + "-resource-claim",
							Namespace: ns.GetName(),
						},
						Spec: resources.ResourceClaimTemplateSpec{
							Spec: resources.ResourceClaimSpec{
								Devices: resources.DeviceClaim{
									Requests: []resources.DeviceRequest{
										{
											Name: "unauthorized-device-class-resource-claim",
											Exactly: &resources.ExactDeviceRequest{
												DeviceClassName: "gpu2.example.com",
												Selectors: []resources.DeviceSelector{
													{
														CEL: &resources.CELDeviceSelector{
															Expression: "device.driver == 'gpu.example.com' && device.attributes['gpu.example.com'].type == 'gpu'",
														},
													},
												},
											},
										},
										{
											Name: "authorized-device-class-resource-claim",
											Exactly: &resources.ExactDeviceRequest{
												DeviceClassName: "gpu.example.com",
												Selectors: []resources.DeviceSelector{
													{
														CEL: &resources.CELDeviceSelector{
															Expression: "device.driver == 'gpu.example.com' && device.attributes['gpu.example.com'].type == 'gpu'",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					}

					err = k8sClient.Create(context.TODO(), g)
					return
				}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
			}
		})
	})
})
