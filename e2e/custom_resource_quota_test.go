//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

var _ = Describe("when Tenant limits custom Resource Quota", func() {
	tnt := &capsulev1beta1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "limiting-resources",
			Annotations: map[string]string{
				"quota.resources.capsule.clastix.io/foos.test.clastix.io_v1": "3",
			},
		},
		Spec: capsulev1beta1.TenantSpec{
			Owners: capsulev1beta1.OwnerListSpec{
				{
					Name: "resource",
					Kind: "User",
				},
			},
		},
	}

	crd := &v1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foos.test.clastix.io",
		},
		Spec: v1.CustomResourceDefinitionSpec{
			Group: "test.clastix.io",
			Names: v1.CustomResourceDefinitionNames{
				Kind:     "Foo",
				ListKind: "FooList",
				Plural:   "foos",
				Singular: "foo",
			},
			Scope: v1.NamespaceScoped,
			Versions: []v1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Schema: &v1.CustomResourceValidation{
						OpenAPIV3Schema: &v1.JSONSchemaProps{
							Type: "object",
							Properties: map[string]v1.JSONSchemaProps{
								"apiVersion": {
									Type: "string",
								},
								"kind": {
									Type: "string",
								},
								"metadata": {
									Type: "object",
								},
							},
						},
					},
				},
			},
		},
	}

	JustBeforeEach(func() {
		utilruntime.Must(v1.AddToScheme(scheme.Scheme))

		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), crd)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
	})

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), crd)).Should(Succeed())

		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})

	It("should block resources in overflow", func() {
		dynamicClient := dynamic.NewForConfigOrDie(cfg)

		for _, i := range []int{1, 2, 3} {
			ns := NewNamespace(fmt.Sprintf("resource-ns-%d", i))

			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			obj := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": fmt.Sprintf("%s/%s", crd.Spec.Group, crd.Spec.Versions[0].Name),
					"kind":       crd.Spec.Names.Kind,
					"metadata": map[string]interface{}{
						"name": fmt.Sprintf("resource-%d", i),
					},
				},
			}

			EventuallyCreation(func() (err error) {
				_, err = dynamicClient.Resource(schema.GroupVersionResource{Group: crd.Spec.Group, Version: crd.Spec.Versions[0].Name, Resource: crd.Spec.Names.Plural}).Namespace(ns.GetName()).Create(context.Background(), obj, metav1.CreateOptions{})
				return
			}).ShouldNot(HaveOccurred())
		}

		for _, i := range []int{1, 2, 3} {
			ns := NewNamespace(fmt.Sprintf("resource-ns-%d", i))

			obj := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": fmt.Sprintf("%s/%s", crd.Spec.Group, crd.Spec.Versions[0].Name),
					"kind":       crd.Spec.Names.Kind,
					"metadata": map[string]interface{}{
						"name": fmt.Sprintf("fail-%d", i),
					},
				},
			}

			EventuallyCreation(func() (err error) {
				_, err = dynamicClient.Resource(schema.GroupVersionResource{Group: crd.Spec.Group, Version: crd.Spec.Versions[0].Name, Resource: crd.Spec.Names.Plural}).Namespace(ns.GetName()).Create(context.Background(), obj, metav1.CreateOptions{})

				return
			}).Should(HaveOccurred())
		}

		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: tnt.GetName()}, tnt)).ShouldNot(HaveOccurred())

		Eventually(func() bool {
			limit, _ := HaveKeyWithValue("quota.resources.capsule.clastix.io/foos.test.clastix.io_v1", "3").Match(tnt.GetAnnotations())
			used, _ := HaveKeyWithValue("used.resources.capsule.clastix.io/foos.test.clastix.io_v1", "3").Match(tnt.GetAnnotations())

			return limit && used
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	})
})
