// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("creating a Namespace for a Tenant with additional metadata", Ordered, Label("namespace", "metadata"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-tenant-metadata-admission",
			Labels: map[string]string{
				"env": "e2e",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "cap",
					Kind:       "dummy",
					Name:       "tenant-metadata",
					UID:        "tenant-metadata",
				},
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-tenant-metadata-admission",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	JustAfterEach(func() {
		EventuallyDeletion(tnt)
	})

	It("should contain additional Namespace metadata", func() {
		By("prepare tenant", func() {
			tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
				AdditionalMetadata: &api.AdditionalMetadataSpec{
					Labels: map[string]string{
						"k8s.io/custom-label":         "foo",
						"clastix.io/custom-label":     "bar",
						"capsule.clastix.io/tenant":   "tenan-override",
						"kubernetes.io/metadata.name": "namespace-override",
					},
					Annotations: map[string]string{
						"k8s.io/custom-annotation":     "bizz",
						"clastix.io/custom-annotation": "buzz",
					},
				},
			}

			EventuallyCreation(func() error {
				tnt.ResourceVersion = ""
				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)

			tnt = GetTenantEventually(tnt)
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		By("checking additional labels", func() {
			Eventually(func() (ok bool) {
				ns = GetNamespaceEventually(ns.GetName())
				for k, v := range tnt.Spec.NamespaceOptions.AdditionalMetadata.Labels {
					if k == "capsule.clastix.io/tenant" || k == "kubernetes.io/metadata.name" {
						continue // this label is managed and shouldn't be set by the user
					}
					if ok, _ = HaveKeyWithValue(k, v).Match(ns.Labels); !ok {
						return
					}
				}
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
		By("checking managed labels", func() {
			Eventually(func() (ok bool) {
				ns = GetNamespaceEventually(ns.GetName())
				if ok, _ = HaveKeyWithValue("capsule.clastix.io/tenant", tnt.GetName()).Match(ns.Labels); !ok {
					return
				}
				if ok, _ = HaveKeyWithValue("kubernetes.io/metadata.name", ns.GetName()).Match(ns.Labels); !ok {
					return
				}
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})

		By("checking additional annotations", func() {
			Eventually(func() (ok bool) {
				ns = GetNamespaceEventually(ns.GetName())
				for k, v := range tnt.Spec.NamespaceOptions.AdditionalMetadata.Annotations {
					if ok, _ = HaveKeyWithValue(k, v).Match(ns.Annotations); !ok {
						return
					}
				}
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
	})

	It("should contain additional Namespace metadata", func() {
		By("prepare tenant", func() {
			tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
				AdditionalMetadataList: []api.AdditionalMetadataSelectorSpec{
					{
						Labels: map[string]string{
							"k8s.io/custom-label":     "foo",
							"clastix.io/custom-label": "bar",
						},
						Annotations: map[string]string{
							"k8s.io/custom-annotation":     "bizz",
							"clastix.io/custom-annotation": "buzz",
						},
					},
					{
						NamespaceSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "matching_namespace_label",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"matching_namespace_label_value"},
								},
							},
						},
						Labels: map[string]string{
							"k8s.io/custom-label_2": "foo",
						},
						Annotations: map[string]string{
							"k8s.io/custom-annotation_2": "bizz",
						},
					},
					{
						NamespaceSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "nonmatching_namespace_label",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"nonmatching_namespace_label_value"},
								},
							},
						},
						Labels: map[string]string{
							"k8s.io/custom-label_3": "foo",
						},
						Annotations: map[string]string{
							"k8s.io/custom-annotation_3": "bizz",
						},
					},
					{
						Labels: map[string]string{
							"projectcapsule.dev/templated-tenant-label":    "{{ tenant.name }}",
							"projectcapsule.dev/templated-namespace-label": "{{ namespace }}",
						},
						Annotations: map[string]string{
							"projectcapsule.dev/templated-tenant-annotation":    "{{ tenant.name }}",
							"projectcapsule.dev/templated-namespace-annotation": "{{ namespace }}",
						},
					},
				},
			}

			EventuallyCreation(func() error {
				tnt.ResourceVersion = ""
				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())

			tnt = GetTenantEventually(tnt)
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel:           tnt.GetName(),
			"matching_namespace_label": "matching_namespace_label_value",
		})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		By("checking templated annotations", func() {
			Eventually(func() (ok bool) {
				ns = GetNamespaceEventually(ns.GetName())
				if ok, _ = HaveKeyWithValue("projectcapsule.dev/templated-tenant-annotation", tnt.Name).Match(ns.Annotations); !ok {
					return
				}
				if ok, _ = HaveKeyWithValue("projectcapsule.dev/templated-namespace-annotation", ns.Name).Match(ns.Annotations); !ok {
					return
				}
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
		By("checking templated labels", func() {
			Eventually(func() (ok bool) {
				ns = GetNamespaceEventually(ns.GetName())
				if ok, _ = HaveKeyWithValue("projectcapsule.dev/templated-tenant-label", tnt.Name).Match(ns.Labels); !ok {
					return
				}
				if ok, _ = HaveKeyWithValue("projectcapsule.dev/templated-namespace-label", ns.Name).Match(ns.Labels); !ok {
					return
				}
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
		By("checking additional labels from entry without node selector", func() {
			Eventually(func() (ok bool) {
				ns = GetNamespaceEventually(ns.GetName())
				for k, v := range tnt.Spec.NamespaceOptions.AdditionalMetadataList[0].Labels {
					if ok, _ = HaveKeyWithValue(k, v).Match(ns.Labels); !ok {
						return
					}
				}
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
		By("checking additional labels from entry with matching node selector", func() {
			Eventually(func() (ok bool) {
				ns = GetNamespaceEventually(ns.GetName())
				for k, v := range tnt.Spec.NamespaceOptions.AdditionalMetadataList[1].Labels {
					if ok, _ = HaveKeyWithValue(k, v).Match(ns.Labels); !ok {
						return
					}
				}
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
		By("checking additional labels from entry with non-matching node selector", func() {
			Eventually(func() (ok bool) {
				ns = GetNamespaceEventually(ns.GetName())
				for k, v := range tnt.Spec.NamespaceOptions.AdditionalMetadataList[2].Labels {
					if ok, _ = HaveKeyWithValue(k, v).Match(ns.Labels); !ok {
						return
					}
				}
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeFalse())
		})
		By("checking additional annotations from entry without node selector", func() {
			Eventually(func() (ok bool) {
				ns = GetNamespaceEventually(ns.GetName())
				for k, v := range tnt.Spec.NamespaceOptions.AdditionalMetadataList[0].Annotations {
					if ok, _ = HaveKeyWithValue(k, v).Match(ns.Annotations); !ok {
						return
					}
				}
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
		By("checking additional annotations from entry with matching node selector", func() {
			Eventually(func() (ok bool) {
				ns = GetNamespaceEventually(ns.GetName())
				for k, v := range tnt.Spec.NamespaceOptions.AdditionalMetadataList[1].Annotations {
					if ok, _ = HaveKeyWithValue(k, v).Match(ns.Annotations); !ok {
						return
					}
				}
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
		By("checking additional annotations from entry with non-matching node selector", func() {
			Eventually(func() (ok bool) {
				ns = GetNamespaceEventually(ns.GetName())
				for k, v := range tnt.Spec.NamespaceOptions.AdditionalMetadataList[2].Annotations {
					if ok, _ = HaveKeyWithValue(k, v).Match(ns.Annotations); !ok {
						return
					}
				}
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeFalse())
		})
	})

	It("should contain additional Namespace metadata", Label("skip-on-openshift"), func() {
		By("prepare tenant", func() {
			tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
				ManagedMetadataOnly: false,
				AdditionalMetadataList: []api.AdditionalMetadataSelectorSpec{
					{
						Labels: map[string]string{
							"clastix.io/custom-label": "bar",
						},
						Annotations: map[string]string{
							"clastix.io/custom-annotation": "buzz",
						},
					},
					{
						Labels: map[string]string{
							"k8s.io/custom-label": "foo",
						},
						Annotations: map[string]string{
							"k8s.io/custom-annotation": "bizz",
						},
					},
				},
			}

			EventuallyCreation(func() error {
				tnt.ResourceVersion = ""
				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, tnt)).Should(Succeed())
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel:           tnt.GetName(),
			"matching_namespace_label": "matching_namespace_label_value",
		})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		By("checking additional labels", func() {
			Eventually(func() (ok bool) {
				ns = GetNamespaceEventually(ns.GetName())
				for _, mv := range tnt.Spec.NamespaceOptions.AdditionalMetadataList {
					for k, v := range mv.Labels {
						if k == "capsule.clastix.io/tenant" || k == "kubernetes.io/metadata.name" {
							continue // this label is managed and shouldn't be set by the user
						}
						if ok, _ = HaveKeyWithValue(k, v).Match(ns.Labels); !ok {
							return
						}
					}
				}

				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
		By("checking managed labels", func() {
			Eventually(func() (ok bool) {
				ns = GetNamespaceEventually(ns.GetName())
				if ok, _ = HaveKeyWithValue("capsule.clastix.io/tenant", tnt.GetName()).Match(ns.Labels); !ok {
					return
				}
				if ok, _ = HaveKeyWithValue("kubernetes.io/metadata.name", ns.GetName()).Match(ns.Labels); !ok {
					return
				}
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})

		By("checking additional annotations", func() {
			Eventually(func() (ok bool) {
				ns = GetNamespaceEventually(ns.GetName())
				for _, mv := range tnt.Spec.NamespaceOptions.AdditionalMetadataList {
					for k, v := range mv.Annotations {
						if ok, _ = HaveKeyWithValue(k, v).Match(ns.Annotations); !ok {
							return
						}
					}
				}

				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})

		By("patching labels and annotations on the Namespace", func() {
			PatchNamespaceEventually(ns, func(current *corev1.Namespace) {
				if current.Labels == nil {
					current.Labels = map[string]string{}
				}
				if current.Annotations == nil {
					current.Annotations = map[string]string{}
				}

				current.Labels["test-label"] = "test-value"
				current.Labels["k8s.io/custom-label"] = "foo-value"
				current.Annotations["test-annotation"] = "test-value"
				current.Annotations["k8s.io/custom-annotation"] = "bizz-value"
			})

			tnt = GetTenantEventually(tnt)
		})

		By("Add additional annotations (Tenant Owner)", func() {
			ns = GetNamespaceEventually(ns.GetName())

			expectedLabels := map[string]string{
				"test-label":                  "test-value",
				"clastix.io/custom-label":     "bar",
				"k8s.io/custom-label":         "foo",
				"matching_namespace_label":    "matching_namespace_label_value",
				"capsule.clastix.io/tenant":   tnt.GetName(),
				"kubernetes.io/metadata.name": ns.GetName(),
				"env":                         "e2e",
			}

			Eventually(func() map[string]string {
				got := &corev1.Namespace{}
				if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, got); err != nil {
					return nil
				}
				ann := got.GetLabels()
				if ann == nil {
					ann = map[string]string{}
				}
				return ann
			}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(expectedLabels))

			expectedAnnotations := map[string]string{
				"test-annotation":              "test-value",
				"clastix.io/custom-annotation": "buzz",
				"k8s.io/custom-annotation":     "bizz",
			}
			Eventually(func() map[string]string {
				got := &corev1.Namespace{}
				if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, got); err != nil {
					return nil
				}
				ann := got.GetAnnotations()
				if ann == nil {
					ann = map[string]string{}
				}
				return ann
			}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(expectedAnnotations))

			By("verify tenant status", func() {
				Eventually(func(g Gomega) {
					tnt = GetTenantEventually(tnt)

					condition := tnt.Status.Conditions.GetConditionByType(meta.ReadyCondition)
					Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

					Expect(condition.Status).To(Equal(metav1.ConditionTrue), "Expected tenant condition status to be True")
					Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected tenant condition type to be Ready")
					Expect(condition.Reason).To(Equal(meta.SucceededReason), "Expected tenant condition reason to be Succeeded")
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			})

			By("verify namespace status", func() {
				Eventually(func(g Gomega) {
					currentTenant := GetTenantEventually(tnt)
					currentNamespace := GetNamespaceEventually(ns.GetName())

					instance := currentTenant.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{
						Name: currentNamespace.GetName(),
						UID:  currentNamespace.GetUID(),
					})
					g.Expect(instance).NotTo(BeNil(), "Namespace instance should not be nil")

					condition := instance.Conditions.GetConditionByType(meta.ReadyCondition)
					g.Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

					g.Expect(instance.Name).To(Equal(currentNamespace.GetName()))
					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(condition.Type).To(Equal(meta.ReadyCondition))
					g.Expect(condition.Reason).To(Equal(meta.SucceededReason))

					expectedMetadata := &capsulev1beta2.TenantStatusNamespaceMetadata{
						Labels: map[string]string{
							"clastix.io/custom-label": "bar",
							"k8s.io/custom-label":     "foo",
						},
						Annotations: map[string]string{
							"clastix.io/custom-annotation": "buzz",
							"k8s.io/custom-annotation":     "bizz",
						},
					}

					g.Expect(instance.Metadata).To(Equal(expectedMetadata))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			})
		})

		By("change managed additional metadata", func() {
			UpdateTenantEventually(tnt, func(t *capsulev1beta2.Tenant) {
				t.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
					ManagedMetadataOnly: false,
					AdditionalMetadataList: []api.AdditionalMetadataSelectorSpec{
						{
							Labels: map[string]string{
								"clastix.io/custom-label": "bar",
							},
						},
						{
							Annotations: map[string]string{
								"k8s.io/custom-annotation": "bizz",
							},
						},
					},
				}
			})
		})

		By("verify metadata lifecycle (valid update)", func() {
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).To(Succeed())

			expectedLabels := map[string]string{
				"test-label":                  "test-value",
				"clastix.io/custom-label":     "bar",
				"matching_namespace_label":    "matching_namespace_label_value",
				"capsule.clastix.io/tenant":   tnt.GetName(),
				"kubernetes.io/metadata.name": ns.GetName(),
				"env":                         "e2e",
			}
			Eventually(func() map[string]string {
				got := &corev1.Namespace{}
				if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, got); err != nil {
					return nil
				}
				ann := got.GetLabels()
				if ann == nil {
					ann = map[string]string{}
				}
				return ann
			}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(expectedLabels))

			expectedAnnotations := map[string]string{
				"test-annotation":          "test-value",
				"k8s.io/custom-annotation": "bizz",
			}
			Eventually(func() map[string]string {
				got := &corev1.Namespace{}
				if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, got); err != nil {
					return nil
				}
				ann := got.GetAnnotations()
				if ann == nil {
					ann = map[string]string{}
				}
				return ann
			}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(expectedAnnotations))

			By("verify tenant status", func() {
				Eventually(func(g Gomega) {
					current := &capsulev1beta2.Tenant{}
					err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, current)
					g.Expect(err).NotTo(HaveOccurred())

					condition := current.Status.Conditions.GetConditionByType(meta.ReadyCondition)
					g.Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue), "Expected tenant condition status to be True")
					g.Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected tenant condition type to be Ready")
					g.Expect(condition.Reason).To(Equal(meta.SucceededReason), "Expected tenant condition reason to be Succeeded")
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			})

			By("verify namespace status", func() {
				Eventually(func(g Gomega) {
					current := &capsulev1beta2.Tenant{}
					err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, current)
					g.Expect(err).NotTo(HaveOccurred())

					currentNS := &corev1.Namespace{}
					err = k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, currentNS)
					g.Expect(err).NotTo(HaveOccurred())

					instance := current.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{
						Name: currentNS.GetName(),
						UID:  currentNS.GetUID(),
					})
					g.Expect(instance).NotTo(BeNil(), "Namespace instance should not be nil")

					condition := instance.Conditions.GetConditionByType(meta.ReadyCondition)
					g.Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

					g.Expect(instance.Name).To(Equal(currentNS.GetName()))
					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue), "Expected namespace condition status to be True")
					g.Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected namespace condition type to be Ready")
					g.Expect(condition.Reason).To(Equal(meta.SucceededReason), "Expected namespace condition reason to be Succeeded")

					expectedMetadata := &capsulev1beta2.TenantStatusNamespaceMetadata{
						Labels: map[string]string{
							"clastix.io/custom-label": "bar",
						},
						Annotations: map[string]string{
							"k8s.io/custom-annotation": "bizz",
						},
					}

					g.Expect(instance.Metadata).To(Equal(expectedMetadata))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			})

		})

		By("change managed additional metadata (provoke an error)", func() {
			UpdateTenantEventually(tnt, func(t *capsulev1beta2.Tenant) {
				t.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
					ManagedMetadataOnly: false,
					AdditionalMetadataList: []api.AdditionalMetadataSelectorSpec{
						{
							Labels: map[string]string{
								"clastix.io???custom-label": "bar",
							},
						},
					},
				}
			})

			TenantReadyFalse(tnt)
		})

		By("verify metadata lifecycle (faulty update)", func() {
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).To(Succeed())

			expectedLabels := map[string]string{
				"test-label":                  "test-value",
				"clastix.io/custom-label":     "bar",
				"matching_namespace_label":    "matching_namespace_label_value",
				"capsule.clastix.io/tenant":   tnt.GetName(),
				"kubernetes.io/metadata.name": ns.GetName(),
				"env":                         "e2e",
			}
			Eventually(func() map[string]string {
				got := &corev1.Namespace{}
				if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, got); err != nil {
					return nil
				}
				ann := got.GetLabels()
				if ann == nil {
					ann = map[string]string{}
				}
				return ann
			}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(expectedLabels))

			expectedAnnotations := map[string]string{
				"test-annotation":          "test-value",
				"k8s.io/custom-annotation": "bizz",
			}
			Eventually(func() map[string]string {
				got := &corev1.Namespace{}
				if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, got); err != nil {
					return nil
				}
				ann := got.GetAnnotations()
				if ann == nil {
					ann = map[string]string{}
				}
				return ann
			}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(expectedAnnotations))

			By("verify tenant status", func() {
				Eventually(func(g Gomega) {
					t := &capsulev1beta2.Tenant{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)).To(Succeed())

					condition := t.Status.Conditions.GetConditionByType(meta.ReadyCondition)
					Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

					Expect(condition.Status).To(Equal(metav1.ConditionFalse), "Expected tenant condition status to be True")
					Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected tenant condition type to be Ready")
					Expect(condition.Reason).To(Equal(meta.FailedReason), "Expected tenant condition reason to be Succeeded")
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			})

			By("verify namespace status", func() {
				Eventually(func(g Gomega) {
					t := &capsulev1beta2.Tenant{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)).To(Succeed())

					instance := t.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: ns.GetName(), UID: ns.GetUID()})
					Expect(instance).NotTo(BeNil(), "Namespace instance should not be nil")

					condition := instance.Conditions.GetConditionByType(meta.ReadyCondition)
					Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

					Expect(instance.Name).To(Equal(ns.GetName()))
					Expect(condition.Status).To(Equal(metav1.ConditionFalse), "Expected namespace condition status to be True")
					Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected namespace condition type to be Ready")
					Expect(condition.Reason).To(Equal(meta.FailedReason), "Expected namespace condition reason to be Succeeded")

					expectedMetadata := &capsulev1beta2.TenantStatusNamespaceMetadata{
						Labels: map[string]string{
							"clastix.io/custom-label": "bar",
						},
						Annotations: map[string]string{
							"k8s.io/custom-annotation": "bizz",
						},
					}

					Expect(instance.Metadata).To(Equal(expectedMetadata))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			})
		})

		By("change managed additional metadata (empty update)", func() {
			Eventually(func() error {
				t := &capsulev1beta2.Tenant{}
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)).To(Succeed())

				t.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
					ManagedMetadataOnly:    false,
					AdditionalMetadataList: []api.AdditionalMetadataSelectorSpec{},
				}

				return k8sClient.Update(context.TODO(), t)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("verify metadata lifecycle (empty update)", func() {
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).To(Succeed())

			expectedLabels := map[string]string{
				"test-label":                  "test-value",
				"matching_namespace_label":    "matching_namespace_label_value",
				"capsule.clastix.io/tenant":   tnt.GetName(),
				"kubernetes.io/metadata.name": ns.GetName(),
				"env":                         "e2e",
			}
			Eventually(func() map[string]string {
				got := &corev1.Namespace{}
				if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, got); err != nil {
					return nil
				}
				ann := got.GetLabels()
				if ann == nil {
					ann = map[string]string{}
				}
				return ann
			}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(expectedLabels))

			expectedAnnotations := map[string]string{
				"test-annotation": "test-value",
			}
			Eventually(func() map[string]string {
				got := &corev1.Namespace{}
				if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, got); err != nil {
					return nil
				}
				ann := got.GetAnnotations()
				if ann == nil {
					ann = map[string]string{}
				}
				return ann
			}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(expectedAnnotations))

			By("verify tenant status", func() {
				Eventually(func(g Gomega) {
					t := &capsulev1beta2.Tenant{}

					g.Expect(k8sClient.Get(
						context.TODO(),
						types.NamespacedName{Name: tnt.GetName()},
						t,
					)).To(Succeed())

					condition := t.Status.Conditions.GetConditionByType(meta.ReadyCondition)
					g.Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

					g.Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected tenant condition type to be Ready")
					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue), "Expected tenant condition status to be True")
					g.Expect(condition.Reason).To(Equal(meta.SucceededReason), "Expected tenant condition reason to be Succeeded")
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			})

			By("verify namespace status", func() {
				Eventually(func(g Gomega) {
					t := &capsulev1beta2.Tenant{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)).To(Succeed())

					instance := t.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: ns.GetName(), UID: ns.GetUID()})
					Expect(instance).NotTo(BeNil(), "Namespace instance should not be nil")

					condition := instance.Conditions.GetConditionByType(meta.ReadyCondition)
					Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

					Expect(instance.Name).To(Equal(ns.GetName()))
					Expect(condition.Status).To(Equal(metav1.ConditionTrue), "Expected namespace condition status to be True")
					Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected namespace condition type to be Ready")
					Expect(condition.Reason).To(Equal(meta.SucceededReason), "Expected namespace condition reason to be Succeeded")

					expectedMetadata := &capsulev1beta2.TenantStatusNamespaceMetadata{}
					Expect(instance.Metadata).To(Equal(expectedMetadata))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			})
		})
	})
})
