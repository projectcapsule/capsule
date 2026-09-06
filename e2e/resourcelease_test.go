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
	apimeta "github.com/projectcapsule/capsule/pkg/api/meta"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/resourcelease"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
	tpl "github.com/projectcapsule/capsule/pkg/template"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const globalResourceLeaseTemplateSelectorLabel = "e2e.projectcapsule.dev/resourcelease-template"

func globalResourceLeaseTemplateReference(name string) capsulev1beta2.GlobalResourceLeaseTemplateReference {
	return capsulev1beta2.GlobalResourceLeaseTemplateReference{
		Kind: capsulev1beta2.GlobalResourceLeaseTemplateKind,
		Name: name,
	}
}

var _ = Describe("creating a GlobalResourceLeaseTemplate", Ordered, Label("resource-lease"), func() {

	var (
		ctx             context.Context
		brt             *capsulev1beta2.GlobalResourceLeaseTemplate
		defaultDuration = 5 * time.Second
	)

	BeforeEach(func() {
		ctx = context.TODO()
		brt = &capsulev1beta2.GlobalResourceLeaseTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name: "e2e-resourcelease",
			},
			Spec: capsulev1beta2.GlobalResourceLeaseTemplateSpec{
				Approvals: resourcelease.ApprovalSpec{Auto: true},
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
							Name: "e2e-resourcelease-cm",
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
			t := &capsulev1beta2.GlobalResourceLeaseTemplate{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: brt.GetName()}, t)).Should(Succeed())
		})
		It("reconciles unrestricted namespace availability into status", func() {
			expectGlobalResourceLeaseTemplateNamespaces(ctx, brt.Name, "*")
		})
		It("should create a ConfigMap and delete it after timeout", func() {
			br := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-resourcelease-br",
					Namespace: "default",
				},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: globalResourceLeaseTemplateReference(brt.GetName()),
				},
			}
			defer EventuallyDeletion(br)

			EventuallyCreation(func() error {
				return k8sClient.Create(ctx, br)
			}).Should(Succeed())

			current := &capsulev1beta2.ResourceLease{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: br.Name, Namespace: br.Namespace}, current)).To(Succeed())
				g.Expect(current.Status.Request.Template).NotTo(BeNil())
				g.Expect(current.Status.Request.Template.Kind).To(Equal(capsulev1beta2.GlobalResourceLeaseTemplateKind))
				g.Expect(current.Status.Request.Template.Name).To(Equal(brt.Name))
				g.Expect(current.Status.Request.Template.ResourceVersion).NotTo(BeEmpty())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-resourcelease-cm", Namespace: br.Namespace}, cm)).To(Succeed())
				g.Expect(cm.Labels).To(HaveKeyWithValue(apimeta.CreatedByCapsuleLabel, apimeta.ValueControllerResourceLease))
				g.Expect(cm.Labels).To(HaveKeyWithValue(apimeta.ProtectedByCapsuleLabel, apimeta.ValueControllerResourceLease))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			cm.Data["key"] = "tampered"
			err := k8sClient.Update(ctx, cm)
			Expect(apierrors.IsForbidden(err)).To(BeTrue())
			Expect(err).To(MatchError(ContainSubstring("can only be changed by the Capsule controller")))

			// should be deleted after duration
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-resourcelease-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
	})

	Describe("Deletion lifecycle", func() {
		BeforeEach(func() {
			brt.Spec.DefaultDuration = &metav1.Duration{Duration: 4 * time.Second}
			keepFor := resourcelease.ExtendedDuration(6 * time.Second)
			brt.Spec.KeepFor = &keepFor
		})

		It("rejects deletion while active and archived, then deletes after retention expires", func() {
			br := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-resourcelease-deletion-lifecycle", Namespace: "default"},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: globalResourceLeaseTemplateReference(brt.Name),
				},
			}
			defer EventuallyDeletion(br)
			EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

			current := &capsulev1beta2.ResourceLease{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: br.Name, Namespace: br.Namespace}, current)).To(Succeed())
				g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.ResourceLeasePhaseActive))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			err := k8sClient.Delete(ctx, current)
			Expect(err).To(MatchError(ContainSubstring("cannot be deleted before it has expired")))

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: br.Name, Namespace: br.Namespace}, current)).To(Succeed())
				g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.ResourceLeasePhaseExpired))
				g.Expect(current.Status.KeepUntil).NotTo(BeNil())
				g.Expect(current.Status.KeepUntil.After(time.Now())).To(BeTrue())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			err = k8sClient.Delete(ctx, current)
			Expect(err).To(MatchError(ContainSubstring("cannot be deleted before archive retention expires")))

			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: br.Name, Namespace: br.Namespace}, current)

				return apierrors.IsNotFound(err)
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
	})

	Describe("Protection disabled for a target", func() {
		BeforeEach(func() {
			protect := false
			brt.Spec.Resources[0].Policy.Protect = &protect
		})

		It("allows the managed resource to be changed", func() {
			br := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-resourcelease-unprotected", Namespace: "default"},
				Spec:       capsulev1beta2.ResourceLeaseSpec{Template: globalResourceLeaseTemplateReference(brt.GetName())},
			}
			defer EventuallyDeletion(br)
			EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-resourcelease-cm", Namespace: br.Namespace}, cm)).To(Succeed())
				g.Expect(cm.Labels).NotTo(HaveKey(apimeta.ProtectedByCapsuleLabel))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			cm.Data["key"] = "changed"
			Expect(k8sClient.Update(ctx, cm)).To(Succeed())
		})
	})

	Describe("Orphan deletion policy", func() {
		BeforeEach(func() {
			brt.Spec.DefaultDuration = nil
			brt.Spec.Resources[0].Policy.Deletion = apiruntime.ResourceDeletionPolicyOrphan
		})

		It("retains the resource and removes Capsule lifecycle metadata", func() {
			br := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-resourcelease-orphan", Namespace: "default"},
				Spec:       capsulev1beta2.ResourceLeaseSpec{Template: globalResourceLeaseTemplateReference(brt.GetName())},
			}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-resourcelease-cm", Namespace: br.Namespace}, cm)).To(Succeed())
				g.Expect(cm.Labels).To(HaveKeyWithValue(apimeta.CreatedByCapsuleLabel, apimeta.ValueControllerResourceLease))
				g.Expect(cm.Labels).To(HaveKeyWithValue(apimeta.ProtectedByCapsuleLabel, apimeta.ValueControllerResourceLease))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			defer EventuallyDeletion(cm)

			expireActiveResourceLease(ctx, br)
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: br.Name, Namespace: br.Namespace}, br)
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, cm)).To(Succeed())
				g.Expect(cm.Data).To(HaveKeyWithValue("key", "value"))
				g.Expect(cm.Labels).NotTo(HaveKey(apimeta.CreatedByCapsuleLabel))
				g.Expect(cm.Labels).NotTo(HaveKey(apimeta.NewManagedByCapsuleLabel))
				g.Expect(cm.Labels).NotTo(HaveKey(apimeta.ProtectedByCapsuleLabel))
				g.Expect(cm.Labels).NotTo(HaveKey(apimeta.AppManagedByLabel))
				g.Expect(cm.Annotations).NotTo(HaveKey(apimeta.ResourceLeaseServiceAccountAnnotation))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			cm.Data["key"] = "managed-after-orphaning"
			Expect(k8sClient.Update(ctx, cm)).To(Succeed())
		})
	})

	Describe("Cluster-scoped targets", func() {
		BeforeEach(func() {
			brt.Spec.DefaultDuration = nil
			brt.Spec.Resources = []apiruntime.ResourceTemplate{{
				Targets: []runtime.RawExtension{{Object: &rbacv1.ClusterRole{
					TypeMeta:   metav1.TypeMeta{APIVersion: rbacv1.SchemeGroupVersion.String(), Kind: "ClusterRole"},
					ObjectMeta: metav1.ObjectMeta{Name: "e2e-resourcelease-cluster-role"},
					Rules: []rbacv1.PolicyRule{{
						APIGroups: []string{"apps"},
						Resources: []string{"deployments"},
						Verbs:     []string{"get"},
					}},
				}}},
			}}
		})

		It("cascades deletion through the ResourceLease finalizer without an owner reference", func() {
			br := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-resourcelease-cluster-scope", Namespace: "default"},
				Spec:       capsulev1beta2.ResourceLeaseSpec{Template: globalResourceLeaseTemplateReference(brt.GetName())},
			}
			defer EventuallyDeletion(br)
			EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

			role := &rbacv1.ClusterRole{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-resourcelease-cluster-role"}, role)).To(Succeed())
				g.Expect(role.OwnerReferences).To(BeEmpty())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			expireActiveResourceLease(ctx, br)
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
			br := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-resourcelease-br",
					Namespace: "default",
				},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: globalResourceLeaseTemplateReference(brt.GetName()),
				},
			}
			defer EventuallyDeletion(br)

			EventuallyCreation(func() error {
				return k8sClient.Create(ctx, br)
			}).Should(Succeed())

			cm := &corev1.ConfigMap{}
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-resourcelease-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			time.Sleep(defaultDuration + 2*time.Second)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-resourcelease-cm", Namespace: br.Namespace}, cm)).Should(Succeed())

			expireActiveResourceLease(ctx, br)
		})
	})

	Describe("Approval required", func() {
		BeforeEach(func() {
			brt.Spec.Approvals.Auto = false
		})
		It("resource lease need approval", func() {

			br := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-resourcelease-br",
					Namespace: "default",
				},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: globalResourceLeaseTemplateReference(brt.GetName()),
				},
			}
			defer EventuallyDeletion(br)

			EventuallyCreation(func() error {
				return k8sClient.Create(ctx, br)
			}).Should(Succeed())

			approveResourceLease(ctx, br)

			cm := &corev1.ConfigMap{}
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-resourcelease-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			// should be deleted after duration
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-resourcelease-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
	})

	Describe("Automatic approval with OR conditions", func() {
		BeforeEach(func() {
			brt.Spec.Approvals = resourcelease.ApprovalSpec{
				Auto: true,
				Approvers: capsulerbac.UserListSpec{{
					Kind: capsulerbac.UserOwner,
					Name: "an-irrelevant-manual-approver",
				}},
				Conditions: []string{
					"request.spec.reason == 'not this one'",
					"request.spec.reason == 'open sesame'",
				},
			}
		})
		It("auto approves when any condition matches and ignores manual approvers", func() {
			br := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-resourcelease-br",
					Namespace: "default",
				},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: globalResourceLeaseTemplateReference(brt.GetName()),
					Reason:   "open sesame",
				},
			}
			defer EventuallyDeletion(br)

			EventuallyCreation(func() error {
				return k8sClient.Create(ctx, br)
			}).Should(Succeed())

			cm := &corev1.ConfigMap{}
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-resourcelease-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			// should be deleted after duration
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-resourcelease-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})

		It("rejects a resource lease when none of the automatic approval conditions match", func() {
			br := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-resourcelease-br",
					Namespace: "default",
				},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: globalResourceLeaseTemplateReference(brt.GetName()),
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
			brt.Spec.Approvals = resourcelease.ApprovalSpec{
				Auto:       true,
				Conditions: []string{`requestor.name == "alice" && "developers" in requestor.groups`},
			}
		})

		It("auto-approves a matching authenticated requestor", func() {
			grantResourceLeaseNamespaceAdmin(ctx, "default", "alice")

			aliceClient := impersonationClient("alice", []string{"developers"})
			br := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-resourcelease-requestor-alice", Namespace: "default"},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: globalResourceLeaseTemplateReference(brt.GetName()),
				},
			}
			defer EventuallyDeletion(br)

			EventuallyCreation(func() error { return aliceClient.Create(ctx, br) }).Should(Succeed())

			cm := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-resourcelease-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		It("rejects a non-matching authenticated requestor", func() {
			grantResourceLeaseNamespaceAdmin(ctx, "default", "bob")

			bobClient := impersonationClient("bob", []string{"developers"})
			br := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-resourcelease-requestor-bob", Namespace: "default"},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: globalResourceLeaseTemplateReference(brt.GetName()),
				},
			}

			Eventually(func(g Gomega) {
				err := bobClient.Create(ctx, br)
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(ContainSubstring("approval conditions not satisfied for template")))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
	})

	Describe("Manual approval authorization", func() {
		BeforeEach(func() {
			brt.Spec.Approvals = resourcelease.ApprovalSpec{
				Approvers: capsulerbac.UserListSpec{{Kind: capsulerbac.UserOwner, Name: "charlie"}},
				Conditions: []string{
					`reviewer.name == "nobody"`,
					`"admin" in reviewer.groups`,
				},
			}
		})

		It("requires an explicit approver and any one CEL condition", func() {
			grantResourceLeaseNamespaceAdmin(ctx, "default", "bob")
			grantResourceLeaseNamespaceAdmin(ctx, "default", "charlie")

			br := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-resourcelease-reviewer", Namespace: "default"},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: globalResourceLeaseTemplateReference(brt.GetName()),
				},
			}
			defer EventuallyDeletion(br)
			EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

			bobClient := impersonationClient("bob", []string{"users", "admin"})
			requested := &capsulev1beta2.ResourceLease{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: br.Name, Namespace: br.Namespace}, requested)).To(Succeed())
				g.Expect(requested.Status.Phase).To(Equal(capsulev1beta2.ResourceLeasePhaseRequested))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			properties, err := requested.GenerateRequestStatus()
			Expect(err).NotTo(HaveOccurred())
			Expect(requested.ApproveLease(&resourcelease.AccessEntity{Name: "spoofed"}, properties, "")).To(Succeed())
			err = bobClient.Status().Update(ctx, requested)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("is not permitted to approve requests for template")))

			charlieClient := impersonationClient("charlie", []string{"users", "admin"})
			requested = &capsulev1beta2.ResourceLease{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: br.Name, Namespace: br.Namespace}, requested)).To(Succeed())
			properties, err = requested.GenerateRequestStatus()
			Expect(err).NotTo(HaveOccurred())
			Expect(requested.ApproveLease(&resourcelease.AccessEntity{Name: "spoofed"}, properties, "")).To(Succeed())
			Expect(charlieClient.Status().Update(ctx, requested)).To(Succeed())

			cm := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-resourcelease-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		When("the approver is a group and no CEL conditions are configured", func() {
			BeforeEach(func() {
				brt.Spec.Approvals = resourcelease.ApprovalSpec{
					Approvers: capsulerbac.UserListSpec{{Kind: capsulerbac.GroupOwner, Name: "on-call"}},
				}
			})

			It("only permits authenticated members of the group", func() {
				grantResourceLeaseNamespaceAdmin(ctx, "default", "alice")
				grantResourceLeaseNamespaceAdmin(ctx, "default", "bob")

				br := &capsulev1beta2.ResourceLease{
					ObjectMeta: metav1.ObjectMeta{Name: "e2e-resourcelease-group-approver", Namespace: "default"},
					Spec: capsulev1beta2.ResourceLeaseSpec{
						Template: globalResourceLeaseTemplateReference(brt.GetName()),
					},
				}
				defer EventuallyDeletion(br)
				EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

				requested := &capsulev1beta2.ResourceLease{}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: br.Name, Namespace: br.Namespace}, requested)).To(Succeed())
					g.Expect(requested.Status.Phase).To(Equal(capsulev1beta2.ResourceLeasePhaseRequested))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				properties, err := requested.GenerateRequestStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(requested.ApproveLease(&resourcelease.AccessEntity{Name: "spoofed"}, properties, "")).To(Succeed())
				err = impersonationClient("alice", []string{"developers"}).Status().Update(ctx, requested)
				Expect(err).To(MatchError(ContainSubstring("is not permitted to approve requests for template")))

				requested = &capsulev1beta2.ResourceLease{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: br.Name, Namespace: br.Namespace}, requested)).To(Succeed())
				properties, err = requested.GenerateRequestStatus()
				Expect(err).NotTo(HaveOccurred())
				Expect(requested.ApproveLease(&resourcelease.AccessEntity{Name: "spoofed"}, properties, "")).To(Succeed())
				Expect(impersonationClient("bob", []string{"developers", "on-call"}).Status().Update(ctx, requested)).To(Succeed())
			})
		})
	})

	Describe("Namespace selection", func() {
		var (
			allowedNamespace *corev1.Namespace
			deniedNamespace  *corev1.Namespace
		)

		BeforeEach(func() {
			allowedNamespace = NewNamespace("")
			allowedNamespace.Labels[globalResourceLeaseTemplateSelectorLabel] = allowedNamespace.Name
			deniedNamespace = NewNamespace("")

			NamespaceCreationAdmin(allowedNamespace, defaultTimeoutInterval).Should(Succeed())
			NamespaceCreationAdmin(deniedNamespace, defaultTimeoutInterval).Should(Succeed())
			DeferCleanup(func() {
				EventuallyDeletion(allowedNamespace)
				EventuallyDeletion(deniedNamespace)
			})

			brt.Spec.Approvals.Auto = false
			brt.Spec.NamespaceSelectors = []selectors.NamespaceSelector{{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{globalResourceLeaseTemplateSelectorLabel: allowedNamespace.Name},
				},
			}}
		})

		It("reconciles namespace label changes into template status", func() {
			expectGlobalResourceLeaseTemplateNamespaces(ctx, brt.Name, allowedNamespace.Name)

			deniedNamespace.Labels[globalResourceLeaseTemplateSelectorLabel] = allowedNamespace.Name
			Expect(k8sClient.Update(ctx, deniedNamespace)).To(Succeed())
			expectGlobalResourceLeaseTemplateNamespaces(ctx, brt.Name, allowedNamespace.Name, deniedNamespace.Name)

			delete(allowedNamespace.Labels, globalResourceLeaseTemplateSelectorLabel)
			Expect(k8sClient.Update(ctx, allowedNamespace)).To(Succeed())
			expectGlobalResourceLeaseTemplateNamespaces(ctx, brt.Name, deniedNamespace.Name)
		})

		It("allows a selected namespace to reference the template", func() {
			expectGlobalResourceLeaseTemplateNamespaces(ctx, brt.Name, allowedNamespace.Name)

			request := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-resourcelease-selected-namespace",
					Namespace: allowedNamespace.Name,
				},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: globalResourceLeaseTemplateReference(brt.Name),
				},
			}
			DeferCleanup(func() {
				expireResourceLeaseForCleanup(ctx, request)
				EventuallyDeletion(request)
			})

			EventuallyCreation(func() error {
				return k8sClient.Create(ctx, request)
			}).Should(Succeed())
		})

		It("rejects an unselected namespace referencing the template", func() {
			expectGlobalResourceLeaseTemplateNamespaces(ctx, brt.Name, allowedNamespace.Name)

			request := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-resourcelease-unselected-namespace",
					Namespace: deniedNamespace.Name,
				},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: globalResourceLeaseTemplateReference(brt.Name),
				},
			}

			err := k8sClient.Create(ctx, request)
			Expect(err).To(MatchError(ContainSubstring(
				"template " + brt.Name + " is not available in namespace " + deniedNamespace.Name,
			)))
		})

		It("rejects a reference to a template that does not exist", func() {
			request := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-resourcelease-missing-template",
					Namespace: allowedNamespace.Name,
				},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: globalResourceLeaseTemplateReference(brt.Name + "-missing"),
				},
			}

			err := k8sClient.Create(ctx, request)
			Expect(err).To(MatchError(ContainSubstring("template " + brt.Name + "-missing not found")))
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
							Name: "e2e-resourcelease-cm",
						},
						Data: map[string]string{"key": "{{.value}}"},
					}}},
				},
			}
			brt.Spec.ParamSchema = &runtime.RawExtension{Raw: []byte(`{
				"type":"object",
				"required":["value"],
				"properties":{"value":{"type":"string","pattern":"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"}}
			}`)}
		})
		It("should create correct a ConfigMap data", func() {
			br := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-resourcelease-br",
					Namespace: "default",
				},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: globalResourceLeaseTemplateReference(brt.GetName()),
					Params:   &runtime.RawExtension{Raw: []byte(`{"value": "test-value"}`)},
				},
			}
			defer func() {
				expireResourceLeaseForCleanup(ctx, br)
				EventuallyDeletion(br)
			}()

			EventuallyCreation(func() error {
				err := k8sClient.Create(ctx, br)
				return err
			}).Should(Succeed())

			cm := &corev1.ConfigMap{}
			Eventually(func() (err error) {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "e2e-resourcelease-cm", Namespace: br.Namespace}, cm)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(cm.Data["key"]).Should(Equal("test-value"))
		})

		It("rejects invalid parameters at admission", func() {
			br := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-resourcelease-invalid-params",
					Namespace: "default",
				},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: globalResourceLeaseTemplateReference(brt.GetName()),
					Params:   &runtime.RawExtension{Raw: []byte(`{"value":"admin:sad"}`)},
				},
			}
			err := k8sClient.Create(ctx, br)
			Expect(apierrors.IsForbidden(err)).To(BeTrue())
			Expect(err).To(MatchError(ContainSubstring("value in body should match")))
		})
	})

	Describe("Adopting an existing resource", func() {
		BeforeEach(func() {
			brt.Spec.Resources[0].Policy.Creation = apiruntime.ResourceCreationPolicyMerge
		})

		It("should prune only the fields managed by the resource lease", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-resourcelease-cm",
					Namespace: "default",
				},
				Data: map[string]string{"existing": "preserved"},
			}
			defer EventuallyDeletion(cm)
			EventuallyCreation(func() error {
				cm.ResourceVersion = ""

				return k8sClient.Create(ctx, cm)
			}).Should(Succeed())

			br := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-resourcelease-adopt",
					Namespace: "default",
				},
				Spec: capsulev1beta2.ResourceLeaseSpec{Template: globalResourceLeaseTemplateReference(brt.GetName())},
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
			brt.Spec.ParamSchema = &runtime.RawExtension{Raw: []byte(`{
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
  name: e2e-resourcelease-cm
data:
  loaded: {{ (index $.context.resources.settings 0).data.value }}
`}}
		})

		It("makes parameter-selected context available to every rendered template", func() {
			source := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-resourcelease-context", Namespace: "default"},
				Data:       map[string]string{"value": "from-context"},
			}
			defer EventuallyDeletion(source)
			EventuallyCreation(func() error { return k8sClient.Create(ctx, source) }).Should(Succeed())

			br := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-resourcelease-context", Namespace: "default"},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: globalResourceLeaseTemplateReference(brt.Name),
					Params: &runtime.RawExtension{Raw: []byte(`{
						"sourceName":"e2e-resourcelease-context"
					}`)},
				},
			}
			defer EventuallyDeletion(br)
			EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

			Eventually(func(g Gomega) {
				actual := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "e2e-resourcelease-cm", Namespace: "default",
				}, actual)).To(Succeed())
				g.Expect(actual.Data).To(HaveKeyWithValue("loaded", "from-context"))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
	})
})

func expectGlobalResourceLeaseTemplateNamespaces(ctx context.Context, name string, expected ...string) {
	Eventually(func(g Gomega) {
		current := &capsulev1beta2.GlobalResourceLeaseTemplate{}
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, current)).To(Succeed())
		g.Expect(current.Status.ObservedGeneration).To(Equal(current.Generation))
		g.Expect(current.Status.Namespaces).To(ConsistOf(expected))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func expireActiveResourceLease(ctx context.Context, request *capsulev1beta2.ResourceLease) {
	Eventually(func(g Gomega) {
		current := &capsulev1beta2.ResourceLease{}
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: request.Name, Namespace: request.Namespace,
		}, current)).To(Succeed())
		g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.ResourceLeasePhaseActive))

		before := current.DeepCopy()
		current.Status.Phase = capsulev1beta2.ResourceLeasePhaseExpired
		g.Expect(k8sClient.Status().Patch(
			ctx,
			current,
			client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}),
		)).To(Succeed())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

// expireResourceLeaseForCleanup advances requests which failed before activation
// through the lifecycle instead of bypassing deletion admission. Tests should
// use expireActiveResourceLease when activation itself is under test.
func expireResourceLeaseForCleanup(ctx context.Context, request *capsulev1beta2.ResourceLease) {
	Eventually(func(g Gomega) {
		current := &capsulev1beta2.ResourceLease{}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name: request.Name, Namespace: request.Namespace,
		}, current)
		if apierrors.IsNotFound(err) {
			return
		}

		g.Expect(err).NotTo(HaveOccurred())
		if current.Status.Phase == capsulev1beta2.ResourceLeasePhaseExpired {
			return
		}

		before := current.DeepCopy()
		current.Status.Phase = capsulev1beta2.ResourceLeasePhaseExpired
		g.Expect(k8sClient.Status().Patch(
			ctx,
			current,
			client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}),
		)).To(Succeed())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func grantResourceLeaseNamespaceAdmin(ctx context.Context, namespace, username string) {
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "e2e-resourcelease-admin-",
			Namespace:    namespace,
		},
		Subjects: []rbacv1.Subject{{
			APIGroup: rbacv1.GroupName,
			Kind:     rbacv1.UserKind,
			Name:     username,
		}},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     "admin",
		},
	}

	Expect(k8sClient.Create(ctx, roleBinding)).To(Succeed())
	DeferCleanup(func() {
		EventuallyDeletion(roleBinding)
	})
}

func approveResourceLease(ctx context.Context, br *capsulev1beta2.ResourceLease) {
	br2 := &capsulev1beta2.ResourceLease{}
	Eventually(func() (err error) {
		err = k8sClient.Get(ctx, types.NamespacedName{Name: br.GetName(), Namespace: br.Namespace}, br2)
		if err != nil {
			return err
		}
		if br2.Status.Phase != capsulev1beta2.ResourceLeasePhaseRequested {
			return errors.New("resource lease not in requested phase")
		}
		return nil
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	Expect(br2.Status.Request).ShouldNot(BeNil())

	before := br2.DeepCopy()
	br2.Status.Phase = capsulev1beta2.ResourceLeasePhaseApproved
	Expect(k8sClient.Status().Patch(
		ctx,
		br2,
		client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}),
	)).Should(Succeed())
}
