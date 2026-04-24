// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
	"github.com/projectcapsule/capsule/pkg/template"
)

var _ = Describe("Creating a TenantResource object", Label("tenantresource"), func() {
	originConfig := &capsulev1beta2.CapsuleConfiguration{}

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenantresource-e2e",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "solar-user",
							Kind: "User",
						},
					},
				},
			},
			AdditionalRoleBindings: []rbac.AdditionalRoleBindingsSpec{
				{
					ClusterRoleName: "admin",
					Subjects: []rbacv1.Subject{
						{
							Kind: "User",
							Name: "bob",
						},
					},
				},
			},
		},
	}

	tntItem := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dummy-secret",
			Namespace: "solar-system",
			Labels: map[string]string{
				"replicate": "true",
				"source":    "static",
			},
		},
		Type: corev1.SecretTypeOpaque,
	}

	crossNamespaceItem := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cross-reference-secret",
			Namespace: "default",
			Labels: map[string]string{
				"replicate": "true",
				"source":    "static",
			},
		},
		Type: corev1.SecretTypeOpaque,
	}

	testLabels := map[string]string{
		"labels.energy.io/replicate": "namespaced",
	}
	testAnnotations := map[string]string{
		"annotations.energy.io/replicate": "namespaced",
	}

	tr := &capsulev1beta2.TenantResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "replicate-energies",
			Namespace: "solar-system",
		},
		Spec: capsulev1beta2.TenantResourceSpec{
			ServiceAccount: &meta.LocalRFC1123ObjectReference{
				Name: meta.RFC1123Name("replicator"),
			},
			TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
				ResyncPeriod:    metav1.Duration{Duration: time.Minute},
				PruningOnDelete: ptr.To(true),
				Resources: []capsulev1beta2.ResourceSpec{
					{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"replicate": "true",
							},
						},
						NamespacedItems: []template.ResourceReference{
							{
								Kind:       "Secret",
								Namespace:  "solar-system",
								APIVersion: "v1",
								Selector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"replicate": "true",
									},
								},
							},
						},
						RawItems: []capsulev1beta2.RawExtension{
							{
								RawExtension: runtime.RawExtension{
									Object: &corev1.Secret{
										TypeMeta: metav1.TypeMeta{
											Kind:       "Secret",
											APIVersion: "v1",
										},
										ObjectMeta: metav1.ObjectMeta{
											Name:        "raw-secret-1",
											Labels:      testLabels,
											Annotations: testAnnotations,
										},
										Type: corev1.SecretTypeOpaque,
										Data: map[string][]byte{
											"{{ tenant.name }}": []byte("Cg=="),
											"{{ namespace }}":   []byte("Cg=="),
										},
									},
								},
							},
							{
								RawExtension: runtime.RawExtension{
									Object: &corev1.Secret{
										TypeMeta: metav1.TypeMeta{
											Kind:       "Secret",
											APIVersion: "v1",
										},
										ObjectMeta: metav1.ObjectMeta{
											Name:        "raw-secret-2",
											Labels:      testLabels,
											Annotations: testAnnotations,
										},
										Type: corev1.SecretTypeOpaque,
										Data: map[string][]byte{
											"{{ tenant.name }}": []byte("Cg=="),
											"{{ namespace }}":   []byte("Cg=="),
										},
									},
								},
							},
							{
								RawExtension: runtime.RawExtension{
									Object: &corev1.Secret{
										TypeMeta: metav1.TypeMeta{
											Kind:       "Secret",
											APIVersion: "v1",
										},
										ObjectMeta: metav1.ObjectMeta{
											Name:        "raw-secret-3",
											Labels:      testLabels,
											Annotations: testAnnotations,
										},
										Type: corev1.SecretTypeOpaque,
										Data: map[string][]byte{
											"{{ tenant.name }}": []byte("Cg=="),
											"{{ namespace }}":   []byte("Cg=="),
										},
									},
								},
							},
						},
						AdditionalMetadata: &api.AdditionalMetadataSpec{
							Labels: map[string]string{
								"labels.energy.io": "replicate",
							},
							Annotations: map[string]string{
								"annotations.energy.io": "replicate",
							},
						},
					},
				},
			},
		},
	}

	JustBeforeEach(func() {
		Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: defaultConfigurationName}, originConfig)).To(Succeed())

		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			crossNamespaceItem.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), crossNamespaceItem)
		}).Should(Succeed())
	})

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), crossNamespaceItem)).Should(Succeed())
		EventuallyDeletion(tnt)

		// Restore Configuration
		Eventually(func() error {
			c := &capsulev1beta2.CapsuleConfiguration{}
			if err := k8sClient.Get(context.Background(), client.ObjectKey{Name: originConfig.Name}, c); err != nil {
				return err
			}
			// Apply the initial configuration from originConfig to c
			c.Spec = originConfig.Spec
			return k8sClient.Update(context.Background(), c)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("correctly inherit impersonation (sa configs)", func() {
		solarNs := []string{"solar-one", "solar-two", "solar-three"}

		By("creating solar Namespaces", func() {
			for _, ns := range append(solarNs, "solar-system") {
				namespace := NewNamespace(ns, map[string]string{
					meta.TenantLabel: tnt.GetName(),
				})

				NamespaceCreation(namespace, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			}
		})

		t := tr.DeepCopy()
		t.ResourceVersion = ""
		t.Spec.ServiceAccount = nil

		By("creating the TenantResource (without serviceaccount)", func() {
			EventuallyCreation(func() error {
				return k8sClient.Create(context.TODO(), t)
			}).Should(Succeed())
		})

		By("verifying impersonation status (Default Controller)", func() {
			Eventually(func(g Gomega) {
				stat := capsulev1beta2.TenantResource{}
				g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{
					Name:      t.GetName(),
					Namespace: t.GetNamespace(),
				}, &stat)).To(Succeed())

				g.Expect(stat.Status.ServiceAccount).ToNot(BeNil())

				g.Expect(stat.Status.ServiceAccount.Name).To(Equal(
					meta.RFC1123Name("capsule"),
				))

				g.Expect(stat.Status.ServiceAccount.Namespace).To(Equal(
					meta.RFC1123SubdomainName("capsule-system"),
				))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("Adding default sa in Capsule Configuration", func() {
			ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
				configuration.Spec.Impersonation.TenantDefaultServiceAccount = "default"
			})
		})

		By("verifying impersonation status (default propagated)", func() {
			Eventually(func(g Gomega) {
				stat := capsulev1beta2.TenantResource{}
				g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{
					Name:      t.GetName(),
					Namespace: t.GetNamespace(),
				}, &stat)).To(Succeed())

				g.Expect(stat.Status.ServiceAccount).ToNot(BeNil())

				g.Expect(stat.Status.ServiceAccount.Name).To(Equal(
					meta.RFC1123Name("default"),
				))

				g.Expect(stat.Status.ServiceAccount.Namespace).To(Equal(
					meta.RFC1123SubdomainName(tr.GetNamespace()),
				))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("creating the TenantResource (with serviceaccount)", func() {

			c := &capsulev1beta2.TenantResource{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: t.GetName(), Namespace: t.GetNamespace()}, c)).Should(Succeed())

			c.Spec.ServiceAccount = &meta.LocalRFC1123ObjectReference{
				Name: meta.RFC1123Name("custom-account"),
			}
			Expect(k8sClient.Update(context.TODO(), c)).To(Succeed())
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: t.GetName(), Namespace: t.GetNamespace()}, c))

			Expect(c.Status.ServiceAccount).To(Equal(
				&meta.NamespacedRFC1123ObjectReferenceWithNamespace{
					Name:      meta.RFC1123Name("custom-account"),
					Namespace: meta.RFC1123SubdomainName(t.GetNamespace()),
				},
			))
		})

		By("removing serviceaccount from the TenantResource", func() {
			c := &capsulev1beta2.TenantResource{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: t.GetName(), Namespace: t.GetNamespace()}, c)).Should(Succeed())

			c.Spec.ServiceAccount = nil
			Expect(k8sClient.Update(context.TODO(), c)).To(Succeed())
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: t.GetName(), Namespace: t.GetNamespace()}, c))

			Expect(c.Status.ServiceAccount).To(Equal(
				&meta.NamespacedRFC1123ObjectReferenceWithNamespace{
					Name:      meta.RFC1123Name("default"),
					Namespace: meta.RFC1123SubdomainName(t.GetNamespace()),
				},
			))
		})

	})

	It("Verify Adoption", func() {
		solarNs := []string{"solar-one"}

		tntResource := &capsulev1beta2.TenantResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "e2e-replicate-adoption",
				Namespace: "solar-system",
			},
			Spec: capsulev1beta2.TenantResourceSpec{
				ServiceAccount: &meta.LocalRFC1123ObjectReference{
					Name: meta.RFC1123Name("replicator"),
				},
				TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{
					ResyncPeriod:    metav1.Duration{Duration: time.Minute},
					PruningOnDelete: ptr.To(true),
					Resources: []capsulev1beta2.ResourceSpec{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"replicate": "true",
								},
							},
							RawItems: []capsulev1beta2.RawExtension{
								{
									RawExtension: runtime.RawExtension{
										Object: &corev1.Secret{
											TypeMeta: metav1.TypeMeta{
												Kind:       "Secret",
												APIVersion: "v1",
											},
											ObjectMeta: metav1.ObjectMeta{
												Name:        "raw-secret-3",
												Labels:      testLabels,
												Annotations: testAnnotations,
											},
											Type: corev1.SecretTypeOpaque,
											Data: map[string][]byte{
												"{{ tenant.name }}": []byte("Cg=="),
												"{{ namespace }}":   []byte("Cg=="),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		By("creating solar Namespaces", func() {
			for _, ns := range append(solarNs, "solar-system") {
				namespace := NewNamespace(ns, map[string]string{
					meta.TenantLabel: tnt.GetName(),
				})

				NamespaceCreation(namespace, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			}
		})

		By("distributing rbac", func() {
			EnsureServiceAccount(context.TODO(), k8sClient, tntResource.Spec.ServiceAccount.Name.String(), tr.GetNamespace())
			EnsureRoleAndBindingForNamespaces(context.TODO(), k8sClient, tr.Spec.ServiceAccount.Name.String(), tr.GetNamespace(), append(solarNs, "solar-system"))
		})

		By("labelling Namespaces", func() {
			for _, name := range []string{"solar-one"} {
				EventuallyWithOffset(1, func() error {
					ns := corev1.Namespace{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: name}, &ns)).Should(Succeed())

					labels := ns.GetLabels()
					if labels == nil {
						return fmt.Errorf("missing labels")
					}
					labels["replicate"] = "true"
					ns.SetLabels(labels)

					return k8sClient.Update(context.TODO(), &ns)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})

		By("creating the prexisting element", func() {
			EventuallyCreation(func() error {
				sec := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "raw-secret-3",
						Namespace: "solar-one",
					},
					StringData: map[string]string{
						"Something": "Existing",
					},
				}
				return k8sClient.Create(context.TODO(), sec)
			}).Should(Succeed())
		})

		By("creating the TenantResource", func() {
			EventuallyCreation(func() error {
				return k8sClient.Create(context.TODO(), tntResource)
			}).Should(Succeed())
		})

		By("verifing status", func() {
			stat := capsulev1beta2.TenantResource{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tr.GetName(), Namespace: tr.GetNamespace()}, &stat)).ToNot(HaveOccurred())
			Expect(stat.Status.Size).To(Equal(uint(0)))

			// Verify a set of expected items exist
			expected := []gvk.ResourceID{
				{
					Version: "v1",
					Kind:    "Secret",
					Name:    "raw-secret-3",
					TenantResourceIDWithOrigin: gvk.TenantResourceIDWithOrigin{
						Origin: "0/raw-2",
						TenantResourceID: gvk.TenantResourceID{
							Tenant: tnt.GetName(),
						},
					},
				},
			}

			for _, e := range expected {
				for _, ns := range solarNs {
					e.Namespace = ns

					obj := stat.Status.ProcessedItems.GetItem(e)

					Expect(obj.ObjectReferenceStatusCondition.Message).WithOffset(1).To(Equal("apply failed for item "+e.Origin+": evaluating managed metadata: object "+e.Version+"/"+e.Kind+" "+e.Namespace+"/"+e.Name+"exists and cannot be adopted"),
						"unexpected message",
					)

					Expect(obj).WithOffset(1).ToNot(BeNil(),
						"processed item not found: kind=%s name=%s namespace=%s",
						e.Kind, e.Name, ns,
					)

					Expect(obj.ObjectReferenceStatusCondition.Created).WithOffset(1).To(BeFalse(),
						"item marked created: kind=%s name=%s namespace=%s",
						e.Kind, e.Name, ns,
					)

					Expect(obj.ObjectReferenceStatusCondition.Type).WithOffset(1).To(Equal(meta.ReadyCondition),
						"unexpected condition type: kind=%s name=%s namespace=%s",
						e.Kind, e.Name, ns,
					)

					Expect(obj.ObjectReferenceStatusCondition.Status).WithOffset(1).To(Equal(metav1.ConditionFalse),
						"condition not true: kind=%s name=%s namespace=%s",
						e.Kind, e.Name, ns,
					)

					Expect(obj.ObjectReferenceStatusCondition.Message).WithOffset(1).To(Equal("apply failed for item "+e.Origin+": evaluating managed metadata: object "+e.Version+"/"+e.Kind+" "+e.Namespace+"/"+e.Name+"exists and cannot be adopted"),
						"unexpected message",
					)
				}
			}

			// Ready Condition
			rdyCondition := stat.Status.Conditions.GetConditionByType(meta.ReadyCondition)
			Expect(rdyCondition.Reason).To(Equal(meta.FailedReason))
			Expect(rdyCondition.Type).To(Equal(meta.ReadyCondition))
			Expect(rdyCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(rdyCondition.Message).To(Equal("reconciled"))
		})
	})

	It("should replicate resources to all Tenant Namespaces", Label("skip"), func() {
		solarNs := []string{"solar-one", "solar-two", "solar-three"}

		By("creating solar Namespaces", func() {
			for _, ns := range append(solarNs, "solar-system") {
				namespace := NewNamespace(ns, map[string]string{
					meta.TenantLabel: tnt.GetName(),
				})

				NamespaceCreation(namespace, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			}
		})

		By("distributing rbac", func() {
			EnsureServiceAccount(context.TODO(), k8sClient, tr.Spec.ServiceAccount.Name.String(), tr.GetNamespace())
			EnsureRoleAndBindingForNamespaces(context.TODO(), k8sClient, tr.Spec.ServiceAccount.Name.String(), tr.GetNamespace(), append(solarNs, "solar-system"))
		})

		By("labelling Namespaces", func() {
			for _, name := range []string{"solar-one", "solar-two", "solar-three"} {
				EventuallyWithOffset(1, func() error {
					ns := corev1.Namespace{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: name}, &ns)).Should(Succeed())

					labels := ns.GetLabels()
					if labels == nil {
						return fmt.Errorf("missing labels")
					}
					labels["replicate"] = "true"
					ns.SetLabels(labels)

					return k8sClient.Update(context.TODO(), &ns)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		})

		By("creating the namespaced item", func() {
			EventuallyCreation(func() error {
				return k8sClient.Create(context.TODO(), tntItem)
			}).Should(Succeed())
		})

		By("creating the TenantResource", func() {
			EventuallyCreation(func() error {
				return k8sClient.Create(context.TODO(), tr)
			}).Should(Succeed())
		})

		By("verifying impersonation status", func() {
			Eventually(func(g Gomega) {
				stat := capsulev1beta2.TenantResource{}
				g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{
					Name:      tr.GetName(),
					Namespace: tr.GetNamespace(),
				}, &stat)).To(Succeed())

				g.Expect(stat.Status.ServiceAccount).ToNot(BeNil())

				g.Expect(stat.Status.ServiceAccount.Name).To(Equal(
					meta.RFC1123Name(tr.Spec.ServiceAccount.Name.String()),
				))

				g.Expect(stat.Status.ServiceAccount.Namespace).To(Equal(
					meta.RFC1123SubdomainName(tr.GetNamespace()),
				))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		for _, ns := range solarNs {
			By(fmt.Sprintf("waiting for replicated resources in %s Namespace", ns), func() {
				Eventually(func() []corev1.Secret {
					r, err := labels.NewRequirement("labels.energy.io", selection.DoubleEquals, []string{"replicate"})
					if err != nil {
						return nil
					}

					secrets := corev1.SecretList{}
					err = k8sClient.List(context.TODO(), &secrets, &client.ListOptions{LabelSelector: labels.NewSelector().Add(*r), Namespace: ns})
					if err != nil {
						return nil
					}

					return secrets.Items
				}, defaultTimeoutInterval, defaultPollInterval).Should(HaveLen(4))
			})

			By(fmt.Sprintf("ensuring replicated secrets in %s do not have matched labels (avoiding loops)", ns), func() {
				Eventually(func(g Gomega) {
					r, err := labels.NewRequirement("source", selection.DoubleEquals, []string{"static"})
					g.Expect(err).ToNot(HaveOccurred())

					secrets := corev1.SecretList{}
					g.Expect(k8sClient.List(context.TODO(), &secrets, &client.ListOptions{
						LabelSelector: labels.NewSelector().Add(*r),
						Namespace:     ns,
					})).To(Succeed())

					g.Expect(secrets.Items).To(HaveLen(1))

					for _, s := range secrets.Items {
						_, has := s.Labels["replicate"]
						g.Expect(has).To(BeFalse(),
							"secret %s/%s unexpectedly has label %q (labels=%v)",
							s.Namespace, s.Name, "replicate", s.Labels,
						)
					}
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			})

			By(fmt.Sprintf("ensuring raw items are templated in %s Namespace", ns), func() {
				for _, name := range []string{"raw-secret-1", "raw-secret-2", "raw-secret-3"} {
					secret := corev1.Secret{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: ns}, &secret)).ToNot(HaveOccurred())

					Expect(secret.Data).To(HaveKey(tnt.Name))
					Expect(secret.Data).To(HaveKey(ns))
				}
			})
		}

		By("verifing status", func() {
			stat := capsulev1beta2.TenantResource{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tr.GetName(), Namespace: tr.GetNamespace()}, &stat)).ToNot(HaveOccurred())
			Expect(stat.Status.Size).To(Equal(uint(12)))

			// Verify a set of expected items exist
			expected := []gvk.ResourceID{
				{
					Version: "v1",
					Kind:    "Secret",
					Name:    "dummy-secret",
					TenantResourceIDWithOrigin: gvk.TenantResourceIDWithOrigin{
						Origin: "replica",
						TenantResourceID: gvk.TenantResourceID{
							Tenant: tnt.GetName(),
						},
					},
				},
				{
					Version: "v1",
					Kind:    "Secret",
					Name:    "raw-secret-1",
					TenantResourceIDWithOrigin: gvk.TenantResourceIDWithOrigin{
						Origin: "0/raw-0",
						TenantResourceID: gvk.TenantResourceID{
							Tenant: tnt.GetName(),
						},
					},
				},
				{
					Version: "v1",
					Kind:    "Secret",
					Name:    "raw-secret-2",
					TenantResourceIDWithOrigin: gvk.TenantResourceIDWithOrigin{
						Origin: "0/raw-1",
						TenantResourceID: gvk.TenantResourceID{
							Tenant: tnt.GetName(),
						},
					},
				},
				{
					Version: "v1",
					Kind:    "Secret",
					Name:    "raw-secret-3",
					TenantResourceIDWithOrigin: gvk.TenantResourceIDWithOrigin{
						Origin: "0/raw-2",
						TenantResourceID: gvk.TenantResourceID{
							Tenant: tnt.GetName(),
						},
					},
				},
			}

			for _, e := range expected {
				for _, ns := range solarNs {
					e.Namespace = ns

					obj := stat.Status.ProcessedItems.GetItem(e)

					Expect(obj).WithOffset(1).ToNot(BeNil(),
						"processed item not found: kind=%s name=%s namespace=%s",
						e.Kind, e.Name, ns,
					)

					Expect(obj.ObjectReferenceStatusCondition.Created).WithOffset(1).To(BeTrue(),
						"item not marked created: kind=%s name=%s namespace=%s",
						e.Kind, e.Name, ns,
					)

					Expect(obj.ObjectReferenceStatusCondition.Type).WithOffset(1).To(Equal(meta.ReadyCondition),
						"unexpected condition type: kind=%s name=%s namespace=%s",
						e.Kind, e.Name, ns,
					)

					Expect(obj.ObjectReferenceStatusCondition.Status).WithOffset(1).To(Equal(metav1.ConditionTrue),
						"condition not true: kind=%s name=%s namespace=%s",
						e.Kind, e.Name, ns,
					)
				}
			}

			// Ready Condition
			rdyCondition := stat.Status.Conditions.GetConditionByType(meta.ReadyCondition)
			Expect(rdyCondition.Message).To(Equal("reconciled"))
			Expect(rdyCondition.Reason).To(Equal(meta.SucceededReason))
			Expect(rdyCondition.Type).To(Equal(meta.ReadyCondition))
			Expect(rdyCondition.Status).To(Equal(metav1.ConditionTrue))
		})

		By("using a Namespace selector", func() {
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tr.GetName(), Namespace: "solar-system"}, tr)).ToNot(HaveOccurred())

			tr.Spec.Resources[0].NamespaceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"kubernetes.io/metadata.name": "solar-three",
				},
			}

			Expect(k8sClient.Update(context.TODO(), tr)).ToNot(HaveOccurred())

			checkFn := func(ns string) func() []corev1.Secret {
				return func() []corev1.Secret {
					r, err := labels.NewRequirement("labels.energy.io", selection.DoubleEquals, []string{"replicate"})
					if err != nil {
						return nil
					}

					secrets := corev1.SecretList{}
					err = k8sClient.List(context.TODO(), &secrets, &client.ListOptions{LabelSelector: labels.NewSelector().Add(*r), Namespace: ns})
					if err != nil {
						return nil
					}

					return secrets.Items
				}
			}

			for _, ns := range []string{"solar-one", "solar-two"} {
				Eventually(checkFn(ns), defaultTimeoutInterval, defaultPollInterval).Should(HaveLen(0))
			}

			Eventually(checkFn("solar-three"), defaultTimeoutInterval, defaultPollInterval).Should(HaveLen(4))
		})

		By("checking if replicated object have annotations and labels", func() {
			for _, name := range []string{"dummy-secret", "raw-secret-1", "raw-secret-2", "raw-secret-3"} {
				secret := corev1.Secret{}
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: "solar-three"}, &secret)).ToNot(HaveOccurred())

				for k, v := range tr.Spec.Resources[0].AdditionalMetadata.Labels {
					_, err := HaveKeyWithValue(k, v).Match(secret.GetLabels())
					Expect(err).ToNot(HaveOccurred())
				}
				for k, v := range testLabels {
					_, err := HaveKeyWithValue(k, v).Match(secret.GetLabels())
					Expect(err).ToNot(HaveOccurred())
				}
				for k, v := range tr.Spec.Resources[0].AdditionalMetadata.Annotations {
					_, err := HaveKeyWithValue(k, v).Match(secret.GetAnnotations())
					Expect(err).ToNot(HaveOccurred())
				}
				for k, v := range testAnnotations {
					_, err := HaveKeyWithValue(k, v).Match(secret.GetAnnotations())
					Expect(err).ToNot(HaveOccurred())
				}
			}
		})

		By("checking replicated object cannot be deleted by a Tenant Owner", func() {
			for _, name := range []string{"dummy-secret", "raw-secret-1", "raw-secret-2", "raw-secret-3"} {
				cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

				Consistently(func() error {
					return cs.CoreV1().Secrets("solar-three").Delete(context.TODO(), name, metav1.DeleteOptions{})
				}, 10*time.Second, time.Second).Should(HaveOccurred())
			}
		})

		By("checking replicated object cannot be deleted by additional bindings", func() {
			for _, name := range []string{"dummy-secret", "raw-secret-1", "raw-secret-2", "raw-secret-3"} {
				cs := ownerClient(rbac.UserSpec{
					Kind: rbac.OwnerKind(tnt.Spec.AdditionalRoleBindings[0].Subjects[0].Kind),
					Name: tnt.Spec.AdditionalRoleBindings[0].Subjects[0].Name,
				})

				Consistently(func() error {
					return cs.CoreV1().Secrets("solar-three").Delete(context.TODO(), name, metav1.DeleteOptions{})
				}, 10*time.Second, time.Second).Should(HaveOccurred())
			}
		})

		By("checking replicated object cannot be update by a Tenant Owner", func() {
			for _, name := range []string{"dummy-secret", "raw-secret-1", "raw-secret-2", "raw-secret-3"} {
				cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

				Consistently(func() error {
					secret, err := cs.CoreV1().Secrets("solar-three").Get(context.TODO(), name, metav1.GetOptions{})
					if err != nil {
						return err
					}

					secret.SetLabels(nil)
					secret.SetAnnotations(nil)

					_, err = cs.CoreV1().Secrets("solar-three").Update(context.TODO(), secret, metav1.UpdateOptions{})

					return err
				}, 10*time.Second, time.Second).Should(HaveOccurred())
			}
		})

		By("checking replicated object cannot be update by additional bindings", func() {
			for _, name := range []string{"dummy-secret", "raw-secret-1", "raw-secret-2", "raw-secret-3"} {
				cs := ownerClient(rbac.UserSpec{
					Kind: rbac.OwnerKind(tnt.Spec.AdditionalRoleBindings[0].Subjects[0].Kind),
					Name: tnt.Spec.AdditionalRoleBindings[0].Subjects[0].Name,
				})

				Consistently(func() error {
					secret, err := cs.CoreV1().Secrets("solar-three").Get(context.TODO(), name, metav1.GetOptions{})
					if err != nil {
						return err
					}

					secret.SetLabels(nil)
					secret.SetAnnotations(nil)

					_, err = cs.CoreV1().Secrets("solar-three").Update(context.TODO(), secret, metav1.UpdateOptions{})

					return err
				}, 10*time.Second, time.Second).Should(HaveOccurred())
			}
		})

		By("checking that cross-namespace objects are not replicated", func() {
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tr.GetName(), Namespace: "solar-system"}, tr)).ToNot(HaveOccurred())
			tr.Spec.Resources[0].NamespacedItems = append(tr.Spec.Resources[0].NamespacedItems, template.ResourceReference{
				Kind:       crossNamespaceItem.Kind,
				Namespace:  crossNamespaceItem.GetName(),
				APIVersion: crossNamespaceItem.APIVersion,
				Selector: &metav1.LabelSelector{
					MatchLabels: crossNamespaceItem.GetLabels(),
				},
			})

			Expect(k8sClient.Update(context.TODO(), tr)).ToNot(HaveOccurred())
			// Ensuring that although the deletion of TenantResource object,
			// the replicated objects are not deleted.
			Consistently(func() error {
				return k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: solarNs[rand.Intn(len(solarNs))], Name: crossNamespaceItem.GetName()}, &corev1.Secret{})
			}, 10*time.Second, time.Second).Should(HaveOccurred())
		})

		By("checking pruning is deleted", func() {
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tr.GetName(), Namespace: "solar-system"}, tr)).ToNot(HaveOccurred())
			Expect(*tr.Spec.PruningOnDelete).Should(BeTrue())

			tr.Spec.PruningOnDelete = ptr.To(false)

			Expect(k8sClient.Update(context.TODO(), tr)).ToNot(HaveOccurred())

			By("deleting the TenantResource", func() {
				// Ensuring that although the deletion of TenantResource object,
				// the replicated objects are not deleted.
				Expect(k8sClient.Delete(context.TODO(), tr)).Should(Succeed())

				r, err := labels.NewRequirement("labels.energy.io", selection.DoubleEquals, []string{"replicate"})
				Expect(err).ToNot(HaveOccurred())

				Consistently(func() []corev1.Secret {
					secrets := corev1.SecretList{}

					err = k8sClient.List(context.TODO(), &secrets, &client.ListOptions{LabelSelector: labels.NewSelector().Add(*r), Namespace: "solar-three"})
					Expect(err).ToNot(HaveOccurred())

					return secrets.Items
				}, 10*time.Second, time.Second).Should(HaveLen(4))
			})
		})
	})

})
