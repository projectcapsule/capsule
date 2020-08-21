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
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/clastix/capsule/api/v1alpha1"
)

const (
	defaultTimeoutInterval = 10 * time.Second
	defaultPollInterval    = time.Second
)

func NewNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func NamespaceCreationShouldSucceed(ns *corev1.Namespace, t *v1alpha1.Tenant) {
	cs := ownerClient(t)
	Eventually(func() (err error) {
		_, err = cs.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		return
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func NamespaceShouldBeManagedByTenant(ns *corev1.Namespace, t *v1alpha1.Tenant) {
	Eventually(func() v1alpha1.NamespaceList {
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: t.GetName()}, t)).Should(Succeed())
		return t.Status.Namespaces
	}, defaultTimeoutInterval, defaultPollInterval).Should(ContainElement(ns.GetName()))
}
