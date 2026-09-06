// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	apimeta "github.com/projectcapsule/capsule/pkg/api/meta"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	resourceleaseapi "github.com/projectcapsule/capsule/pkg/api/resourcelease"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	evt "github.com/projectcapsule/capsule/pkg/runtime/events"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	resourceLeaseLifecycleTemplateName = "e2e-resourcelease-lifecycle"
	resourceLeaseRenderingTemplateName = "e2e-resourcelease-rendering-failure"
	resourceLeaseLifecycleReviewer     = "e2e-resourcelease-reviewer"
	resourceLeaseRenderingPreviewName  = "e2e-resourcelease-rendering-preview"
)

var _ = Describe(
	"ResourceLease lifecycle admission",
	Ordered,
	Serial,
	Label("resource-lease", "lifecycle", "admission"),
	func() {
		var (
			ctx               context.Context
			lifecycleTemplate *capsulev1beta2.GlobalResourceLeaseTemplate
			renderingTemplate *capsulev1beta2.GlobalResourceLeaseTemplate
			reviewerClient    client.Client
		)

		BeforeAll(func() {
			ctx = context.Background()
			lifecycleTemplate = lifecycleResourceLeaseTemplate()
			renderingTemplate = renderingFailureResourceLeaseTemplate()

			EventuallyCreation(func() error {
				return k8sClient.Create(ctx, lifecycleTemplate)
			}).Should(Succeed())
			EventuallyCreation(func() error {
				return k8sClient.Create(ctx, renderingTemplate)
			}).Should(Succeed())

			grantResourceLeaseNamespaceAdmin(ctx, "default", resourceLeaseLifecycleReviewer)
			reviewerClient = impersonationClient(resourceLeaseLifecycleReviewer, []string{"reviewers"})
		})

		AfterAll(func() {
			EventuallyDeletion(renderingTemplate)
			EventuallyDeletion(lifecycleTemplate)
		})

		It("prevents controller-owned status from being hijacked during approval", func() {
			request := newLifecycleResourceLease(
				"e2e-resourcelease-transition-hijack",
				lifecycleTemplate.Name,
				"e2e-resourcelease-transition-original",
			)
			EventuallyCreation(func() error { return k8sClient.Create(ctx, request) }).Should(Succeed())
			DeferCleanup(func() { cleanupLifecycleResourceLease(ctx, request) })

			requested := waitForResourceLeasePhase(ctx, request, capsulev1beta2.ResourceLeasePhaseRequested)
			requireRenderedResourceLeaseStatus(requested)

			injectedResources := []apiruntime.RenderedResource{{
				Targets: []runtime.RawExtension{{Raw: []byte(`{
					"apiVersion":"v1",
					"kind":"Secret",
					"metadata":{"name":"e2e-resourcelease-transition-injected"}
				}`)}},
			}}

			By("rejecting controller-owned status changes when no transition was requested")
			tampered := requested.DeepCopy()
			tampered.Status.Request.Resources = injectedResources
			err := reviewerClient.Status().Patch(
				ctx,
				tampered,
				client.MergeFromWithOptions(requested, client.MergeFromWithOptimisticLock{}),
			)
			Expect(apierrors.IsForbidden(err)).To(BeTrue())
			Expect(err).To(MatchError(ContainSubstring(
				"rendered resources can only be changed by the Capsule controller",
			)))

			By("rejecting a shortcut from Requested directly to Active")
			invalidTransition := waitForResourceLeasePhase(ctx, request, capsulev1beta2.ResourceLeasePhaseRequested)
			invalidTransitionBefore := invalidTransition.DeepCopy()
			invalidTransition.Status.Phase = capsulev1beta2.ResourceLeasePhaseActive
			err = reviewerClient.Status().Patch(
				ctx,
				invalidTransition,
				client.MergeFromWithOptions(
					invalidTransitionBefore,
					client.MergeFromWithOptimisticLock{},
				),
			)
			Expect(apierrors.IsForbidden(err)).To(BeTrue())
			Expect(err).To(MatchError(ContainSubstring(
				"invalid ResourceLease transition: can only activate an approved request",
			)))

			By("discarding injected status while applying an authenticated phase transition")
			requested = waitForResourceLeasePhase(ctx, request, capsulev1beta2.ResourceLeasePhaseRequested)
			expectedApproved := requested.Status.Request.DeepCopy()
			expectedServiceAccount := requested.Status.Request.Impersonation.DeepCopy()
			expectedTemplate := requested.Status.Request.Template.DeepCopy()

			hijacked := requested.DeepCopy()
			hijacked.Status.Phase = capsulev1beta2.ResourceLeasePhaseApproved
			hijacked.Status.Request.Resources = injectedResources
			hijacked.Status.Request.Impersonation = &apimeta.NamespacedRFC1123ObjectReferenceWithNamespace{
				Name:      "injected-runner",
				Namespace: "kube-system",
			}
			hijacked.Status.Request.Template = &capsulev1beta2.ResolvedResourceLeaseTemplateReference{
				ResourceLeaseTemplateReference: globalResourceLeaseTemplateReference("injected-template"),
				ResourceVersion:                "injected-version",
			}
			hijacked.Status.Request.Approvals = &resourceleaseapi.ApprovalSpec{
				Auto:       true,
				Conditions: []string{"false"},
			}
			hijacked.Status.Review = &capsulev1beta2.ReviewInfo{
				Reviewer: &resourceleaseapi.AccessEntity{
					Name: "mallory",
					Type: resourceleaseapi.AccessEntityTypeSystem,
				},
				Verdict: capsulev1beta2.ResourceLeaseVerdictDenied,
				Message: "reviewed from an untrusted payload",
			}
			hijacked.Status.Transitions = append(hijacked.Status.Transitions, capsulev1beta2.ResourceLeaseTransition{
				Type:      capsulev1beta2.ResourceLeasePhaseApproved,
				Timestamp: metav1.Now(),
				Actor:     capsulev1beta2.ResourceLeaseTransitionActor{Name: "mallory", Type: resourceleaseapi.AccessEntityTypeSystem},
				Reason:    "InjectedTransition",
			})
			hijacked.Status.Size = 999

			Expect(reviewerClient.Status().Patch(
				ctx,
				hijacked,
				client.MergeFromWithOptions(requested, client.MergeFromWithOptimisticLock{}),
			)).To(Succeed())

			active := waitForResourceLeasePhase(ctx, request, capsulev1beta2.ResourceLeasePhaseActive)
			Expect(active.Status.Request).To(Equal(expectedApproved))
			Expect(active.Status.Request.Impersonation).To(Equal(expectedServiceAccount))
			Expect(active.Status.Request.Template).To(Equal(expectedTemplate))
			Expect(active.Status.Size).To(Equal(uint(1)))
			Expect(active.Status.Review).NotTo(BeNil())
			Expect(active.Status.Review.Reviewer).NotTo(BeNil())
			Expect(active.Status.Review.Reviewer.Name).To(Equal(resourceLeaseLifecycleReviewer))
			Expect(active.Status.Review.Reviewer.Type).To(Equal(resourceleaseapi.AccessEntityTypeUser))
			Expect(active.Status.Review.Verdict).To(Equal(capsulev1beta2.ResourceLeaseVerdictApproved))
			Expect(active.Status.Review.Message).To(Equal("reviewed from an untrusted payload"))
			createdTransition := active.LatestTransition(capsulev1beta2.ResourceLeasePhaseCreated)
			Expect(createdTransition).NotTo(BeNil())
			Expect(createdTransition.Actor.Name).To(Equal(active.Spec.Requestor.Name))
			Expect(createdTransition.Actor.Type).To(Equal(active.Spec.Requestor.Type))
			Expect(createdTransition.Timestamp).To(Equal(active.CreationTimestamp))
			Expect(active.Status.Transitions[0].Type).To(Equal(capsulev1beta2.ResourceLeasePhaseCreated))
			approvedTransition := active.LatestTransition(capsulev1beta2.ResourceLeasePhaseApproved)
			Expect(approvedTransition).NotTo(BeNil())
			Expect(approvedTransition.Actor.Name).To(Equal(resourceLeaseLifecycleReviewer))
			Expect(approvedTransition.Actor.Type).To(Equal(resourceleaseapi.AccessEntityTypeUser))
			Expect(approvedTransition.Reason).To(Equal("ApprovedByUser"))
			activeTransition := active.LatestTransition(capsulev1beta2.ResourceLeasePhaseActive)
			Expect(activeTransition).NotTo(BeNil())
			Expect(activeTransition.Actor.Type).To(Equal(resourceleaseapi.AccessEntityTypeSystem))
			expectResourceLeaseEvent(
				ctx,
				active,
				capsulev1beta2.ResourceLeasePhaseApproved,
				evt.ReasonResourceLeaseApproved,
				evt.ActionApproved,
			)
			for _, transition := range active.Status.Transitions {
				Expect(transition.Actor.Name).NotTo(Equal("mallory"))
				Expect(transition.Reason).NotTo(Equal("InjectedTransition"))
			}

			original := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "e2e-resourcelease-transition-original", Namespace: request.Namespace,
			}, original)).To(Succeed())

			injected := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: "e2e-resourcelease-transition-injected", Namespace: request.Namespace,
			}, injected)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		Describe("deletion protection", func() {
			It("allows a requested request awaiting review to be cancelled", func() {
				request := newLifecycleResourceLease(
					"e2e-resourcelease-delete-requested",
					lifecycleTemplate.Name,
					"e2e-resourcelease-delete-requested-target",
				)
				EventuallyCreation(func() error { return k8sClient.Create(ctx, request) }).Should(Succeed())
				DeferCleanup(func() { cleanupLifecycleResourceLease(ctx, request) })

				requested := waitForResourceLeasePhase(ctx, request, capsulev1beta2.ResourceLeasePhaseRequested)
				Expect(k8sClient.Delete(ctx, requested)).To(Succeed())
				Eventually(func() bool {
					current := &capsulev1beta2.ResourceLease{}
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current)

					return apierrors.IsNotFound(err)
				}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
			})

			It("allows a pending request to be cancelled", func() {
				request := newLifecycleResourceLease(
					"e2e-resourcelease-delete-pending",
					lifecycleTemplate.Name,
					"e2e-resourcelease-delete-pending-target",
				)
				EventuallyCreation(func() error { return k8sClient.Create(ctx, request) }).Should(Succeed())
				DeferCleanup(func() { cleanupLifecycleResourceLease(ctx, request) })

				requested := waitForResourceLeasePhase(ctx, request, capsulev1beta2.ResourceLeasePhaseRequested)
				before := requested.DeepCopy()
				Expect(requested.SetPending()).To(Succeed())
				controllerClient := impersonationClient(
					ControllerServiceAccountFull,
					serviceAccountGroups(ControllerNamespace),
				)
				Expect(controllerClient.Status().Patch(
					ctx,
					requested,
					client.MergeFromWithOptions(
						before,
						client.MergeFromWithOptimisticLock{},
					),
				)).To(Succeed())

				pending := waitForResourceLeasePhase(ctx, request, capsulev1beta2.ResourceLeasePhasePending)
				Expect(k8sClient.Delete(ctx, pending)).To(Succeed())
				Eventually(func() bool {
					current := &capsulev1beta2.ResourceLease{}
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current)

					return apierrors.IsNotFound(err)
				}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
			})

			It("protects a denied request", func() {
				request := newLifecycleResourceLease(
					"e2e-resourcelease-delete-denied",
					lifecycleTemplate.Name,
					"e2e-resourcelease-delete-denied-target",
				)
				EventuallyCreation(func() error { return k8sClient.Create(ctx, request) }).Should(Succeed())
				DeferCleanup(func() { cleanupLifecycleResourceLease(ctx, request) })

				patchLifecyclePhase(ctx, reviewerClient, request, capsulev1beta2.ResourceLeasePhaseDenied)
				denied := waitForResourceLeasePhase(ctx, request, capsulev1beta2.ResourceLeasePhaseDenied)
				expectResourceLeaseEvent(
					ctx,
					denied,
					capsulev1beta2.ResourceLeasePhaseDenied,
					evt.ReasonResourceLeaseDenied,
					evt.ActionDenied,
				)
				expectResourceLeaseDeletionDenied(ctx, request, capsulev1beta2.ResourceLeasePhaseDenied)
			})

			It("protects an approved request before its start time", func() {
				request := newLifecycleResourceLease(
					"e2e-resourcelease-delete-approved",
					lifecycleTemplate.Name,
					"e2e-resourcelease-delete-approved-target",
				)
				startTime := metav1.NewTime(time.Now().Add(10 * time.Minute))
				request.Spec.StartTime = &startTime
				EventuallyCreation(func() error { return k8sClient.Create(ctx, request) }).Should(Succeed())
				DeferCleanup(func() { cleanupLifecycleResourceLease(ctx, request) })

				patchLifecyclePhase(ctx, reviewerClient, request, capsulev1beta2.ResourceLeasePhaseApproved)
				waitForResourceLeasePhase(ctx, request, capsulev1beta2.ResourceLeasePhaseApproved)
				expectResourceLeaseDeletionDenied(ctx, request, capsulev1beta2.ResourceLeasePhaseApproved)
			})

			It("protects an active request", func() {
				request := newLifecycleResourceLease(
					"e2e-resourcelease-delete-active",
					lifecycleTemplate.Name,
					"e2e-resourcelease-delete-active-target",
				)
				EventuallyCreation(func() error { return k8sClient.Create(ctx, request) }).Should(Succeed())
				DeferCleanup(func() { cleanupLifecycleResourceLease(ctx, request) })

				patchLifecyclePhase(ctx, reviewerClient, request, capsulev1beta2.ResourceLeasePhaseApproved)
				waitForResourceLeasePhase(ctx, request, capsulev1beta2.ResourceLeasePhaseActive)
				expectResourceLeaseDeletionDenied(ctx, request, capsulev1beta2.ResourceLeasePhaseActive)
			})

			It("does not block deletion of its namespace", func() {
				namespace := NewNamespace("")
				NamespaceCreationAdmin(namespace, defaultTimeoutInterval).Should(Succeed())
				DeferCleanup(func() { ForceDeleteNamespace(ctx, namespace.Name) })

				grantResourceLeaseNamespaceAdmin(ctx, namespace.Name, resourceLeaseLifecycleReviewer)

				request := newLifecycleResourceLease(
					"e2e-resourcelease-delete-with-namespace",
					lifecycleTemplate.Name,
					"e2e-resourcelease-delete-with-namespace-target",
				)
				request.Namespace = namespace.Name
				EventuallyCreation(func() error { return k8sClient.Create(ctx, request) }).Should(Succeed())
				DeferCleanup(func() { cleanupLifecycleResourceLease(ctx, request) })

				patchLifecyclePhase(ctx, reviewerClient, request, capsulev1beta2.ResourceLeasePhaseApproved)
				waitForResourceLeasePhase(ctx, request, capsulev1beta2.ResourceLeasePhaseActive)
				expectResourceLeaseDeletionDenied(ctx, request, capsulev1beta2.ResourceLeasePhaseActive)

				By("allowing namespace termination to override lifecycle and archive retention")
				Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())

				Eventually(func() bool {
					current := &capsulev1beta2.ResourceLease{}
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current)

					return apierrors.IsNotFound(err)
				}, defaultTerminationTimeoutInterval, defaultPollInterval).Should(BeTrue())

				Eventually(func() bool {
					current := &corev1.Namespace{}
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(namespace), current)

					return apierrors.IsNotFound(err)
				}, defaultTerminationTimeoutInterval, defaultPollInterval).Should(BeTrue())
			})

			It("allows a configured administrator to delete an active request", func() {
				original := &capsulev1beta2.CapsuleConfiguration{}
				Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, original)).To(Succeed())
				originalAdministrators := append(capsulerbac.UserListSpec(nil), original.Spec.Administrators...)
				DeferCleanup(func() {
					ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
						configuration.Spec.Administrators = originalAdministrators
					})
				})
				ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
					configuration.Spec.Administrators = append(
						configuration.Spec.Administrators,
						capsulerbac.UserSpec{Kind: capsulerbac.UserOwner, Name: resourceLeaseLifecycleReviewer},
					)
				})

				request := newLifecycleResourceLease(
					"e2e-resourcelease-delete-active-admin",
					lifecycleTemplate.Name,
					"e2e-resourcelease-delete-active-admin-target",
				)
				EventuallyCreation(func() error { return k8sClient.Create(ctx, request) }).Should(Succeed())
				DeferCleanup(func() { cleanupLifecycleResourceLease(ctx, request) })

				patchLifecyclePhase(ctx, reviewerClient, request, capsulev1beta2.ResourceLeasePhaseApproved)
				waitForResourceLeasePhase(ctx, request, capsulev1beta2.ResourceLeasePhaseActive)

				Eventually(func() error {
					current := &capsulev1beta2.ResourceLease{}
					if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current); err != nil {
						return client.IgnoreNotFound(err)
					}

					return reviewerClient.Delete(ctx, current)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				Eventually(func() bool {
					current := &capsulev1beta2.ResourceLease{}
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current)

					return apierrors.IsNotFound(err)
				}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
			})

			It("protects an expired request until archive retention ends", func() {
				request := newLifecycleResourceLease(
					"e2e-resourcelease-delete-archived",
					lifecycleTemplate.Name,
					"e2e-resourcelease-delete-archived-target",
				)
				EventuallyCreation(func() error { return k8sClient.Create(ctx, request) }).Should(Succeed())
				DeferCleanup(func() { cleanupLifecycleResourceLease(ctx, request) })

				requested := waitForResourceLeasePhase(ctx, request, capsulev1beta2.ResourceLeasePhaseRequested)
				before := requested.DeepCopy()
				keepFor := resourceleaseapi.ExtendedDuration(8 * time.Second)
				requested.Status.Request.KeepFor = &keepFor
				requested.Status.Phase = capsulev1beta2.ResourceLeasePhaseApproved
				Expect(reviewerClient.Status().Patch(
					ctx,
					requested,
					client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}),
				)).To(Succeed())

				waitForResourceLeasePhase(ctx, request, capsulev1beta2.ResourceLeasePhaseActive)
				expireActiveResourceLease(ctx, request)

				archived := waitForResourceLeasePhase(ctx, request, capsulev1beta2.ResourceLeasePhaseExpired)
				Expect(archived.Status.KeepUntil).NotTo(BeNil())
				Expect(archived.Status.KeepUntil.After(time.Now())).To(BeTrue())
				expectResourceLeaseDeletionDenied(ctx, request, capsulev1beta2.ResourceLeasePhaseExpired)

				Eventually(func() bool {
					current := &capsulev1beta2.ResourceLease{}
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current)

					return apierrors.IsNotFound(err)
				}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
			})
		})

		It("keeps a failed render observable and blocks application and approval", func() {
			request := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-resourcelease-rendering-failure",
					Namespace: "default",
				},
				Spec: capsulev1beta2.ResourceLeaseSpec{
					Template: globalResourceLeaseTemplateReference(renderingTemplate.Name),
				},
			}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, request) }).Should(Succeed())
			DeferCleanup(func() { cleanupLifecycleResourceLease(ctx, request) })

			current := &capsulev1beta2.ResourceLease{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current)).To(Succeed())
				ready := k8smeta.FindStatusCondition(current.Status.Conditions, apimeta.ReadyCondition)
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(ready.Reason).To(Equal("TemplateRenderingFailed"))
				g.Expect(ready.Message).To(ContainSubstring("rendering resource 1 template"))
				g.Expect(ready.Message).To(ContainSubstring("map has no entry for key"))
				g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.ResourceLeasePhaseCreated))
				g.Expect(current.Status.Request.Template).NotTo(BeNil())
				g.Expect(current.Status.Request.Template.Name).To(Equal(renderingTemplate.Name))
				g.Expect(current.Status.Request.Template.ResourceVersion).NotTo(BeEmpty())
				g.Expect(current.Status.Request).NotTo(BeNil())
				g.Expect(current.Status.Request.Resources).To(HaveLen(1))
				g.Expect(current.Status.Size).To(BeZero())
				g.Expect(current.Status.ProcessedItems).To(BeEmpty())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			By("retaining successful render output only as a status preview")
			Consistently(func() bool {
				preview := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: resourceLeaseRenderingPreviewName, Namespace: request.Namespace,
				}, preview)

				return apierrors.IsNotFound(err)
			}, 4*time.Second, 500*time.Millisecond).Should(BeTrue())

			By("rejecting approval while the rendered snapshot is not ready")
			before := current.DeepCopy()
			current.Status.Phase = capsulev1beta2.ResourceLeasePhaseApproved
			err := reviewerClient.Status().Patch(
				ctx,
				current,
				client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}),
			)
			Expect(apierrors.IsForbidden(err)).To(BeTrue())
			Expect(err).To(MatchError(ContainSubstring(
				"cannot approve ResourceLease: rendered resources are not ready",
			)))

			current = &capsulev1beta2.ResourceLease{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current)).To(Succeed())
			Expect(current.Status.Phase).To(Equal(capsulev1beta2.ResourceLeasePhaseCreated))
			Expect(k8sClient.Delete(ctx, current)).To(Succeed())
		})
	},
)

func lifecycleResourceLeaseTemplate() *capsulev1beta2.GlobalResourceLeaseTemplate {
	return &capsulev1beta2.GlobalResourceLeaseTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: resourceLeaseLifecycleTemplateName},
		Spec: capsulev1beta2.GlobalResourceLeaseTemplateSpec{
			Approvals: resourceleaseapi.ApprovalSpec{
				Approvers: capsulerbac.UserListSpec{{
					Kind: capsulerbac.UserOwner,
					Name: resourceLeaseLifecycleReviewer,
				}},
			},
			ParamSchema: &runtime.RawExtension{Raw: []byte(`{
				"type":"object",
				"required":["targetName"],
				"properties":{"targetName":{"type":"string"}}
			}`)},
			Resources: []apiruntime.ResourceTemplate{{
				Targets: []runtime.RawExtension{{Raw: []byte(`{
					"apiVersion":"v1",
					"kind":"ConfigMap",
					"metadata":{"name":"{{ .targetName }}"},
					"data":{"source":"lifecycle-template"}
				}`)}},
			}},
		},
	}
}

func renderingFailureResourceLeaseTemplate() *capsulev1beta2.GlobalResourceLeaseTemplate {
	return &capsulev1beta2.GlobalResourceLeaseTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: resourceLeaseRenderingTemplateName},
		Spec: capsulev1beta2.GlobalResourceLeaseTemplateSpec{
			Approvals: resourceleaseapi.ApprovalSpec{
				Approvers: capsulerbac.UserListSpec{{
					Kind: capsulerbac.UserOwner,
					Name: resourceLeaseLifecycleReviewer,
				}},
			},
			Resources: []apiruntime.ResourceTemplate{
				{
					Targets: []runtime.RawExtension{{Raw: []byte(fmt.Sprintf(`{
						"apiVersion":"v1",
						"kind":"ConfigMap",
						"metadata":{"name":%q},
						"data":{"source":"successful-preview"}
					}`, resourceLeaseRenderingPreviewName))}},
				},
				{
					Template: `
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: e2e-resourcelease-rendering-never-applied
data:
  missing: {{ $.params.missing }}
`,
				},
			},
		},
	}
}

func newLifecycleResourceLease(name, templateName, targetName string) *capsulev1beta2.ResourceLease {
	return &capsulev1beta2.ResourceLease{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: capsulev1beta2.ResourceLeaseSpec{
			Template: globalResourceLeaseTemplateReference(templateName),
			Params: &runtime.RawExtension{Raw: []byte(fmt.Sprintf(
				`{"targetName":%q}`,
				targetName,
			))},
		},
	}
}

func requireRenderedResourceLeaseStatus(request *capsulev1beta2.ResourceLease) {
	Expect(request.Status.Request).NotTo(BeNil())
	Expect(request.Status.Request.Resources).NotTo(BeEmpty())
	Expect(request.Status.Request.Impersonation).NotTo(BeNil())
	Expect(request.Status.Request.Template).NotTo(BeNil())
}

func waitForResourceLeasePhase(
	ctx context.Context,
	request *capsulev1beta2.ResourceLease,
	phase capsulev1beta2.ResourceLeasePhase,
) *capsulev1beta2.ResourceLease {
	current := &capsulev1beta2.ResourceLease{}
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current)).To(Succeed())
		g.Expect(current.Status.Phase).To(Equal(phase))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

	return current.DeepCopy()
}

func expectResourceLeaseEvent(
	ctx context.Context,
	request *capsulev1beta2.ResourceLease,
	phase capsulev1beta2.ResourceLeasePhase,
	reason,
	action string,
) {
	cs := clusterAdminClient()
	transition := request.LatestTransition(phase)
	Expect(transition).NotTo(BeNil())

	Eventually(func(g Gomega) {
		eventList, err := cs.CoreV1().Events(request.Namespace).List(ctx, metav1.ListOptions{})
		g.Expect(err).NotTo(HaveOccurred())

		for _, event := range eventList.Items {
			if event.InvolvedObject.UID == request.UID &&
				event.Reason == reason &&
				event.Action == action {
				g.Expect(event.Labels).To(HaveKeyWithValue(apimeta.EventActorLabel, transition.Actor.Name))
				g.Expect(event.Labels).To(HaveKeyWithValue(
					apimeta.EventActorKindLabel,
					transition.Actor.Type.String(),
				))

				return
			}
		}

		g.Expect(eventList.Items).To(ContainElement(WithTransform(
			func(event corev1.Event) string {
				return fmt.Sprintf("%s/%s/%s", event.InvolvedObject.UID, event.Reason, event.Action)
			},
			Equal(fmt.Sprintf("%s/%s/%s", request.UID, reason, action)),
		)))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func patchLifecyclePhase(
	ctx context.Context,
	actor client.Client,
	request *capsulev1beta2.ResourceLease,
	phase capsulev1beta2.ResourceLeasePhase,
) {
	current := waitForResourceLeasePhase(ctx, request, capsulev1beta2.ResourceLeasePhaseRequested)
	before := current.DeepCopy()
	current.Status.Phase = phase
	Expect(actor.Status().Patch(
		ctx,
		current,
		client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}),
	)).To(Succeed())
}

func expectResourceLeaseDeletionDenied(
	ctx context.Context,
	request *capsulev1beta2.ResourceLease,
	phase capsulev1beta2.ResourceLeasePhase,
) {
	current := waitForResourceLeasePhase(ctx, request, phase)
	err := k8sClient.Delete(ctx, current)
	Expect(apierrors.IsForbidden(err)).To(BeTrue())
	Expect(err).To(MatchError(ContainSubstring("cannot be deleted before")))
	if phase != capsulev1beta2.ResourceLeasePhaseExpired {
		Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("current phase: %s", phase))))
	} else {
		Expect(err).To(MatchError(ContainSubstring("archive retention expires")))
	}

	persisted := &capsulev1beta2.ResourceLease{}
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(request), persisted)).To(Succeed())
	Expect(persisted.DeletionTimestamp.IsZero()).To(BeTrue())
	Expect(persisted.Status.Phase).To(Equal(phase))
}

func cleanupLifecycleResourceLease(ctx context.Context, request *capsulev1beta2.ResourceLease) {
	expireResourceLeaseForCleanup(ctx, request)
	EventuallyDeletion(request)
}
