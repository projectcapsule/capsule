// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("creating a BreakRequestTemplate", Ordered, Label("break-the-glass"), func() {

	var (
		ctx             context.Context
		brt             *capsulev1beta2.BreakRequestTemplate
		defaultDuration = 5 * time.Second
	)

	BeforeEach(func() {
		ctx = context.TODO()
		brt = &capsulev1beta2.BreakRequestTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name: "e2e-btg",
			},
			Spec: capsulev1beta2.BreakRequestTemplateSpec{
				AutoApprove: true,
				DefaultDuration: &metav1.Duration{
					Duration: defaultDuration,
				},
				Items: map[string]breaktheglass.TemplateItem{
					"config": {
						ManifestTemplate: runtime.RawExtension{
							Object: &corev1.ConfigMap{
								TypeMeta: metav1.TypeMeta{
									Kind:       "ConfigMap",
									APIVersion: "v1",
								},
								ObjectMeta: metav1.ObjectMeta{
									Name: "e2e-btg-cm",
								},
								Data: map[string]string{"key": "value"},
							},
						},
					},
				},
			},
		}

	})
	JustBeforeEach(func() {
		ctx = context.TODO()
		EventuallyCreation(func() error {
			brt.ResourceVersion = ""
			return k8sClient.Create(ctx, brt)
		}).Should(Succeed())
	})

	JustAfterEach(func() {
		EventuallyDeletion(brt)
	})

	Describe("Duration set to "+defaultDuration.String(), func() {
		It("should exist", func() {
			t := &capsulev1beta2.BreakRequestTemplate{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: brt.GetName()}, t)).Should(Succeed())
		})
		It("should create a ConfigMap and delete it after timeout", func() {
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-btg-br",
					Namespace: "default",
				},
				Spec: capsulev1beta2.BreakRequestSpec{
					TemplateName: brt.GetName(),
				},
			}
			defer EventuallyDeletion(br)

			EventuallyCreation(func() error {
				return k8sClient.Create(ctx, br)
			}).Should(Succeed())

			cm := &corev1.ConfigMap{}
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-btg-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			// should be deleted after duration
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-btg-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
	})

	Describe("No duration defined", func() {
		BeforeEach(func() {
			brt.Spec.DefaultDuration = nil
		})
		It("should create a ConfigMap and keep it", func() {
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-btg-br",
					Namespace: "default",
				},
				Spec: capsulev1beta2.BreakRequestSpec{
					TemplateName: brt.GetName(),
				},
			}
			defer EventuallyDeletion(br)

			EventuallyCreation(func() error {
				return k8sClient.Create(ctx, br)
			}).Should(Succeed())

			cm := &corev1.ConfigMap{}
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-btg-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			time.Sleep(defaultDuration + 2*time.Second)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-btg-cm", Namespace: br.Namespace}, cm)).Should(Succeed())
		})
	})

	Describe("Approval required", func() {
		BeforeEach(func() {
			brt.Spec.AutoApprove = false
		})
		It("break request need approval", func() {

			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-btg-br",
					Namespace: "default",
				},
				Spec: capsulev1beta2.BreakRequestSpec{
					TemplateName: brt.GetName(),
				},
			}
			defer EventuallyDeletion(br)

			EventuallyCreation(func() error {
				return k8sClient.Create(ctx, br)
			}).Should(Succeed())

			approveBreakRequest(ctx, br)

			cm := &corev1.ConfigMap{}
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-btg-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			// should be deleted after duration
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-btg-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
	})

	Describe("Approval required with condition", func() {
		BeforeEach(func() {
			brt.Spec.AutoApprove = true
			brt.Spec.ApprovalCondition = "request.spec.reason == 'open sesame'"
		})
		It("break request should be auto approved by condition", func() {
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-btg-br",
					Namespace: "default",
				},
				Spec: capsulev1beta2.BreakRequestSpec{
					TemplateName: brt.GetName(),
					Reason:       "open sesame",
				},
			}
			defer EventuallyDeletion(br)

			EventuallyCreation(func() error {
				return k8sClient.Create(ctx, br)
			}).Should(Succeed())

			cm := &corev1.ConfigMap{}
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-btg-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			// should be deleted after duration
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-btg-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})

		It("break request needs approval when condition not matches", func() {

			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-btg-br",
					Namespace: "default",
				},
				Spec: capsulev1beta2.BreakRequestSpec{
					TemplateName: brt.GetName(),
					Reason:       "test",
				},
			}
			defer EventuallyDeletion(br)

			EventuallyCreation(func() error {
				return k8sClient.Create(ctx, br)
			}).Should(Succeed())

			approveBreakRequest(ctx, br)

			cm := &corev1.ConfigMap{}
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-btg-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			// should be deleted after duration
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-btg-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
	})

	Describe("Template with parameter", func() {
		BeforeEach(func() {
			brt.Spec.Items = map[string]breaktheglass.TemplateItem{
				"config": {
					ParamSchema: runtime.RawExtension{Raw: []byte(`{"type": "object", "required": ["value"], "properties": {"value": {"type": "string"}}}`)},
					ManifestTemplate: runtime.RawExtension{
						Object: &corev1.ConfigMap{
							TypeMeta: metav1.TypeMeta{
								Kind:       "ConfigMap",
								APIVersion: "v1",
							},
							ObjectMeta: metav1.ObjectMeta{
								Name: "e2e-btg-cm",
							},
							Data: map[string]string{"key": "{{.value}}"},
						},
					},
				},
			}
		})
		It("should create correct a ConfigMap data", func() {
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-btg-br",
					Namespace: "default",
				},
				Spec: capsulev1beta2.BreakRequestSpec{
					TemplateName: brt.GetName(),
					Params:       map[string]runtime.RawExtension{"config": {Raw: []byte(`{"value": "test-value"}`)}},
				},
			}
			defer EventuallyDeletion(br)

			EventuallyCreation(func() error {
				err := k8sClient.Create(ctx, br)
				return err
			}).Should(Succeed())

			cm := &corev1.ConfigMap{}
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-btg-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(cm.Data["key"]).Should(Equal("test-value"))
		})
	})
})

func approveBreakRequest(ctx context.Context, br *capsulev1beta2.BreakRequest) {
	br2 := &capsulev1beta2.BreakRequest{}
	Eventually(func() (err error) {
		err = k8sClient.Get(ctx, types.NamespacedName{Name: br.GetName(), Namespace: br.Namespace}, br2)
		if err != nil {
			return err
		}
		if br2.Status.Phase != capsulev1beta2.RequestPhaseRequested {
			return errors.New("break request not in requested phase")
		}
		return nil
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	Expect(br2.Status.Approved).Should(BeNil())

	props, err := br2.GenerateApprovedProperties()
	Expect(err).ShouldNot(HaveOccurred())

	Expect(br2.ApproveRequest(&breaktheglass.AccessEntity{Type: breaktheglass.AccessEntityTypeUser, Name: "test-user"}, props, "")).Should(Succeed())
	Expect(k8sClient.Status().Update(ctx, br2)).Should(Succeed())
}
