// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	apimeta "github.com/projectcapsule/capsule/pkg/api/meta"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/resourcepermit"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	tpl "github.com/projectcapsule/capsule/pkg/template"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	resourcePermitImpersonationTemplateName  = "e2e-resourcepermit-impersonation"
	resourcePermitImpersonationTargetName    = "e2e-resourcepermit-impersonation-target"
	resourcePermitImpersonationContextName   = "e2e-resourcepermit-impersonation-context"
	resourcePermitTemplateServiceAccount     = "e2e-resourcepermit-template-runner"
	resourcePermitDefaultServiceAccount      = "e2e-resourcepermit-default-runner"
	resourcePermitLocalDefaultServiceAccount = "e2e-resourcepermit-local-default-runner"
	resourcePermitReadOnlyServiceAccount     = "e2e-resourcepermit-readonly-runner"
	resourcePermitRetryRequester             = "e2e-resourcepermit-retry-requester"
)

var _ = Describe(
	"ResourcePermit impersonation configuration",
	Ordered,
	Serial,
	Label("resource-permit", "config", "impersonation"),
	func() {
		var (
			ctx                     context.Context
			brt                     *capsulev1beta2.GlobalResourcePermitTemplate
			namespace               *corev1.Namespace
			serviceAccountNamespace string
		)

		BeforeEach(func() {
			ctx = context.Background()
			// Delete target resources before the execution identities they reference.
			serviceAccountNamespace = createResourcePermitTestNamespace(ctx).Name
			namespace = createResourcePermitTestNamespace(ctx)
			brt = &capsulev1beta2.GlobalResourcePermitTemplate{
				Name: resourcePermitImpersonationTemplateName,
				Spec: capsulev1beta2.GlobalResourcePermitTemplateSpec{
					Approvals: resourcepermit.ApprovalSpec{Auto: true},
					Resources: []apiruntime.ResourceTemplate{{Template: `
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: e2e-resourcepermit-impersonation-target
data:
  key: value
`}},
				},
			}
		})

		JustBeforeEach(func() {
			EventuallyCreation(func() error {
				brt.ResourceVersion = ""

				return k8sClient.Create(ctx, brt)
			}).Should(Succeed())
		})

		JustAfterEach(func() {
			EventuallyDeletion(brt)
		})

		Context("with an explicit template ServiceAccount", func() {
			BeforeEach(func() {
				brt.Spec.Impersonation = resourcePermitServiceAccountReference(
					serviceAccountNamespace,
					resourcePermitTemplateServiceAccount,
				)
				brt.Spec.Context = &tpl.TemplateContext{Resources: []*tpl.TemplateResourceReference{{
					APIVersion: "v1", Kind: "ConfigMap",
					Name:  resourcePermitImpersonationContextName,
					Index: "settings",
				}}}
				brt.Spec.Resources = []apiruntime.ResourceTemplate{{Template: `
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: e2e-resourcepermit-impersonation-target
data:
  loaded: {{ (index $.context.resources.settings 0).data.value }}
`}}

				grantResourcePermitServiceAccount(
					serviceAccountNamespace,
					resourcePermitTemplateServiceAccount,
					namespace.Name,
					[]string{"get", "list", "watch", "create", "update", "patch", "delete"},
				)

				source := &corev1.ConfigMap{
					Name: resourcePermitImpersonationContextName, Namespace: namespace.Name,
					Data: map[string]string{"value": "loaded-by-template-service-account"},
				}
				EventuallyCreation(func() error { return k8sClient.Create(ctx, source) }).Should(Succeed())
				DeferCleanup(func() { EventuallyDeletion(source) })
			})

			It("uses the identity for context, apply, protected updates, and deletion", func() {
				br := newImpersonatedResourcePermit(namespace.Name, "e2e-resourcepermit-impersonated", brt.Name)
				DeferCleanup(func() {
					expireResourcePermitForCleanup(ctx, br)
					EventuallyDeletion(br)
				})
				EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

				expectedUsername := serviceAccountUsername(
					serviceAccountNamespace,
					resourcePermitTemplateServiceAccount,
				)
				cm := resourcePermitManagedConfigMap(br.Namespace)
				Eventually(func(g Gomega) {
					current := &capsulev1beta2.ResourcePermit{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					expectResourcePermitServiceAccount(
						g,
						current,
						serviceAccountNamespace,
						resourcePermitTemplateServiceAccount,
					)

					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
					g.Expect(cm.Data).To(HaveKeyWithValue("loaded", "loaded-by-template-service-account"))
					g.Expect(cm.Annotations).To(HaveKeyWithValue(
						apimeta.ResourcePermitServiceAccountAnnotation,
						expectedUsername,
					))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				templateClient := impersonationClient(
					expectedUsername,
					serviceAccountGroups(serviceAccountNamespace),
				)
				cm.Data["updated"] = "by-template-service-account"
				Expect(templateClient.Update(ctx, cm)).To(Succeed())

				By("protecting the template ServiceAccount copied to ResourcePermit status")
				executionServiceAccount := &corev1.ServiceAccount{
					Name:      resourcePermitTemplateServiceAccount,
					Namespace: serviceAccountNamespace}
				Eventually(func() bool {
					err := k8sClient.Delete(ctx, executionServiceAccount, client.DryRunAll)

					return apierrors.IsForbidden(err)
				}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())

				expireActiveResourcePermit(ctx, br)
				expectResourcePermitAndConfigMapDeleted(ctx, br, cm)

				By("allowing deletion after the referencing ResourcePermit has expired")
				Eventually(func() error {
					return k8sClient.Delete(ctx, executionServiceAccount)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				Eventually(func() bool {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(executionServiceAccount), executionServiceAccount)

					return apierrors.IsNotFound(err)
				}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
			})
		})

		Context("without a template ServiceAccount", func() {
			It("records and uses the Capsule controller ServiceAccount when no default is configured", func() {
				original := &capsulev1beta2.CapsuleConfiguration{}
				Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, original)).To(Succeed())
				originalImpersonation := original.Spec.Impersonation
				DeferCleanup(func() {
					ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
						configuration.Spec.Impersonation = originalImpersonation
					})
				})

				ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
					configuration.Spec.Impersonation.GlobalDefaultServiceAccount = ""
					configuration.Spec.Impersonation.GlobalDefaultServiceAccountNamespace = ""
				})

				br := newImpersonatedResourcePermit(namespace.Name, "e2e-resourcepermit-controller-identity", brt.Name)
				DeferCleanup(func() {
					expireResourcePermitForCleanup(ctx, br)
					EventuallyDeletion(br)
				})
				EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

				expectedUsername := serviceAccountUsername(
					ControllerNamespace,
					ControllerServiceAccount,
				)
				cm := resourcePermitManagedConfigMap(br.Namespace)
				Eventually(func(g Gomega) {
					current := &capsulev1beta2.ResourcePermit{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					expectResourcePermitServiceAccount(
						g,
						current,
						ControllerNamespace,
						ControllerServiceAccount,
					)

					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
					g.Expect(cm.Annotations).To(HaveKeyWithValue(
						apimeta.ResourcePermitServiceAccountAnnotation,
						expectedUsername,
					))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				expireActiveResourcePermit(ctx, br)
				expectResourcePermitAndConfigMapDeleted(ctx, br, cm)
			})

			It("uses the global default ServiceAccount from CapsuleConfiguration", func() {
				grantResourcePermitServiceAccount(
					serviceAccountNamespace,
					resourcePermitDefaultServiceAccount,
					namespace.Name,
					[]string{"get", "list", "watch", "create", "update", "patch", "delete"},
				)

				original := &capsulev1beta2.CapsuleConfiguration{}
				Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, original)).To(Succeed())
				originalImpersonation := original.Spec.Impersonation
				DeferCleanup(func() {
					ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
						configuration.Spec.Impersonation = originalImpersonation
					})
				})

				ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
					configuration.Spec.Impersonation.GlobalDefaultServiceAccount =
						apimeta.RFC1123Name(resourcePermitDefaultServiceAccount)
					configuration.Spec.Impersonation.GlobalDefaultServiceAccountNamespace =
						apimeta.RFC1123SubdomainName(serviceAccountNamespace)
				})

				br := newImpersonatedResourcePermit(namespace.Name, "e2e-resourcepermit-default-impersonation", brt.Name)
				DeferCleanup(func() {
					expireResourcePermitForCleanup(ctx, br)
					EventuallyDeletion(br)
				})
				EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

				expectedUsername := serviceAccountUsername(
					serviceAccountNamespace,
					resourcePermitDefaultServiceAccount,
				)
				cm := resourcePermitManagedConfigMap(br.Namespace)
				Eventually(func(g Gomega) {
					current := &capsulev1beta2.ResourcePermit{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					expectResourcePermitServiceAccount(
						g,
						current,
						serviceAccountNamespace,
						resourcePermitDefaultServiceAccount,
					)
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
					g.Expect(cm.Annotations).To(HaveKeyWithValue(
						apimeta.ResourcePermitServiceAccountAnnotation,
						expectedUsername,
					))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				expireActiveResourcePermit(ctx, br)
				expectResourcePermitAndConfigMapDeleted(ctx, br, cm)
			})
		})

		Context("without sufficient target permissions", func() {
			var requesterClient client.Client

			BeforeEach(func() {
				brt.Spec.Impersonation = resourcePermitServiceAccountReference(
					serviceAccountNamespace,
					resourcePermitReadOnlyServiceAccount,
				)
				grantResourcePermitServiceAccount(
					serviceAccountNamespace,
					resourcePermitReadOnlyServiceAccount,
					namespace.Name,
					[]string{"get", "list", "watch"},
				)
				grantResourcePermitNamespaceAdmin(ctx, namespace.Name, resourcePermitRetryRequester)
				requesterClient = impersonationClient(
					resourcePermitRetryRequester,
					[]string{"system:authenticated"},
				)
			})

			It("fails preflight and lets the requester retry after permissions are fixed", func() {
				br := newImpersonatedResourcePermit(namespace.Name, "e2e-resourcepermit-impersonation-forbidden", brt.Name)
				DeferCleanup(func() {
					expireResourcePermitForCleanup(ctx, br)
					EventuallyDeletion(br)
				})
				EventuallyCreation(func() error { return requesterClient.Create(ctx, br) }).Should(Succeed())

				Eventually(func(g Gomega) {
					current := &capsulev1beta2.ResourcePermit{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					expectResourcePermitServiceAccount(
						g,
						current,
						serviceAccountNamespace,
						resourcePermitReadOnlyServiceAccount,
					)

					ready := k8smeta.FindStatusCondition(current.Status.Conditions, apimeta.ReadyCondition)
					g.Expect(ready).NotTo(BeNil())
					g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
					g.Expect(ready.Reason).To(Equal("ResourceDryRunFailed"))
					g.Expect(ready.Message).To(ContainSubstring("forbidden"))
					g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.ResourcePermitPhaseFailed))
					g.Expect(current.Status.Failure).NotTo(BeNil())
					g.Expect(current.Status.Failure.Stage).To(Equal(capsulev1beta2.ResourcePermitFailureStagePreflight))
					g.Expect(current.Status.Failure.RetryPhase).To(Equal(capsulev1beta2.ResourcePermitPhaseApproved))
					g.Expect(current.Status.ProcessedItems).To(BeEmpty())
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				cm := resourcePermitManagedConfigMap(br.Namespace)
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())

				By("granting the missing permissions and retrying as the requester")
				grantResourcePermitServiceAccount(
					serviceAccountNamespace,
					resourcePermitReadOnlyServiceAccount,
					namespace.Name,
					[]string{"get", "list", "watch", "create", "update", "patch", "delete"},
				)
				patchResourcePermitPhaseAs(
					ctx,
					requesterClient,
					br,
					capsulev1beta2.ResourcePermitPhaseRetrying,
				)

				Eventually(func(g Gomega) {
					current := &capsulev1beta2.ResourcePermit{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.ResourcePermitPhaseActive))
					g.Expect(current.Status.Failure).To(BeNil())
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				By("letting the requester expire the recovered request")
				patchResourcePermitPhaseAs(
					ctx,
					requesterClient,
					br,
					capsulev1beta2.ResourcePermitPhaseExpired,
				)
				expectResourcePermitAndConfigMapDeleted(ctx, br, cm)
			})

			Context("when target permissions change after preflight", func() {
				BeforeEach(func() {
					// Hold activation until write permissions have been revoked.
					brt.Spec.Approvals = resourcepermit.ApprovalSpec{
						Approvers: capsulerbac.UserListSpec{{
							Kind: capsulerbac.UserOwner,
							Name: resourcePermitRetryRequester,
						}},
					}
					grantResourcePermitServiceAccount(
						serviceAccountNamespace,
						resourcePermitReadOnlyServiceAccount,
						namespace.Name,
						[]string{"get", "list", "watch", "create", "update", "patch", "delete"},
					)
				})

				It("enters Failed when write permissions are revoked and recovers on retry", func() {
					br := newImpersonatedResourcePermit(namespace.Name, "e2e-resourcepermit-activation-retry", brt.Name)
					DeferCleanup(func() {
						expireResourcePermitForCleanup(ctx, br)
						EventuallyDeletion(br)
					})
					EventuallyCreation(func() error { return requesterClient.Create(ctx, br) }).Should(Succeed())

					Eventually(func(g Gomega) {
						current := &capsulev1beta2.ResourcePermit{}
						g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
						g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.ResourcePermitPhaseRequested))
						ready := k8smeta.FindStatusCondition(current.Status.Conditions, apimeta.ReadyCondition)
						g.Expect(ready).NotTo(BeNil())
						g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
					}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

					By("protecting the resolved ServiceAccount after successful preflight")
					serviceAccount := &corev1.ServiceAccount{
						Name:      resourcePermitReadOnlyServiceAccount,
						Namespace: serviceAccountNamespace}
					Eventually(func(g Gomega) {
						err := k8sClient.Delete(ctx, serviceAccount, client.DryRunAll)
						g.Expect(apierrors.IsForbidden(err)).To(BeTrue(), "expected admission denial, got: %v", err)
						g.Expect(err).To(MatchError(ContainSubstring(
							"used by unexpired ResourcePermit " + br.Namespace + "/" + br.Name,
						)))
					}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

					setConfigMapWriteAccess := func(allowed bool) {
						verbs := []string{"get", "list", "watch"}
						if allowed {
							verbs = append(verbs, "create", "update", "patch", "delete")
						}
						bindServiceAccountToNamespacedResource(
							serviceAccountNamespace,
							resourcePermitReadOnlyServiceAccount,
							namespace.Name,
							[]string{"configmaps"},
							verbs,
						)
						// Wait for RBAC propagation before approval or retry.
						Eventually(func(g Gomega) {
							for _, verb := range []string{"create", "patch"} {
								review := &authorizationv1.SubjectAccessReview{
									Spec: authorizationv1.SubjectAccessReviewSpec{
										User:   serviceAccountUsername(serviceAccountNamespace, resourcePermitReadOnlyServiceAccount),
										Groups: serviceAccountGroups(serviceAccountNamespace),
										ResourceAttributes: &authorizationv1.ResourceAttributes{
											Namespace: namespace.Name,
											Verb:      verb,
											Resource:  "configmaps",
											Name:      resourcePermitImpersonationTargetName,
										},
									},
								}
								g.Expect(k8sClient.Create(ctx, review)).To(Succeed())
								g.Expect(review.Status.EvaluationError).To(BeEmpty())
								g.Expect(review.Status.Allowed).To(Equal(allowed), "%s ConfigMap permission: %+v", verb, review.Status)
							}
						}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
					}
					// Restore access before permit cleanup, including when an assertion fails.
					DeferCleanup(setConfigMapWriteAccess, true)
					By("revoking write permissions and approving the stored snapshot")
					setConfigMapWriteAccess(false)
					patchResourcePermitPhaseAs(ctx, requesterClient, br, capsulev1beta2.ResourcePermitPhaseApproved)

					Eventually(func(g Gomega) {
						current := &capsulev1beta2.ResourcePermit{}
						g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
						g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.ResourcePermitPhaseFailed))
						g.Expect(current.Status.Failure).NotTo(BeNil())
						g.Expect(current.Status.Failure.Stage).To(Equal(capsulev1beta2.ResourcePermitFailureStageActivation))
						g.Expect(current.Status.Failure.RetryPhase).To(Equal(capsulev1beta2.ResourcePermitPhaseApproved))
						ready := k8smeta.FindStatusCondition(current.Status.Conditions, apimeta.ReadyCondition)
						g.Expect(ready).NotTo(BeNil())
						g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
						g.Expect(ready.Reason).To(Equal("ResourceApplyFailed"))
						g.Expect(ready.Message).To(ContainSubstring("forbidden"))
					}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

					cm := resourcePermitManagedConfigMap(br.Namespace)
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)
					Expect(apierrors.IsNotFound(err)).To(BeTrue(), "failed activation must not create the target: %v", err)

					By("restoring write permissions and retrying the stored approved snapshot")
					setConfigMapWriteAccess(true)
					patchResourcePermitPhaseAs(
						ctx,
						requesterClient,
						br,
						capsulev1beta2.ResourcePermitPhaseRetrying,
					)

					Eventually(func(g Gomega) {
						current := &capsulev1beta2.ResourcePermit{}
						g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
						g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.ResourcePermitPhaseActive),
							"ResourcePermit retry did not activate: failure=%+v, conditions=%+v, resources=%+v",
							current.Status.Failure, current.Status.Conditions, current.Status.ProcessedItems)
						g.Expect(current.Status.Failure).To(BeNil())
						g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
					}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

					patchResourcePermitPhaseAs(
						ctx,
						requesterClient,
						br,
						capsulev1beta2.ResourcePermitPhaseExpired,
					)
					expectResourcePermitAndConfigMapDeleted(ctx, br, cm)
				})
			})
		})
	},
)

var _ = Describe(
	"Namespaced ResourcePermitTemplate impersonation configuration",
	Ordered,
	Serial,
	Label("resource-permit", "config", "impersonation"),
	func() {
		var (
			ctx       context.Context
			brt       *capsulev1beta2.ResourcePermitTemplate
			namespace *corev1.Namespace
		)

		BeforeEach(func() {
			ctx = context.Background()
			namespace = createResourcePermitTestNamespace(ctx)

			original := &capsulev1beta2.CapsuleConfiguration{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, original)).To(Succeed())
			originalImpersonation := original.Spec.Impersonation
			DeferCleanup(func() {
				ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
					configuration.Spec.Impersonation = originalImpersonation
				})
			})

			grantResourcePermitServiceAccount(
				namespace.Name,
				resourcePermitLocalDefaultServiceAccount,
				namespace.Name,
				[]string{"get", "list", "watch", "create", "update", "patch", "delete"},
			)
			ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
				configuration.Spec.Impersonation.TenantDefaultServiceAccount =
					apimeta.RFC1123Name(resourcePermitLocalDefaultServiceAccount)
			})

			brt = &capsulev1beta2.ResourcePermitTemplate{
				Name: "e2e-resourcepermit-local-template", Namespace: namespace.Name,
				Spec: capsulev1beta2.ResourcePermitTemplateSpec{
					Approvals: resourcepermit.ApprovalSpec{Auto: true},
					Resources: []apiruntime.ResourceTemplate{{Template: `
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: e2e-resourcepermit-impersonation-target
data:
  source: namespaced-template
`}},
				},
			}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, brt) }).Should(Succeed())
			DeferCleanup(func() { EventuallyDeletion(brt) })
		})

		It("uses the namespace-local configured default and records template provenance", func() {
			br := &capsulev1beta2.ResourcePermit{
				Name: "e2e-resourcepermit-local-default", Namespace: namespace.Name,
				Spec: capsulev1beta2.ResourcePermitSpec{Template: capsulev1beta2.ResourcePermitTemplateReference{
					Kind: capsulev1beta2.ResourcePermitTemplateKind,
					Name: brt.Name,
				}},
			}
			DeferCleanup(func() {
				expireResourcePermitForCleanup(ctx, br)
				EventuallyDeletion(br)
			})
			EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

			cm := resourcePermitManagedConfigMap(br.Namespace)
			Eventually(func(g Gomega) {
				current := &capsulev1beta2.ResourcePermit{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
				expectResourcePermitServiceAccount(
					g,
					current,
					namespace.Name,
					resourcePermitLocalDefaultServiceAccount,
				)
				g.Expect(current.Status.Request.Template).NotTo(BeNil())
				g.Expect(current.Status.Request.Template.Kind).To(Equal(capsulev1beta2.ResourcePermitTemplateKind))
				g.Expect(current.Status.Request.Template.Name).To(Equal(brt.Name))
				g.Expect(current.Status.Request.Template.ResourceVersion).NotTo(BeEmpty())

				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
				g.Expect(cm.Data).To(HaveKeyWithValue("source", "namespaced-template"))
				g.Expect(cm.Annotations).To(HaveKeyWithValue(
					apimeta.ResourcePermitServiceAccountAnnotation,
					serviceAccountUsername(namespace.Name, resourcePermitLocalDefaultServiceAccount),
				))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			expireActiveResourcePermit(ctx, br)
			expectResourcePermitAndConfigMapDeleted(ctx, br, cm)
		})

		It("does not resolve a namespaced template from another namespace", func() {
			otherNamespace := createResourcePermitTestNamespace(ctx)

			br := &capsulev1beta2.ResourcePermit{
				Name: "e2e-resourcepermit-cross-namespace", Namespace: otherNamespace.Name,
				Spec: capsulev1beta2.ResourcePermitSpec{Template: capsulev1beta2.ResourcePermitTemplateReference{
					Kind: capsulev1beta2.ResourcePermitTemplateKind,
					Name: brt.Name,
				}},
			}

			err := k8sClient.Create(ctx, br)
			Expect(apierrors.IsForbidden(err)).To(BeTrue())
			Expect(err).To(MatchError(ContainSubstring("template e2e-resourcepermit-local-template not found")))
		})
	},
)

func resourcePermitServiceAccountReference(
	namespace,
	name string,
) *apimeta.NamespacedRFC1123ObjectReferenceWithNamespace {
	return &apimeta.NamespacedRFC1123ObjectReferenceWithNamespace{
		Name:      apimeta.RFC1123Name(name),
		Namespace: apimeta.RFC1123SubdomainName(namespace),
	}
}

func newImpersonatedResourcePermit(namespace, name, template string) *capsulev1beta2.ResourcePermit {
	return &capsulev1beta2.ResourcePermit{
		Name: name, Namespace: namespace,
		Spec: capsulev1beta2.ResourcePermitSpec{
			Template: globalResourcePermitTemplateReference(template),
		},
	}
}

func resourcePermitManagedConfigMap(namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		Name:      resourcePermitImpersonationTargetName,
		Namespace: namespace,
	}
}

func grantResourcePermitServiceAccount(namespace, name, targetNamespace string, configMapVerbs []string) {
	ensureServiceAccount(namespace, name)
	bindServiceAccountToNamespacedResource(
		namespace,
		name,
		targetNamespace,
		[]string{"configmaps"},
		configMapVerbs,
	)
	bindServiceAccountToClusterResources(
		namespace,
		name,
		name+"-namespaces",
		name+"-namespaces-binding",
		[]rbacv1.PolicyRule{{
			APIGroups: []string{""},
			Resources: []string{"namespaces"},
			Verbs:     []string{"get"},
		}},
	)
}

func expectResourcePermitServiceAccount(
	g Gomega,
	request *capsulev1beta2.ResourcePermit,
	namespace,
	name string,
) {
	g.Expect(request.Status.Request).NotTo(BeNil(), "ResourcePermit status is not initialized: %+v", request.Status)
	g.Expect(request.Status.Request.Impersonation).ToNot(BeNil())
	g.Expect(request.Status.Request.Impersonation.Name.String()).To(Equal(name))
	g.Expect(request.Status.Request.Impersonation.Namespace.String()).To(Equal(namespace))
}

func expectResourcePermitAndConfigMapDeleted(
	ctx context.Context,
	request *capsulev1beta2.ResourcePermit,
	configMap *corev1.ConfigMap,
) {
	Eventually(func(g Gomega) {
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())

		current := &capsulev1beta2.ResourcePermit{}
		err = k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current)
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func patchResourcePermitPhaseAs(
	ctx context.Context,
	actor client.Client,
	request *capsulev1beta2.ResourcePermit,
	phase capsulev1beta2.ResourcePermitPhase,
) {
	Eventually(func() error {
		current := &capsulev1beta2.ResourcePermit{}
		if err := actor.Get(ctx, client.ObjectKeyFromObject(request), current); err != nil {
			return err
		}

		before := current.DeepCopy()
		current.Status.Phase = phase

		return actor.Status().Patch(
			ctx,
			current,
			client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}),
		)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}
