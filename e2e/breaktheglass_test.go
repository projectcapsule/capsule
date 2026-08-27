// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"errors"
	"time"

	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

var _ = FDescribe("creating a BreakRequestTemplate", Ordered, Label("break-the-glass"), func() {

	var (
		ctx             context.Context
		brt             *capsulev1beta2.BreakRequestTemplate
		defaultDuration = 5 * time.Second
		cmName          string
	)

	BeforeAll(func() {
		ctx = context.TODO()
		for _, user := range []string{"alice", "bob", "charlie"} {
			crb := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "e2e-btg-" + user,
				},
				Subjects: []rbacv1.Subject{
					{
						Kind: "User",
						Name: user,
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "cluster-admin",
				},
			}
			err := k8sClient.Create(ctx, crb)
			if err != nil {
				Expect(err).Should(MatchError(ContainSubstring("already exists")))
			}
		}
	})

	AfterAll(func() {
		for _, user := range []string{"alice", "bob", "charlie"} {
			crb := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "e2e-btg-" + user,
				},
			}
			_ = k8sClient.Delete(ctx, crb)
		}
	})

	BeforeEach(func() {
		ctx = context.TODO()
		cmName = fmt.Sprintf("e2e-btg-cm-%d", metav1.Now().UnixNano()) // Initial unique name
		brt = &capsulev1beta2.BreakRequestTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("e2e-btg-%d", metav1.Now().UnixNano()),
			},
			Spec: capsulev1beta2.BreakRequestTemplateSpec{
				AutoApprove: true,
				DefaultDuration: &metav1.Duration{
					Duration: defaultDuration,
				},
				Templates: []runtime.RawExtension{{
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
		BeforeEach(func() {
			cmName = "timeout-cm"
			brt.Spec.Templates[0].Object.(*corev1.ConfigMap).Name = cmName
		})
		It("should exist", func() {
			t := &capsulev1beta2.BreakRequestTemplate{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: brt.GetName()}, t)).Should(Succeed())
		})
		It("should create a ConfigMap and delete it after timeout", func() {
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("e2e-btg-create-cm-and-delete-%d", metav1.Now().UnixNano()),
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
				return k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			// should be deleted after duration
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
	})

	Describe("No duration defined", func() {
		BeforeEach(func() {
			brt.Spec.DefaultDuration = nil
			cmName = "keep-cm"
			brt.Spec.Templates[0].Object.(*corev1.ConfigMap).Name = cmName
		})
		It("should create a ConfigMap and keep it", func() {
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("e2e-btg-create-cm-keep-%d", metav1.Now().UnixNano()),
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
				return k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			time.Sleep(defaultDuration + 2*time.Second)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: br.Namespace}, cm)).Should(Succeed())
		})
	})

	Describe("Approval required", func() {
		BeforeEach(func() {
			brt.Spec.AutoApprove = false
			cmName = "manual-cm"
			brt.Spec.Templates[0].Object.(*corev1.ConfigMap).Name = cmName
		})
		It("break request need approval", func() {
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("e2e-btg-need-approval-%d", metav1.Now().UnixNano()),
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
				return k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			// should be deleted after duration
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
	})

	Describe("Approval required with condition", func() {
		BeforeEach(func() {
			brt.Spec.AutoApprove = true
			brt.Spec.ApprovalCondition = "request.spec.reason == 'open sesame'"
			cmName = "condition-cm"
			brt.Spec.Templates[0].Object.(*corev1.ConfigMap).Name = cmName
		})
		It("break request should be auto approved by condition", func() {
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("e2e-btg-auto-approve-%d", metav1.Now().UnixNano()),
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
				return k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			// should be deleted after duration
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
	})

	Describe("Template with parameter", func() {
		BeforeEach(func() {
			cmName = "param-cm"
			brt.Spec.Templates = []runtime.RawExtension{
				{
					Object: &corev1.ConfigMap{
						TypeMeta: metav1.TypeMeta{
							Kind:       "ConfigMap",
							APIVersion: "v1",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: cmName,
						},
						Data: map[string]string{"key": "{{.value}}"},
					},
				},
			}
			brt.Spec.ParamSchema = &runtime.RawExtension{Raw: []byte(`{"type": "object", "required": ["value"], "properties": {"value": {"type": "string"}}}`)}
		})
		It("should create correct a ConfigMap data", func() {
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("e2e-btg-correct-cm-data-%d", metav1.Now().UnixNano()),
					Namespace: "default",
				},
				Spec: capsulev1beta2.BreakRequestSpec{
					TemplateName: brt.GetName(),
					Params:       &runtime.RawExtension{Raw: []byte(`{"value": "test-value"}`)},
				},
			}
			defer EventuallyDeletion(br)

			EventuallyCreation(func() error {
				return k8sClient.Create(ctx, br)
			}).Should(Succeed())

			cm := &corev1.ConfigMap{}
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(cm.Data["key"]).Should(Equal("test-value"))
		})
	})

	Describe("Approval based on requestor information", func() {
		BeforeEach(func() {
			brt.Spec.AutoApprove = true
			brt.Spec.ApprovalCondition = "requestor.name == 'alice'"
			cmName = "alice-cm"
			brt.Spec.Templates[0].Object.(*corev1.ConfigMap).Name = cmName
		})
		It("should be auto-approved when requested by alice", func() {
			aliceClient := impersonationClient("alice", []string{"users"})
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("alice-br-%d", metav1.Now().UnixNano()),
					Namespace: "default",
				},
				Spec: capsulev1beta2.BreakRequestSpec{
					TemplateName: brt.GetName(),
					// Manual override because webhooks are missing in cluster
					Requestor: breaktheglass.AccessEntity{
						Name: "alice",
						Type: breaktheglass.AccessEntityTypeUser,
					},
				},
			}
			defer EventuallyDeletion(br)

			EventuallyCreation(func() error {
				return aliceClient.Create(ctx, br)
			}).Should(Succeed())

			cm := &corev1.ConfigMap{}
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
		It("should not be auto-approved when requested by bob", func() {
			bobClient := impersonationClient("bob", []string{"users"})
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("bob-br-%d", metav1.Now().UnixNano()),
					Namespace: "default",
				},
				Spec: capsulev1beta2.BreakRequestSpec{
					TemplateName: brt.GetName(),
					// Manual override because webhooks are missing in cluster
					Requestor: breaktheglass.AccessEntity{
						Name: "bob",
						Type: breaktheglass.AccessEntityTypeUser,
					},
				},
			}
			defer EventuallyDeletion(br)

			err := bobClient.Create(ctx, br)
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("approval conditions not satisfied for template"))
		})
	})

	Describe("Approval based on reviewer information", func() {
		BeforeEach(func() {
			brt.Spec.AutoApprove = false
			brt.Spec.ApprovalCondition = "'admin' in reviewer.groups"
			cmName = "review-cm"
			brt.Spec.Templates[0].Object.(*corev1.ConfigMap).Name = cmName
		})
		It("should allow approval by a user in admin group", func() {
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("review-br-%d", metav1.Now().UnixNano()),
					Namespace: "default",
				},
				Spec: capsulev1beta2.BreakRequestSpec{
					TemplateName: brt.GetName(),
					// Manual override because webhooks are missing in cluster
					Requestor: breaktheglass.AccessEntity{
						Name: "bob",
						Type: breaktheglass.AccessEntityTypeUser,
					},
				},
			}
			defer EventuallyDeletion(br)

			EventuallyCreation(func() error {
				return k8sClient.Create(ctx, br)
			}).Should(Succeed())

			// Try to approve as bob (not in admin group)
			bobClient := impersonationClient("bob", []string{"users"})
			br2 := &capsulev1beta2.BreakRequest{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: br.Name, Namespace: br.Namespace}, br2)).Should(Succeed())

			props, err := br2.GenerateApprovedProperties()
			Expect(err).ShouldNot(HaveOccurred())
			Expect(br2.ApproveRequest(&breaktheglass.AccessEntity{Type: breaktheglass.AccessEntityTypeUser, Name: "bob", Groups: []string{"users"}}, props, "")).Should(Succeed())

			// Should be denied by validation webhook
			err = bobClient.Status().Update(ctx, br2)
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("approval conditions not satisfied for template"))

			// Approve as charlie (in admin group)
			charlieClient := impersonationClient("charlie", []string{"admin", "users"})
			br3 := &capsulev1beta2.BreakRequest{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: br.Name, Namespace: br.Namespace}, br3)).Should(Succeed())

			props, err = br3.GenerateApprovedProperties()
			Expect(err).Should(Succeed())
			Expect(br3.ApproveRequest(&breaktheglass.AccessEntity{Type: breaktheglass.AccessEntityTypeUser, Name: "charlie", Groups: []string{"users", "admin"}}, props, "")).Should(Succeed())

			// Should be denied by validation webhook
			err = charlieClient.Status().Update(ctx, br3)
			Expect(err).Should(Succeed())
		})
	})
})

func approveBreakRequest(ctx context.Context, br *capsulev1beta2.BreakRequest) {
	GinkgoHelper()
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
	err = k8sClient.Status().Update(ctx, br2)
	Expect(err).ShouldNot(HaveOccurred())
}
