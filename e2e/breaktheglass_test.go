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
	apimeta "github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	tpl "github.com/projectcapsule/capsule/pkg/template"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func breakRequestTemplateReference(name string) capsulev1beta2.BreakRequestTemplateReference {
	return capsulev1beta2.BreakRequestTemplateReference{
		Kind: capsulev1beta2.BreakRequestTemplateKind,
		Name: name,
	}
}

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
				Resources: []apiruntime.ResourceTemplate{{
					Targets: []runtime.RawExtension{{Object: &corev1.ConfigMap{
						TypeMeta: metav1.TypeMeta{
							Kind:       "ConfigMap",
							APIVersion: "v1",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: "e2e-btg-cm",
						},
						Data: map[string]string{"key": "value"},
					}}},
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
					Template: breakRequestTemplateReference(brt.GetName()),
				},
			}
			defer EventuallyDeletion(br)

			EventuallyCreation(func() error {
				return k8sClient.Create(ctx, br)
			}).Should(Succeed())

			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-btg-cm", Namespace: br.Namespace}, cm)).To(Succeed())
				g.Expect(cm.Labels).To(HaveKeyWithValue(apimeta.CreatedByCapsuleLabel, apimeta.ValueControllerBreakTheGlass))
				g.Expect(cm.Labels).To(HaveKeyWithValue(apimeta.ProtectedByCapsuleLabel, apimeta.ValueControllerBreakTheGlass))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			cm.Data["key"] = "tampered"
			err := k8sClient.Update(ctx, cm)
			Expect(apierrors.IsForbidden(err)).To(BeTrue())
			Expect(err).To(MatchError(ContainSubstring("can only be changed by the Capsule controller")))

			// should be deleted after duration
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-btg-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
	})

	Describe("Protection disabled for a target", func() {
		BeforeEach(func() {
			protect := false
			brt.Spec.Resources[0].Policy.Protect = &protect
		})

		It("allows the managed resource to be changed", func() {
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-btg-unprotected", Namespace: "default"},
				Spec:       capsulev1beta2.BreakRequestSpec{Template: breakRequestTemplateReference(brt.GetName())},
			}
			defer EventuallyDeletion(br)
			EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-btg-cm", Namespace: br.Namespace}, cm)).To(Succeed())
				g.Expect(cm.Labels).NotTo(HaveKey(apimeta.ProtectedByCapsuleLabel))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			cm.Data["key"] = "changed"
			Expect(k8sClient.Update(ctx, cm)).To(Succeed())
		})
	})

	Describe("Cluster-scoped targets", func() {
		BeforeEach(func() {
			brt.Spec.DefaultDuration = nil
			brt.Spec.Resources = []apiruntime.ResourceTemplate{{
				Targets: []runtime.RawExtension{{Object: &rbacv1.ClusterRole{
					TypeMeta:   metav1.TypeMeta{APIVersion: rbacv1.SchemeGroupVersion.String(), Kind: "ClusterRole"},
					ObjectMeta: metav1.ObjectMeta{Name: "e2e-btg-cluster-role"},
					Rules: []rbacv1.PolicyRule{{
						APIGroups: []string{"apps"},
						Resources: []string{"deployments"},
						Verbs:     []string{"get"},
					}},
				}}},
			}}
		})

		It("cascades deletion through the BreakRequest finalizer without an owner reference", func() {
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-btg-cluster-scope", Namespace: "default"},
				Spec:       capsulev1beta2.BreakRequestSpec{Template: breakRequestTemplateReference(brt.GetName())},
			}
			defer EventuallyDeletion(br)
			EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

			role := &rbacv1.ClusterRole{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-btg-cluster-role"}, role)).To(Succeed())
				g.Expect(role.OwnerReferences).To(BeEmpty())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			Expect(k8sClient.Delete(ctx, br)).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: role.Name}, role)
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
					Template: breakRequestTemplateReference(brt.GetName()),
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
					Template: breakRequestTemplateReference(brt.GetName()),
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
					Template: breakRequestTemplateReference(brt.GetName()),
					Reason:   "open sesame",
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

		It("rejects a break request when the automatic approval condition does not match", func() {
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-btg-br",
					Namespace: "default",
				},
				Spec: capsulev1beta2.BreakRequestSpec{
					Template: breakRequestTemplateReference(brt.GetName()),
					Reason:   "test",
				},
			}

			err := k8sClient.Create(ctx, br)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("approval conditions not satisfied for template")))
		})
	})

	Describe("Approval based on requestor identity", func() {
		BeforeEach(func() {
			brt.Spec.AutoApprove = true
			brt.Spec.ApprovalCondition = `requestor.name == "alice" && "developers" in requestor.groups`
		})

		It("auto-approves a matching authenticated requestor", func() {
			aliceClient := impersonationClient("alice", []string{"developers"})
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-btg-requestor-alice", Namespace: "default"},
				Spec: capsulev1beta2.BreakRequestSpec{
					Template: breakRequestTemplateReference(brt.GetName()),
				},
			}
			defer EventuallyDeletion(br)

			EventuallyCreation(func() error { return aliceClient.Create(ctx, br) }).Should(Succeed())

			cm := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-btg-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		It("rejects a non-matching authenticated requestor", func() {
			bobClient := impersonationClient("bob", []string{"developers"})
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-btg-requestor-bob", Namespace: "default"},
				Spec: capsulev1beta2.BreakRequestSpec{
					Template: breakRequestTemplateReference(brt.GetName()),
				},
			}

			err := bobClient.Create(ctx, br)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("approval conditions not satisfied for template")))
		})
	})

	Describe("Approval based on reviewer identity", func() {
		BeforeEach(func() {
			brt.Spec.AutoApprove = false
			brt.Spec.ApprovalCondition = `"admin" in reviewer.groups`
		})

		It("only permits an authenticated reviewer in the required group", func() {
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-btg-reviewer", Namespace: "default"},
				Spec: capsulev1beta2.BreakRequestSpec{
					Template: breakRequestTemplateReference(brt.GetName()),
				},
			}
			defer EventuallyDeletion(br)
			EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

			bobClient := impersonationClient("bob", []string{"users"})
			requested := &capsulev1beta2.BreakRequest{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: br.Name, Namespace: br.Namespace}, requested)).To(Succeed())
				g.Expect(requested.Status.Phase).To(Equal(capsulev1beta2.RequestPhaseRequested))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			properties, err := requested.GenerateApprovedProperties()
			Expect(err).NotTo(HaveOccurred())
			Expect(requested.ApproveRequest(&breaktheglass.AccessEntity{Name: "spoofed"}, properties, "")).To(Succeed())
			err = bobClient.Status().Update(ctx, requested)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("approval conditions not satisfied for template")))

			charlieClient := impersonationClient("charlie", []string{"users", "admin"})
			requested = &capsulev1beta2.BreakRequest{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: br.Name, Namespace: br.Namespace}, requested)).To(Succeed())
			properties, err = requested.GenerateApprovedProperties()
			Expect(err).NotTo(HaveOccurred())
			Expect(requested.ApproveRequest(&breaktheglass.AccessEntity{Name: "spoofed"}, properties, "")).To(Succeed())
			Expect(charlieClient.Status().Update(ctx, requested)).To(Succeed())

			cm := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-btg-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
	})

	Describe("Template with parameter", func() {
		BeforeEach(func() {
			brt.Spec.Resources = []apiruntime.ResourceTemplate{
				{
					Targets: []runtime.RawExtension{{Object: &corev1.ConfigMap{
						TypeMeta: metav1.TypeMeta{
							Kind:       "ConfigMap",
							APIVersion: "v1",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: "e2e-btg-cm",
						},
						Data: map[string]string{"key": "{{.value}}"},
					}}},
				},
			}
			brt.Spec.ParamSchema = runtime.RawExtension{Raw: []byte(`{"type": "object", "required": ["value"], "properties": {"value": {"type": "string"}}}`)}
		})
		It("should create correct a ConfigMap data", func() {
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-btg-br",
					Namespace: "default",
				},
				Spec: capsulev1beta2.BreakRequestSpec{
					Template: breakRequestTemplateReference(brt.GetName()),
					Params:   &runtime.RawExtension{Raw: []byte(`{"value": "test-value"}`)},
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

	Describe("Adopting an existing resource", func() {
		BeforeEach(func() {
			brt.Spec.Resources[0].Policy.Creation = apiruntime.ResourceCreationPolicyMerge
		})

		It("should prune only the fields managed by the break request", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-btg-cm",
					Namespace: "default",
				},
				Data: map[string]string{"existing": "preserved"},
			}
			defer EventuallyDeletion(cm)
			EventuallyCreation(func() error {
				cm.ResourceVersion = ""

				return k8sClient.Create(ctx, cm)
			}).Should(Succeed())

			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-btg-adopt",
					Namespace: "default",
				},
				Spec: capsulev1beta2.BreakRequestSpec{Template: breakRequestTemplateReference(brt.GetName())},
			}
			defer EventuallyDeletion(br)
			EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

			Eventually(func(g Gomega) {
				actual := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: cm.Name, Namespace: cm.Namespace,
				}, actual)).To(Succeed())
				g.Expect(actual.Data).To(HaveKeyWithValue("existing", "preserved"))
				g.Expect(actual.Data).To(HaveKeyWithValue("key", "value"))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				actual := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: cm.Name, Namespace: cm.Namespace,
				}, actual)).To(Succeed())
				g.Expect(actual.Data).To(Equal(map[string]string{"existing": "preserved"}))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
	})

	Describe("Loading template context", func() {
		BeforeEach(func() {
			brt.Spec.ParamSchema = runtime.RawExtension{Raw: []byte(`{
				"type":"object",
				"required":["sourceName"],
				"properties":{"sourceName":{"type":"string"}}
			}`)}
			brt.Spec.Context = &tpl.TemplateContext{Resources: []*tpl.TemplateResourceReference{{
				ResourceReference: tpl.ResourceReference{
					VersionKind: apiruntime.VersionKind{APIVersion: "v1", Kind: "ConfigMap"},
					Name:        "{{ .sourceName }}",
				},
				Index: "settings",
			}}}
			brt.Spec.Resources = []apiruntime.ResourceTemplate{{Template: `
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: e2e-btg-cm
data:
  loaded: {{ (index $.context.resources.settings 0).data.value }}
`}}
		})

		It("makes parameter-selected context available to every rendered template", func() {
			source := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-btg-context", Namespace: "default"},
				Data:       map[string]string{"value": "from-context"},
			}
			defer EventuallyDeletion(source)
			EventuallyCreation(func() error { return k8sClient.Create(ctx, source) }).Should(Succeed())

			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-btg-context", Namespace: "default"},
				Spec: capsulev1beta2.BreakRequestSpec{
					Template: breakRequestTemplateReference(brt.Name),
					Params: &runtime.RawExtension{Raw: []byte(`{
						"sourceName":"e2e-btg-context"
					}`)},
				},
			}
			defer EventuallyDeletion(br)
			EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

			Eventually(func(g Gomega) {
				actual := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "e2e-btg-cm", Namespace: "default",
				}, actual)).To(Succeed())
				g.Expect(actual.Data).To(HaveKeyWithValue("loaded", "from-context"))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
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
