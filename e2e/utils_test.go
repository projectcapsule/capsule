//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	versionUtil "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

const (
	defaultTimeoutInterval = 20 * time.Second
	defaultPollInterval    = time.Second
)

func NewNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func NamespaceCreation(ns *corev1.Namespace, owner capsulev1beta1.OwnerSpec, timeout time.Duration) AsyncAssertion {
	cs := ownerClient(owner)
	return Eventually(func() (err error) {
		_, err = cs.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		return
	}, timeout, defaultPollInterval)
}

func TenantNamespaceList(t *capsulev1beta1.Tenant, timeout time.Duration) AsyncAssertion {
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

func ModifyCapsuleConfigurationOpts(fn func(configuration *capsulev1alpha1.CapsuleConfiguration)) {
	config := &capsulev1alpha1.CapsuleConfiguration{}
	Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: "default"}, config)).ToNot(HaveOccurred())

	fn(config)

	Expect(k8sClient.Update(context.Background(), config)).ToNot(HaveOccurred())

	time.Sleep(1 * time.Second)
}

func CheckForOwnerRoleBindings(ns *corev1.Namespace, owner capsulev1beta1.OwnerSpec, roles map[string]bool) func() error {
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

		if owner.Kind == capsulev1beta1.ServiceAccountOwner {
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
