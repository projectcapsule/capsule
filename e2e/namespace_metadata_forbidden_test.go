// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("creating a Namespace with user-specified labels and annotations", Ordered, Label("config", "namespace", "metadata", "forbidden"), func() {
	originConfig := &capsulev1beta2.CapsuleConfiguration{}

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-user-metadata-forbidden",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			NamespaceOptions: &capsulev1beta2.NamespaceOptions{
				ForbiddenLabels: api.ForbiddenListSpec{
					Exact: []string{"foo", "bar"},
					Regex: "^gatsby-.*$|^managed\\.projectcapsule\\.dev/",
				},
				ForbiddenAnnotations: api.ForbiddenListSpec{
					Exact: []string{"foo", "bar"},
					Regex: "^gatsby-.*$|^managed\\.projectcapsule\\.dev/",
				},
				AdditionalMetadataList: []api.AdditionalMetadataSelectorSpec{
					{
						Labels: map[string]string{
							"managed.projectcapsule.dev/customer": "acme",
							"managed.projectcapsule.dev/tenant":   "{{ tenant.name }}",
						},
						Annotations: map[string]string{
							"managed.projectcapsule.dev/source": "capsule",
						},
					},
				},
			},
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-user-metadata-forbidden",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	admin := rbac.UserSpec{
		Name: "admin",
		Kind: "User",
	}

	JustBeforeEach(func() {
		Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: defaultConfigurationName}, originConfig)).To(Succeed())

		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())

		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)

		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.Administrators = []rbac.UserSpec{admin}
		})
	})

	JustAfterEach(func() {
		EventuallyDeletion(tnt)

		Eventually(func() error {
			c := &capsulev1beta2.CapsuleConfiguration{}
			if err := k8sClient.Get(context.Background(), client.ObjectKey{Name: originConfig.Name}, c); err != nil {
				return err
			}
			c.Spec = originConfig.Spec
			return k8sClient.Update(context.Background(), c)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("should allow", func() {
		ctx := context.TODO()

		By("specifying non-forbidden labels", func() {
			ns := NewNamespace("", map[string]string{
				"bim":            "baz",
				meta.TenantLabel: tnt.GetName(),
			})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
		})

		By("specifying non-forbidden annotations", func() {
			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})
			ns.SetAnnotations(map[string]string{"bim": "baz"})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
		})

		By("allowing forbidden-prefix labels and annotations when they are managed by Capsule", func() {
			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns)
				g.Expect(err).ToNot(HaveOccurred())

				g.Expect(ns.GetLabels()).To(HaveKeyWithValue("managed.projectcapsule.dev/customer", "acme"))
				g.Expect(ns.GetLabels()).To(HaveKeyWithValue("managed.projectcapsule.dev/tenant", tnt.GetName()))
				g.Expect(ns.GetAnnotations()).To(HaveKeyWithValue("managed.projectcapsule.dev/source", "capsule"))
			}, defaultTimeoutInterval, time.Second).Should(Succeed())
		})

		By("creating a Namespace with forbidden labels as an admin", func() {
			ns := NewNamespace("", map[string]string{
				"foo":            "bar",
				"gatsby-custom":  "value",
				meta.TenantLabel: tnt.GetName(),
			})

			NamespaceCreation(ns, admin, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
		})

		By("creating a Namespace with forbidden annotations as an admin", func() {
			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})
			ns.SetAnnotations(map[string]string{"foo": "bar", "gatsby-custom": "value"})

			NamespaceCreation(ns, admin, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
		})

		By("updating a Namespace with a new forbidden label as an admin", func() {
			ns := NewNamespace("admin-update-labels", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())

			original := ns.DeepCopy()
			ns.Labels["foo"] = "bar"
			ns.Labels["gatsby-custom"] = "value"

			Eventually(func() error {
				return impersonationClient(admin.Name, nil).Patch(ctx, ns, client.MergeFrom(original))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("updating a Namespace with a new forbidden annotation as an admin", func() {
			ns := NewNamespace("admin-update-annotations", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())

			original := ns.DeepCopy()
			ns.SetAnnotations(map[string]string{"foo": "bar", "gatsby-custom": "value"})

			Eventually(func() error {
				return impersonationClient(admin.Name, nil).Patch(ctx, ns, client.MergeFrom(original))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
	})

	It("should fail when creating a Namespace", func() {
		By("specifying forbidden labels using exact match", func() {
			ns := NewNamespace("", map[string]string{
				"foo":            "bar",
				meta.TenantLabel: tnt.GetName(),
			})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceIsNotPartOfTenant(tnt, ns).Should(Succeed())
		})

		By("specifying forbidden labels using regex match", func() {
			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tnt.GetName(),
				"gatsby-foo":     "bar",
			})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceIsNotPartOfTenant(tnt, ns).Should(Succeed())
		})

		By("specifying forbidden annotations using exact match", func() {
			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})
			ns.SetAnnotations(map[string]string{"foo": "bar"})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceIsNotPartOfTenant(tnt, ns).Should(Succeed())
		})

		By("specifying forbidden annotations using regex match", func() {
			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})
			ns.SetAnnotations(map[string]string{"gatsby-foo": "bar"})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceIsNotPartOfTenant(tnt, ns).Should(Succeed())
		})

		By("specifying forbidden-prefix labels that are not managed by Capsule", func() {
			ns := NewNamespace("", map[string]string{
				meta.TenantLabel:                        tnt.GetName(),
				"managed.projectcapsule.dev/user-label": "should-be-denied",
			})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceIsNotPartOfTenant(tnt, ns).Should(Succeed())
		})

		By("specifying forbidden-prefix annotations that are not managed by Capsule", func() {
			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})
			ns.SetAnnotations(map[string]string{
				"managed.projectcapsule.dev/user-annotation": "should-be-denied",
			})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceIsNotPartOfTenant(tnt, ns).Should(Succeed())
		})
	})

	It("should fail when updating a Namespace", Label("skip-on-openshift"), func() {
		role := &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ns-patch",
			},
			Rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"patch", "update"},
					APIGroups: []string{""},
					Resources: []string{"namespaces"},
				},
			},
		}

		roleBinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ns-patch",
			},
			Subjects: []rbacv1.Subject{
				{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     tnt.Spec.Owners[0].Kind.String(),
					Name:     tnt.Spec.Owners[0].Name,
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     role.GetName(),
			},
		}

		rbacPatch := func(ns string) {
			role := role.DeepCopy()
			role.SetNamespace(ns)
			Expect(k8sClient.Create(context.Background(), role)).To(Succeed())

			roleBinding := roleBinding.DeepCopy()
			roleBinding.SetNamespace(ns)
			Expect(k8sClient.Create(context.Background(), roleBinding)).To(Succeed())
		}

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		By("specifying forbidden labels using exact match", func() {
			ns := NewNamespace("forbidden-labels-exact-match", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

			rbacPatch(ns.GetName())

			Consistently(func() error {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns); err != nil {
					return nil
				}

				labels := ns.GetLabels()
				if labels == nil {
					labels = map[string]string{}
				}
				labels["foo"] = "bar"
				ns.SetLabels(labels)

				_, err := cs.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})

				return err
			}, 10*time.Second, time.Second).ShouldNot(Succeed())
		})

		By("specifying forbidden labels using regex match", func() {
			ns := NewNamespace("forbidden-labels-regex-match", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

			rbacPatch(ns.GetName())

			Consistently(func() error {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns); err != nil {
					return nil
				}

				labels := ns.GetLabels()
				if labels == nil {
					labels = map[string]string{}
				}
				labels["gatsby-foo"] = "bar"
				ns.SetLabels(labels)

				_, err := cs.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})

				return err
			}, 3*time.Second, time.Second).ShouldNot(Succeed())
		})

		By("specifying forbidden annotations using exact match", func() {
			ns := NewNamespace("forbidden-annotations-exact-match", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

			rbacPatch(ns.GetName())

			Consistently(func() error {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns); err != nil {
					return nil
				}

				annotations := ns.GetAnnotations()
				if annotations == nil {
					annotations = map[string]string{}
				}
				annotations["foo"] = "bar"
				ns.SetAnnotations(annotations)

				_, err := cs.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})

				return err
			}, 10*time.Second, time.Second).ShouldNot(Succeed())
		})

		By("specifying forbidden annotations using regex match", func() {
			ns := NewNamespace("forbidden-annotations-regex-match", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

			rbacPatch(ns.GetName())

			Consistently(func() error {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns); err != nil {
					return nil
				}

				annotations := ns.GetAnnotations()
				if annotations == nil {
					annotations = map[string]string{}
				}
				annotations["gatsby-foo"] = "bar"
				ns.SetAnnotations(annotations)

				_, err := cs.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})

				return err
			}, 10*time.Second, time.Second).ShouldNot(Succeed())
		})

		By("specifying forbidden-prefix labels that are not managed by Capsule", func() {
			ns := NewNamespace("forbidden-managed-prefix-label-update", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

			rbacPatch(ns.GetName())

			Consistently(func() error {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns); err != nil {
					return nil
				}

				labels := ns.GetLabels()
				if labels == nil {
					labels = map[string]string{}
				}
				labels["managed.projectcapsule.dev/user-label"] = "should-be-denied"
				ns.SetLabels(labels)

				_, err := cs.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})

				return err
			}, 10*time.Second, time.Second).ShouldNot(Succeed())
		})

		By("specifying forbidden-prefix annotations that are not managed by Capsule", func() {
			ns := NewNamespace("forbidden-managed-prefix-annotation-update", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

			rbacPatch(ns.GetName())

			Consistently(func() error {
				if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns); err != nil {
					return nil
				}

				annotations := ns.GetAnnotations()
				if annotations == nil {
					annotations = map[string]string{}
				}
				annotations["managed.projectcapsule.dev/user-annotation"] = "should-be-denied"
				ns.SetAnnotations(annotations)

				_, err := cs.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})

				return err
			}, 10*time.Second, time.Second).ShouldNot(Succeed())
		})

		By("restoring a Capsule-managed forbidden-prefix label value on update", func() {
			ns := NewNamespace("forbidden-managed-label-value-update", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(ns.GetLabels()).To(HaveKeyWithValue("managed.projectcapsule.dev/customer", "acme"))
			}, defaultTimeoutInterval, time.Second).Should(Succeed())

			rbacPatch(ns.GetName())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns)
				g.Expect(err).ToNot(HaveOccurred())

				labels := ns.GetLabels()
				if labels == nil {
					labels = map[string]string{}
				}

				labels["managed.projectcapsule.dev/customer"] = "tampered"
				ns.SetLabels(labels)

				_, err = cs.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})
				g.Expect(err).ToNot(HaveOccurred())
			}, defaultTimeoutInterval, time.Second).Should(Succeed())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns)
				g.Expect(err).ToNot(HaveOccurred())

				g.Expect(ns.GetLabels()).To(HaveKeyWithValue("managed.projectcapsule.dev/customer", "acme"))
				g.Expect(ns.GetLabels()).To(HaveKeyWithValue("managed.projectcapsule.dev/tenant", tnt.GetName()))
			}, defaultTimeoutInterval, time.Second).Should(Succeed())
		})

		By("restoring a Capsule-managed forbidden-prefix annotation value on update", func() {
			ns := NewNamespace("forbidden-managed-annotation-value-update", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(ns.GetAnnotations()).To(HaveKeyWithValue("managed.projectcapsule.dev/source", "capsule"))
			}, defaultTimeoutInterval, time.Second).Should(Succeed())

			rbacPatch(ns.GetName())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns)
				g.Expect(err).ToNot(HaveOccurred())

				annotations := ns.GetAnnotations()
				if annotations == nil {
					annotations = map[string]string{}
				}

				annotations["managed.projectcapsule.dev/source"] = "tampered"
				ns.SetAnnotations(annotations)

				_, err = cs.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})
				g.Expect(err).ToNot(HaveOccurred())
			}, defaultTimeoutInterval, time.Second).Should(Succeed())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, ns)
				g.Expect(err).ToNot(HaveOccurred())

				g.Expect(ns.GetAnnotations()).To(HaveKeyWithValue("managed.projectcapsule.dev/source", "capsule"))
			}, defaultTimeoutInterval, time.Second).Should(Succeed())
		})
	})
})
