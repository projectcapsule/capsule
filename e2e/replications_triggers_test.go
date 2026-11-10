// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	registrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
	"github.com/projectcapsule/capsule/pkg/template"
)

var _ = Describe("Replication admission triggers", Label("replications", "replication-triggers"), func() {
	for _, global := range []bool{false, true} {
		It(fmt.Sprintf("matches scoped audiences and selectors and coalesces %s notifications", map[bool]string{true: "global", false: "local"}[global]), func() {
			ctx := context.Background()
			prefix := "trigger-" + rand.String(6)
			owners := []rbac.UserSpec{{Kind: rbac.UserOwner, Name: prefix + "-alice"}, {Kind: rbac.UserOwner, Name: prefix + "-bob"}}
			tenants := make([]*capsulev1beta2.Tenant, 2)
			for i := range tenants {
				tenants[i] = &capsulev1beta2.Tenant{Name: fmt.Sprintf("%s-%d", prefix, i), Labels: map[string]string{"env": "e2e", "trigger-run": fmt.Sprintf("%s-%d", prefix, i)}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{UserSpec: owners[i]}}}}
				Expect(k8sClient.Create(ctx, tenants[i])).To(Succeed())
				DeferCleanup(DeleteInBackground, tenants[i])
				TenantReadyTrue(tenants[i])
			}
			source := NewNamespace(prefix+"-source", map[string]string{meta.TenantLabel: tenants[0].Name, "trigger-source": prefix})
			peer := NewNamespace(prefix+"-peer", map[string]string{meta.TenantLabel: tenants[0].Name})
			foreign := NewNamespace(prefix+"-foreign", map[string]string{meta.TenantLabel: tenants[1].Name})
			for i, ns := range []*corev1.Namespace{source, peer, foreign} {
				owner := owners[0]
				tenant := tenants[0]
				size := uint(i + 1)
				if i == 2 {
					owner = owners[1]
					tenant = tenants[1]
					size = 1
				}
				NamespaceCreation(ns, owner, defaultTimeoutInterval).Should(Succeed())
				DeferCleanup(DeleteNamespaceInBackground, ctx, ns.Name)
				TenantNamespaceReady(tenant, ns, size)
			}
			ensureServiceAccount(source.Name, "default")
			bindServiceAccountToTenantResourceManager(source.Name, "default", source.Name)
			selector := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "trigger-run", Operator: metav1.LabelSelectorOpIn, Values: []string{prefix}}}}
			trigger := capsulev1beta2.TenantResourceTrigger{Name: "source-changes", Operations: []registrationv1.OperationType{registrationv1.Create, registrationv1.Update, registrationv1.Delete}, APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"secrets"}, ObjectSelector: selector, Audience: []rules.Audience{{Kind: rules.AudienceKindCustom, Name: string(rules.CustomAudienceTenantOwner)}}, MatchConditions: []registrationv1.MatchCondition{{Name: "content-changed", Expression: "request.operation != 'UPDATE' || object.data != oldObject.data"}}}
			outputTrigger := *trigger.DeepCopy()
			outputTrigger.Name = "output-feedback"
			outputTrigger.Resources = []string{"configmaps"}
			outputTrigger.MatchConditions = nil
			outputTrigger.Audience = nil
			if global {
				trigger.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"trigger-source": prefix}}
				outputTrigger.NamespaceSelector = trigger.NamespaceSelector.DeepCopy()
			}
			common := capsulev1beta2.TenantResourceCommonSpec{TriggerMinInterval: metav1.Duration{Duration: 24 * time.Hour}, ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Triggers: []capsulev1beta2.TenantResourceTrigger{trigger, outputTrigger}, Resources: []capsulev1beta2.ResourceSpec{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"trigger-source": prefix}}, Context: &template.TemplateContext{Resources: []*template.TemplateResourceReference{{Index: "sources", APIVersion: "v1", Kind: "Secret", Namespace: source.Name, Selector: selector}}}, Generators: []capsulev1beta2.TemplateItemSpec{{Template: fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: trigger-output\n  labels:\n    trigger-run: %s\ndata:\n  count: '{{ if $.sources }}{{ len $.sources }}{{ else }}0{{ end }}'\n  values: '{{ range $.sources }}{{ index .data \"value\" }}{{ end }}'\n", prefix)}}}}}
			var resource client.Object
			if global {
				resource = &capsulev1beta2.GlobalTenantResource{Name: prefix, Labels: map[string]string{"env": "e2e", "trigger-test": prefix}, Spec: capsulev1beta2.GlobalTenantResourceSpec{TenantResourceCommonSpec: common, Scope: api.ResourceScopeNamespace, TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"trigger-run": prefix + "-0"}}}}
			} else {
				resource = &capsulev1beta2.TenantResource{Name: prefix, Namespace: source.Name, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: common}}
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			DeferCleanup(func() {
				if global {
					cleanupGlobalTenantResources(ctx, client.MatchingLabels{"trigger-test": prefix})
				} else {
					cleanupTenantResourcesWithDefaultServiceAccount(ctx, source.Name)
				}
			})
			// The parent must be pruned before fixture namespaces disappear.
			DeferCleanup(func() { ignoreNotFound(k8sClient.Delete(ctx, resource)) })
			status := func() capsulev1beta2.TenantResourceCommonStatus {
				var s capsulev1beta2.TenantResourceCommonStatus
				if global {
					obj := &capsulev1beta2.GlobalTenantResource{}
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resource), obj)).To(Succeed())
					s = obj.Status.TenantResourceCommonStatus
				} else {
					obj := &capsulev1beta2.TenantResource{}
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resource), obj)).To(Succeed())
					s = obj.Status.TenantResourceCommonStatus
				}
				return s
			}
			updateTriggers := func(edit func(*capsulev1beta2.TenantResourceCommonSpec)) {
				Eventually(func() error {
					var obj client.Object
					var spec *capsulev1beta2.TenantResourceCommonSpec
					if global {
						r := &capsulev1beta2.GlobalTenantResource{}
						obj = r
						spec = &r.Spec.TenantResourceCommonSpec
					} else {
						r := &capsulev1beta2.TenantResource{}
						obj = r
						spec = &r.Spec.TenantResourceCommonSpec
					}
					if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(resource), obj); err != nil {
						return err
					}
					edit(spec)
					return k8sClient.Update(ctx, obj)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				Eventually(func(g Gomega) {
					var obj client.Object
					if global {
						obj = &capsulev1beta2.GlobalTenantResource{}
					} else {
						obj = &capsulev1beta2.TenantResource{}
					}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resource), obj)).To(Succeed())
					g.Expect(status().ObservedGeneration).To(Equal(obj.GetGeneration()))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
			Eventually(func(g Gomega) {
				var obj client.Object
				if global {
					obj = &capsulev1beta2.GlobalTenantResource{}
				} else {
					obj = &capsulev1beta2.TenantResource{}
				}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resource), obj)).To(Succeed())
				s := status()
				g.Expect(s.ObservedGeneration).To(Equal(obj.GetGeneration()))
				ready := s.Conditions.GetConditionByType(meta.ReadyCondition)
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionTrue), fmt.Sprintf("%s; processed items: %+v", ready.Message, s.ProcessedItems))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			waitPredicate := func(t capsulev1beta2.TenantResourceTrigger) {
				id := replication.MatchID(&t)
				Eventually(func(g Gomega) {
					list := &registrationv1.ValidatingWebhookConfigurationList{}
					g.Expect(k8sClient.List(ctx, list)).To(Succeed())
					found := false
					for _, cfg := range list.Items {
						for _, hook := range cfg.Webhooks {
							if hook.ClientConfig.Service != nil && hook.ClientConfig.Service.Path != nil && *hook.ClientConfig.Service.Path == replication.TriggerPath+id {
								found = true
							}
						}
					}
					g.Expect(found).To(BeTrue())
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
			waitPredicate(trigger)
			owner := ownerClient(owners[0])
			otherOwner := ownerClient(owners[1])
			createSecret := func(namespace, name string, labels map[string]string, dry bool) {
				_, err := owner.CoreV1().Secrets(namespace).Create(ctx, &corev1.Secret{Name: name, Labels: labels, Data: map[string][]byte{"value": []byte("initial")}}, metav1.CreateOptions{DryRun: func() []string {
					if dry {
						return []string{metav1.DryRunAll}
					}
					return nil
				}()})
				Expect(err).ToNot(HaveOccurred())
			}
			By("excluding nonmatching objects, other namespaces, dry runs, and non-audience actors")
			createSecret(source.Name, "unselected", nil, false)
			createSecret(source.Name, "source-dry", map[string]string{"trigger-run": prefix}, true)
			createSecret(peer.Name, "source-peer", map[string]string{"trigger-run": prefix}, false)
			_, err := otherOwner.CoreV1().Secrets(foreign.Name).Create(ctx, &corev1.Secret{Name: "source-other", Labels: map[string]string{"trigger-run": prefix}, Data: map[string][]byte{"value": []byte("other")}}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			// Administrator can write the source but is outside TenantOwner audience.
			Expect(k8sClient.Create(ctx, &corev1.Secret{Name: "source-admin", Namespace: source.Name, Labels: map[string]string{"trigger-run": prefix}, Data: map[string][]byte{"value": []byte("admin")}})).To(Succeed())
			Consistently(func() *capsulev1beta2.TenantResourceTriggerStatus { return status().Trigger }, 2*time.Second, 200*time.Millisecond).Should(BeNil())
			Expect(k8sClient.Delete(ctx, &corev1.Secret{Name: "source-admin", Namespace: source.Name})).To(Succeed())
			By("reconciling matching creates and preserving the final update in a burst")
			createSecret(source.Name, "source-positive", map[string]string{"trigger-run": prefix}, false)
			Eventually(func() bool { return status().Trigger != nil }, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
			update := func(value string) {
				Eventually(func() error {
					secret, err := owner.CoreV1().Secrets(source.Name).Get(ctx, "source-positive", metav1.GetOptions{})
					if err != nil {
						return err
					}
					secret.Data["value"] = []byte(value)
					_, err = owner.CoreV1().Secrets(source.Name).Update(ctx, secret, metav1.UpdateOptions{})
					return err
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
			first := status().Trigger.LastTriggeredAt
			By("shortening an existing day-long notification cooldown")
			updateTriggers(func(spec *capsulev1beta2.TenantResourceCommonSpec) {
				spec.TriggerMinInterval.Duration = 4 * time.Second
			})
			for _, value := range []string{"one", "two", "three", "final"} {
				update(value)
			}
			Eventually(func(g Gomega) {
				cm := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "trigger-output", Namespace: source.Name}, cm)).To(Succeed())
				g.Expect(cm.Data).To(Equal(map[string]string{"count": "1", "values": base64.StdEncoding.EncodeToString([]byte("final"))}))
				s := status()
				g.Expect(s.Trigger.LastTriggeredAt.Sub(first.Time)).To(BeNumerically(">=", 4*time.Second-time.Millisecond))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			for _, ns := range []*corev1.Namespace{peer, foreign} {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "trigger-output", Namespace: ns.Name}, &corev1.ConfigMap{})
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}
			By("not reacting to metadata-only updates or managed outputs")
			baseline := status().Trigger.ObservedRequest
			Eventually(func() error {
				secret, err := owner.CoreV1().Secrets(source.Name).Get(ctx, "source-positive", metav1.GetOptions{})
				if err != nil {
					return err
				}
				secret.Annotations = map[string]string{"test": "metadata-only"}
				_, err = owner.CoreV1().Secrets(source.Name).Update(ctx, secret, metav1.UpdateOptions{})
				return err
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Consistently(func() string { return status().Trigger.ObservedRequest }, 5*time.Second, 200*time.Millisecond).Should(Equal(baseline))
			By("reconciling deletion from the old object")
			Expect(owner.CoreV1().Secrets(source.Name).Delete(ctx, "source-positive", metav1.DeleteOptions{})).To(Succeed())
			Eventually(func(g Gomega) {
				cm := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "trigger-output", Namespace: source.Name}, cm)).To(Succeed())
				g.Expect(cm.Data["count"]).To(Equal("0"))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			By("recovering a dependency while a day-long trigger token is pending")
			dependencyCommon := capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, DependsOn: []meta.LocalRFC1123ObjectReference{{Name: meta.RFC1123Name(prefix + "-missing")}}, Resources: []capsulev1beta2.ResourceSpec{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/trigger-dependency": prefix}}, RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"trigger-dependency-output"}}`)}}}}}
			var dependency client.Object
			if global {
				dependency = &capsulev1beta2.GlobalTenantResource{Name: prefix + "-dependency", Labels: map[string]string{"env": "e2e", "trigger-test": prefix}, Spec: capsulev1beta2.GlobalTenantResourceSpec{TenantResourceCommonSpec: dependencyCommon, Scope: api.ResourceScopeNamespace, TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"trigger-run": prefix + "-0"}}}}
			} else {
				dependency = &capsulev1beta2.TenantResource{Name: prefix + "-dependency", Namespace: source.Name, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: dependencyCommon}}
			}
			Expect(k8sClient.Create(ctx, dependency)).To(Succeed())
			updateTriggers(func(spec *capsulev1beta2.TenantResourceCommonSpec) {
				spec.TriggerMinInterval.Duration = 24 * time.Hour
				spec.DependsOn = []meta.LocalRFC1123ObjectReference{{Name: meta.RFC1123Name(dependency.GetName())}}
			})
			Eventually(func() bool {
				s := status()
				condition := s.Conditions.GetConditionByType(meta.ReadyCondition)
				return condition != nil && condition.Status == metav1.ConditionFalse
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
			stagePending := func(token string) {
				Eventually(func() error {
					var obj client.Object
					if global {
						obj = &capsulev1beta2.GlobalTenantResource{}
					} else {
						obj = &capsulev1beta2.TenantResource{}
					}
					if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(resource), obj); err != nil {
						return err
					}
					annotations := obj.GetAnnotations()
					if annotations == nil {
						annotations = map[string]string{}
					}
					annotations[replication.TriggerAnnotation] = token
					obj.SetAnnotations(annotations)
					return k8sClient.Update(ctx, obj)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
			pending := "dependency-pending-" + prefix
			stagePending(pending)
			Expect(status().Trigger.ObservedRequest).NotTo(Equal(pending))
			Eventually(func() error {
				var obj client.Object
				var spec *capsulev1beta2.TenantResourceCommonSpec
				if global {
					r := &capsulev1beta2.GlobalTenantResource{}
					obj = r
					spec = &r.Spec.TenantResourceCommonSpec
				} else {
					r := &capsulev1beta2.TenantResource{}
					obj = r
					spec = &r.Spec.TenantResourceCommonSpec
				}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dependency), obj); err != nil {
					return err
				}
				spec.DependsOn = nil
				return k8sClient.Update(ctx, obj)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(func(g Gomega) {
				var obj client.Object
				var childStatus *capsulev1beta2.TenantResourceCommonStatus
				if global {
					r := &capsulev1beta2.GlobalTenantResource{}
					obj = r
					childStatus = &r.Status.TenantResourceCommonStatus
				} else {
					r := &capsulev1beta2.TenantResource{}
					obj = r
					childStatus = &r.Status.TenantResourceCommonStatus
				}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dependency), obj)).To(Succeed())
				g.Expect(childStatus.ObservedGeneration).To(Equal(obj.GetGeneration()))
				g.Expect(meta.IsStatusConditionTrue(childStatus.Conditions, meta.ReadyCondition)).To(BeTrue(), fmt.Sprintf("dependency status: %+v", childStatus))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(func(g Gomega) {
				s := status()
				g.Expect(s.Trigger.ObservedRequest).To(Equal(pending))
				g.Expect(meta.IsStatusConditionTrue(s.Conditions, meta.ReadyCondition)).To(BeTrue())
			}, 15*time.Second, defaultPollInterval).Should(Succeed())
			updateTriggers(func(spec *capsulev1beta2.TenantResourceCommonSpec) { spec.DependsOn = nil })
			By("reconciling namespace removal while a trigger cooldown is pending")
			pending = "namespace-pending-" + prefix
			stagePending(pending)
			Expect(owner.CoreV1().Namespaces().Delete(ctx, peer.Name, metav1.DeleteOptions{})).To(Succeed())
			DeleteNamespaceInBackground(ctx, peer.Name)
			TenantNamespaceReady(tenants[0], source, 1)
			Eventually(func(g Gomega) {
				s := status()
				g.Expect(s.Trigger.ObservedRequest).To(Equal(pending))
				g.Expect(meta.IsStatusConditionTrue(s.Conditions, meta.ReadyCondition)).To(BeTrue())
				cm := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "trigger-output", Namespace: source.Name}, cm)).To(Succeed())
				g.Expect(cm.Data["count"]).To(Equal("0"))
			}, 15*time.Second, defaultPollInterval).Should(Succeed())
			By("allowing periodic recovery to handle pending work during a longer cooldown")
			updateTriggers(func(spec *capsulev1beta2.TenantResourceCommonSpec) {
				spec.TriggerMinInterval.Duration = 30 * time.Second
				spec.ResyncPeriod.Duration = 2 * time.Second
			})
			periodicToken := "periodic-pending-" + prefix
			periodicBaseline := status().Trigger.ObservedRequest
			Eventually(func() error {
				var obj client.Object
				if global {
					obj = &capsulev1beta2.GlobalTenantResource{}
				} else {
					obj = &capsulev1beta2.TenantResource{}
				}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(resource), obj); err != nil {
					return err
				}
				annotations := obj.GetAnnotations()
				if annotations == nil {
					annotations = map[string]string{}
				}
				annotations[replication.TriggerAnnotation] = periodicToken
				obj.SetAnnotations(annotations)
				return k8sClient.Update(ctx, obj)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(func(g Gomega) {
				var obj client.Object
				if global {
					obj = &capsulev1beta2.GlobalTenantResource{}
				} else {
					obj = &capsulev1beta2.TenantResource{}
				}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resource), obj)).To(Succeed())
				s := status()
				g.Expect(s.Trigger.ObservedRequest).NotTo(Equal(periodicBaseline), fmt.Sprintf("pending=%s status=%+v", obj.GetAnnotations()[replication.TriggerAnnotation], s))
				g.Expect(s.Trigger.ObservedRequest).To(Equal(obj.GetAnnotations()[replication.TriggerAnnotation]))
			}, 8*time.Second, 200*time.Millisecond).Should(Succeed())
			By("using the current audience after a policy update")
			updateTriggers(func(spec *capsulev1beta2.TenantResourceCommonSpec) {
				spec.Triggers[0].Audience = []rules.Audience{{Kind: rules.AudienceKindGroup, Name: prefix + "-selected"}}
			})
			baseline = status().Trigger.ObservedRequest
			createSecret(source.Name, "source-owner-after-policy", map[string]string{"trigger-run": prefix}, false)
			Consistently(func() string { return status().Trigger.ObservedRequest }, 2*time.Second, 200*time.Millisecond).Should(Equal(baseline))
			selectedOwner := impersonationClientSet(owners[0].Name, []string{"projectcapsule.dev", owners[0].Name, prefix + "-selected"})
			_, err = selectedOwner.CoreV1().Secrets(source.Name).Create(ctx, &corev1.Secret{Name: "source-selected-after-policy", Labels: map[string]string{"trigger-run": prefix}, Data: map[string][]byte{"value": []byte("selected")}}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() string { return status().Trigger.ObservedRequest }, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Equal(baseline))
			if global {
				By("retiring the old native predicate after source selector changes")
				trigger.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"trigger-source": "unselected-" + prefix}}
				updateTriggers(func(spec *capsulev1beta2.TenantResourceCommonSpec) {
					spec.Triggers[0].NamespaceSelector = trigger.NamespaceSelector.DeepCopy()
				})
				waitPredicate(trigger)
				baseline = status().Trigger.ObservedRequest
				_, err = selectedOwner.CoreV1().Secrets(source.Name).Create(ctx, &corev1.Secret{Name: "source-after-selector", Labels: map[string]string{"trigger-run": prefix}, Data: map[string][]byte{"value": []byte("excluded")}}, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
				Consistently(func() string { return status().Trigger.ObservedRequest }, 2*time.Second, 200*time.Millisecond).Should(Equal(baseline))
			}
			By("rejecting invalid trigger configuration while real tenants are present")
			for _, invalid := range []struct {
				edit   func(*capsulev1beta2.TenantResourceTrigger)
				reason string
			}{
				{edit: func(t *capsulev1beta2.TenantResourceTrigger) {
					t.MatchConditions = []registrationv1.MatchCondition{{Name: "invalid", Expression: "object["}}
				}, reason: "match"},
				{edit: func(t *capsulev1beta2.TenantResourceTrigger) {
					t.Resources = []string{"*", "secrets"}
				}, reason: "overlap"},
			} {
				Eventually(func() error {
					var obj client.Object
					var spec *capsulev1beta2.TenantResourceCommonSpec
					if global {
						r := &capsulev1beta2.GlobalTenantResource{}
						obj = r
						spec = &r.Spec.TenantResourceCommonSpec
					} else {
						r := &capsulev1beta2.TenantResource{}
						obj = r
						spec = &r.Spec.TenantResourceCommonSpec
					}
					if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(resource), obj); err != nil {
						return err
					}
					generation := obj.GetGeneration()
					invalid.edit(&spec.Triggers[0])
					err := k8sClient.Update(ctx, obj)
					if err == nil {
						return StopTrying("invalid trigger unexpectedly persisted")
					}
					if apierrors.IsConflict(err) {
						return err
					}
					if !apierrors.IsForbidden(err) && !apierrors.IsInvalid(err) {
						return fmt.Errorf("unexpected rejection: %w", err)
					}
					Expect(err.Error()).To(ContainSubstring(invalid.reason))
					Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resource), obj)).To(Succeed())
					Expect(obj.GetGeneration()).To(Equal(generation))
					return nil
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
			// Cross-tenant writes remain forbidden irrespective of trigger audience.
			_, err = owner.CoreV1().Secrets(foreign.Name).Create(ctx, &corev1.Secret{Name: "forbidden"}, metav1.CreateOptions{})
			Expect(apierrors.IsForbidden(err)).To(BeTrue())
		})
	}
})
