//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/api"
)

var _ = Describe("Creating a GlobalTenantResource object", func() {
	solar := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "energy-solar",
			Labels: map[string]string{
				"replicate": "true",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "solar-user",
					Kind: "User",
				},
			},
		},
	}

	wind := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "energy-wind",
			Labels: map[string]string{
				"replicate": "true",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "wind-user",
					Kind: "User",
				},
			},
		},
	}

	namespacedItem := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dummy-secret",
			Namespace: "default",
			Labels: map[string]string{
				"replicate": "true",
			},
		},
		Type: corev1.SecretTypeOpaque,
	}

	gtr := &capsulev1beta2.GlobalTenantResource{
		ObjectMeta: metav1.ObjectMeta{
			Name: "replicate-energies",
		},
		Spec: capsulev1beta2.GlobalTenantResourceSpec{
			TenantSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"replicate": "true",
				},
			},
			TenantResourceSpec: capsulev1beta2.TenantResourceSpec{
				ResyncPeriod:    metav1.Duration{Duration: time.Minute},
				PruningOnDelete: pointer.Bool(true),
				Resources: []capsulev1beta2.ResourceSpec{
					{
						NamespacedItems: []capsulev1beta2.ObjectReference{
							{
								ObjectReferenceAbstract: capsulev1beta2.ObjectReferenceAbstract{
									Kind:       "Secret",
									Namespace:  "default",
									APIVersion: "v1",
								},
								Selector: metav1.LabelSelector{
									MatchLabels: map[string]string{
										"replicate": "true",
									},
								},
							},
						},
						RawItems: []capsulev1beta2.RawExtension{
							{
								RawExtension: runtime.RawExtension{
									Object: &corev1.Secret{
										TypeMeta: metav1.TypeMeta{
											Kind:       "Secret",
											APIVersion: "v1",
										},
										ObjectMeta: metav1.ObjectMeta{
											Name: "raw-secret-1",
										},
										Type: corev1.SecretTypeOpaque,
									},
								},
							},
							{
								RawExtension: runtime.RawExtension{
									Object: &corev1.Secret{
										TypeMeta: metav1.TypeMeta{
											Kind:       "Secret",
											APIVersion: "v1",
										},
										ObjectMeta: metav1.ObjectMeta{
											Name: "raw-secret-2",
										},
										Type: corev1.SecretTypeOpaque,
									},
								},
							},

							{
								RawExtension: runtime.RawExtension{
									Object: &corev1.Secret{
										TypeMeta: metav1.TypeMeta{
											Kind:       "Secret",
											APIVersion: "v1",
										},
										ObjectMeta: metav1.ObjectMeta{
											Name: "raw-secret-3",
										},
										Type: corev1.SecretTypeOpaque,
									},
								},
							},
						},
						AdditionalMetadata: &api.AdditionalMetadataSpec{
							Labels: map[string]string{
								"labels.energy.io": "replicate",
							},
							Annotations: map[string]string{
								"annotations.energy.io": "replicate",
							},
						},
					},
				},
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), solar)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), wind)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), gtr)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), namespacedItem)
		}).Should(Succeed())
	})

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), solar)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), wind)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), gtr)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), namespacedItem)).Should(Succeed())
	})

	It("should replicate resources to all Tenants", func() {
		solarNs, windNs := []string{"solar-one", "solar-two", "solar-three"}, []string{"wind-one", "wind-two", "wind-three"}

		By("creating solar Namespaces", func() {
			for _, ns := range solarNs {
				NamespaceCreation(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, solar.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			}
		})

		By("creating wind Namespaces", func() {
			for _, ns := range windNs {
				NamespaceCreation(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, wind.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			}
		})

		for _, ns := range append(solarNs, windNs...) {
			By(fmt.Sprintf("waiting for replicated resources in %s Namespace", ns), func() {
				Eventually(func() []corev1.Secret {
					r, err := labels.NewRequirement("labels.energy.io", selection.DoubleEquals, []string{"replicate"})
					if err != nil {
						return nil
					}

					secrets := corev1.SecretList{}
					err = k8sClient.List(context.TODO(), &secrets, &client.ListOptions{LabelSelector: labels.NewSelector().Add(*r), Namespace: ns})
					if err != nil {
						return nil
					}

					return secrets.Items
				}, defaultTimeoutInterval, defaultPollInterval).Should(HaveLen(4))
			})
		}

		By("removing a Namespace from labels", func() {
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: wind.GetName()}, wind)).ToNot(HaveOccurred())

			wind.SetLabels(nil)
			Expect(k8sClient.Update(context.TODO(), wind)).ToNot(HaveOccurred())

			By("expecting no more items in the wind Tenant namespaces due to label update", func() {
				for _, ns := range windNs {
					Eventually(func() []corev1.Secret {
						r, err := labels.NewRequirement("labels.energy.io", selection.DoubleEquals, []string{"replicate"})
						if err != nil {
							return nil
						}

						secrets := corev1.SecretList{}
						err = k8sClient.List(context.TODO(), &secrets, &client.ListOptions{LabelSelector: labels.NewSelector().Add(*r), Namespace: ns})
						if err != nil {
							return nil
						}

						return secrets.Items
					}, defaultTimeoutInterval, defaultPollInterval).Should(HaveLen(0))
				}
			})
		})

		By("using a Namespace selector", func() {
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: gtr.GetName()}, gtr)).ToNot(HaveOccurred())

			gtr.Spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"name": "solar-three",
				},
			}

			Expect(k8sClient.Update(context.TODO(), gtr)).ToNot(HaveOccurred())

			checkFn := func(ns string) func() []corev1.Secret {
				return func() []corev1.Secret {
					r, err := labels.NewRequirement("labels.energy.io", selection.DoubleEquals, []string{"replicate"})
					if err != nil {
						return nil
					}

					secrets := corev1.SecretList{}
					err = k8sClient.List(context.TODO(), &secrets, &client.ListOptions{LabelSelector: labels.NewSelector().Add(*r), Namespace: ns})
					if err != nil {
						return nil
					}

					return secrets.Items
				}
			}

			for _, ns := range []string{"solar-one", "solar-two"} {
				Eventually(checkFn(ns), defaultTimeoutInterval, defaultPollInterval).Should(HaveLen(0))
			}

			Eventually(checkFn("solar-three"), defaultTimeoutInterval, defaultPollInterval).Should(HaveLen(4))
		})

		By("checking if replicated object have annotations and labels", func() {
			for _, name := range []string{"dummy-secret", "raw-secret-1", "raw-secret-2", "raw-secret-3"} {
				secret := corev1.Secret{}
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: "solar-three"}, &secret)).ToNot(HaveOccurred())

				for k, v := range gtr.Spec.Resources[0].AdditionalMetadata.Labels {
					_, err := HaveKeyWithValue(k, v).Match(secret.GetLabels())
					Expect(err).ToNot(HaveOccurred())
				}

				for k, v := range gtr.Spec.Resources[0].AdditionalMetadata.Annotations {
					_, err := HaveKeyWithValue(k, v).Match(secret.GetAnnotations())
					Expect(err).ToNot(HaveOccurred())
				}
			}
		})
	})
})
