// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	versionUtil "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/utils"
)

const (
	defaultTimeoutInterval                    = 60 * time.Second
	defaultTerminationTimeoutInterval         = 60 * time.Second
	defaultPollInterval                       = 2 * time.Second
	defaultConfigurationName                  = "default"
	e2eClientQPS                      float32 = 1000
	e2eClientBurst                    int     = 2000
)

func tuneE2ERestConfig(c *rest.Config) *rest.Config {
	c.QPS = e2eClientQPS
	c.Burst = e2eClientBurst

	return c
}

func mergeMaps(base map[string]string, extra map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func ignoreNotFound(err error) error {
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func NewService(svc types.NamespacedName) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svc.Name,
			Namespace: svc.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Port: int32(80)},
			},
		},
	}
}

func ServiceCreation(svc *corev1.Service, owner rbac.UserSpec, timeout time.Duration) AsyncAssertion {
	cs := ownerClient(owner)
	return Eventually(func() (err error) {
		_, err = cs.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
		return
	}, timeout, defaultPollInterval)
}

func NewNamespace(name string, labels ...map[string]string) *corev1.Namespace {
	if len(name) == 0 {
		name = rand.String(10)
	}

	namespaceLabels := make(map[string]string)

	if len(labels) > 0 {
		for _, lab := range labels {
			for k, v := range lab {
				namespaceLabels[k] = v
			}
		}
	}

	namespaceLabels["env"] = "e2e"

	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: namespaceLabels,
		},
	}
}

func NamespaceCreationAdmin(ns *corev1.Namespace, timeout time.Duration) AsyncAssertion {
	return Eventually(func() (err error) {
		return k8sClient.Create(
			context.TODO(),
			ns,
		)
	}, timeout, defaultPollInterval)
}

func NamespaceDeletionAdmin(ns *corev1.Namespace, timeout time.Duration) AsyncAssertion {
	return Eventually(func() (err error) {
		return k8sClient.Delete(
			context.TODO(),
			ns,
		)
	}, timeout, defaultPollInterval)
}

func ForceDeleteNamespace(ctx context.Context, name string) {
	Eventually(func() error {
		ns := &corev1.Namespace{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, ns)
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}

		// Trigger deletion if not already happening
		if ns.DeletionTimestamp.IsZero() {
			if err := k8sClient.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
			return fmt.Errorf("namespace %s deletion triggered", name)
		}

		// Force-remove finalizers (THIS is the key part)
		if len(ns.Finalizers) > 0 {
			ns.Finalizers = nil
			if err := k8sClient.Update(ctx, ns); err != nil {
				return err
			}
			return fmt.Errorf("namespace %s finalizers removed", name)
		}

		// wait until fully gone
		return fmt.Errorf("namespace %s still terminating", name)
	}, defaultTerminationTimeoutInterval, defaultPollInterval).Should(Succeed(),
		"failed to force delete namespace %s", name)
}

func NamespaceCreation(ns *corev1.Namespace, owner rbac.UserSpec, timeout time.Duration) AsyncAssertion {
	cs := ownerClient(owner)
	return Eventually(func() (err error) {
		_, err = cs.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		return
	}, timeout, defaultPollInterval)
}

func TenantNamespaceReady(
	tnt *capsulev1beta2.Tenant,
	ns *corev1.Namespace,
	expectedSize uint,
) {
	Eventually(func(g Gomega) {
		t := &capsulev1beta2.Tenant{}
		err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)
		g.Expect(err).NotTo(HaveOccurred())

		g.Expect(t.Status.Size).To(
			Equal(expectedSize),
			"expected tenant %s status size to be %d, got %d",
			t.GetName(),
			expectedSize,
			t.Status.Size,
		)

		currentNS := &corev1.Namespace{}
		err = k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, currentNS)
		g.Expect(err).NotTo(HaveOccurred())

		instance := t.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{
			Name: currentNS.GetName(),
			UID:  currentNS.GetUID(),
		})
		g.Expect(instance).NotTo(BeNil(), "Namespace instance should not be nil")

		condition := instance.Conditions.GetConditionByType(meta.ReadyCondition)
		g.Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

		g.Expect(instance.Name).To(Equal(currentNS.GetName()))
		g.Expect(condition.Status).To(Equal(metav1.ConditionTrue), "Expected namespace condition status to be True")
		g.Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected namespace condition type to be Ready")
		g.Expect(condition.Reason).To(Equal(meta.SucceededReason), "Expected namespace condition reason to be Succeeded")
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func NamespaceIsNotPartOfTenant(
	tnt *capsulev1beta2.Tenant,
	ns *corev1.Namespace,
) AsyncAssertion {
	return Eventually(func() error {
		currentNS := &corev1.Namespace{}
		nsUID := ns.GetUID()

		if err := k8sClient.Get(
			context.TODO(),
			types.NamespacedName{Name: ns.GetName()},
			currentNS,
		); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to get namespace %s: %w", ns.GetName(), err)
			}
		} else {
			nsUID = currentNS.GetUID()
		}

		t := &capsulev1beta2.Tenant{}
		if err := k8sClient.Get(
			context.TODO(),
			types.NamespacedName{Name: tnt.GetName()},
			t,
		); err != nil {
			return fmt.Errorf("failed to get tenant: %w", err)
		}

		namespaces := t.Status.Namespaces
		if ok, _ := ContainElement(ns.GetName()).Match(namespaces); ok {
			return fmt.Errorf(
				"expected tenant %s not to contain namespace %s, but got: %v",
				t.GetName(),
				ns.GetName(),
				namespaces,
			)
		}

		instance := t.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{
			Name: ns.GetName(),
			UID:  nsUID,
		})
		if instance != nil {
			return fmt.Errorf(
				"expected tenant %s not to contain instance for namespace %s (uid=%s), but got: %+v",
				t.GetName(),
				ns.GetName(),
				nsUID,
				instance,
			)
		}

		return nil
	}, defaultTimeoutInterval, defaultPollInterval)
}

func NamespaceIsPartOfTenant(
	tnt *capsulev1beta2.Tenant,
	ns *corev1.Namespace,
) AsyncAssertion {
	return Eventually(func() error {
		currentNS := &corev1.Namespace{}
		if err := k8sClient.Get(
			context.TODO(),
			types.NamespacedName{Name: ns.GetName()},
			currentNS,
		); err != nil {
			return fmt.Errorf("failed to get namespace %s: %w", ns.GetName(), err)
		}

		t := &capsulev1beta2.Tenant{}
		if err := k8sClient.Get(
			context.TODO(),
			types.NamespacedName{Name: tnt.GetName()},
			t,
		); err != nil {
			return fmt.Errorf("failed to get tenant: %w", err)
		}

		namespaces := t.Status.Namespaces
		if ok, _ := ContainElement(currentNS.GetName()).Match(namespaces); !ok {
			return fmt.Errorf(
				"expected tenant %s to contain namespace %s, but got: %v",
				t.GetName(),
				currentNS.GetName(),
				namespaces,
			)
		}

		instance := t.Status.GetInstance(
			&capsulev1beta2.TenantStatusNamespaceItem{
				Name: currentNS.GetName(),
				UID:  currentNS.GetUID(),
			},
		)
		if instance == nil {
			return fmt.Errorf(
				"expected tenant %s to contain instance for namespace %s (uid=%s)",
				t.GetName(),
				currentNS.GetName(),
				currentNS.GetUID(),
			)
		}

		return nil
	}, defaultTimeoutInterval, defaultPollInterval)
}

func GetTenantOwnerReference(
	tnt *capsulev1beta2.Tenant,
) (metav1.OwnerReference, error) {

	t := &capsulev1beta2.Tenant{}
	if err := k8sClient.Get(
		context.TODO(),
		types.NamespacedName{Name: tnt.GetName()},
		t,
	); err != nil {
		return metav1.OwnerReference{}, fmt.Errorf("failed to get tenant: %w", err)
	}

	gvk := capsulev1beta2.GroupVersion.WithKind("Tenant")
	return metav1.OwnerReference{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       t.GetName(),
		UID:        t.GetUID(),
	}, nil
}

func GetTenantOwnerReferenceAsPatch(
	tnt *capsulev1beta2.Tenant,
) (map[string]interface{}, error) {
	ownerRef, err := GetTenantOwnerReference(tnt)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"apiVersion": ownerRef.APIVersion,
		"kind":       ownerRef.Kind,
		"name":       ownerRef.Name,
		"uid":        string(ownerRef.UID),
	}, nil

}

func PatchTenantLabelForNamespace(tnt *capsulev1beta2.Tenant, ns *corev1.Namespace, cs kubernetes.Interface, timeout time.Duration) AsyncAssertion {
	return Eventually(func() (err error) {
		patch := map[string]interface{}{
			"metadata": map[string]interface{}{
				"labels": map[string]interface{}{
					meta.TenantLabel: tnt.GetName(),
				},
			},
		}

		return PatchNamespace(ns, cs, patch)
	}, timeout, defaultPollInterval)
}

func PatchNamespace(ns *corev1.Namespace, cs kubernetes.Interface, patch map[string]interface{}) error {
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return err
	}

	_, err = cs.CoreV1().Namespaces().Patch(
		context.Background(),
		ns.GetName(),
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	)

	return err
}

func PatchTenantOwnerReferenceForNamespace(
	tnt *capsulev1beta2.Tenant,
	ns *corev1.Namespace,
	cs kubernetes.Interface,
	timeout time.Duration,
) AsyncAssertion {
	return Eventually(func() error {
		// Build ownerRef for the tenant
		ownerRef := metav1.OwnerReference{
			APIVersion: capsulev1beta2.GroupVersion.String(),
			Kind:       "Tenant",
			Name:       tnt.GetName(),
			UID:        tnt.GetUID(),
		}

		patch := map[string]interface{}{
			"metadata": map[string]interface{}{
				"ownerReferences": []map[string]interface{}{
					{
						"apiVersion": ownerRef.APIVersion,
						"kind":       ownerRef.Kind,
						"name":       ownerRef.Name,
						"uid":        string(ownerRef.UID),
					},
				},
			},
		}

		patchBytes, err := json.Marshal(patch)
		Expect(err).ToNot(HaveOccurred())

		_, err = cs.CoreV1().Namespaces().Patch(
			context.Background(),
			ns.GetName(),
			types.StrategicMergePatchType,
			patchBytes,
			metav1.PatchOptions{},
		)

		return err
	}, timeout, defaultPollInterval)
}

func GetTenantEventually(tnt *capsulev1beta2.Tenant) *capsulev1beta2.Tenant {
	t := &capsulev1beta2.Tenant{}

	Eventually(func() error {
		return k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

	return t
}

func GetNamespaceEventually(name string) *corev1.Namespace {
	ns := &corev1.Namespace{}

	Eventually(func() error {
		return k8sClient.Get(context.TODO(), types.NamespacedName{Name: name}, ns)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

	return ns
}

func UpdateTenantEventually(tnt *capsulev1beta2.Tenant, mutator func(*capsulev1beta2.Tenant)) {
	Eventually(func() error {
		current := &capsulev1beta2.Tenant{}
		if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, current); err != nil {
			return err
		}

		mutator(current)

		return k8sClient.Update(context.TODO(), current)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func UpdateTenantEventuallyShouldFail(tnt *capsulev1beta2.Tenant, mutator func(*capsulev1beta2.Tenant)) {
	Eventually(func() error {
		current := &capsulev1beta2.Tenant{}
		if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, current); err != nil {
			return err
		}

		mutator(current)

		return k8sClient.Update(context.TODO(), current)
	}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
}

func PatchNamespaceEventually(ns *corev1.Namespace, mutator func(*corev1.Namespace)) {
	Eventually(func() error {
		current := &corev1.Namespace{}
		if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, current); err != nil {
			return err
		}

		before := current.DeepCopy()
		mutator(current)

		return k8sClient.Patch(context.TODO(), current, client.MergeFrom(before))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func ExpectNamespaceNotAssignedToTenant(ctx context.Context, name string) {
	ns := &corev1.Namespace{}

	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, ns)).To(Succeed())

		g.Expect(ns.GetOwnerReferences()).To(BeEmpty(), "namespace must not have ownerReferences")
		g.Expect(ns.GetLabels()).NotTo(HaveKey(meta.TenantLabel), "namespace must not have tenant label")
	}).WithTimeout(defaultTimeoutInterval).Should(Succeed())
}

func TenantNamespaceList(tnt *capsulev1beta2.Tenant, timeout time.Duration) AsyncAssertion {
	t := &capsulev1beta2.Tenant{}
	return Eventually(func() []string {
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)).Should(Succeed())
		return t.Status.Namespaces
	}, timeout, defaultPollInterval)
}

func ModifyNode(fn func(node *corev1.Node) error) error {
	nodeList := &corev1.NodeList{}

	Expect(k8sClient.List(context.Background(), nodeList)).ToNot(HaveOccurred())

	return fn(&nodeList.Items[0])
}

func EventuallyCreation(f interface{}) AsyncAssertion {
	return Eventually(f, defaultTimeoutInterval, defaultPollInterval)
}

func EventuallyDeletion(obj client.Object) {
	key := client.ObjectKeyFromObject(obj)

	Eventually(func() error {
		// Retry delete until the object is really gone.
		err := k8sClient.Delete(context.TODO(), obj)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}

		// Read into a fresh copy to avoid stale in-memory state.
		current := obj.DeepCopyObject().(client.Object)
		err = k8sClient.Get(context.TODO(), key, current)
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}

		return fmt.Errorf("%T %q still exists", obj, obj.GetName())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func ModifyCapsuleConfigurationOpts(fn func(configuration *capsulev1beta2.CapsuleConfiguration)) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		config := &capsulev1beta2.CapsuleConfiguration{}

		if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: defaultConfigurationName}, config); err != nil {
			return err
		}

		fn(config)

		return k8sClient.Update(context.Background(), config)
	})

	Expect(err).ToNot(HaveOccurred())
}

func CheckForOwnerRoleBindings(ns *corev1.Namespace, owner rbac.OwnerSpec, roles map[string]bool) func() error {
	if roles == nil {
		roles = map[string]bool{
			"admin":                     false,
			"capsule-namespace-deleter": false,
		}
	}

	return func() (err error) {
		roleBindings := &rbacv1.RoleBindingList{}

		if err = k8sClient.List(context.Background(), roleBindings, client.InNamespace(ns.GetName())); err != nil {
			return fmt.Errorf("cannot retrieve list of rolebindings: %w", err)
		}

		var ownerName string

		if owner.Kind == rbac.ServiceAccountOwner {
			parts := strings.Split(owner.Name, ":")

			ownerName = parts[3]
		} else {
			ownerName = owner.Name
		}

		for _, roleBinding := range roleBindings.Items {
			_, ok := roles[roleBinding.RoleRef.Name]
			if !ok {
				continue
			}

			subject := roleBinding.Subjects[0]

			if subject.Name != ownerName {
				continue
			}

			roles[roleBinding.RoleRef.Name] = true
		}

		for role, found := range roles {
			if !found {
				return fmt.Errorf("role %s for %s.%s has not been reconciled", role, owner.Kind.String(), owner.Name)
			}
		}

		return nil
	}
}

func VerifyTenantRoleBindings(
	tnt *capsulev1beta2.Tenant,
) {
	Eventually(func(g Gomega) {
		roles := tnt.GetRoleBindings()

		// List all RoleBindings once per namespace to avoid repeated API calls.
		for _, ns := range tnt.Status.Namespaces {
			for _, role := range roles {
				rbName := meta.NameForManagedRoleBindings(utils.RoleBindingHashFunc(role))

				rb := &rbacv1.RoleBinding{}
				err := k8sClient.Get(context.Background(), client.ObjectKey{
					Namespace: ns,
					Name:      rbName,
				}, rb)

				g.Expect(err).ToNot(HaveOccurred(),
					"expected RoleBinding %s/%s to exist (Owner: %s)", ns, rbName, role.Subjects,
				)

				g.Expect(rb.RoleRef.Name).To(Equal(role.ClusterRoleName),
					"expected RoleBinding %s/%s to have RoleRef.Name=%q",
					ns, rbName, role.ClusterRoleName)

				g.Expect(rb.Subjects).ToNot(BeEmpty(),
					"expected RoleBinding %s/%s to have at least one subject", ns, rbName)

				g.Expect(rb.Subjects).To(ConsistOf(role.Subjects),
					"expected RoleBinding %s/%s to have exact subjects",
					ns, rb.Name,
				)

			}
		}

	}).WithTimeout(30 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
}

func normalizeOwners(in rbac.OwnerStatusListSpec) rbac.OwnerStatusListSpec {
	// copy to avoid mutating the original
	out := make(rbac.OwnerStatusListSpec, len(in))
	copy(out, in)

	// sort outer slice by kind+name
	sort.Sort(rbac.GetByKindAndName(out))

	// sort roles inside each owner so role order doesn't matter
	for i := range out {
		sort.Strings(out[i].ClusterRoles)
	}

	return out
}

func EnsureServiceAccount(ctx context.Context, c client.Client, name string, namespace string) {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	err := c.Create(ctx, sa)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).ToNot(HaveOccurred())
	}
}

func EnsureRoleAndBindingForNamespaces(ctx context.Context, c client.Client, saName string, saNamespace string, namespaces []string) {
	for _, ns := range namespaces {
		role := &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:      saName + "-" + saNamespace,
				Namespace: ns,
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"secrets"},
					Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
				},
			},
		}

		err := c.Create(ctx, role)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		rb := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      saName + "-" + saNamespace,
				Namespace: ns,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      saName,
					Namespace: saNamespace,
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     saName + "-" + saNamespace,
			},
		}

		err = c.Create(ctx, rb)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}
	}
}

func GetKubernetesVersion() *versionUtil.Version {
	var serverVersion *version.Info
	var err error
	var cs kubernetes.Interface
	var ver *versionUtil.Version

	cs, err = kubernetes.NewForConfig(cfg)
	Expect(err).ToNot(HaveOccurred())

	serverVersion, err = cs.Discovery().ServerVersion()
	Expect(err).ToNot(HaveOccurred())

	ver, err = versionUtil.ParseGeneric(serverVersion.String())
	Expect(err).ToNot(HaveOccurred())

	return ver
}

func GrantEphemeralContainersUpdate(ns string, username string) (cleanup func()) {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-ephemeralcontainers",
			Namespace: ns,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods/ephemeralcontainers"},
				Verbs:     []string{"update", "patch"},
			},
			// Optional but often useful for the test flow:
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-ephemeralcontainers",
			Namespace: ns,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     rbacv1.UserKind,
				Name:     username,
				APIGroup: rbacv1.GroupName,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     role.Name,
		},
	}

	// Create-or-update (simple)
	EventuallyCreation(func() error {
		_ = k8sClient.Delete(context.Background(), rb)
		_ = k8sClient.Delete(context.Background(), role)

		if err := k8sClient.Create(context.Background(), role); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
		if err := k8sClient.Create(context.Background(), rb); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
		return nil
	}).Should(Succeed())

	// Give RBAC a moment to propagate in the apiserver authorizer cache
	Eventually(func() error {
		cs := ownerClient(rbac.UserSpec{Name: username, Kind: "User"})
		_, err := cs.CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{Limit: 1})
		return err
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

	return func() {
		// Best-effort cleanup
		_ = k8sClient.Delete(context.Background(), rb)
		_ = k8sClient.Delete(context.Background(), role)
	}
}

func DeepCompare(expected, actual interface{}) (bool, string) {
	expVal := reflect.ValueOf(expected)
	actVal := reflect.ValueOf(actual)

	// If the kinds differ, they are not equal.
	if expVal.Kind() != actVal.Kind() {
		return false, fmt.Sprintf("kind mismatch: %v vs %v", expVal.Kind(), actVal.Kind())
	}

	switch expVal.Kind() {
	case reflect.Slice, reflect.Array:
		// Convert slices to []interface{} for ElementsMatch.
		expSlice := make([]interface{}, expVal.Len())
		actSlice := make([]interface{}, actVal.Len())
		for i := 0; i < expVal.Len(); i++ {
			expSlice[i] = expVal.Index(i).Interface()
		}
		for i := 0; i < actVal.Len(); i++ {
			actSlice[i] = actVal.Index(i).Interface()
		}
		// Use a dummy tester to capture error messages.
		dummy := &dummyT{}
		if !assert.ElementsMatch(dummy, expSlice, actSlice) {
			return false, fmt.Sprintf("slice mismatch: %v", dummy.errors)
		}
		return true, ""
	case reflect.Struct:
		// Iterate over fields and compare recursively.
		for i := 0; i < expVal.NumField(); i++ {
			fieldName := expVal.Type().Field(i).Name
			ok, msg := DeepCompare(expVal.Field(i).Interface(), actVal.Field(i).Interface())
			if !ok {
				return false, fmt.Sprintf("field %s mismatch: %s", fieldName, msg)
			}
		}
		return true, ""
	default:
		// Fallback to reflect.DeepEqual for other types.
		if !reflect.DeepEqual(expected, actual) {
			return false, fmt.Sprintf("expected %v but got %v", expected, actual)
		}
		return true, ""
	}
}

// dummyT implements a minimal TestingT for testify.
type dummyT struct {
	errors []string
}

func (d *dummyT) Errorf(format string, args ...interface{}) {
	d.errors = append(d.errors, fmt.Sprintf(format, args...))
}

func MakePod(namespace, name string, labels map[string]string, annotations map[string]string, image string, cpuRequest string, emptyDirSize string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			SecurityContext: nobodyPodSecurityContext(),
			Containers: []corev1.Container{
				{
					Name:            "main",
					Image:           image,
					SecurityContext: restrictedContainerSecurityContext(),
				},
			},
			RestartPolicy: corev1.RestartPolicyAlways,
		},
	}

	if cpuRequest != "" {
		pod.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse(cpuRequest),
		}
	}

	if emptyDirSize != "" {
		pod.Spec.Volumes = []corev1.Volume{
			{
				Name: "cache",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{
						SizeLimit: ptr.To(resource.MustParse(emptyDirSize)),
					},
				},
			},
		}
		pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{
				Name:      "cache",
				MountPath: "/cache",
			},
		}
	}

	return pod
}

func MakeDeployment(namespace, name string, replicas int32, labels map[string]string, cpuRequest string) *appsv1.Deployment {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: mergeMaps(map[string]string{"app": name}, labels),
				},
				Spec: corev1.PodSpec{
					SecurityContext: nobodyPodSecurityContext(),
					Containers: []corev1.Container{
						{
							Name:            "nginx",
							Image:           "nginx:1.27.0",
							SecurityContext: restrictedContainerSecurityContext(),
						},
					},
				},
			},
		},
	}

	if cpuRequest != "" {
		dep.Spec.Template.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse(cpuRequest),
		}
	}

	return dep
}

func ExpectPodsForDeployment(ctx context.Context, namespace, app string, expected int) {
	Eventually(func(g Gomega) {
		pods := &corev1.PodList{}

		g.Expect(k8sClient.List(ctx, pods,
			client.InNamespace(namespace),
			client.MatchingLabels{
				"app": app,
			},
		)).To(Succeed())

		g.Expect(len(pods.Items)).To(Equal(expected),
			"unexpected pod count for deployment app=%q in namespace %s",
			app,
			namespace,
		)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func nobodyPodSecurityContext() *corev1.PodSecurityContext {
	return &corev1.PodSecurityContext{
		RunAsNonRoot: ptr.To(true),
		RunAsUser:    ptr.To[int64](65534),
		RunAsGroup:   ptr.To[int64](65534),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

func restrictedContainerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{
				"ALL",
			},
		},
	}
}

func MakePVC(namespace, name, size string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(size),
				},
			},
		},
	}
}

func ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) {
	Eventually(func() error {
		dep := &appsv1.Deployment{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, dep); err != nil {
			return err
		}
		dep.Spec.Replicas = ptr.To(replicas)
		return k8sClient.Update(ctx, dep)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func UpdatePodLabels(ctx context.Context, namespace, name string, labels map[string]string) {
	Eventually(func() error {
		pod := &corev1.Pod{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, pod); err != nil {
			return err
		}
		pod.Labels = labels
		return k8sClient.Update(ctx, pod)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func UpdatePodImage(ctx context.Context, namespace, name, image string) {
	Eventually(func() error {
		pod := &corev1.Pod{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, pod); err != nil {
			return err
		}
		pod.Spec.Containers[0].Image = image
		return k8sClient.Update(ctx, pod)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func TenantReadyTrue(tnt *capsulev1beta2.Tenant) {
	TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
}

func TenantReadyFalse(tnt *capsulev1beta2.Tenant) {
	TenantReady(tnt, metav1.ConditionFalse, defaultTimeoutInterval)
}

func TenantReady(
	tnt *capsulev1beta2.Tenant,
	expected metav1.ConditionStatus,
	timeoutInterval time.Duration,
) {
	Eventually(func(g Gomega) {
		current := &capsulev1beta2.Tenant{}
		err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: tnt.GetName()}, current)
		g.Expect(err).NotTo(HaveOccurred())

		condition := current.Status.Conditions.GetConditionByType(meta.ReadyCondition)
		g.Expect(condition).NotTo(BeNil(), "expected Tenant %q to have Ready condition", tnt.GetName())

		g.Expect(condition.Status).To(
			Equal(expected),
			"expected Tenant %q Ready condition to be %s, got %s: reason=%q message=%q",
			tnt.GetName(),
			expected,
			condition.Status,
			condition.Reason,
			condition.Message,
		)

		for _, owner := range current.Spec.Owners {
			g.Expect(current.Status.Owners).To(
				ContainElement(owner.CoreOwnerSpec),
				"expected Tenant %q status.owners to contain spec owner %+v; current status.owners=%+v",
				tnt.GetName(),
				owner.CoreOwnerSpec,
				current.Status.Owners,
			)
		}

		g.Expect(current.Status.ObservedGeneration).To(
			Equal(current.GetGeneration()),
			"expected Tenant %q status.observedGeneration (%d) to equal metadata.generation (%d)",
			tnt.GetName(),
			current.Status.ObservedGeneration,
			current.GetGeneration(),
		)
	}, timeoutInterval, defaultPollInterval).Should(Succeed())
}

func EnsureRuntimeClass(ctx context.Context, rtc *nodev1.RuntimeClass) {
	Eventually(func() error {
		desired := rtc.DeepCopy()
		desired.ResourceVersion = ""

		_, err := controllerutil.CreateOrUpdate(ctx, k8sClient, desired, func() error {
			desired.Handler = rtc.Handler
			desired.Overhead = rtc.Overhead
			desired.Scheduling = rtc.Scheduling

			labels := desired.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}
			for key, value := range rtc.GetLabels() {
				labels[key] = value
			}
			labels["env"] = "e2e"
			desired.SetLabels(labels)

			annotations := desired.GetAnnotations()
			if annotations == nil {
				annotations = map[string]string{}
			}
			for key, value := range rtc.GetAnnotations() {
				annotations[key] = value
			}
			desired.SetAnnotations(annotations)

			return nil
		})

		return err
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

const namespaceTerminationHoldFinalizer = "e2e.projectcapsule.dev/hold-termination"

func holdNamespaceTerminating(ctx context.Context, name string) func() {
	Eventually(func() error {
		ns := &corev1.Namespace{}

		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, ns); err != nil {
			return err
		}

		if controllerutil.AddFinalizer(ns, namespaceTerminationHoldFinalizer) {
			return k8sClient.Update(ctx, ns)
		}

		return nil
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

	Eventually(func() error {
		ns := &corev1.Namespace{}

		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, ns); err != nil {
			return err
		}

		if ns.DeletionTimestamp != nil {
			return nil
		}

		return k8sClient.Delete(ctx, ns)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

	Eventually(func(g Gomega) {
		ns := &corev1.Namespace{}

		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, ns)).To(Succeed())
		g.Expect(ns.DeletionTimestamp).ToNot(BeNil())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

	return func() {
		Eventually(func() error {
			ns := &corev1.Namespace{}

			if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, ns); err != nil {
				if apierrors.IsNotFound(err) {
					return nil
				}

				return err
			}

			if controllerutil.RemoveFinalizer(ns, namespaceTerminationHoldFinalizer) {
				return k8sClient.Update(ctx, ns)
			}

			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}
}
