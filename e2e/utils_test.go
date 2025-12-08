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
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	versionUtil "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

const (
	defaultTimeoutInterval   = 40 * time.Second
	defaultPollInterval      = time.Second
	defaultConfigurationName = "default"
)

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

func ServiceCreation(svc *corev1.Service, owner api.UserSpec, timeout time.Duration) AsyncAssertion {
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
	namespaceLabels["env"] = "e2e"

	if len(labels) > 0 {
		namespaceLabels = labels[0]
	}

	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: namespaceLabels,
		},
	}
}

func NamespaceCreation(ns *corev1.Namespace, owner api.UserSpec, timeout time.Duration) AsyncAssertion {
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

func ModifyCapsuleConfigurationOpts(fn func(configuration *capsulev1beta2.CapsuleConfiguration)) {
	config := &capsulev1beta2.CapsuleConfiguration{}
	Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: defaultConfigurationName}, config)).ToNot(HaveOccurred())

	fn(config)

	Expect(k8sClient.Update(context.Background(), config)).ToNot(HaveOccurred())
}

func CheckForOwnerRoleBindings(ns *corev1.Namespace, owner api.OwnerSpec, roles map[string]bool) func() error {
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

		if owner.Kind == api.ServiceAccountOwner {
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
		// List all RoleBindings once per namespace to avoid repeated API calls.
		for _, ns := range tnt.Status.Namespaces {
			for i, owner := range tnt.Status.Owners {
				for _, role := range owner.ClusterRoles {
					rbName := fmt.Sprintf("capsule-%s-%d-%s", tnt.Name, i, role)

					rb := &rbacv1.RoleBinding{}
					err := k8sClient.Get(context.Background(), client.ObjectKey{
						Namespace: ns,
						Name:      rbName,
					}, rb)

					g.Expect(err).ToNot(HaveOccurred(),
						"expected RoleBinding %s/%s to exist", ns, rbName)

					g.Expect(rb.RoleRef.Name).To(Equal(role),
						"expected RoleBinding %s/%s to have RoleRef.Name=%q",
						ns, rbName, role)

					g.Expect(rb.Subjects).ToNot(BeEmpty(),
						"expected RoleBinding %s/%s to have at least one subject", ns, rbName)

					foundSubject := false
					for _, s := range rb.Subjects {
						if s.Kind == string(owner.Kind) && s.Name == owner.Name {
							foundSubject = true
							break
						}
					}

					g.Expect(foundSubject).To(BeTrue(),
						"expected RoleBinding %s/%s to contain subject %s/%s",
						ns, rb.Name, owner.Kind, owner.Name)

				}
			}
		}
	}).WithTimeout(30 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
}

func normalizeOwners(in api.OwnerStatusListSpec) api.OwnerStatusListSpec {
	// copy to avoid mutating the original
	out := make(api.OwnerStatusListSpec, len(in))
	copy(out, in)

	// sort outer slice by kind+name
	sort.Sort(api.GetByKindAndName(out))

	// sort roles inside each owner so role order doesn't matter
	for i := range out {
		sort.Strings(out[i].ClusterRoles)
	}

	return out
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
