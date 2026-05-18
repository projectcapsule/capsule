// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"math/rand"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("creating several Namespaces for a Tenant", Label("namespace", "hijack"), func() {
	tnt_1 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "capsule-ns-attack-1",
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

	tnt_2 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "capsule-ns-attack-2",
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

	randomTenantReference := func() (string, types.UID) {
		return fmt.Sprintf("random-tenant-%d", rand.Int()), types.UID(fmt.Sprintf("%d", rand.Int()))
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			tnt_1.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), tnt_1)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			tnt_2.ResourceVersion = ""

			return k8sClient.Create(context.TODO(), tnt_2)
		}).Should(Succeed())
	})

	JustAfterEach(func() {
		EventuallyDeletion(tnt_1)
		EventuallyDeletion(tnt_2)
	})

	It("Can't hijack offlimits namespace (Ownerreferences)", func() {
		tenant := getTenant(tnt_1.Name)

		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: kubeSystem.GetName()}, kubeSystem)).Should(Succeed())

		for _, owner := range tnt_1.Spec.Owners {
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
		tenant := getTenant(tnt_1.Name)

		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: kubeSystem.GetName()}, kubeSystem)).Should(Succeed())

		for _, owner := range tnt_1.Spec.Owners {
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
		tenant := getTenant(tnt_1.Name)

		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: kubeSystem.GetName()}, kubeSystem)).Should(Succeed())

		for _, owner := range tnt_1.Spec.Owners {
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

	It("Owners can patch managed namespaces but ownerReference changes should be reverted", func() {
		tenant := getTenant(tnt_1.Name)

		for _, owner := range tnt_1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tenant.GetName(),
			})
			NamespaceCreation(ns, owner.UserSpec, defaultTimeoutInterval).Should(Succeed())

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
		tenant := getTenant(tnt_1.Name)

		for _, owner := range tnt_1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tenant.GetName(),
			})
			NamespaceCreation(ns, owner.UserSpec, defaultTimeoutInterval).Should(Succeed())

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
		tenant := getTenant(tnt_1.Name)

		for _, owner := range tnt_1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tenant.GetName(),
			})
			NamespaceCreation(ns, owner.UserSpec, defaultTimeoutInterval).Should(Succeed())

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
		tenantA := getTenant(tnt_1.Name)
		tenantB := getTenant(tnt_2.Name)

		for _, owner := range tnt_1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tenantA.GetName(),
			})
			NamespaceCreation(ns, owner.UserSpec, defaultTimeoutInterval).Should(Succeed())

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
		tenant := getTenant(tnt_1.Name)

		for _, owner := range tnt_1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tenant.GetName(),
			})
			NamespaceCreation(ns, owner.UserSpec, defaultTimeoutInterval).Should(Succeed())

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

	It("Owners can not patch unmanaged namespaces into a Tenant", func() {
		tenant := getTenant(tnt_1.Name)

		unmanaged := NewNamespace("")
		Expect(k8sClient.Create(context.TODO(), unmanaged)).Should(Succeed())

		for _, owner := range tnt_1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			patchLabel := []byte(fmt.Sprintf(
				`{"metadata":{"labels":{"%s":"%s"}}}`,
				meta.TenantLabel,
				tenant.GetName(),
			))

			_, err := cs.CoreV1().Namespaces().Patch(context.TODO(), unmanaged.Name, types.StrategicMergePatchType, patchLabel, metav1.PatchOptions{})
			Expect(err).To(HaveOccurred())

			patchOwnerReference := []byte(fmt.Sprintf(
				`{"metadata":{"ownerReferences":[{"apiVersion":"%s","kind":"Tenant","name":"%s","uid":"%s"}]}}`,
				capsulev1beta2.GroupVersion.String(),
				tenant.GetName(),
				tenant.GetUID(),
			))

			_, err = cs.CoreV1().Namespaces().Patch(context.TODO(), unmanaged.Name, types.StrategicMergePatchType, patchOwnerReference, metav1.PatchOptions{})
			Expect(err).To(HaveOccurred())

			expectNoTenantOwnership(unmanaged.Name, tenant)
		}
	})

	It("Namespace status updates by owners can not change tenant ownerReferences", func() {
		tenant := getTenant(tnt_1.Name)

		createNamespaceStatusRBACForOwner(tenant)
		DeferCleanup(func(tnt *capsulev1beta2.Tenant) {
			deleteNamespaceStatusRBACForOwner(tnt)
		}, tenant)

		for _, owner := range tnt_1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tenant.GetName(),
			})
			NamespaceCreation(ns, owner.UserSpec, defaultTimeoutInterval).Should(Succeed())

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
		tenant := getTenant(tnt_1.Name)

		createNamespaceStatusRBACForOwner(tenant)
		DeferCleanup(func(tnt *capsulev1beta2.Tenant) {
			deleteNamespaceStatusRBACForOwner(tnt)
		}, tenant)

		for _, owner := range tnt_1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tenant.GetName(),
			})
			NamespaceCreation(ns, owner.UserSpec, defaultTimeoutInterval).Should(Succeed())

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
		tenantA := getTenant(tnt_1.Name)
		tenantB := getTenant(tnt_2.Name)

		createNamespaceStatusRBACForOwner(tenantA)
		DeferCleanup(func(tnt *capsulev1beta2.Tenant) {
			deleteNamespaceStatusRBACForOwner(tnt)
		}, tenantA)

		createNamespaceStatusRBACForOwner(tenantB)
		DeferCleanup(func(tnt *capsulev1beta2.Tenant) {
			deleteNamespaceStatusRBACForOwner(tnt)
		}, tenantB)

		for _, owner := range tnt_1.Spec.Owners {
			cs := ownerClient(owner.UserSpec)

			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: tenantA.GetName(),
			})
			NamespaceCreation(ns, owner.UserSpec, defaultTimeoutInterval).Should(Succeed())

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
		tenant := getTenant(tnt_1.Name)

		createNamespaceStatusRBACForOwner(tenant)
		DeferCleanup(func(tnt *capsulev1beta2.Tenant) {
			deleteNamespaceStatusRBACForOwner(tnt)
		}, tenant)

		unmanaged := NewNamespace("")
		Expect(k8sClient.Create(context.TODO(), unmanaged)).Should(Succeed())

		for _, owner := range tnt_1.Spec.Owners {
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
