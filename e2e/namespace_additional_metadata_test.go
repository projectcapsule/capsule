// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

var _ = Describe("creating a Namespace for a Tenant with additional metadata", func() {
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
			NamespaceOptions: &capsulev1beta2.NamespaceOptions{
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
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})

	It("should contain additional Namespace metadata", func() {
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
})

var _ = Describe("creating a Namespace for a Tenant with additional metadata list", func() {
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
			NamespaceOptions: &capsulev1beta2.NamespaceOptions{
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
				},
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})

	It("should contain additional Namespace metadata", func() {
		labels := map[string]string{
			"matching_namespace_label": "matching_namespace_label_value",
		}
		ns := NewNamespace("", labels)
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

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
})
