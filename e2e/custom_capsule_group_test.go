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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("creating a Namespace as Tenant owner with custom --capsule-group", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenantassignedcustomgroup",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "alice",
				Kind: "User",
			},
			NamespacesMetadata: v1alpha1.AdditionalMetadata{},
			ServicesMetadata:   v1alpha1.AdditionalMetadata{},
			StorageClasses:     []string{},
			IngressClasses:     []string{},
			LimitRanges:        []corev1.LimitRangeSpec{},
			NamespaceQuota:     10,
			NodeSelector:       map[string]string{},
			ResourceQuota:      []corev1.ResourceQuotaSpec{},
		},
	}
	JustBeforeEach(func() {
		tnt.ResourceVersion = ""
		Expect(k8sClient.Create(context.TODO(), tnt)).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})
	It("should fail", func() {
		args := append(defaulManagerPodArgs, []string{"--capsule-user-group=test"}...)
		ModifyCapsuleManagerPodArgs(args)
		CapsuleClusterGroupParamShouldBeUpdated("test", podRecreationTimeoutInterval)
		ns := NewNamespace("cg-namespace-fail")
		NamespaceCreationShouldNotSucceed(ns, tnt, podRecreationTimeoutInterval)
	})
	It("should succeed and be available in Tenant namespaces list", func() {
		ModifyCapsuleManagerPodArgs(defaulManagerPodArgs)
		CapsuleClusterGroupParamShouldBeUpdated("capsule.clastix.io", podRecreationTimeoutInterval)
		ns := NewNamespace("cg-namespace")
		NamespaceCreationShouldSucceed(ns, tnt, podRecreationTimeoutInterval)
		NamespaceShouldBeManagedByTenant(ns, tnt, podRecreationTimeoutInterval)

	})
})
