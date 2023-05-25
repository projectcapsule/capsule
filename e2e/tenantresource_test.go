//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"math/rand"
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

var _ = Describe("Creating a TenantResource object", func() {
	solar := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "energy-solar",
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

	tntItem := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dummy-secret",
			Namespace: "solar-system",
			Labels: map[string]string{
				"replicate": "true",
			},
		},
		Type: corev1.SecretTypeOpaque,
	}

	crossNamespaceItem := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cross-reference-secret",
			Namespace: "default",
			Labels: map[string]string{
				"replicate": "true",
			},
		},
		Type: corev1.SecretTypeOpaque,
	}

	tr := &capsulev1beta2.TenantResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "replicate-energies",
			Namespace: "solar-system",
		},
		Spec: capsulev1beta2.TenantResourceSpec{
			ResyncPeriod:    metav1.Duration{Duration: time.Minute},
			PruningOnDelete: pointer.Bool(true),
			Resources: []capsulev1beta2.ResourceSpec{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"replicate": "true",
						},
					},
					NamespacedItems: []capsulev1beta2.ObjectReference{
						{
							ObjectReferenceAbstract: capsulev1beta2.ObjectReferenceAbstract{
								Kind:       "Secret",
								Namespace:  "solar-system",
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
									Data: map[string][]byte{
										"{{ tenant.name }}": []byte("Cg=="),
										"{{ namespace }}":   []byte("Cg=="),
									},
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
									Data: map[string][]byte{
										"{{ tenant.name }}": []byte("Cg=="),
										"{{ namespace }}":   []byte("Cg=="),
									},
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
									Data: map[string][]byte{
										"{{ tenant.name }}": []byte("Cg=="),
										"{{ namespace }}":   []byte("Cg=="),
									},
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
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), solar)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), crossNamespaceItem)
		}).Should(Succeed())
	})

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), crossNamespaceItem)).Should(Succeed())
		_ = k8sClient.Delete(context.TODO(), solar)
	})

	It("should replicate resources to all Tenant Namespaces", func() {
		solarNs := []string{"solar-one", "solar-two", "solar-three"}

		By("creating solar Namespaces", func() {
			for _, ns := range append(solarNs, "solar-system") {
				NamespaceCreation(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, solar.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			}
		})

		By("labelling Namespaces", func() {
			for _, name := range []string{"solar-one", "solar-two", "solar-three"} {
				EventuallyWithOffset(1, func() error {
					ns := corev1.Namespace{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: name}, &ns)).Should(Succeed())

					labels := ns.GetLabels()
					if labels == nil {
						return fmt.Errorf("missing labels")
					}
					labels["replicate"] = "true"
					ns.SetLabels(labels)

					return k8sClient.Update(context.TODO(), &ns)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})

		By("creating the namespaced item", func() {
			EventuallyCreation(func() error {
				return k8sClient.Create(context.TODO(), tntItem)
			}).Should(Succeed())
		})

		By("creating the TenantResource", func() {
			EventuallyCreation(func() error {
				return k8sClient.Create(context.TODO(), tr)
			}).Should(Succeed())
		})

		for _, ns := range solarNs {
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

			By(fmt.Sprintf("ensuring raw items are templated in %s Namespace", ns), func() {
				for _, name := range []string{"raw-secret-1", "raw-secret-2", "raw-secret-3"} {
					secret := corev1.Secret{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: ns}, &secret)).ToNot(HaveOccurred())

					Expect(secret.Data).To(HaveKey(solar.Name))
					Expect(secret.Data).To(HaveKey(ns))
				}
			})
		}

		By("using a Namespace selector", func() {
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tr.GetName(), Namespace: "solar-system"}, tr)).ToNot(HaveOccurred())

			tr.Spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"name": "solar-three",
				},
			}

			Expect(k8sClient.Update(context.TODO(), tr)).ToNot(HaveOccurred())

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

				for k, v := range tr.Spec.Resources[0].AdditionalMetadata.Labels {
					_, err := HaveKeyWithValue(k, v).Match(secret.GetLabels())
					Expect(err).ToNot(HaveOccurred())
				}

				for k, v := range tr.Spec.Resources[0].AdditionalMetadata.Annotations {
					_, err := HaveKeyWithValue(k, v).Match(secret.GetAnnotations())
					Expect(err).ToNot(HaveOccurred())
				}
			}
		})

		By("checking replicated object cannot be deleted by a Tenant Owner", func() {
			for _, name := range []string{"dummy-secret", "raw-secret-1", "raw-secret-2", "raw-secret-3"} {
				cs := ownerClient(solar.Spec.Owners[0])

				Consistently(func() error {
					return cs.CoreV1().Secrets("solar-three").Delete(context.TODO(), name, metav1.DeleteOptions{})
				}, 10*time.Second, time.Second).Should(HaveOccurred())
			}
		})

		By("checking replicated object cannot be update by a Tenant Owner", func() {
			for _, name := range []string{"dummy-secret", "raw-secret-1", "raw-secret-2", "raw-secret-3"} {
				cs := ownerClient(solar.Spec.Owners[0])

				Consistently(func() error {
					secret, err := cs.CoreV1().Secrets("solar-three").Get(context.TODO(), name, metav1.GetOptions{})
					if err != nil {
						return err
					}

					secret.SetLabels(nil)
					secret.SetAnnotations(nil)

					_, err = cs.CoreV1().Secrets("solar-three").Update(context.TODO(), secret, metav1.UpdateOptions{})

					return err
				}, 10*time.Second, time.Second).Should(HaveOccurred())
			}
		})

		By("checking that cross-namespace objects are not replicated", func() {
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tr.GetName(), Namespace: "solar-system"}, tr)).ToNot(HaveOccurred())
			tr.Spec.Resources[0].NamespacedItems = append(tr.Spec.Resources[0].NamespacedItems, capsulev1beta2.ObjectReference{
				ObjectReferenceAbstract: capsulev1beta2.ObjectReferenceAbstract{
					Kind:       crossNamespaceItem.Kind,
					Namespace:  crossNamespaceItem.GetName(),
					APIVersion: crossNamespaceItem.APIVersion,
				},
				Selector: metav1.LabelSelector{
					MatchLabels: crossNamespaceItem.GetLabels(),
				},
			})

			Expect(k8sClient.Update(context.TODO(), tr)).ToNot(HaveOccurred())
			// Ensuring that although the deletion of TenantResource object,
			// the replicated objects are not deleted.
			Consistently(func() error {
				return k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: solarNs[rand.Intn(len(solarNs))], Name: crossNamespaceItem.GetName()}, &corev1.Secret{})
			}, 10*time.Second, time.Second).Should(HaveOccurred())
		})

		By("checking pruning is deleted", func() {
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tr.GetName(), Namespace: "solar-system"}, tr)).ToNot(HaveOccurred())
			Expect(*tr.Spec.PruningOnDelete).Should(BeTrue())

			tr.Spec.PruningOnDelete = pointer.Bool(false)

			Expect(k8sClient.Update(context.TODO(), tr)).ToNot(HaveOccurred())

			By("deleting the TenantResource", func() {
				// Ensuring that although the deletion of TenantResource object,
				// the replicated objects are not deleted.
				Expect(k8sClient.Delete(context.TODO(), tr)).Should(Succeed())

				r, err := labels.NewRequirement("labels.energy.io", selection.DoubleEquals, []string{"replicate"})
				Expect(err).ToNot(HaveOccurred())

				Consistently(func() []corev1.Secret {
					secrets := corev1.SecretList{}

					err = k8sClient.List(context.TODO(), &secrets, &client.ListOptions{LabelSelector: labels.NewSelector().Add(*r), Namespace: "solar-three"})
					Expect(err).ToNot(HaveOccurred())

					return secrets.Items
				}, 10*time.Second, time.Second).Should(HaveLen(4))
			})
		})
	})
})
