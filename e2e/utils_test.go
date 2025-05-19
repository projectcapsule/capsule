// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

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
)

const (
	defaultTimeoutInterval = 40 * time.Second
	defaultPollInterval    = time.Second
)

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

func ServiceCreation(svc *corev1.Service, owner capsulev1beta2.OwnerSpec, timeout time.Duration) AsyncAssertion {
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

	var namespaceLabels map[string]string
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

func NamespaceCreation(ns *corev1.Namespace, owner capsulev1beta2.OwnerSpec, timeout time.Duration) AsyncAssertion {
	cs := ownerClient(owner)
	return Eventually(func() (err error) {
		_, err = cs.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		return
	}, timeout, defaultPollInterval)
}

func TenantNamespaceList(t *capsulev1beta2.Tenant, timeout time.Duration) AsyncAssertion {
	return Eventually(func() []string {
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: t.GetName()}, t)).Should(Succeed())
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
	Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: "default"}, config)).ToNot(HaveOccurred())

	fn(config)

	Expect(k8sClient.Update(context.Background(), config)).ToNot(HaveOccurred())
}

func CheckForOwnerRoleBindings(ns *corev1.Namespace, owner capsulev1beta2.OwnerSpec, roles map[string]bool) func() error {
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

		if owner.Kind == capsulev1beta2.ServiceAccountOwner {
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
