//+build e2e

/*
Copyright 2020 Clastix Labs.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"
	"strconv"
	"time"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/controllers/rbac"
)

const (
	defaultTimeoutInterval       = 20 * time.Second
	podRecreationTimeoutInterval = 90 * time.Second
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

func CapsuleClusterGroupParam(timeout time.Duration) AsyncAssertion {
	capsuleCRB := &rbacv1.ClusterRoleBinding{}

	return Eventually(func() string {
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: rbac.ProvisionerRoleName}, capsuleCRB)).Should(Succeed())
		return capsuleCRB.Subjects[0].Name
	}, timeout, defaultPollInterval)
}

func ModifyCapsuleManagerPodArgs(args []string) {
	capsuleDeployment := &appsv1.Deployment{}
	k8sClient.Get(context.TODO(), types.NamespacedName{Name: capsuleDeploymentName, Namespace: capsuleNamespace}, capsuleDeployment)
	for i, container := range capsuleDeployment.Spec.Template.Spec.Containers {
		if container.Name == capsuleManagerContainerName {
			capsuleDeployment.Spec.Template.Spec.Containers[i].Args = args
		}
	}
	capsuleDeployment.ResourceVersion = ""
	err := k8sClient.Update(context.TODO(), capsuleDeployment)
	Expect(err).ToNot(HaveOccurred())

	Eventually(func() []string {
		var containerArgs []string
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: capsuleDeploymentName, Namespace: capsuleNamespace}, capsuleDeployment)).Should(Succeed())
		for i, container := range capsuleDeployment.Spec.Template.Spec.Containers {
			if container.Name == capsuleManagerContainerName {
				containerArgs = capsuleDeployment.Spec.Template.Spec.Containers[i].Args
			}
		}
		return containerArgs
	}, podRecreationTimeoutInterval, defaultPollInterval).Should(HaveLen(len(args)))

	pl := &corev1.PodList{}
	Eventually(func() []corev1.Pod {
		Expect(k8sClient.List(context.TODO(), pl, client.MatchingLabels{"app.kubernetes.io/instance": "capsule"})).Should(Succeed())
		return pl.Items
	}, podRecreationTimeoutInterval, defaultPollInterval).Should(HaveLen(2))
	Eventually(func() []corev1.Pod {
		Expect(k8sClient.List(context.TODO(), pl, client.MatchingLabels{"app.kubernetes.io/instance": "capsule"})).Should(Succeed())
		return pl.Items
	}, podRecreationTimeoutInterval, defaultPollInterval).Should(HaveLen(1))
	// had to add sleep in order to manager be started
	time.Sleep(defaultTimeoutInterval)
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
