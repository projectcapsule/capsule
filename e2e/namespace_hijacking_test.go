// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"math/rand"
	"sort"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/utils/ptr"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	clt "github.com/projectcapsule/capsule/pkg/runtime/client"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

var _ = Describe("creating several Namespaces for a Tenant", Ordered, Label("namespace", "hijack"), func() {
	t1 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-ns-attack-1",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "gatsby",
							Kind: "User",
						},
					},
				},
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "oidc:group",
							Kind: "Group",
						},
					},
				},
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Kind: "ServiceAccount",
							Name: "system:serviceaccount:attacker-system:attacker",
						},
					},
				},
			},
		},
	}

	t2 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-ns-attack-2",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "gatsby",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	t3 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "e2e-ns-attack-3",
			Labels: map[string]string{"env": "e2e"},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "different-owner",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	grantNamespaceSubresourceUpdate := func(name string, subject rbacv1.Subject) {
		clusterRole := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{
						"namespaces/status",
						"namespaces/finalize",
					},
					Verbs: []string{
						"get",
						"update",
						"patch",
					},
				},
				{
					APIGroups: []string{""},
					Resources: []string{
						"namespaces",
					},
					Verbs: []string{
						"get",
						"update",
						"patch",
					},
				},
			},
		}

		clusterRoleBinding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Subjects: []rbacv1.Subject{
				subject,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     name,
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), clusterRole)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), clusterRoleBinding)
		}).Should(Succeed())
	}

	cleanupNamespaceSubresourceGrant := func(name string) {
		Eventually(func() error {
			return k8sClient.Delete(context.TODO(), &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
			})
		}).Should(SatisfyAny(Succeed(), WithTransform(apierrors.IsNotFound, BeTrue())))

		Eventually(func() error {
			return k8sClient.Delete(context.TODO(), &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
			})
		}).Should(SatisfyAny(Succeed(), WithTransform(apierrors.IsNotFound, BeTrue())))
	}

	expectNoTenantHijackPersisted := func(nsName string, originalTenant, attackerTenant *capsulev1beta2.Tenant) {
		Eventually(func(g Gomega) {
			current := &corev1.Namespace{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: nsName}, current)).Should(Succeed())

			g.Expect(current.Labels).To(HaveKeyWithValue(meta.TenantLabel, originalTenant.GetName()))
			g.Expect(current.Labels).NotTo(HaveKeyWithValue(meta.TenantLabel, attackerTenant.GetName()))

			g.Expect(hasTenantOwnerReferenceByNameAndUID(current, originalTenant.GetName(), originalTenant.GetUID())).
				To(BeTrue(), "namespace should keep original tenant ownerReference")

			g.Expect(hasTenantOwnerReferenceByNameAndUID(current, attackerTenant.GetName(), attackerTenant.GetUID())).
				To(BeFalse(), "namespace must not gain attacker tenant ownerReference")
		}).Should(Succeed())
	}

	kubeSystem := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kube-system",
		},
	}

	getTenant := func(name string) *capsulev1beta2.Tenant {
		tenant := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: name}, tenant)).Should(Succeed())

		return tenant
	}

	getNamespace := func(name string) *corev1.Namespace {
		ns := &corev1.Namespace{}
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: name}, ns)).Should(Succeed())

		return ns
	}

	hasTenantOwnerReference := func(ns *corev1.Namespace, tenant *capsulev1beta2.Tenant) bool {
		for _, ownerRef := range ns.OwnerReferences {
			if ownerRef.APIVersion == capsulev1beta2.GroupVersion.String() &&
				ownerRef.Kind == "Tenant" &&
				ownerRef.Name == tenant.GetName() &&
				ownerRef.UID == tenant.GetUID() {
				return true
			}
		}

		return false
	}

	hasTenantOwnerReferenceByNameAndUID := func(ns *corev1.Namespace, name string, uid types.UID) bool {
		for _, ownerRef := range ns.OwnerReferences {
			if ownerRef.APIVersion == capsulev1beta2.GroupVersion.String() &&
				ownerRef.Kind == "Tenant" &&
				ownerRef.Name == name &&
				ownerRef.UID == uid {
				return true
			}
		}

		return false
	}

	expectOriginalTenantOwnership := func(nsName string, tenant *capsulev1beta2.Tenant) {
		retrievedNs := getNamespace(nsName)

		Expect(retrievedNs.Labels).To(HaveKeyWithValue(meta.TenantLabel, tenant.GetName()))
		Expect(hasTenantOwnerReference(retrievedNs, tenant)).To(BeTrue(), "Namespace should keep original Tenant ownerReference")
	}

	expectNoTenantOwnership := func(nsName string, tenant *capsulev1beta2.Tenant) {
		retrievedNs := getNamespace(nsName)

		Expect(retrievedNs.Labels).NotTo(HaveKeyWithValue(meta.TenantLabel, tenant.GetName()))
		Expect(hasTenantOwnerReference(retrievedNs, tenant)).To(BeFalse(), "Namespace should not have Tenant ownerReference")
	}

	expectNoTenantOwnerReference := func(nsName string, tenant *capsulev1beta2.Tenant) {
		retrievedNs := getNamespace(nsName)

		Expect(hasTenantOwnerReference(retrievedNs, tenant)).To(BeFalse(), "Namespace should not have Tenant ownerReference")
	}

	randomTenantReference := func() (string, types.UID) {
		return fmt.Sprintf("random-tenant-%d", rand.Int()), types.UID(fmt.Sprintf("%d", rand.Int()))
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			t1.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), t1)
		}).Should(Succeed())
		TenantReady(t1, metav1.ConditionTrue, defaultTimeoutInterval)

		EventuallyCreation(func() error {
			t2.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), t2)
		}).Should(Succeed())
		TenantReady(t2, metav1.ConditionTrue, defaultTimeoutInterval)

		EventuallyCreation(func() error {
			t3.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), t3)
		}).Should(Succeed())
		TenantReady(t3, metav1.ConditionTrue, defaultTimeoutInterval)

	})

	JustAfterEach(func() {
		EventuallyDeletion(t1)
		EventuallyDeletion(t2)
		EventuallyDeletion(t3)
	})

	It("Owners can not hijack Tenant ownership through namespaces/status", func() {
		tenantA := getTenant(t1.Name)
		tenantB := getTenant(t2.Name)

		owner := t1.Spec.Owners[0].UserSpec
		cs := ownerClient(owner)

		grantName := "e2e-ns-status-hijack"
		grantNamespaceSubresourceUpdate(grantName, rbacv1.Subject{
			APIGroup: rbacv1.GroupName,
			Kind:     rbacv1.UserKind,
			Name:     owner.Name,
		})
		DeferCleanup(func() {
			cleanupNamespaceSubresourceGrant(grantName)
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tenantA.GetName(),
		})

		NamespaceCreation(ns, owner, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(t1, ns).Should(Succeed())

		current, err := cs.CoreV1().Namespaces().Get(
			context.TODO(),
			ns.Name,
			metav1.GetOptions{},
		)
		Expect(err).ToNot(HaveOccurred())

		hijacked := current.DeepCopy()

		if hijacked.Labels == nil {
			hijacked.Labels = map[string]string{}
		}

		hijacked.Labels[meta.TenantLabel] = tenantB.GetName()

		replaced := false
		for i := range hijacked.OwnerReferences {
			ref := &hijacked.OwnerReferences[i]

			if ref.APIVersion != capsulev1beta2.GroupVersion.String() {
				continue
			}

			if ref.Kind != "Tenant" {
				continue
			}

			if ref.Name != tenantA.GetName() {
				continue
			}

			if ref.UID != tenantA.GetUID() {
				continue
			}

			ref.Name = tenantB.GetName()
			ref.UID = tenantB.GetUID()
			replaced = true

			break
		}

		Expect(replaced).To(BeTrue(), "expected test namespace to contain Tenant ownerReference for %q", tenantA.GetName())

		Expect(hasTenantOwnerReferenceByNameAndUID(hijacked, tenantA.GetName(), tenantA.GetUID())).
			To(BeFalse(), "hijack payload should no longer contain original Tenant ownerReference")
		Expect(hasTenantOwnerReferenceByNameAndUID(hijacked, tenantB.GetName(), tenantB.GetUID())).
			To(BeTrue(), "hijack payload should contain attacker Tenant ownerReference")

		_, err = cs.CoreV1().Namespaces().UpdateStatus(
			context.TODO(),
			hijacked,
			metav1.UpdateOptions{},
		)

		if err != nil {
			By(fmt.Sprintf("namespaces/status hijack attempt was rejected: %v", err))
		}

		expectNoTenantHijackPersisted(ns.Name, tenantA, tenantB)
	})
	It("Owners can not hijack Tenant ownership through namespaces/finalize", func() {
		tenantA := getTenant(t1.Name)
		tenantB := getTenant(t2.Name)

		owner := t1.Spec.Owners[0].UserSpec
		cs := ownerClient(owner)

		grantName := "e2e-ns-finalize-hijack"
		grantNamespaceSubresourceUpdate(grantName, rbacv1.Subject{
			APIGroup: rbacv1.GroupName,
			Kind:     rbacv1.UserKind,
			Name:     owner.Name,
		})
		DeferCleanup(func() {
			cleanupNamespaceSubresourceGrant(grantName)
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tenantA.GetName(),
		})

		NamespaceCreation(ns, owner, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(t1, ns).Should(Succeed())

		current, err := cs.CoreV1().Namespaces().Get(
			context.TODO(),
			ns.Name,
			metav1.GetOptions{},
		)
		Expect(err).ToNot(HaveOccurred())

		hijacked := current.DeepCopy()

		if hijacked.Labels == nil {
			hijacked.Labels = map[string]string{}
		}

		hijacked.Labels[meta.TenantLabel] = tenantB.GetName()

		replaced := false
		for i := range hijacked.OwnerReferences {
			ref := &hijacked.OwnerReferences[i]

			if ref.APIVersion != capsulev1beta2.GroupVersion.String() {
				continue
			}

			if ref.Kind != "Tenant" {
				continue
			}

			if ref.Name != tenantA.GetName() {
				continue
			}

			if ref.UID != tenantA.GetUID() {
				continue
			}

			ref.Name = tenantB.GetName()
			ref.UID = tenantB.GetUID()
			replaced = true

			break
		}

		Expect(replaced).To(BeTrue(), "expected test namespace to contain Tenant ownerReference for %q", tenantA.GetName())

		Expect(hasTenantOwnerReferenceByNameAndUID(hijacked, tenantA.GetName(), tenantA.GetUID())).
			To(BeFalse(), "hijack payload should no longer contain original Tenant ownerReference")
		Expect(hasTenantOwnerReferenceByNameAndUID(hijacked, tenantB.GetName(), tenantB.GetUID())).
			To(BeTrue(), "hijack payload should contain attacker Tenant ownerReference")

		_, err = cs.CoreV1().Namespaces().Finalize(
			context.TODO(),
			hijacked,
			metav1.UpdateOptions{},
		)

		if err != nil {
			By(fmt.Sprintf("namespaces/finalize hijack attempt was rejected: %v", err))
		}

		expectNoTenantHijackPersisted(ns.Name, tenantA, tenantB)
	})

	It("Owners can not add a second Tenant ownerReference to a managed namespace", func() {
		tenantA := getTenant(t1.Name)
		tenantB := getTenant(t2.Name)

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tenantA.GetName(),
			})

			NamespaceCreation(ns, owner.UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(t1, ns).Should(Succeed())

			current, err := cs.CoreV1().Namespaces().Get(
				context.TODO(),
				ns.Name,
				metav1.GetOptions{},
			)
			Expect(err).ToNot(HaveOccurred())

			current.OwnerReferences = append(current.OwnerReferences, metav1.OwnerReference{
				APIVersion: capsulev1beta2.GroupVersion.String(),
				Kind:       "Tenant",
				Name:       tenantB.GetName(),
				UID:        tenantB.GetUID(),
			})

			_, err = cs.CoreV1().Namespaces().Update(
				context.TODO(),
				current,
				metav1.UpdateOptions{},
			)

			Expect(err).To(HaveOccurred())

			Eventually(func(g Gomega) {
				updated := &corev1.Namespace{}

				err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: ns.Name},
					updated,
				)
				g.Expect(err).ToNot(HaveOccurred())

				g.Expect(tenantOwnerReferences(updated)).To(Equal([]string{tenantA.GetName()}))
				g.Expect(updated.Labels).To(HaveKeyWithValue(meta.TenantLabel, tenantA.GetName()))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})

	It("Owners can not hijack unmanaged namespaces with multiple Tenant ownerReferences", func() {
		tenantA := getTenant(t1.Name)
		tenantB := getTenant(t2.Name)

		unmanaged := NewNamespace("")
		Expect(k8sClient.Create(context.TODO(), unmanaged)).Should(Succeed())

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			patch := []byte(fmt.Sprintf(`{
			"metadata": {
				"ownerReferences": [
					{
						"apiVersion": "%s",
						"kind": "Tenant",
						"name": "%s",
						"uid": "%s"
					},
					{
						"apiVersion": "%s",
						"kind": "Tenant",
						"name": "%s",
						"uid": "%s"
					}
				]
			}
		}`,
				capsulev1beta2.GroupVersion.String(),
				tenantA.GetName(),
				tenantA.GetUID(),
				capsulev1beta2.GroupVersion.String(),
				tenantB.GetName(),
				tenantB.GetUID(),
			))

			_, err := cs.CoreV1().Namespaces().Patch(
				context.TODO(),
				unmanaged.Name,
				types.StrategicMergePatchType,
				patch,
				metav1.PatchOptions{},
			)

			Expect(err).To(HaveOccurred())

			Eventually(func(g Gomega) {
				updated := &corev1.Namespace{}

				err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: unmanaged.Name},
					updated,
				)
				g.Expect(err).ToNot(HaveOccurred())

				g.Expect(tenantOwnerReferences(updated)).To(BeEmpty())
				g.Expect(updated.Labels).ToNot(HaveKey(meta.TenantLabel))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})

	It("Tenant A owners can not adopt unmanaged namespaces into Tenant B", func() {
		tenantB := getTenant(t3.Name)

		unmanaged := NewNamespace("")
		Expect(k8sClient.Create(context.TODO(), unmanaged)).Should(Succeed())

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			patch := []byte(fmt.Sprintf(
				`{"metadata":{"labels":{"%s":"%s"},"ownerReferences":[{"apiVersion":"%s","kind":"Tenant","name":"%s","uid":"%s"}]}}`,
				meta.TenantLabel,
				tenantB.GetName(),
				capsulev1beta2.GroupVersion.String(),
				tenantB.GetName(),
				tenantB.GetUID(),
			))

			_, err := cs.CoreV1().Namespaces().Patch(
				context.TODO(),
				unmanaged.Name,
				types.StrategicMergePatchType,
				patch,
				metav1.PatchOptions{},
			)

			Expect(err).To(HaveOccurred())
			expectNoTenantOwnership(unmanaged.Name, tenantB)
		}
	})

	It("Owners can not hijack unmanaged namespaces with controller ownerReference flags", func() {
		tenant := getTenant(t1.Name)

		unmanaged := NewNamespace("")
		Expect(k8sClient.Create(context.TODO(), unmanaged)).Should(Succeed())

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			patch := []byte(fmt.Sprintf(`{
			"metadata":{
				"ownerReferences":[{
					"apiVersion":"%s",
					"kind":"Tenant",
					"name":"%s",
					"uid":"%s",
					"controller":true,
					"blockOwnerDeletion":true
				}]
			}
		}`, capsulev1beta2.GroupVersion.String(), tenant.GetName(), tenant.GetUID()))

			_, err := cs.CoreV1().Namespaces().Patch(
				context.TODO(),
				unmanaged.Name,
				types.StrategicMergePatchType,
				patch,
				metav1.PatchOptions{},
			)

			Expect(err).To(HaveOccurred())
			expectNoTenantOwnership(unmanaged.Name, tenant)
		}
	})

	It("Owners can not smuggle Tenant ownerReference beside unrelated ownerReferences", func() {
		tenant := getTenant(t1.Name)

		unmanaged := NewNamespace("")
		Expect(k8sClient.Create(context.TODO(), unmanaged)).Should(Succeed())

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			patch := []byte(fmt.Sprintf(`{
			"metadata":{
				"ownerReferences":[
					{"apiVersion":"v1","kind":"ConfigMap","name":"dummy","uid":"%s"},
					{"apiVersion":"%s","kind":"Tenant","name":"%s","uid":"%s"}
				]
			}
		}`, types.UID("12345"), capsulev1beta2.GroupVersion.String(), tenant.GetName(), tenant.GetUID()))

			_, err := cs.CoreV1().Namespaces().Patch(
				context.TODO(),
				unmanaged.Name,
				types.StrategicMergePatchType,
				patch,
				metav1.PatchOptions{},
			)

			Expect(err).To(HaveOccurred())
			expectNoTenantOwnership(unmanaged.Name, tenant)
		}
	})

	It("Owners can not create an ownership gap then patch managed namespace metadata", func() {
		tenant := getTenant(t1.Name)

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			ns := NewNamespace("", map[string]string{meta.TenantLabel: tenant.GetName()})
			NamespaceCreation(ns, owner.UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(t1, ns).Should(Succeed())

			remove := []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":null}}}`, meta.TenantLabel))
			_, err := cs.CoreV1().Namespaces().Patch(
				context.TODO(),
				ns.Name,
				types.StrategicMergePatchType,
				remove,
				metav1.PatchOptions{},
			)
			Expect(err).ToNot(HaveOccurred())
			expectOriginalTenantOwnership(ns.Name, tenant)

			patch := []byte(`{"metadata":{"labels":{"attacker.example.com/touched":"true"}}}`)
			_, err = cs.CoreV1().Namespaces().Patch(
				context.TODO(),
				ns.Name,
				types.StrategicMergePatchType,
				patch,
				metav1.PatchOptions{},
			)
			Expect(err).ToNot(HaveOccurred())
			expectOriginalTenantOwnership(ns.Name, tenant)
		}
	})

	It("Can't hijack offlimits namespace (Ownerreferences)", func() {
		tenant := getTenant(t1.Name)

		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: kubeSystem.GetName()}, kubeSystem)).Should(Succeed())

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			patch := []byte(fmt.Sprintf(
				`{"metadata":{"ownerReferences":[{"apiVersion":"%s","kind":"Tenant","name":"%s","uid":"%s"}]}}`,
				capsulev1beta2.GroupVersion.String(),
				tenant.GetName(),
				tenant.GetUID(),
			))

			_, err := cs.CoreV1().Namespaces().Patch(context.TODO(), kubeSystem.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
			Expect(err).To(HaveOccurred())
		}
	})

	It("Can't hijack offlimits namespace (Labels)", func() {
		tenant := getTenant(t1.Name)

		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: kubeSystem.GetName()}, kubeSystem)).Should(Succeed())

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			patch := []byte(fmt.Sprintf(
				`{"metadata":{"labels":{"%s":"%s"}}}`,
				meta.TenantLabel,
				tenant.GetName(),
			))

			_, err := cs.CoreV1().Namespaces().Patch(context.TODO(), kubeSystem.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
			Expect(err).To(HaveOccurred())
		}
	})

	It("Can't hijack offlimits namespace (Annotations)", func() {
		tenant := getTenant(t1.Name)

		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: kubeSystem.GetName()}, kubeSystem)).Should(Succeed())

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			patch := []byte(fmt.Sprintf(
				`{"metadata":{"annotations":{"%s":"%s"}}}`,
				meta.TenantLabel,
				tenant.GetName(),
			))

			_, err := cs.CoreV1().Namespaces().Patch(context.TODO(), kubeSystem.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
			Expect(err).To(HaveOccurred())
		}
	})

	It("Owners can not hijack unmanaged namespaces using JSONPatch add label", func() {
		tenant := getTenant(t1.Name)

		unmanaged := NewNamespace("")
		Expect(k8sClient.Create(context.TODO(), unmanaged)).Should(Succeed())

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			patch := []byte(fmt.Sprintf(`[
			{"op":"add","path":"/metadata/labels","value":{}},
			{"op":"add","path":"/metadata/labels/%s","value":"%s"}
		]`, clt.EscapeJSONPointer(meta.TenantLabel), tenant.GetName()))

			_, err := cs.CoreV1().Namespaces().Patch(
				context.TODO(),
				unmanaged.Name,
				types.JSONPatchType,
				patch,
				metav1.PatchOptions{},
			)

			Expect(err).To(HaveOccurred())
			expectNoTenantOwnership(unmanaged.Name, tenant)
		}
	})

	It("Owners can not hijack unmanaged namespaces using JSONPatch add ownerReference", func() {
		tenant := getTenant(t1.Name)

		unmanaged := NewNamespace("")
		Expect(k8sClient.Create(context.TODO(), unmanaged)).Should(Succeed())

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			patch := []byte(fmt.Sprintf(`[
			{"op":"add","path":"/metadata/ownerReferences","value":[
				{"apiVersion":"%s","kind":"Tenant","name":"%s","uid":"%s"}
			]}
		]`, capsulev1beta2.GroupVersion.String(), tenant.GetName(), tenant.GetUID()))

			_, err := cs.CoreV1().Namespaces().Patch(
				context.TODO(),
				unmanaged.Name,
				types.JSONPatchType,
				patch,
				metav1.PatchOptions{},
			)

			Expect(err).To(HaveOccurred())
			expectNoTenantOwnership(unmanaged.Name, tenant)
		}
	})

	It("Owners can not hijack unmanaged namespaces with matching label and forged ownerReference UID", func() {
		tenant := getTenant(t1.Name)

		unmanaged := NewNamespace("")
		Expect(k8sClient.Create(context.TODO(), unmanaged)).Should(Succeed())

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			_, fakeUID := randomTenantReference()
			patch := []byte(fmt.Sprintf(
				`{"metadata":{"labels":{"%s":"%s"},"ownerReferences":[{"apiVersion":"%s","kind":"Tenant","name":"%s","uid":"%s"}]}}`,
				meta.TenantLabel,
				tenant.GetName(),
				capsulev1beta2.GroupVersion.String(),
				tenant.GetName(),
				fakeUID,
			))

			_, err := cs.CoreV1().Namespaces().Patch(
				context.TODO(),
				unmanaged.Name,
				types.StrategicMergePatchType,
				patch,
				metav1.PatchOptions{},
			)

			Expect(err).To(HaveOccurred())
			expectNoTenantOwnership(unmanaged.Name, tenant)
		}
	})

	It("Owners can not hijack unmanaged namespaces using server-side apply", func() {
		tenant := getTenant(t1.Name)

		unmanaged := NewNamespace("")
		Expect(k8sClient.Create(context.TODO(), unmanaged)).Should(Succeed())

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			apply := []byte(fmt.Sprintf(`{
			"apiVersion":"v1",
			"kind":"Namespace",
			"metadata":{
				"name":"%s",
				"labels":{"%s":"%s"},
				"ownerReferences":[{
					"apiVersion":"%s",
					"kind":"Tenant",
					"name":"%s",
					"uid":"%s"
				}]
			}
		}`,
				unmanaged.Name,
				meta.TenantLabel,
				tenant.GetName(),
				capsulev1beta2.GroupVersion.String(),
				tenant.GetName(),
				tenant.GetUID(),
			))

			_, err := cs.CoreV1().Namespaces().Patch(
				context.TODO(),
				unmanaged.Name,
				types.ApplyPatchType,
				apply,
				metav1.PatchOptions{
					FieldManager: "attacker",
					Force:        ptr.To(true),
				},
			)

			Expect(err).To(HaveOccurred())
			expectNoTenantOwnership(unmanaged.Name, tenant)
		}
	})

	It("Owners can not combine status and metadata patches to adopt unmanaged namespaces", func() {
		tenant := getTenant(t1.Name)
		createNamespaceStatusRBACForOwner(tenant)
		DeferCleanup(func(tnt *capsulev1beta2.Tenant) {
			deleteNamespaceStatusRBACForOwner(tnt)
		}, tenant)

		unmanaged := NewNamespace("")
		Expect(k8sClient.Create(context.TODO(), unmanaged)).Should(Succeed())

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			statusNs, err := cs.CoreV1().Namespaces().Get(context.TODO(), unmanaged.Name, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			if statusNs.Labels == nil {
				statusNs.Labels = map[string]string{}
			}

			statusNs.Labels[meta.TenantLabel] = tenant.GetName()
			statusNs.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: capsulev1beta2.GroupVersion.String(),
				Kind:       "Tenant",
				Name:       tenant.GetName(),
				UID:        tenant.GetUID(),
			}}

			_, _ = cs.CoreV1().Namespaces().UpdateStatus(context.TODO(), statusNs, metav1.UpdateOptions{})

			patch := []byte(fmt.Sprintf(
				`{"metadata":{"labels":{"%s":"%s"}}}`,
				meta.TenantLabel,
				tenant.GetName(),
			))

			_, err = cs.CoreV1().Namespaces().Patch(
				context.TODO(),
				unmanaged.Name,
				types.StrategicMergePatchType,
				patch,
				metav1.PatchOptions{},
			)

			Expect(err).To(HaveOccurred())
			expectNoTenantOwnership(unmanaged.Name, tenant)
		}
	})

	It("Owners can not hijack unmanaged namespaces with valid ownerReference and mismatching label", func() {
		tenant := getTenant(t1.Name)

		unmanaged := NewNamespace("")
		Expect(k8sClient.Create(context.TODO(), unmanaged)).Should(Succeed())

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			patch := []byte(fmt.Sprintf(
				`{"metadata":{"labels":{"%s":"not-%s"},"ownerReferences":[{"apiVersion":"%s","kind":"Tenant","name":"%s","uid":"%s"}]}}`,
				meta.TenantLabel,
				tenant.GetName(),
				capsulev1beta2.GroupVersion.String(),
				tenant.GetName(),
				tenant.GetUID(),
			))

			_, err := cs.CoreV1().Namespaces().Patch(
				context.TODO(),
				unmanaged.Name,
				types.StrategicMergePatchType,
				patch,
				metav1.PatchOptions{},
			)

			Expect(err).To(HaveOccurred())
			expectNoTenantOwnership(unmanaged.Name, tenant)
		}
	})

	It("Owners can patch managed namespaces but ownerReference changes should be reverted", func() {
		tenant := getTenant(t1.Name)

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tenant.GetName(),
			})
			NamespaceCreation(ns, owner.UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(t1, ns).Should(Succeed())

			randomName, randomUID := randomTenantReference()

			patch := []byte(fmt.Sprintf(
				`{"metadata":{"ownerReferences":[{"apiVersion":"%s","kind":"Tenant","name":"%s","uid":"%s"}]}}`,
				capsulev1beta2.GroupVersion.String(),
				randomName,
				randomUID,
			))

			_, err := cs.CoreV1().Namespaces().Patch(context.TODO(), ns.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
			Expect(err).ToNot(HaveOccurred())

			retrievedNs := getNamespace(ns.Name)

			Expect(hasTenantOwnerReferenceByNameAndUID(retrievedNs, randomName, randomUID)).To(BeFalse(), "Namespace should not keep patched Tenant ownerReference")
			Expect(hasTenantOwnerReference(retrievedNs, tenant)).To(BeTrue(), "Namespace should keep original Tenant ownerReference")
		}
	})

	It("Owners can patch managed namespaces but tenant label changes should be reverted", func() {
		tenant := getTenant(t1.Name)

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tenant.GetName(),
			})
			NamespaceCreation(ns, owner.UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(t1, ns).Should(Succeed())

			randomName, _ := randomTenantReference()

			patch := []byte(fmt.Sprintf(
				`{"metadata":{"labels":{"%s":"%s"}}}`,
				meta.TenantLabel,
				randomName,
			))

			_, err := cs.CoreV1().Namespaces().Patch(context.TODO(), ns.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
			Expect(err).ToNot(HaveOccurred())

			expectOriginalTenantOwnership(ns.Name, tenant)
		}
	})

	It("Owners can patch managed namespaces but combined ownership changes should be reverted", func() {
		tenant := getTenant(t1.Name)

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tenant.GetName(),
			})
			NamespaceCreation(ns, owner.UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(t1, ns).Should(Succeed())

			randomName, randomUID := randomTenantReference()

			patch := []byte(fmt.Sprintf(
				`{"metadata":{"labels":{"%s":"%s"},"ownerReferences":[{"apiVersion":"%s","kind":"Tenant","name":"%s","uid":"%s"}]}}`,
				meta.TenantLabel,
				randomName,
				capsulev1beta2.GroupVersion.String(),
				randomName,
				randomUID,
			))

			_, err := cs.CoreV1().Namespaces().Patch(context.TODO(), ns.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
			Expect(err).ToNot(HaveOccurred())

			retrievedNs := getNamespace(ns.Name)

			Expect(retrievedNs.Labels).To(HaveKeyWithValue(meta.TenantLabel, tenant.GetName()))
			Expect(hasTenantOwnerReferenceByNameAndUID(retrievedNs, randomName, randomUID)).To(BeFalse())
			Expect(hasTenantOwnerReference(retrievedNs, tenant)).To(BeTrue())
		}
	})

	It("Owners can not migrate managed namespaces to another Tenant", func() {
		tenantA := getTenant(t1.Name)
		tenantB := getTenant(t2.Name)

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tenantA.GetName(),
			})
			NamespaceCreation(ns, owner.UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(t1, ns).Should(Succeed())

			patch := []byte(fmt.Sprintf(
				`{"metadata":{"labels":{"%s":"%s"},"ownerReferences":[{"apiVersion":"%s","kind":"Tenant","name":"%s","uid":"%s"}]}}`,
				meta.TenantLabel,
				tenantB.GetName(),
				capsulev1beta2.GroupVersion.String(),
				tenantB.GetName(),
				tenantB.GetUID(),
			))

			_, err := cs.CoreV1().Namespaces().Patch(context.TODO(), ns.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
			Expect(err).ToNot(HaveOccurred())

			retrievedNs := getNamespace(ns.Name)

			Expect(retrievedNs.Labels).To(HaveKeyWithValue(meta.TenantLabel, tenantA.GetName()))
			Expect(hasTenantOwnerReference(retrievedNs, tenantA)).To(BeTrue())
			Expect(hasTenantOwnerReference(retrievedNs, tenantB)).To(BeFalse())
		}
	})

	It("Owners can not remove tenant ownership from managed namespaces", func() {
		tenant := getTenant(t1.Name)

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tenant.GetName(),
			})
			NamespaceCreation(ns, owner.UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(t1, ns).Should(Succeed())

			patchRemoveOwnerReferences := []byte(`{"metadata":{"ownerReferences":[]}}`)

			_, err := cs.CoreV1().Namespaces().Patch(context.TODO(), ns.Name, types.StrategicMergePatchType, patchRemoveOwnerReferences, metav1.PatchOptions{})
			Expect(err).ToNot(HaveOccurred())

			expectOriginalTenantOwnership(ns.Name, tenant)

			patchRemoveTenantLabel := []byte(fmt.Sprintf(
				`{"metadata":{"labels":{"%s":null}}}`,
				meta.TenantLabel,
			))

			_, err = cs.CoreV1().Namespaces().Patch(context.TODO(), ns.Name, types.StrategicMergePatchType, patchRemoveTenantLabel, metav1.PatchOptions{})
			Expect(err).ToNot(HaveOccurred())

			expectOriginalTenantOwnership(ns.Name, tenant)
		}
	})

	It("Owners can remove tenant labels from namespaces without Tenant ownerReferences", func() {
		tenant := getTenant(t1.Name)

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			unmanaged := NewNamespace("")
			Expect(k8sClient.Create(context.TODO(), unmanaged)).Should(Succeed())

			patchLabel := []byte(fmt.Sprintf(
				`{"metadata":{"labels":{"%s":"%s"}}}`,
				meta.TenantLabel,
				tenant.GetName(),
			))

			_, err := cs.CoreV1().Namespaces().Patch(context.TODO(), unmanaged.Name, types.StrategicMergePatchType, patchLabel, metav1.PatchOptions{})
			Expect(err).ToNot(HaveOccurred())

			retrievedNs := getNamespace(unmanaged.Name)

			Expect(retrievedNs.Labels).To(HaveKeyWithValue(meta.TenantLabel, tenant.GetName()))
			Expect(hasTenantOwnerReference(retrievedNs, tenant)).To(BeFalse(), "Tenant label alone must not create ownership")

			removeLabel := []byte(fmt.Sprintf(
				`{"metadata":{"labels":{"%s":null}}}`,
				meta.TenantLabel,
			))

			_, err = cs.CoreV1().Namespaces().Patch(context.TODO(), unmanaged.Name, types.StrategicMergePatchType, removeLabel, metav1.PatchOptions{})
			Expect(err).ToNot(HaveOccurred())

			expectNoTenantOwnership(unmanaged.Name, tenant)
		}
	})

	It("Owners can not patch unmanaged namespaces into a Tenant with ownerReferences", func() {
		tenant := getTenant(t1.Name)

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			unmanaged := NewNamespace("")
			Expect(k8sClient.Create(context.TODO(), unmanaged)).Should(Succeed())

			patchOwnerReference := []byte(fmt.Sprintf(
				`{"metadata":{"ownerReferences":[{"apiVersion":"%s","kind":"Tenant","name":"%s","uid":"%s"}]}}`,
				capsulev1beta2.GroupVersion.String(),
				tenant.GetName(),
				tenant.GetUID(),
			))

			_, err := cs.CoreV1().Namespaces().Patch(context.TODO(), unmanaged.Name, types.StrategicMergePatchType, patchOwnerReference, metav1.PatchOptions{})
			Expect(err).To(HaveOccurred())

			expectNoTenantOwnerReference(unmanaged.Name, tenant)
		}
	})

	It("Namespace status updates by owners can not change tenant ownerReferences", func() {
		tenant := getTenant(t1.Name)

		createNamespaceStatusRBACForOwner(tenant)
		DeferCleanup(func(tnt *capsulev1beta2.Tenant) {
			deleteNamespaceStatusRBACForOwner(tnt)
		}, tenant)

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tenant.GetName(),
			})
			NamespaceCreation(ns, owner.UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(t1, ns).Should(Succeed())

			randomName, randomUID := randomTenantReference()

			statusNs, err := cs.CoreV1().Namespaces().Get(context.TODO(), ns.Name, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			statusNs.OwnerReferences = []metav1.OwnerReference{
				{
					APIVersion: capsulev1beta2.GroupVersion.String(),
					Kind:       "Tenant",
					Name:       randomName,
					UID:        randomUID,
				},
			}

			_, err = cs.CoreV1().Namespaces().UpdateStatus(context.TODO(), statusNs, metav1.UpdateOptions{})
			if err != nil {
				expectOriginalTenantOwnership(ns.Name, tenant)

				continue
			}

			retrievedNs := getNamespace(ns.Name)

			Expect(hasTenantOwnerReferenceByNameAndUID(retrievedNs, randomName, randomUID)).To(BeFalse(), "Namespace status update must not change Tenant ownerReference")
			Expect(hasTenantOwnerReference(retrievedNs, tenant)).To(BeTrue(), "Namespace should keep original Tenant ownerReference")
		}
	})

	It("Namespace status updates by owners can not change tenant labels", func() {
		tenant := getTenant(t1.Name)

		createNamespaceStatusRBACForOwner(tenant)
		DeferCleanup(func(tnt *capsulev1beta2.Tenant) {
			deleteNamespaceStatusRBACForOwner(tnt)
		}, tenant)

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tenant.GetName(),
			})
			NamespaceCreation(ns, owner.UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(t1, ns).Should(Succeed())

			randomName, _ := randomTenantReference()

			statusNs, err := cs.CoreV1().Namespaces().Get(context.TODO(), ns.Name, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			if statusNs.Labels == nil {
				statusNs.Labels = map[string]string{}
			}

			statusNs.Labels[meta.TenantLabel] = randomName

			_, err = cs.CoreV1().Namespaces().UpdateStatus(context.TODO(), statusNs, metav1.UpdateOptions{})
			if err != nil {
				expectOriginalTenantOwnership(ns.Name, tenant)

				continue
			}

			expectOriginalTenantOwnership(ns.Name, tenant)
		}
	})

	It("Namespace status updates by owners can not migrate namespaces to another Tenant", func() {
		tenantA := getTenant(t1.Name)
		tenantB := getTenant(t2.Name)

		createNamespaceStatusRBACForOwner(tenantA)
		DeferCleanup(func(tnt *capsulev1beta2.Tenant) {
			deleteNamespaceStatusRBACForOwner(tnt)
		}, tenantA)

		createNamespaceStatusRBACForOwner(tenantB)
		DeferCleanup(func(tnt *capsulev1beta2.Tenant) {
			deleteNamespaceStatusRBACForOwner(tnt)
		}, tenantB)

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tenantA.GetName(),
			})
			NamespaceCreation(ns, owner.UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(t1, ns).Should(Succeed())

			statusNs, err := cs.CoreV1().Namespaces().Get(context.TODO(), ns.Name, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			if statusNs.Labels == nil {
				statusNs.Labels = map[string]string{}
			}

			statusNs.Labels[meta.TenantLabel] = tenantB.GetName()
			statusNs.OwnerReferences = []metav1.OwnerReference{
				{
					APIVersion: capsulev1beta2.GroupVersion.String(),
					Kind:       "Tenant",
					Name:       tenantB.GetName(),
					UID:        tenantB.GetUID(),
				},
			}

			_, err = cs.CoreV1().Namespaces().UpdateStatus(context.TODO(), statusNs, metav1.UpdateOptions{})
			if err != nil {
				retrievedNs := getNamespace(ns.Name)

				Expect(retrievedNs.Labels).To(HaveKeyWithValue(meta.TenantLabel, tenantA.GetName()))
				Expect(hasTenantOwnerReference(retrievedNs, tenantA)).To(BeTrue())
				Expect(hasTenantOwnerReference(retrievedNs, tenantB)).To(BeFalse())

				continue
			}

			retrievedNs := getNamespace(ns.Name)

			Expect(retrievedNs.Labels).To(HaveKeyWithValue(meta.TenantLabel, tenantA.GetName()))
			Expect(hasTenantOwnerReference(retrievedNs, tenantA)).To(BeTrue())
			Expect(hasTenantOwnerReference(retrievedNs, tenantB)).To(BeFalse())
		}
	})

	It("Namespace status updates by owners can not patch unmanaged namespaces into a Tenant", func() {
		tenant := getTenant(t1.Name)

		createNamespaceStatusRBACForOwner(tenant)
		DeferCleanup(func(tnt *capsulev1beta2.Tenant) {
			deleteNamespaceStatusRBACForOwner(tnt)
		}, tenant)

		unmanaged := NewNamespace("")
		Expect(k8sClient.Create(context.TODO(), unmanaged)).Should(Succeed())

		for _, owner := range t1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			statusNs, err := cs.CoreV1().Namespaces().Get(context.TODO(), unmanaged.Name, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			if statusNs.Labels == nil {
				statusNs.Labels = map[string]string{}
			}

			statusNs.Labels[meta.TenantLabel] = tenant.GetName()
			statusNs.OwnerReferences = []metav1.OwnerReference{
				{
					APIVersion: capsulev1beta2.GroupVersion.String(),
					Kind:       "Tenant",
					Name:       tenant.GetName(),
					UID:        tenant.GetUID(),
				},
			}

			_, err = cs.CoreV1().Namespaces().UpdateStatus(context.TODO(), statusNs, metav1.UpdateOptions{})
			if err != nil {
				expectNoTenantOwnership(unmanaged.Name, tenant)

				continue
			}

			expectNoTenantOwnership(unmanaged.Name, tenant)
		}
	})
})

func createNamespaceStatusRBACForOwner(tnt *capsulev1beta2.Tenant) {
	name := "namespace-status-patch-" + tnt.GetName()

	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{
					"namespaces",
					"namespaces/status",
				},
				Verbs: []string{
					"get",
					"patch",
					"update",
				},
			},
		},
	}

	err := k8sClient.Create(context.TODO(), clusterRole)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}

	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     name,
		},
	}

	for _, sub := range tnt.Spec.Owners {
		if sub.Kind != rbac.ServiceAccountOwner {
			clusterRoleBinding.Subjects = append(clusterRoleBinding.Subjects, rbacv1.Subject{
				Kind: string(sub.Kind),
				Name: sub.Name,
			})
		} else {
			namespace, name, err := serviceaccount.SplitUsername(sub.Name)
			Expect(err).NotTo(HaveOccurred())

			clusterRoleBinding.Subjects = append(clusterRoleBinding.Subjects, rbacv1.Subject{
				Kind:      string(sub.Kind),
				Name:      name,
				Namespace: namespace,
			})
		}
	}

	err = k8sClient.Create(context.TODO(), clusterRoleBinding)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func deleteNamespaceStatusRBACForOwner(tnt *capsulev1beta2.Tenant) {
	name := "namespace-status-patch-" + tnt.GetName()

	err := k8sClient.Delete(context.TODO(), &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	})
	if err != nil && !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}

	err = k8sClient.Delete(context.TODO(), &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	})
	if err != nil && !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func tenantOwnerReferences(ns *corev1.Namespace) []string {
	var refs []string

	for _, ref := range ns.GetOwnerReferences() {
		if tenant.IsTenantOwnerReference(ref) {
			refs = append(refs, ref.Name)
		}
	}

	sort.Strings(refs)

	return refs
}
