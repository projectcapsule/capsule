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

	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	versionUtil "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/utils"
)

const (
	defaultTimeoutInterval   = 40 * time.Second
	defaultPollInterval      = time.Second
	defaultConfigurationName = "default"
)

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
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed(),
		"failed to force delete namespace %s", name)
}

func NamespaceCreation(ns *corev1.Namespace, owner rbac.UserSpec, timeout time.Duration) AsyncAssertion {
	cs := ownerClient(owner)
	return Eventually(func() (err error) {
		_, err = cs.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		return
	}, timeout, defaultPollInterval)
}

func NamespaceIsPartOfTenant(
	tnt *capsulev1beta2.Tenant,
	ns *corev1.Namespace,
) func() error {

	return func() error {
		t := &capsulev1beta2.Tenant{}
		if err := k8sClient.Get(
			context.TODO(),
			types.NamespacedName{Name: tnt.GetName()},
			t,
		); err != nil {
			return fmt.Errorf("failed to get tenant: %w", err)
		}

		// reuse existing helper
		namespaces := TenantNamespaceList(t, defaultTimeoutInterval)
		if ok, _ := ContainElements(ns.GetName()).Match(namespaces); ok {
			return fmt.Errorf(
				"expected tenant %s to contain namespace %s, but got: %v",
				t.GetName(), ns.GetName(), namespaces,
			)
		}

		// reuse your existing method
		instance := t.Status.GetInstance(
			&capsulev1beta2.TenantStatusNamespaceItem{
				Name: ns.GetName(),
				UID:  ns.GetUID(),
			})

		if instance == nil {
			return fmt.Errorf(
				"tenant %s does not contain instance for namespace %s (uid=%s)",
				t.GetName(), ns.GetName(), ns.GetUID(),
			)
		}

		return nil
	}
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
			Containers: []corev1.Container{
				{
					Name:  "main",
					Image: image,
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
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:1.27.0",
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
