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
	breaktheglassapi "github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	apimeta "github.com/projectcapsule/capsule/pkg/api/meta"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	breakRequestLifecycleTemplateName = "e2e-btg-lifecycle"
	breakRequestRenderingTemplateName = "e2e-btg-rendering-failure"
	breakRequestLifecycleReviewer     = "e2e-btg-reviewer"
	breakRequestRenderingPreviewName  = "e2e-btg-rendering-preview"
)

var _ = Describe(
	"BreakRequest lifecycle admission",
	Ordered,
	Serial,
	Label("break-the-glass", "lifecycle", "admission"),
	func() {
		var (
			ctx               context.Context
			lifecycleTemplate *capsulev1beta2.GlobalBreakRequestTemplate
			renderingTemplate *capsulev1beta2.GlobalBreakRequestTemplate
			reviewerClient    client.Client
		)

		BeforeAll(func() {
			ctx = context.Background()
			lifecycleTemplate = lifecycleBreakRequestTemplate()
			renderingTemplate = renderingFailureBreakRequestTemplate()

			EventuallyCreation(func() error {
				return k8sClient.Create(ctx, lifecycleTemplate)
			}).Should(Succeed())
			EventuallyCreation(func() error {
				return k8sClient.Create(ctx, renderingTemplate)
			}).Should(Succeed())

			grantBreakRequestNamespaceAdmin(ctx, "default", breakRequestLifecycleReviewer)
			reviewerClient = impersonationClient(breakRequestLifecycleReviewer, []string{"reviewers"})
		})

		AfterAll(func() {
			EventuallyDeletion(renderingTemplate)
			EventuallyDeletion(lifecycleTemplate)
		})

		It("prevents controller-owned status from being hijacked during approval", func() {
			request := newLifecycleBreakRequest(
				"e2e-btg-transition-hijack",
				lifecycleTemplate.Name,
				"e2e-btg-transition-original",
			)
			EventuallyCreation(func() error { return k8sClient.Create(ctx, request) }).Should(Succeed())
			DeferCleanup(func() { cleanupLifecycleBreakRequest(ctx, request) })

			requested := waitForBreakRequestPhase(ctx, request, capsulev1beta2.RequestPhaseRequested)
			requireRenderedBreakRequestStatus(requested)

			injectedResources := []apiruntime.RenderedResource{{
				Targets: []runtime.RawExtension{{Raw: []byte(`{
					"apiVersion":"v1",
					"kind":"Secret",
					"metadata":{"name":"e2e-btg-transition-injected"}
				}`)}},
			}}

			By("rejecting controller-owned status changes when no transition was requested")
			tampered := requested.DeepCopy()
			tampered.Status.Approved.Resources = injectedResources
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
			invalidTransition := waitForBreakRequestPhase(ctx, request, capsulev1beta2.RequestPhaseRequested)
			invalidTransitionBefore := invalidTransition.DeepCopy()
			invalidTransition.Status.Phase = capsulev1beta2.RequestPhaseActive
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
				"invalid BreakRequest transition: can only activate an approved request",
			)))

			By("discarding injected status while applying an authenticated phase transition")
			requested = waitForBreakRequestPhase(ctx, request, capsulev1beta2.RequestPhaseRequested)
			expectedApproved := requested.Status.Approved.DeepCopy()
			expectedServiceAccount := requested.Status.ServiceAccount.DeepCopy()
			expectedTemplate := requested.Status.Template.DeepCopy()

			hijacked := requested.DeepCopy()
			hijacked.Status.Phase = capsulev1beta2.RequestPhaseApproved
			hijacked.Status.Approved.Resources = injectedResources
			hijacked.Status.ServiceAccount = &apimeta.NamespacedRFC1123ObjectReferenceWithNamespace{
				Name:      "injected-runner",
				Namespace: "kube-system",
			}
			hijacked.Status.Template = &capsulev1beta2.ResolvedBreakRequestTemplateReference{
				BreakRequestTemplateReference: globalBreakRequestTemplateReference("injected-template"),
				ResourceVersion:               "injected-version",
			}
			hijacked.Status.Review = &capsulev1beta2.ReviewInfo{
				Reviewer: &breaktheglassapi.AccessEntity{
					Name: "mallory",
					Type: breaktheglassapi.AccessEntityTypeSystem,
				},
				Verdict: capsulev1beta2.RequestVerdictDenied,
				Message: "reviewed from an untrusted payload",
			}
			hijacked.Status.Size = 999

			Expect(reviewerClient.Status().Patch(
				ctx,
				hijacked,
				client.MergeFromWithOptions(requested, client.MergeFromWithOptimisticLock{}),
			)).To(Succeed())

			active := waitForBreakRequestPhase(ctx, request, capsulev1beta2.RequestPhaseActive)
			Expect(active.Status.Approved).To(Equal(expectedApproved))
			Expect(active.Status.ServiceAccount).To(Equal(expectedServiceAccount))
			Expect(active.Status.Template).To(Equal(expectedTemplate))
			Expect(active.Status.Size).To(Equal(uint(1)))
			Expect(active.Status.Review).NotTo(BeNil())
			Expect(active.Status.Review.Reviewer).NotTo(BeNil())
			Expect(active.Status.Review.Reviewer.Name).To(Equal(breakRequestLifecycleReviewer))
			Expect(active.Status.Review.Reviewer.Type).To(Equal(breaktheglassapi.AccessEntityTypeUser))
			Expect(active.Status.Review.Verdict).To(Equal(capsulev1beta2.RequestVerdictApproved))
			Expect(active.Status.Review.Message).To(Equal("reviewed from an untrusted payload"))

			original := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "e2e-btg-transition-original", Namespace: request.Namespace,
			}, original)).To(Succeed())

			injected := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: "e2e-btg-transition-injected", Namespace: request.Namespace,
			}, injected)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		Describe("deletion protection", func() {
			It("allows a requested request awaiting review to be cancelled", func() {
				request := newLifecycleBreakRequest(
					"e2e-btg-delete-requested",
					lifecycleTemplate.Name,
					"e2e-btg-delete-requested-target",
				)
				EventuallyCreation(func() error { return k8sClient.Create(ctx, request) }).Should(Succeed())
				DeferCleanup(func() { cleanupLifecycleBreakRequest(ctx, request) })

				requested := waitForBreakRequestPhase(ctx, request, capsulev1beta2.RequestPhaseRequested)
				Expect(k8sClient.Delete(ctx, requested)).To(Succeed())
				Eventually(func() bool {
					current := &capsulev1beta2.BreakRequest{}
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current)

					return apierrors.IsNotFound(err)
				}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
			})

			It("allows a pending request to be cancelled", func() {
				request := newLifecycleBreakRequest(
					"e2e-btg-delete-pending",
					lifecycleTemplate.Name,
					"e2e-btg-delete-pending-target",
				)
				EventuallyCreation(func() error { return k8sClient.Create(ctx, request) }).Should(Succeed())
				DeferCleanup(func() { cleanupLifecycleBreakRequest(ctx, request) })

				requested := waitForBreakRequestPhase(ctx, request, capsulev1beta2.RequestPhaseRequested)
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

				pending := waitForBreakRequestPhase(ctx, request, capsulev1beta2.RequestPhasePending)
				Expect(k8sClient.Delete(ctx, pending)).To(Succeed())
				Eventually(func() bool {
					current := &capsulev1beta2.BreakRequest{}
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current)

					return apierrors.IsNotFound(err)
				}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
			})

			It("protects a denied request", func() {
				request := newLifecycleBreakRequest(
					"e2e-btg-delete-denied",
					lifecycleTemplate.Name,
					"e2e-btg-delete-denied-target",
				)
				EventuallyCreation(func() error { return k8sClient.Create(ctx, request) }).Should(Succeed())
				DeferCleanup(func() { cleanupLifecycleBreakRequest(ctx, request) })

				patchLifecyclePhase(ctx, reviewerClient, request, capsulev1beta2.RequestPhaseDenied)
				waitForBreakRequestPhase(ctx, request, capsulev1beta2.RequestPhaseDenied)
				expectBreakRequestDeletionDenied(ctx, request, capsulev1beta2.RequestPhaseDenied)
			})

			It("protects an approved request before its start time", func() {
				request := newLifecycleBreakRequest(
					"e2e-btg-delete-approved",
					lifecycleTemplate.Name,
					"e2e-btg-delete-approved-target",
				)
				startTime := metav1.NewTime(time.Now().Add(10 * time.Minute))
				request.Spec.StartTime = &startTime
				EventuallyCreation(func() error { return k8sClient.Create(ctx, request) }).Should(Succeed())
				DeferCleanup(func() { cleanupLifecycleBreakRequest(ctx, request) })

				patchLifecyclePhase(ctx, reviewerClient, request, capsulev1beta2.RequestPhaseApproved)
				waitForBreakRequestPhase(ctx, request, capsulev1beta2.RequestPhaseApproved)
				expectBreakRequestDeletionDenied(ctx, request, capsulev1beta2.RequestPhaseApproved)
			})

			It("protects an active request", func() {
				request := newLifecycleBreakRequest(
					"e2e-btg-delete-active",
					lifecycleTemplate.Name,
					"e2e-btg-delete-active-target",
				)
				EventuallyCreation(func() error { return k8sClient.Create(ctx, request) }).Should(Succeed())
				DeferCleanup(func() { cleanupLifecycleBreakRequest(ctx, request) })

				patchLifecyclePhase(ctx, reviewerClient, request, capsulev1beta2.RequestPhaseApproved)
				waitForBreakRequestPhase(ctx, request, capsulev1beta2.RequestPhaseActive)
				expectBreakRequestDeletionDenied(ctx, request, capsulev1beta2.RequestPhaseActive)
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
						capsulerbac.UserSpec{Kind: capsulerbac.UserOwner, Name: breakRequestLifecycleReviewer},
					)
				})

				request := newLifecycleBreakRequest(
					"e2e-btg-delete-active-admin",
					lifecycleTemplate.Name,
					"e2e-btg-delete-active-admin-target",
				)
				EventuallyCreation(func() error { return k8sClient.Create(ctx, request) }).Should(Succeed())
				DeferCleanup(func() { cleanupLifecycleBreakRequest(ctx, request) })

				patchLifecyclePhase(ctx, reviewerClient, request, capsulev1beta2.RequestPhaseApproved)
				waitForBreakRequestPhase(ctx, request, capsulev1beta2.RequestPhaseActive)

				Eventually(func() error {
					current := &capsulev1beta2.BreakRequest{}
					if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current); err != nil {
						return client.IgnoreNotFound(err)
					}

					return reviewerClient.Delete(ctx, current)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				Eventually(func() bool {
					current := &capsulev1beta2.BreakRequest{}
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current)

					return apierrors.IsNotFound(err)
				}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
			})

			It("protects an expired request until archive retention ends", func() {
				request := newLifecycleBreakRequest(
					"e2e-btg-delete-archived",
					lifecycleTemplate.Name,
					"e2e-btg-delete-archived-target",
				)
				EventuallyCreation(func() error { return k8sClient.Create(ctx, request) }).Should(Succeed())
				DeferCleanup(func() { cleanupLifecycleBreakRequest(ctx, request) })

				requested := waitForBreakRequestPhase(ctx, request, capsulev1beta2.RequestPhaseRequested)
				before := requested.DeepCopy()
				keepFor := breaktheglassapi.ExtendedDuration(8 * time.Second)
				requested.Status.Approved.KeepFor = &keepFor
				requested.Status.Phase = capsulev1beta2.RequestPhaseApproved
				Expect(reviewerClient.Status().Patch(
					ctx,
					requested,
					client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}),
				)).To(Succeed())

				waitForBreakRequestPhase(ctx, request, capsulev1beta2.RequestPhaseActive)
				expireActiveBreakRequest(ctx, request)

				archived := waitForBreakRequestPhase(ctx, request, capsulev1beta2.RequestPhaseExpired)
				Expect(archived.Status.KeepUntil).NotTo(BeNil())
				Expect(archived.Status.KeepUntil.After(time.Now())).To(BeTrue())
				expectBreakRequestDeletionDenied(ctx, request, capsulev1beta2.RequestPhaseExpired)

				Eventually(func() bool {
					current := &capsulev1beta2.BreakRequest{}
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current)

					return apierrors.IsNotFound(err)
				}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
			})
		})

		It("keeps a failed render observable and blocks application, approval, and deletion", func() {
			request := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "e2e-btg-rendering-failure",
					Namespace: "default",
				},
				Spec: capsulev1beta2.BreakRequestSpec{
					Template: globalBreakRequestTemplateReference(renderingTemplate.Name),
				},
			}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, request) }).Should(Succeed())
			DeferCleanup(func() { cleanupLifecycleBreakRequest(ctx, request) })

			current := &capsulev1beta2.BreakRequest{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current)).To(Succeed())
				ready := k8smeta.FindStatusCondition(current.Status.Conditions, apimeta.ReadyCondition)
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(ready.Reason).To(Equal("TemplateRenderingFailed"))
				g.Expect(ready.Message).To(ContainSubstring("rendering resource 1 template"))
				g.Expect(ready.Message).To(ContainSubstring("map has no entry for key"))
				g.Expect(current.Status.Phase).To(BeEmpty())
				g.Expect(current.Status.Template).NotTo(BeNil())
				g.Expect(current.Status.Template.Name).To(Equal(renderingTemplate.Name))
				g.Expect(current.Status.Template.ResourceVersion).NotTo(BeEmpty())
				g.Expect(current.Status.Approved).NotTo(BeNil())
				g.Expect(current.Status.Approved.Resources).To(HaveLen(1))
				g.Expect(current.Status.Size).To(BeZero())
				g.Expect(current.Status.ProcessedItems).To(BeEmpty())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			By("retaining successful render output only as a status preview")
			Consistently(func() bool {
				preview := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: breakRequestRenderingPreviewName, Namespace: request.Namespace,
				}, preview)

				return apierrors.IsNotFound(err)
			}, 4*time.Second, 500*time.Millisecond).Should(BeTrue())

			By("rejecting approval while the rendered snapshot is not ready")
			before := current.DeepCopy()
			current.Status.Phase = capsulev1beta2.RequestPhaseApproved
			err := reviewerClient.Status().Patch(
				ctx,
				current,
				client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}),
			)
			Expect(apierrors.IsForbidden(err)).To(BeTrue())
			Expect(err).To(MatchError(ContainSubstring(
				"cannot approve BreakRequest: rendered resources are not ready",
			)))

			current = &capsulev1beta2.BreakRequest{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current)).To(Succeed())
			Expect(current.Status.Phase).To(BeEmpty())
			expectBreakRequestDeletionDenied(ctx, request, "")
		})
	},
)

func lifecycleBreakRequestTemplate() *capsulev1beta2.GlobalBreakRequestTemplate {
	return &capsulev1beta2.GlobalBreakRequestTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: breakRequestLifecycleTemplateName},
		Spec: capsulev1beta2.GlobalBreakRequestTemplateSpec{
			Approvals: breaktheglassapi.ApprovalSpec{
				Approvers: capsulerbac.UserListSpec{{
					Kind: capsulerbac.UserOwner,
					Name: breakRequestLifecycleReviewer,
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

func renderingFailureBreakRequestTemplate() *capsulev1beta2.GlobalBreakRequestTemplate {
	return &capsulev1beta2.GlobalBreakRequestTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: breakRequestRenderingTemplateName},
		Spec: capsulev1beta2.GlobalBreakRequestTemplateSpec{
			Approvals: breaktheglassapi.ApprovalSpec{
				Approvers: capsulerbac.UserListSpec{{
					Kind: capsulerbac.UserOwner,
					Name: breakRequestLifecycleReviewer,
				}},
			},
			Resources: []apiruntime.ResourceTemplate{
				{
					Targets: []runtime.RawExtension{{Raw: []byte(fmt.Sprintf(`{
						"apiVersion":"v1",
						"kind":"ConfigMap",
						"metadata":{"name":%q},
						"data":{"source":"successful-preview"}
					}`, breakRequestRenderingPreviewName))}},
				},
				{
					Template: `
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: e2e-btg-rendering-never-applied
data:
  missing: {{ $.params.missing }}
`,
				},
			},
		},
	}
}

func newLifecycleBreakRequest(name, templateName, targetName string) *capsulev1beta2.BreakRequest {
	return &capsulev1beta2.BreakRequest{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: capsulev1beta2.BreakRequestSpec{
			Template: globalBreakRequestTemplateReference(templateName),
			Params: &runtime.RawExtension{Raw: []byte(fmt.Sprintf(
				`{"targetName":%q}`,
				targetName,
			))},
		},
	}
}

func requireRenderedBreakRequestStatus(request *capsulev1beta2.BreakRequest) {
	Expect(request.Status.Approved).NotTo(BeNil())
	Expect(request.Status.Approved.Resources).NotTo(BeEmpty())
	Expect(request.Status.ServiceAccount).NotTo(BeNil())
	Expect(request.Status.Template).NotTo(BeNil())
}

func waitForBreakRequestPhase(
	ctx context.Context,
	request *capsulev1beta2.BreakRequest,
	phase capsulev1beta2.RequestPhase,
) *capsulev1beta2.BreakRequest {
	current := &capsulev1beta2.BreakRequest{}
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current)).To(Succeed())
		g.Expect(current.Status.Phase).To(Equal(phase))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

	return current.DeepCopy()
}

func patchLifecyclePhase(
	ctx context.Context,
	actor client.Client,
	request *capsulev1beta2.BreakRequest,
	phase capsulev1beta2.RequestPhase,
) {
	current := waitForBreakRequestPhase(ctx, request, capsulev1beta2.RequestPhaseRequested)
	before := current.DeepCopy()
	current.Status.Phase = phase
	Expect(actor.Status().Patch(
		ctx,
		current,
		client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}),
	)).To(Succeed())
}

func expectBreakRequestDeletionDenied(
	ctx context.Context,
	request *capsulev1beta2.BreakRequest,
	phase capsulev1beta2.RequestPhase,
) {
	current := waitForBreakRequestPhase(ctx, request, phase)
	err := k8sClient.Delete(ctx, current)
	Expect(apierrors.IsForbidden(err)).To(BeTrue())
	Expect(err).To(MatchError(ContainSubstring("cannot be deleted before")))
	if phase != capsulev1beta2.RequestPhaseExpired {
		Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("current phase: %s", phase))))
	} else {
		Expect(err).To(MatchError(ContainSubstring("archive retention expires")))
	}

	persisted := &capsulev1beta2.BreakRequest{}
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(request), persisted)).To(Succeed())
	Expect(persisted.DeletionTimestamp.IsZero()).To(BeTrue())
	Expect(persisted.Status.Phase).To(Equal(phase))
}

func cleanupLifecycleBreakRequest(ctx context.Context, request *capsulev1beta2.BreakRequest) {
	expireBreakRequestForCleanup(ctx, request)
	EventuallyDeletion(request)
}
