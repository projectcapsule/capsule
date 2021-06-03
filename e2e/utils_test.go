//+build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"strconv"
	"time"

	"github.com/clastix/capsule/api/v1alpha1"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
)

const (
	defaultTimeoutInterval       = 20 * time.Second
	defaultPollInterval          = time.Second
)

func NewNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func NamespaceCreation(ns *corev1.Namespace, t *v1alpha1.Tenant, timeout time.Duration) AsyncAssertion {
	cs := ownerClient(t)
	return Eventually(func() (err error) {
		_, err = cs.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		return
	}, timeout, defaultPollInterval)
}

func TenantNamespaceList(t *v1alpha1.Tenant, timeout time.Duration) AsyncAssertion {
	return Eventually(func() []string {
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: t.GetName()}, t)).Should(Succeed())
		return t.Status.Namespaces
	}, timeout, defaultPollInterval)
}

func EventuallyCreation(f interface{}) AsyncAssertion {
	return Eventually(f, defaultTimeoutInterval, defaultPollInterval)
}

func ModifyCapsuleConfigurationOpts(fn func(configuration *v1alpha1.CapsuleConfiguration)) {
	config := &v1alpha1.CapsuleConfiguration{}
	Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: "default"}, config)).ToNot(HaveOccurred())

	fn(config)

	Expect(k8sClient.Update(context.Background(), config)).ToNot(HaveOccurred())

	time.Sleep(time.Second)
}

func KindInTenantRoleBindingAssertions(ns *corev1.Namespace, timeout time.Duration) (out []AsyncAssertion) {
	for _, rbn := range tenantRoleBindingNames {
		rb := &rbacv1.RoleBinding{}
		out = append(out, Eventually(func() string {
			if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: rbn, Namespace: ns.GetName()}, rb); err != nil {
				return ""
			}
			return rb.Subjects[0].Kind
		}, timeout, defaultPollInterval))
	}
	return
}

func GetKubernetesSemVer() (major, minor int, ver string) {
	var v *version.Info
	var err error
	var cs kubernetes.Interface

	cs, err = kubernetes.NewForConfig(cfg)
	Expect(err).ToNot(HaveOccurred())

	v, err = cs.Discovery().ServerVersion()
	Expect(err).ToNot(HaveOccurred())
	major, err = strconv.Atoi(v.Major)
	Expect(err).ToNot(HaveOccurred())
	minor, err = strconv.Atoi(v.Minor)
	Expect(err).ToNot(HaveOccurred())
	ver = v.String()

	return
}
