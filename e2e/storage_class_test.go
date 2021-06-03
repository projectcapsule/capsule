//+build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
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
			Owner: v1alpha1.OwnerSpec{
				Name: "storage",
				Kind: "User",
			},
			StorageClasses: &v1alpha1.AllowedListSpec{
				Exact: []string{
					"cephfs",
					"glusterfs",
				},
				Regex: "^oil-.*$",
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})

	It("should fails", func() {
		ns := NewNamespace("storage-class-disallowed")
		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("non-specifying it", func() {
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
		By("specifying a forbidden one", func() {
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

	It("should allow", func() {
		ns := NewNamespace("storage-class-allowed")
		cs := ownerClient(tnt)

		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
		By("using exact matches", func() {
			for _, c := range tnt.Spec.StorageClasses.Exact {
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
		By("using a regex match", func() {
			allowedClass := "oil-storage"
			Eventually(func() (err error) {
				p := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: allowedClass,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &allowedClass,
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
		})
	})
})
