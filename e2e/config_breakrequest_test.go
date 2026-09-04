// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
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
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	breakRequestImpersonationTemplateName  = "e2e-btg-impersonation"
	breakRequestImpersonationTargetName    = "e2e-btg-impersonation-target"
	breakRequestImpersonationContextName   = "e2e-btg-impersonation-context"
	breakRequestServiceAccountNamespace    = "capsule-system"
	breakRequestTemplateServiceAccount     = "e2e-btg-template-runner"
	breakRequestDefaultServiceAccount      = "e2e-btg-default-runner"
	breakRequestLocalDefaultServiceAccount = "e2e-btg-local-default-runner"
	breakRequestReadOnlyServiceAccount     = "e2e-btg-readonly-runner"
	breakRequestRetryRequester             = "e2e-btg-retry-requester"
)

var _ = Describe(
	"BreakRequest impersonation configuration",
	Ordered,
	Serial,
	Label("break-the-glass", "config", "impersonation"),
	func() {
		var (
			ctx context.Context
			brt *capsulev1beta2.GlobalBreakRequestTemplate
		)

		BeforeEach(func() {
			ctx = context.Background()
			brt = &capsulev1beta2.GlobalBreakRequestTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: breakRequestImpersonationTemplateName},
				Spec: capsulev1beta2.GlobalBreakRequestTemplateSpec{
					Approvals: breaktheglass.ApprovalSpec{Auto: true},
					Resources: []apiruntime.ResourceTemplate{{Template: `
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: e2e-btg-impersonation-target
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
				brt.Spec.Impersonation = breakRequestServiceAccountReference(
					breakRequestServiceAccountNamespace,
					breakRequestTemplateServiceAccount,
				)
				brt.Spec.Context = &tpl.TemplateContext{Resources: []*tpl.TemplateResourceReference{{
					ResourceReference: tpl.ResourceReference{
						VersionKind: apiruntime.VersionKind{APIVersion: "v1", Kind: "ConfigMap"},
						Name:        breakRequestImpersonationContextName,
					},
					Index: "settings",
				}}}
				brt.Spec.Resources = []apiruntime.ResourceTemplate{{Template: `
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: e2e-btg-impersonation-target
data:
  loaded: {{ (index $.context.resources.settings 0).data.value }}
`}}

				grantBreakRequestServiceAccount(
					breakRequestServiceAccountNamespace,
					breakRequestTemplateServiceAccount,
					[]string{"get", "list", "watch", "create", "update", "patch", "delete"},
				)

				source := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: breakRequestImpersonationContextName, Namespace: "default"},
					Data:       map[string]string{"value": "loaded-by-template-service-account"},
				}
				EventuallyCreation(func() error { return k8sClient.Create(ctx, source) }).Should(Succeed())
				DeferCleanup(func() { EventuallyDeletion(source) })
			})

			It("uses the identity for context, apply, protected updates, and deletion", func() {
				br := newImpersonatedBreakRequest("e2e-btg-impersonated", brt.Name)
				DeferCleanup(func() {
					expireBreakRequestForCleanup(ctx, br)
					EventuallyDeletion(br)
				})
				EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

				expectedUsername := serviceAccountUsername(
					breakRequestServiceAccountNamespace,
					breakRequestTemplateServiceAccount,
				)
				cm := breakRequestManagedConfigMap(br.Namespace)
				Eventually(func(g Gomega) {
					current := &capsulev1beta2.BreakRequest{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					expectBreakRequestServiceAccount(
						g,
						current,
						breakRequestServiceAccountNamespace,
						breakRequestTemplateServiceAccount,
					)

					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
					g.Expect(cm.Data).To(HaveKeyWithValue("loaded", "loaded-by-template-service-account"))
					g.Expect(cm.Annotations).To(HaveKeyWithValue(
						apimeta.BreakRequestServiceAccountAnnotation,
						expectedUsername,
					))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				templateClient := impersonationClient(
					expectedUsername,
					serviceAccountGroups(breakRequestServiceAccountNamespace),
				)
				cm.Data["updated"] = "by-template-service-account"
				Expect(templateClient.Update(ctx, cm)).To(Succeed())

				By("protecting the template ServiceAccount copied to BreakRequest status")
				executionServiceAccount := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
					Name:      breakRequestTemplateServiceAccount,
					Namespace: breakRequestServiceAccountNamespace,
				}}
				Eventually(func() bool {
					err := k8sClient.Delete(ctx, executionServiceAccount, client.DryRunAll)

					return apierrors.IsForbidden(err)
				}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())

				expireActiveBreakRequest(ctx, br)
				expectBreakRequestAndConfigMapDeleted(ctx, br, cm)

				By("allowing deletion after the referencing BreakRequest has expired")
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

				br := newImpersonatedBreakRequest("e2e-btg-controller-identity", brt.Name)
				DeferCleanup(func() {
					expireBreakRequestForCleanup(ctx, br)
					EventuallyDeletion(br)
				})
				EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

				expectedUsername := serviceAccountUsername(
					ControllerNamespace,
					ControllerServiceAccount,
				)
				cm := breakRequestManagedConfigMap(br.Namespace)
				Eventually(func(g Gomega) {
					current := &capsulev1beta2.BreakRequest{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					expectBreakRequestServiceAccount(
						g,
						current,
						ControllerNamespace,
						ControllerServiceAccount,
					)

					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
					g.Expect(cm.Annotations).To(HaveKeyWithValue(
						apimeta.BreakRequestServiceAccountAnnotation,
						expectedUsername,
					))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				expireActiveBreakRequest(ctx, br)
				expectBreakRequestAndConfigMapDeleted(ctx, br, cm)
			})

			It("uses the global default ServiceAccount from CapsuleConfiguration", func() {
				grantBreakRequestServiceAccount(
					breakRequestServiceAccountNamespace,
					breakRequestDefaultServiceAccount,
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
						apimeta.RFC1123Name(breakRequestDefaultServiceAccount)
					configuration.Spec.Impersonation.GlobalDefaultServiceAccountNamespace =
						apimeta.RFC1123SubdomainName(breakRequestServiceAccountNamespace)
				})

				br := newImpersonatedBreakRequest("e2e-btg-default-impersonation", brt.Name)
				DeferCleanup(func() {
					expireBreakRequestForCleanup(ctx, br)
					EventuallyDeletion(br)
				})
				EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

				expectedUsername := serviceAccountUsername(
					breakRequestServiceAccountNamespace,
					breakRequestDefaultServiceAccount,
				)
				cm := breakRequestManagedConfigMap(br.Namespace)
				Eventually(func(g Gomega) {
					current := &capsulev1beta2.BreakRequest{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					expectBreakRequestServiceAccount(
						g,
						current,
						breakRequestServiceAccountNamespace,
						breakRequestDefaultServiceAccount,
					)
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
					g.Expect(cm.Annotations).To(HaveKeyWithValue(
						apimeta.BreakRequestServiceAccountAnnotation,
						expectedUsername,
					))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				expireActiveBreakRequest(ctx, br)
				expectBreakRequestAndConfigMapDeleted(ctx, br, cm)
			})
		})

		Context("without sufficient target permissions", func() {
			var requesterClient client.Client

			BeforeEach(func() {
				brt.Spec.Impersonation = breakRequestServiceAccountReference(
					breakRequestServiceAccountNamespace,
					breakRequestReadOnlyServiceAccount,
				)
				grantBreakRequestServiceAccount(
					breakRequestServiceAccountNamespace,
					breakRequestReadOnlyServiceAccount,
					[]string{"get", "list", "watch"},
				)
				grantBreakRequestNamespaceAdmin(ctx, "default", breakRequestRetryRequester)
				requesterClient = impersonationClient(
					breakRequestRetryRequester,
					[]string{"system:authenticated"},
				)
			})

			It("fails preflight and lets the requester retry after permissions are fixed", func() {
				br := newImpersonatedBreakRequest("e2e-btg-impersonation-forbidden", brt.Name)
				DeferCleanup(func() {
					expireBreakRequestForCleanup(ctx, br)
					EventuallyDeletion(br)
				})
				EventuallyCreation(func() error { return requesterClient.Create(ctx, br) }).Should(Succeed())

				Eventually(func(g Gomega) {
					current := &capsulev1beta2.BreakRequest{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					expectBreakRequestServiceAccount(
						g,
						current,
						breakRequestServiceAccountNamespace,
						breakRequestReadOnlyServiceAccount,
					)

					ready := k8smeta.FindStatusCondition(current.Status.Conditions, apimeta.ReadyCondition)
					g.Expect(ready).NotTo(BeNil())
					g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
					g.Expect(ready.Reason).To(Equal("ResourceDryRunFailed"))
					g.Expect(ready.Message).To(ContainSubstring("forbidden"))
					g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.RequestPhaseFailed))
					g.Expect(current.Status.Failure).NotTo(BeNil())
					g.Expect(current.Status.Failure.Stage).To(Equal(capsulev1beta2.RequestFailureStagePreflight))
					g.Expect(current.Status.Failure.RetryPhase).To(Equal(capsulev1beta2.RequestPhaseApproved))
					g.Expect(current.Status.ProcessedItems).To(BeEmpty())
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				cm := breakRequestManagedConfigMap(br.Namespace)
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())

				By("granting the missing permissions and retrying as the requester")
				grantBreakRequestServiceAccount(
					breakRequestServiceAccountNamespace,
					breakRequestReadOnlyServiceAccount,
					[]string{"get", "list", "watch", "create", "update", "patch", "delete"},
				)
				patchBreakRequestPhaseAs(
					ctx,
					requesterClient,
					br,
					capsulev1beta2.RequestPhaseRetrying,
				)

				Eventually(func(g Gomega) {
					current := &capsulev1beta2.BreakRequest{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.RequestPhaseActive))
					g.Expect(current.Status.Failure).To(BeNil())
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				By("letting the requester expire the recovered request")
				patchBreakRequestPhaseAs(
					ctx,
					requesterClient,
					br,
					capsulev1beta2.RequestPhaseExpired,
				)
				expectBreakRequestAndConfigMapDeleted(ctx, br, cm)
			})

			It("enters Failed when the ServiceAccount disappears after preflight and recovers on retry", func() {
				grantBreakRequestServiceAccount(
					breakRequestServiceAccountNamespace,
					breakRequestReadOnlyServiceAccount,
					[]string{"get", "list", "watch", "create", "update", "patch", "delete"},
				)

				br := newImpersonatedBreakRequest("e2e-btg-activation-retry", brt.Name)
				startTime := metav1.NewTime(time.Now().Add(20 * time.Second))
				br.Spec.StartTime = &startTime
				DeferCleanup(func() {
					expireBreakRequestForCleanup(ctx, br)
					EventuallyDeletion(br)
				})
				EventuallyCreation(func() error { return requesterClient.Create(ctx, br) }).Should(Succeed())

				Eventually(func(g Gomega) {
					current := &capsulev1beta2.BreakRequest{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.RequestPhaseApproved))
					ready := k8smeta.FindStatusCondition(current.Status.Conditions, apimeta.ReadyCondition)
					g.Expect(ready).NotTo(BeNil())
					g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				By("deleting the resolved ServiceAccount after the successful preflight")
				serviceAccount := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
					Name:      breakRequestReadOnlyServiceAccount,
					Namespace: breakRequestServiceAccountNamespace,
				}}
				EventuallyDeletion(serviceAccount)

				Eventually(func(g Gomega) {
					current := &capsulev1beta2.BreakRequest{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.RequestPhaseFailed))
					g.Expect(current.Status.Failure).NotTo(BeNil())
					g.Expect(current.Status.Failure.Stage).To(Equal(capsulev1beta2.RequestFailureStageActivation))
					g.Expect(current.Status.Failure.RetryPhase).To(Equal(capsulev1beta2.RequestPhaseApproved))
					ready := k8smeta.FindStatusCondition(current.Status.Conditions, apimeta.ReadyCondition)
					g.Expect(ready).NotTo(BeNil())
					g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
					g.Expect(ready.Reason).To(Equal("ImpersonationFailed"))
					g.Expect(ready.Message).To(ContainSubstring("not found"))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				By("recreating the ServiceAccount and retrying the stored approved snapshot")
				grantBreakRequestServiceAccount(
					breakRequestServiceAccountNamespace,
					breakRequestReadOnlyServiceAccount,
					[]string{"get", "list", "watch", "create", "update", "patch", "delete"},
				)
				patchBreakRequestPhaseAs(
					ctx,
					requesterClient,
					br,
					capsulev1beta2.RequestPhaseRetrying,
				)

				cm := breakRequestManagedConfigMap(br.Namespace)
				Eventually(func(g Gomega) {
					current := &capsulev1beta2.BreakRequest{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.RequestPhaseActive))
					g.Expect(current.Status.Failure).To(BeNil())
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				patchBreakRequestPhaseAs(
					ctx,
					requesterClient,
					br,
					capsulev1beta2.RequestPhaseExpired,
				)
				expectBreakRequestAndConfigMapDeleted(ctx, br, cm)
			})
		})
	},
)

var _ = Describe(
	"Namespaced BreakRequestTemplate impersonation configuration",
	Ordered,
	Serial,
	Label("break-the-glass", "config", "impersonation"),
	func() {
		var (
			ctx context.Context
			brt *capsulev1beta2.BreakRequestTemplate
		)

		BeforeEach(func() {
			ctx = context.Background()

			original := &capsulev1beta2.CapsuleConfiguration{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, original)).To(Succeed())
			originalImpersonation := original.Spec.Impersonation
			DeferCleanup(func() {
				ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
					configuration.Spec.Impersonation = originalImpersonation
				})
			})

			grantBreakRequestServiceAccount(
				"default",
				breakRequestLocalDefaultServiceAccount,
				[]string{"get", "list", "watch", "create", "update", "patch", "delete"},
			)
			ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
				configuration.Spec.Impersonation.TenantDefaultServiceAccount =
					apimeta.RFC1123Name(breakRequestLocalDefaultServiceAccount)
			})

			brt = &capsulev1beta2.BreakRequestTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-btg-local-template", Namespace: "default"},
				Spec: capsulev1beta2.BreakRequestTemplateSpec{
					Approvals: breaktheglass.ApprovalSpec{Auto: true},
					Resources: []apiruntime.ResourceTemplate{{Template: `
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: e2e-btg-impersonation-target
data:
  source: namespaced-template
`}},
				},
			}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, brt) }).Should(Succeed())
			DeferCleanup(func() { EventuallyDeletion(brt) })
		})

		It("uses the namespace-local configured default and records template provenance", func() {
			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-btg-local-default", Namespace: "default"},
				Spec: capsulev1beta2.BreakRequestSpec{Template: capsulev1beta2.BreakRequestTemplateReference{
					Kind: capsulev1beta2.BreakRequestTemplateKind,
					Name: brt.Name,
				}},
			}
			DeferCleanup(func() {
				expireBreakRequestForCleanup(ctx, br)
				EventuallyDeletion(br)
			})
			EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

			cm := breakRequestManagedConfigMap(br.Namespace)
			Eventually(func(g Gomega) {
				current := &capsulev1beta2.BreakRequest{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
				expectBreakRequestServiceAccount(
					g,
					current,
					"default",
					breakRequestLocalDefaultServiceAccount,
				)
				g.Expect(current.Status.Request.Template).NotTo(BeNil())
				g.Expect(current.Status.Request.Template.Kind).To(Equal(capsulev1beta2.BreakRequestTemplateKind))
				g.Expect(current.Status.Request.Template.Name).To(Equal(brt.Name))
				g.Expect(current.Status.Request.Template.ResourceVersion).NotTo(BeEmpty())

				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
				g.Expect(cm.Data).To(HaveKeyWithValue("source", "namespaced-template"))
				g.Expect(cm.Annotations).To(HaveKeyWithValue(
					apimeta.BreakRequestServiceAccountAnnotation,
					serviceAccountUsername("default", breakRequestLocalDefaultServiceAccount),
				))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			expireActiveBreakRequest(ctx, br)
			expectBreakRequestAndConfigMapDeleted(ctx, br, cm)
		})

		It("does not resolve a namespaced template from another namespace", func() {
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "e2e-btg-local-other"}}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, namespace) }).Should(Succeed())
			DeferCleanup(func() { EventuallyDeletion(namespace) })

			br := &capsulev1beta2.BreakRequest{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-btg-cross-namespace", Namespace: namespace.Name},
				Spec: capsulev1beta2.BreakRequestSpec{Template: capsulev1beta2.BreakRequestTemplateReference{
					Kind: capsulev1beta2.BreakRequestTemplateKind,
					Name: brt.Name,
				}},
			}

			err := k8sClient.Create(ctx, br)
			Expect(apierrors.IsForbidden(err)).To(BeTrue())
			Expect(err).To(MatchError(ContainSubstring("template e2e-btg-local-template not found")))
		})
	},
)

func breakRequestServiceAccountReference(
	namespace,
	name string,
) *apimeta.NamespacedRFC1123ObjectReferenceWithNamespace {
	return &apimeta.NamespacedRFC1123ObjectReferenceWithNamespace{
		Name:      apimeta.RFC1123Name(name),
		Namespace: apimeta.RFC1123SubdomainName(namespace),
	}
}

func newImpersonatedBreakRequest(name, template string) *capsulev1beta2.BreakRequest {
	return &capsulev1beta2.BreakRequest{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: capsulev1beta2.BreakRequestSpec{
			Template: globalBreakRequestTemplateReference(template),
		},
	}
}

func breakRequestManagedConfigMap(namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      breakRequestImpersonationTargetName,
			Namespace: namespace,
		},
	}
}

func grantBreakRequestServiceAccount(namespace, name string, configMapVerbs []string) {
	ensureServiceAccount(namespace, name)
	bindServiceAccountToNamespacedResource(
		namespace,
		name,
		"default",
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

func expectBreakRequestServiceAccount(
	g Gomega,
	request *capsulev1beta2.BreakRequest,
	namespace,
	name string,
) {
	g.Expect(request.Status.Request.Impersonation).ToNot(BeNil())
	g.Expect(request.Status.Request.Impersonation.Name.String()).To(Equal(name))
	g.Expect(request.Status.Request.Impersonation.Namespace.String()).To(Equal(namespace))
}

func expectBreakRequestAndConfigMapDeleted(
	ctx context.Context,
	request *capsulev1beta2.BreakRequest,
	configMap *corev1.ConfigMap,
) {
	Eventually(func(g Gomega) {
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())

		current := &capsulev1beta2.BreakRequest{}
		err = k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current)
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func patchBreakRequestPhaseAs(
	ctx context.Context,
	actor client.Client,
	request *capsulev1beta2.BreakRequest,
	phase capsulev1beta2.RequestPhase,
) {
	Eventually(func() error {
		current := &capsulev1beta2.BreakRequest{}
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
