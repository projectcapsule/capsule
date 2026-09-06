// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	apimeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/resourcelease"
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
	resourceLeaseImpersonationTemplateName  = "e2e-resourcelease-impersonation"
	resourceLeaseImpersonationTargetName    = "e2e-resourcelease-impersonation-target"
	resourceLeaseImpersonationContextName   = "e2e-resourcelease-impersonation-context"
	resourceLeaseServiceAccountNamespace    = "capsule-system"
	resourceLeaseTemplateServiceAccount     = "e2e-resourcelease-template-runner"
	resourceLeaseDefaultServiceAccount      = "e2e-resourcelease-default-runner"
	resourceLeaseLocalDefaultServiceAccount = "e2e-resourcelease-local-default-runner"
	resourceLeaseReadOnlyServiceAccount     = "e2e-resourcelease-readonly-runner"
	resourceLeaseRetryLeaseer               = "e2e-resourcelease-retry-requester"
)

var _ = Describe(
	"ResourceLease impersonation configuration",
	Ordered,
	Serial,
	Label("resource-lease", "config", "impersonation"),
	func() {
		var (
			ctx context.Context
			brt *capsulev1beta2.GlobalResourceLeaseTemplate
		)

		BeforeEach(func() {
			ctx = context.Background()
			brt = &capsulev1beta2.GlobalResourceLeaseTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: resourceLeaseImpersonationTemplateName},
				Spec: capsulev1beta2.GlobalResourceLeaseTemplateSpec{
					Approvals: resourcelease.ApprovalSpec{Auto: true},
					Resources: []apiruntime.ResourceTemplate{{Template: `
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: e2e-resourcelease-impersonation-target
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
				brt.Spec.Impersonation = resourceLeaseServiceAccountReference(
					resourceLeaseServiceAccountNamespace,
					resourceLeaseTemplateServiceAccount,
				)
				brt.Spec.Context = &tpl.TemplateContext{Resources: []*tpl.TemplateResourceReference{{
					ResourceReference: tpl.ResourceReference{
						VersionKind: apiruntime.VersionKind{APIVersion: "v1", Kind: "ConfigMap"},
						Name:        resourceLeaseImpersonationContextName,
					},
					Index: "settings",
				}}}
				brt.Spec.Resources = []apiruntime.ResourceTemplate{{Template: `
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: e2e-resourcelease-impersonation-target
data:
  loaded: {{ (index $.context.resources.settings 0).data.value }}
`}}

				grantResourceLeaseServiceAccount(
					resourceLeaseServiceAccountNamespace,
					resourceLeaseTemplateServiceAccount,
					[]string{"get", "list", "watch", "create", "update", "patch", "delete"},
				)

				source := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: resourceLeaseImpersonationContextName, Namespace: "default"},
					Data:       map[string]string{"value": "loaded-by-template-service-account"},
				}
				EventuallyCreation(func() error { return k8sClient.Create(ctx, source) }).Should(Succeed())
				DeferCleanup(func() { EventuallyDeletion(source) })
			})

			It("uses the identity for context, apply, protected updates, and deletion", func() {
				br := newImpersonatedResourceLease("e2e-resourcelease-impersonated", brt.Name)
				DeferCleanup(func() {
					expireResourceLeaseForCleanup(ctx, br)
					EventuallyDeletion(br)
				})
				EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

				expectedUsername := serviceAccountUsername(
					resourceLeaseServiceAccountNamespace,
					resourceLeaseTemplateServiceAccount,
				)
				cm := resourceLeaseManagedConfigMap(br.Namespace)
				Eventually(func(g Gomega) {
					current := &capsulev1beta2.ResourceLease{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					expectResourceLeaseServiceAccount(
						g,
						current,
						resourceLeaseServiceAccountNamespace,
						resourceLeaseTemplateServiceAccount,
					)

					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
					g.Expect(cm.Data).To(HaveKeyWithValue("loaded", "loaded-by-template-service-account"))
					g.Expect(cm.Annotations).To(HaveKeyWithValue(
						apimeta.ResourceLeaseServiceAccountAnnotation,
						expectedUsername,
					))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				templateClient := impersonationClient(
					expectedUsername,
					serviceAccountGroups(resourceLeaseServiceAccountNamespace),
				)
				cm.Data["updated"] = "by-template-service-account"
				Expect(templateClient.Update(ctx, cm)).To(Succeed())

				By("protecting the template ServiceAccount copied to ResourceLease status")
				executionServiceAccount := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
					Name:      resourceLeaseTemplateServiceAccount,
					Namespace: resourceLeaseServiceAccountNamespace,
				}}
				Eventually(func() bool {
					err := k8sClient.Delete(ctx, executionServiceAccount, client.DryRunAll)

					return apierrors.IsForbidden(err)
				}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())

				expireActiveResourceLease(ctx, br)
				expectResourceLeaseAndConfigMapDeleted(ctx, br, cm)

				By("allowing deletion after the referencing ResourceLease has expired")
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

				br := newImpersonatedResourceLease("e2e-resourcelease-controller-identity", brt.Name)
				DeferCleanup(func() {
					expireResourceLeaseForCleanup(ctx, br)
					EventuallyDeletion(br)
				})
				EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

				expectedUsername := serviceAccountUsername(
					ControllerNamespace,
					ControllerServiceAccount,
				)
				cm := resourceLeaseManagedConfigMap(br.Namespace)
				Eventually(func(g Gomega) {
					current := &capsulev1beta2.ResourceLease{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					expectResourceLeaseServiceAccount(
						g,
						current,
						ControllerNamespace,
						ControllerServiceAccount,
					)

					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
					g.Expect(cm.Annotations).To(HaveKeyWithValue(
						apimeta.ResourceLeaseServiceAccountAnnotation,
						expectedUsername,
					))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				expireActiveResourceLease(ctx, br)
				expectResourceLeaseAndConfigMapDeleted(ctx, br, cm)
			})

			It("uses the global default ServiceAccount from CapsuleConfiguration", func() {
				grantResourceLeaseServiceAccount(
					resourceLeaseServiceAccountNamespace,
					resourceLeaseDefaultServiceAccount,
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
						apimeta.RFC1123Name(resourceLeaseDefaultServiceAccount)
					configuration.Spec.Impersonation.GlobalDefaultServiceAccountNamespace =
						apimeta.RFC1123SubdomainName(resourceLeaseServiceAccountNamespace)
				})

				br := newImpersonatedResourceLease("e2e-resourcelease-default-impersonation", brt.Name)
				DeferCleanup(func() {
					expireResourceLeaseForCleanup(ctx, br)
					EventuallyDeletion(br)
				})
				EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

				expectedUsername := serviceAccountUsername(
					resourceLeaseServiceAccountNamespace,
					resourceLeaseDefaultServiceAccount,
				)
				cm := resourceLeaseManagedConfigMap(br.Namespace)
				Eventually(func(g Gomega) {
					current := &capsulev1beta2.ResourceLease{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					expectResourceLeaseServiceAccount(
						g,
						current,
						resourceLeaseServiceAccountNamespace,
						resourceLeaseDefaultServiceAccount,
					)
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
					g.Expect(cm.Annotations).To(HaveKeyWithValue(
						apimeta.ResourceLeaseServiceAccountAnnotation,
						expectedUsername,
					))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				expireActiveResourceLease(ctx, br)
				expectResourceLeaseAndConfigMapDeleted(ctx, br, cm)
			})
		})

		Context("without sufficient target permissions", func() {
			var requesterClient client.Client

			BeforeEach(func() {
				brt.Spec.Impersonation = resourceLeaseServiceAccountReference(
					resourceLeaseServiceAccountNamespace,
					resourceLeaseReadOnlyServiceAccount,
				)
				grantResourceLeaseServiceAccount(
					resourceLeaseServiceAccountNamespace,
					resourceLeaseReadOnlyServiceAccount,
					[]string{"get", "list", "watch"},
				)
				grantResourceLeaseNamespaceAdmin(ctx, "default", resourceLeaseRetryLeaseer)
				requesterClient = impersonationClient(
					resourceLeaseRetryLeaseer,
					[]string{"system:authenticated"},
				)
			})

			It("fails preflight and lets the requester retry after permissions are fixed", func() {
				br := newImpersonatedResourceLease("e2e-resourcelease-impersonation-forbidden", brt.Name)
				DeferCleanup(func() {
					expireResourceLeaseForCleanup(ctx, br)
					EventuallyDeletion(br)
				})
				EventuallyCreation(func() error { return requesterClient.Create(ctx, br) }).Should(Succeed())

				Eventually(func(g Gomega) {
					current := &capsulev1beta2.ResourceLease{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					expectResourceLeaseServiceAccount(
						g,
						current,
						resourceLeaseServiceAccountNamespace,
						resourceLeaseReadOnlyServiceAccount,
					)

					ready := k8smeta.FindStatusCondition(current.Status.Conditions, apimeta.ReadyCondition)
					g.Expect(ready).NotTo(BeNil())
					g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
					g.Expect(ready.Reason).To(Equal("ResourceDryRunFailed"))
					g.Expect(ready.Message).To(ContainSubstring("forbidden"))
					g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.ResourceLeasePhaseFailed))
					g.Expect(current.Status.Failure).NotTo(BeNil())
					g.Expect(current.Status.Failure.Stage).To(Equal(capsulev1beta2.ResourceLeaseFailureStagePreflight))
					g.Expect(current.Status.Failure.RetryPhase).To(Equal(capsulev1beta2.ResourceLeasePhaseApproved))
					g.Expect(current.Status.ProcessedItems).To(BeEmpty())
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				cm := resourceLeaseManagedConfigMap(br.Namespace)
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())

				By("granting the missing permissions and retrying as the requester")
				grantResourceLeaseServiceAccount(
					resourceLeaseServiceAccountNamespace,
					resourceLeaseReadOnlyServiceAccount,
					[]string{"get", "list", "watch", "create", "update", "patch", "delete"},
				)
				patchResourceLeasePhaseAs(
					ctx,
					requesterClient,
					br,
					capsulev1beta2.ResourceLeasePhaseRetrying,
				)

				Eventually(func(g Gomega) {
					current := &capsulev1beta2.ResourceLease{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.ResourceLeasePhaseActive))
					g.Expect(current.Status.Failure).To(BeNil())
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				By("letting the requester expire the recovered request")
				patchResourceLeasePhaseAs(
					ctx,
					requesterClient,
					br,
					capsulev1beta2.ResourceLeasePhaseExpired,
				)
				expectResourceLeaseAndConfigMapDeleted(ctx, br, cm)
			})

			It("enters Failed when the ServiceAccount disappears after preflight and recovers on retry", func() {
				grantResourceLeaseServiceAccount(
					resourceLeaseServiceAccountNamespace,
					resourceLeaseReadOnlyServiceAccount,
					[]string{"get", "list", "watch", "create", "update", "patch", "delete"},
				)

				br := newImpersonatedResourceLease("e2e-resourcelease-activation-retry", brt.Name)
				startTime := metav1.NewTime(time.Now().Add(20 * time.Second))
				br.Spec.StartTime = &startTime
				DeferCleanup(func() {
					expireResourceLeaseForCleanup(ctx, br)
					EventuallyDeletion(br)
				})
				EventuallyCreation(func() error { return requesterClient.Create(ctx, br) }).Should(Succeed())

				Eventually(func(g Gomega) {
					current := &capsulev1beta2.ResourceLease{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.ResourceLeasePhaseApproved))
					ready := k8smeta.FindStatusCondition(current.Status.Conditions, apimeta.ReadyCondition)
					g.Expect(ready).NotTo(BeNil())
					g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				By("deleting the resolved ServiceAccount after the successful preflight")
				serviceAccount := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
					Name:      resourceLeaseReadOnlyServiceAccount,
					Namespace: resourceLeaseServiceAccountNamespace,
				}}
				EventuallyDeletion(serviceAccount)

				Eventually(func(g Gomega) {
					current := &capsulev1beta2.ResourceLease{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.ResourceLeasePhaseFailed))
					g.Expect(current.Status.Failure).NotTo(BeNil())
					g.Expect(current.Status.Failure.Stage).To(Equal(capsulev1beta2.ResourceLeaseFailureStageActivation))
					g.Expect(current.Status.Failure.RetryPhase).To(Equal(capsulev1beta2.ResourceLeasePhaseApproved))
					ready := k8smeta.FindStatusCondition(current.Status.Conditions, apimeta.ReadyCondition)
					g.Expect(ready).NotTo(BeNil())
					g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
					g.Expect(ready.Reason).To(Equal("ImpersonationFailed"))
					g.Expect(ready.Message).To(ContainSubstring("not found"))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				By("recreating the ServiceAccount and retrying the stored approved snapshot")
				grantResourceLeaseServiceAccount(
					resourceLeaseServiceAccountNamespace,
					resourceLeaseReadOnlyServiceAccount,
					[]string{"get", "list", "watch", "create", "update", "patch", "delete"},
				)
				patchResourceLeasePhaseAs(
					ctx,
					requesterClient,
					br,
					capsulev1beta2.ResourceLeasePhaseRetrying,
				)

				cm := resourceLeaseManagedConfigMap(br.Namespace)
				Eventually(func(g Gomega) {
					current := &capsulev1beta2.ResourceLease{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
					g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.ResourceLeasePhaseActive))
					g.Expect(current.Status.Failure).To(BeNil())
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

				patchResourceLeasePhaseAs(
					ctx,
					requesterClient,
					br,
					capsulev1beta2.ResourceLeasePhaseExpired,
				)
				expectResourceLeaseAndConfigMapDeleted(ctx, br, cm)
			})
		})
	},
)

var _ = Describe(
	"Namespaced ResourceLeaseTemplate impersonation configuration",
	Ordered,
	Serial,
	Label("resource-lease", "config", "impersonation"),
	func() {
		var (
			ctx context.Context
			brt *capsulev1beta2.ResourceLeaseTemplate
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

			grantResourceLeaseServiceAccount(
				"default",
				resourceLeaseLocalDefaultServiceAccount,
				[]string{"get", "list", "watch", "create", "update", "patch", "delete"},
			)
			ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
				configuration.Spec.Impersonation.TenantDefaultServiceAccount =
					apimeta.RFC1123Name(resourceLeaseLocalDefaultServiceAccount)
			})

			brt = &capsulev1beta2.ResourceLeaseTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-resourcelease-local-template", Namespace: "default"},
				Spec: capsulev1beta2.ResourceLeaseTemplateSpec{
					Approvals: resourcelease.ApprovalSpec{Auto: true},
					Resources: []apiruntime.ResourceTemplate{{Template: `
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: e2e-resourcelease-impersonation-target
data:
  source: namespaced-template
`}},
				},
			}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, brt) }).Should(Succeed())
			DeferCleanup(func() { EventuallyDeletion(brt) })
		})

		It("uses the namespace-local configured default and records template provenance", func() {
			br := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-resourcelease-local-default", Namespace: "default"},
				Spec: capsulev1beta2.ResourceLeaseSpec{Template: capsulev1beta2.ResourceLeaseTemplateReference{
					Kind: capsulev1beta2.ResourceLeaseTemplateKind,
					Name: brt.Name,
				}},
			}
			DeferCleanup(func() {
				expireResourceLeaseForCleanup(ctx, br)
				EventuallyDeletion(br)
			})
			EventuallyCreation(func() error { return k8sClient.Create(ctx, br) }).Should(Succeed())

			cm := resourceLeaseManagedConfigMap(br.Namespace)
			Eventually(func(g Gomega) {
				current := &capsulev1beta2.ResourceLease{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(br), current)).To(Succeed())
				expectResourceLeaseServiceAccount(
					g,
					current,
					"default",
					resourceLeaseLocalDefaultServiceAccount,
				)
				g.Expect(current.Status.Request.Template).NotTo(BeNil())
				g.Expect(current.Status.Request.Template.Kind).To(Equal(capsulev1beta2.ResourceLeaseTemplateKind))
				g.Expect(current.Status.Request.Template.Name).To(Equal(brt.Name))
				g.Expect(current.Status.Request.Template.ResourceVersion).NotTo(BeEmpty())

				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
				g.Expect(cm.Data).To(HaveKeyWithValue("source", "namespaced-template"))
				g.Expect(cm.Annotations).To(HaveKeyWithValue(
					apimeta.ResourceLeaseServiceAccountAnnotation,
					serviceAccountUsername("default", resourceLeaseLocalDefaultServiceAccount),
				))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			expireActiveResourceLease(ctx, br)
			expectResourceLeaseAndConfigMapDeleted(ctx, br, cm)
		})

		It("does not resolve a namespaced template from another namespace", func() {
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "e2e-resourcelease-local-other"}}
			EventuallyCreation(func() error { return k8sClient.Create(ctx, namespace) }).Should(Succeed())
			DeferCleanup(func() { EventuallyDeletion(namespace) })

			br := &capsulev1beta2.ResourceLease{
				ObjectMeta: metav1.ObjectMeta{Name: "e2e-resourcelease-cross-namespace", Namespace: namespace.Name},
				Spec: capsulev1beta2.ResourceLeaseSpec{Template: capsulev1beta2.ResourceLeaseTemplateReference{
					Kind: capsulev1beta2.ResourceLeaseTemplateKind,
					Name: brt.Name,
				}},
			}

			err := k8sClient.Create(ctx, br)
			Expect(apierrors.IsForbidden(err)).To(BeTrue())
			Expect(err).To(MatchError(ContainSubstring("template e2e-resourcelease-local-template not found")))
		})
	},
)

func resourceLeaseServiceAccountReference(
	namespace,
	name string,
) *apimeta.NamespacedRFC1123ObjectReferenceWithNamespace {
	return &apimeta.NamespacedRFC1123ObjectReferenceWithNamespace{
		Name:      apimeta.RFC1123Name(name),
		Namespace: apimeta.RFC1123SubdomainName(namespace),
	}
}

func newImpersonatedResourceLease(name, template string) *capsulev1beta2.ResourceLease {
	return &capsulev1beta2.ResourceLease{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: capsulev1beta2.ResourceLeaseSpec{
			Template: globalResourceLeaseTemplateReference(template),
		},
	}
}

func resourceLeaseManagedConfigMap(namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceLeaseImpersonationTargetName,
			Namespace: namespace,
		},
	}
}

func grantResourceLeaseServiceAccount(namespace, name string, configMapVerbs []string) {
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

func expectResourceLeaseServiceAccount(
	g Gomega,
	request *capsulev1beta2.ResourceLease,
	namespace,
	name string,
) {
	g.Expect(request.Status.Request.Impersonation).ToNot(BeNil())
	g.Expect(request.Status.Request.Impersonation.Name.String()).To(Equal(name))
	g.Expect(request.Status.Request.Impersonation.Namespace.String()).To(Equal(namespace))
}

func expectResourceLeaseAndConfigMapDeleted(
	ctx context.Context,
	request *capsulev1beta2.ResourceLease,
	configMap *corev1.ConfigMap,
) {
	Eventually(func(g Gomega) {
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())

		current := &capsulev1beta2.ResourceLease{}
		err = k8sClient.Get(ctx, client.ObjectKeyFromObject(request), current)
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func patchResourceLeasePhaseAs(
	ctx context.Context,
	actor client.Client,
	request *capsulev1beta2.ResourceLease,
	phase capsulev1beta2.ResourceLeasePhase,
) {
	Eventually(func() error {
		current := &capsulev1beta2.ResourceLease{}
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
