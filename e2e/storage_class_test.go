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
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("when Tenant handles Storage classes", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "storage-class",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: "storage",
			StorageClasses: []string{
				"cephfs",
				"glusterfs",
			},
			IngressClasses:  []string{},
			LimitRanges:     []corev1.LimitRangeSpec{},
			NamespaceQuota:  3,
			NodeSelector:    map[string]string{},
			NetworkPolicies: []networkingv1.NetworkPolicySpec{},
			ResourceQuota:   []corev1.ResourceQuotaSpec{},
		},
	}
	JustBeforeEach(func() {
		tnt.ResourceVersion = ""
		Expect(k8sClient.Create(context.TODO(), tnt)).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})
	It("should block non allowed Storage Class", func() {
		ns := NewNamespace("storage-class-disallowed")
		NamespaceCreationShouldSucceed(ns, tnt)
		NamespaceShouldBeManagedByTenant(ns, tnt)

		By("non-specifying the class", func() {
			Eventually(func() (err error) {
				cs := ownerClient(tnt)
				p := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: "denied-pvc",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("3Gi"),
							},
						},
					},
				}
				_, err = cs.CoreV1().PersistentVolumeClaims(ns.GetName()).Create(context.TODO(), p, metav1.CreateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
		By("specifying a forbidden class", func() {
			Eventually(func() (err error) {
				cs := ownerClient(tnt)
				p := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mighty-storage",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("3Gi"),
							},
						},
					},
				}
				_, err = cs.CoreV1().PersistentVolumeClaims(ns.GetName()).Create(context.TODO(), p, metav1.CreateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())
		})
	})
	It("should allow enabled Storage Class", func() {
		ns := NewNamespace("storage-class-allowed")
		cs := ownerClient(tnt)

		NamespaceCreationShouldSucceed(ns, tnt)
		NamespaceShouldBeManagedByTenant(ns, tnt)

		for _, c := range tnt.Spec.StorageClasses {
			Eventually(func() (err error) {
				p := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: c,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: pointer.StringPtr(c),
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("3Gi"),
							},
						},
					},
				}
				_, err = cs.CoreV1().PersistentVolumeClaims(ns.GetName()).Create(context.TODO(), p, metav1.CreateOptions{})
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})
})
