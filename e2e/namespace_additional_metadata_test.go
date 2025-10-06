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
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/meta"
)

var _ = Describe("creating a Namespace for a Tenant with additional metadata", Label("namespace", "metadata"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-metadata",
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
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "gatsby",
					Kind: "User",
				},
			},
		},
	}

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
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

			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, tnt)).Should(Succeed())
		})

		ns := NewNamespace("")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("checking additional labels", func() {
			Eventually(func() (ok bool) {
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
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
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
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
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
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

			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, tnt)).Should(Succeed())
		})

		labels := map[string]string{
			"matching_namespace_label": "matching_namespace_label_value",
		}
		ns := NewNamespace("", labels)
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("checking templated annotations", func() {
			Eventually(func() (ok bool) {
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
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
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
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
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
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
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
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
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
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
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
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
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
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
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
				for k, v := range tnt.Spec.NamespaceOptions.AdditionalMetadataList[2].Annotations {
					if ok, _ = HaveKeyWithValue(k, v).Match(ns.Annotations); !ok {
						return
					}
				}
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeFalse())
		})
	})

	It("should contain additional Namespace metadata", func() {
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

		labels := map[string]string{
			"matching_namespace_label": "matching_namespace_label_value",
		}

		ns := NewNamespace("", labels)
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("checking additional labels", func() {
			Eventually(func() (ok bool) {
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
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
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
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
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
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
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).To(Succeed())

			before := ns.DeepCopy()
			ns.Labels["test-label"] = "test-value"
			ns.Labels["k8s.io/custom-label"] = "foo-value"
			ns.Annotations["test-annotation"] = "test-value"
			ns.Annotations["k8s.io/custom-annotation"] = "bizz-value"

			Expect(k8sClient.Patch(context.TODO(), ns, client.MergeFrom(before))).To(Succeed())
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, tnt)).Should(Succeed())
		})

		By("Add additional annotations (Tenant Owner)", func() {
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())

			expectedLabels := map[string]string{
				"test-label":                  "test-value",
				"clastix.io/custom-label":     "bar",
				"k8s.io/custom-label":         "foo",
				"matching_namespace_label":    "matching_namespace_label_value",
				"capsule.clastix.io/tenant":   tnt.GetName(),
				"kubernetes.io/metadata.name": ns.GetName(),
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
				condition := tnt.Status.Conditions.GetConditionByType(meta.ReadyCondition)
				Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

				Expect(condition.Status).To(Equal(metav1.ConditionTrue), "Expected tenant condition status to be True")
				Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected tenant condition type to be Ready")
				Expect(condition.Reason).To(Equal(meta.SucceededReason), "Expected tenant condition reason to be Succeeded")
			})

			By("verify namespace status", func() {
				instance := tnt.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: ns.GetName(), UID: ns.GetUID()})
				Expect(instance).NotTo(BeNil(), "Namespace instance should not be nil")

				condition := instance.Conditions.GetConditionByType(meta.ReadyCondition)
				Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

				Expect(instance.Name).To(Equal(ns.GetName()))
				Expect(condition.Status).To(Equal(metav1.ConditionTrue), "Expected namespace condition status to be True")
				Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected namespace condition type to be Ready")
				Expect(condition.Reason).To(Equal(meta.SucceededReason), "Expected namespace condition reason to be Succeeded")

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

				Expect(instance.Metadata).To(Equal(expectedMetadata))
			})
		})

		By("change managed additional metadata", func() {
			tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
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

			Expect(k8sClient.Update(context.TODO(), tnt)).Should(Succeed())
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, tnt)).Should(Succeed())
		})

		By("verify metadata lifecycle (valid update)", func() {
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).To(Succeed())

			expectedLabels := map[string]string{
				"test-label":                  "test-value",
				"clastix.io/custom-label":     "bar",
				"matching_namespace_label":    "matching_namespace_label_value",
				"capsule.clastix.io/tenant":   tnt.GetName(),
				"kubernetes.io/metadata.name": ns.GetName(),
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
				condition := tnt.Status.Conditions.GetConditionByType(meta.ReadyCondition)
				Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

				Expect(condition.Status).To(Equal(metav1.ConditionTrue), "Expected tenant condition status to be True")
				Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected tenant condition type to be Ready")
				Expect(condition.Reason).To(Equal(meta.SucceededReason), "Expected tenant condition reason to be Succeeded")
			})

			By("verify namespace status", func() {
				instance := tnt.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: ns.GetName(), UID: ns.GetUID()})
				Expect(instance).NotTo(BeNil(), "Namespace instance should not be nil")

				condition := instance.Conditions.GetConditionByType(meta.ReadyCondition)
				Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

				Expect(instance.Name).To(Equal(ns.GetName()))
				Expect(condition.Status).To(Equal(metav1.ConditionTrue), "Expected namespace condition status to be True")
				Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected namespace condition type to be Ready")
				Expect(condition.Reason).To(Equal(meta.SucceededReason), "Expected namespace condition reason to be Succeeded")

				expectedMetadata := &capsulev1beta2.TenantStatusNamespaceMetadata{
					Labels: map[string]string{
						"clastix.io/custom-label": "bar",
					},
					Annotations: map[string]string{
						"k8s.io/custom-annotation": "bizz",
					},
				}

				Expect(instance.Metadata).To(Equal(expectedMetadata))
			})

		})

		By("change managed additional metadata (provoke an error)", func() {
			tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
				ManagedMetadataOnly: false,
				AdditionalMetadataList: []api.AdditionalMetadataSelectorSpec{
					{
						Labels: map[string]string{
							"clastix.io???custom-label": "bar",
						},
					},
				},
			}

			Expect(k8sClient.Update(context.TODO(), tnt)).Should(Succeed())
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, tnt)).Should(Succeed())
		})

		By("verify metadata lifecycle (faulty update)", func() {
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).To(Succeed())

			expectedLabels := map[string]string{
				"test-label":                  "test-value",
				"clastix.io/custom-label":     "bar",
				"matching_namespace_label":    "matching_namespace_label_value",
				"capsule.clastix.io/tenant":   tnt.GetName(),
				"kubernetes.io/metadata.name": ns.GetName(),
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
				condition := tnt.Status.Conditions.GetConditionByType(meta.ReadyCondition)
				Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

				Expect(condition.Status).To(Equal(metav1.ConditionFalse), "Expected tenant condition status to be True")
				Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected tenant condition type to be Ready")
				Expect(condition.Reason).To(Equal(meta.FailedReason), "Expected tenant condition reason to be Succeeded")
			})

			By("verify namespace status", func() {
				instance := tnt.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: ns.GetName(), UID: ns.GetUID()})
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
			})
		})

		By("change managed additional metadata (empty update)", func() {
			tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
				ManagedMetadataOnly:    false,
				AdditionalMetadataList: []api.AdditionalMetadataSelectorSpec{},
			}

			Expect(k8sClient.Update(context.TODO(), tnt)).Should(Succeed())
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, tnt)).Should(Succeed())
		})

		By("verify metadata lifecycle (empty update)", func() {
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).To(Succeed())

			expectedLabels := map[string]string{
				"test-label":                  "test-value",
				"matching_namespace_label":    "matching_namespace_label_value",
				"capsule.clastix.io/tenant":   tnt.GetName(),
				"kubernetes.io/metadata.name": ns.GetName(),
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
				condition := tnt.Status.Conditions.GetConditionByType(meta.ReadyCondition)
				Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

				Expect(condition.Status).To(Equal(metav1.ConditionTrue), "Expected tenant condition status to be True")
				Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected tenant condition type to be Ready")
				Expect(condition.Reason).To(Equal(meta.SucceededReason), "Expected tenant condition reason to be Succeeded")
			})

			By("verify namespace status", func() {
				instance := tnt.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: ns.GetName(), UID: ns.GetUID()})
				Expect(instance).NotTo(BeNil(), "Namespace instance should not be nil")

				condition := instance.Conditions.GetConditionByType(meta.ReadyCondition)
				Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

				Expect(instance.Name).To(Equal(ns.GetName()))
				Expect(condition.Status).To(Equal(metav1.ConditionTrue), "Expected namespace condition status to be True")
				Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected namespace condition type to be Ready")
				Expect(condition.Reason).To(Equal(meta.SucceededReason), "Expected namespace condition reason to be Succeeded")

				expectedMetadata := &capsulev1beta2.TenantStatusNamespaceMetadata{}
				Expect(instance.Metadata).To(Equal(expectedMetadata))
			})
		})
	})
})
