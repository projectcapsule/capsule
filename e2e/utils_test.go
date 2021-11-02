//+build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
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

func KindInTenantRoleBindingAssertions(ns *corev1.Namespace, timeout time.Duration) (out []AsyncAssertion) {
	for _, rbn := range tenantRoleBindingNames {
		rb := &rbacv1.RoleBinding{}
		out = append(out, Eventually(func() []string {
			if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: rbn, Namespace: ns.GetName()}, rb); err != nil {
				return nil
			}
			var subjects []string
			for _, subject := range rb.Subjects {
				subjects = append(subjects, subject.Kind)
			}
			return subjects
		}, timeout, defaultPollInterval))
	}
	return
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
