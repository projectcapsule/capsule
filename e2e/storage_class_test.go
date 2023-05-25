//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

// "sigs.k8s.io/controller-runtime/pkg/client"

package e2e

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/api"
)

var _ = Describe("when Tenant handles Storage classes", func() {
	tntNoDefaults := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "storage-class-selector",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "selector",
					Kind: "User",
				},
			},
			StorageClasses: &api.DefaultAllowedListSpec{
				SelectorAllowedListSpec: api.SelectorAllowedListSpec{
					AllowedListSpec: api.AllowedListSpec{
						Exact: []string{"cephfs", "glusterfs"},
						Regex: "^oil-.*$",
					},
					LabelSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"env": "customer",
						},
					},
				},
			},
		},
	}

	tntWithDefault := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "storage-class-default",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "default",
					Kind: "User",
				},
			},
			StorageClasses: &api.DefaultAllowedListSpec{
				Default: "tenant-default",
				SelectorAllowedListSpec: api.SelectorAllowedListSpec{
					LabelSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"name": "tenant-default",
						},
					},
				},
			},
		},
	}

	tenantDefault := storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-default",
			Labels: map[string]string{
				"name": "tenant-default",
				"env":  "e2e",
			},
		},
		Provisioner: "kubernetes.io/no-provisioner",
	}
	globalDefault := storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "global-default",
			Labels: map[string]string{
				"env": "customer",
			},
		},
		Provisioner: "kubernetes.io/no-provisioner",
	}
	disallowedGlobalDefault := storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "disallowed-global-default",
			Labels: map[string]string{
				"name": "disallowed-global-default",
				"env":  "e2e",
			},
		},
		Provisioner: "kubernetes.io/no-provisioner",
	}

	JustBeforeEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{tntNoDefaults, tntWithDefault} {
			EventuallyCreation(func() error {
				tnt.ResourceVersion = ""

				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
		}
	})
	JustAfterEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{tntNoDefaults, tntWithDefault} {
			Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
		}

		Eventually(func() (err error) {
			req, _ := labels.NewRequirement("env", selection.Exists, nil)

			return k8sClient.DeleteAllOf(context.TODO(), &storagev1.StorageClass{}, &client.DeleteAllOfOptions{
				ListOptions: client.ListOptions{
					LabelSelector: labels.NewSelector().Add(*req),
				},
			})
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("should fail", func() {
		k8sClient.Create(context.TODO(), tntNoDefaults)

		ns := NewNamespace("")
		NamespaceCreation(ns, tntNoDefaults.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntNoDefaults, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("non-specifying it", func() {
			Eventually(func() (err error) {
				cs := ownerClient(tntNoDefaults.Spec.Owners[0])
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
				cs := ownerClient(tntNoDefaults.Spec.Owners[0])
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
		By("specifying with not matching label", func() {
			for i, sc := range []string{"internal-hdd", "internal-ssd"} {
				storageName := strings.Join([]string{sc, "-", strconv.Itoa(i)}, "")
				class := &storagev1.StorageClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("sc-%s", storageName),
						Labels: map[string]string{
							"env": "internal",
						},
					},
					Provisioner: "kubernetes.io/no-provisioner",
				}
				Expect(k8sClient.Create(context.TODO(), class)).Should(Succeed())

				p := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: storageName,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &storageName,
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("3Gi"),
							},
						},
					},
				}

				cs := ownerClient(tntNoDefaults.Spec.Owners[0])

				EventuallyCreation(func() error {
					_, err := cs.CoreV1().PersistentVolumeClaims(ns.GetName()).Create(context.Background(), p, metav1.CreateOptions{})
					return err
				}).ShouldNot(Succeed())
			}
		})

	})

	It("should allow", func() {
		ns := NewNamespace("")
		cs := ownerClient(tntNoDefaults.Spec.Owners[0])

		NamespaceCreation(ns, tntNoDefaults.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntNoDefaults, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
		By("using exact matches", func() {
			for _, c := range tntNoDefaults.Spec.StorageClasses.Exact {
				Eventually(func() (err error) {
					p := &corev1.PersistentVolumeClaim{
						ObjectMeta: metav1.ObjectMeta{
							Name: c,
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: pointer.String(c),
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
		By("using a selector match", func() {
			for i, sc := range []string{"customer-hdd", "customer-ssd"} {
				storageName := strings.Join([]string{sc, "-", strconv.Itoa(i)}, "")
				class := &storagev1.StorageClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: storageName,
						Labels: map[string]string{
							"env": "customer",
						},
					},
					Provisioner: "kubernetes.io/no-provisioner",
				}
				Expect(k8sClient.Create(context.TODO(), class)).Should(Succeed())

				EventuallyCreation(func() error {
					p := &corev1.PersistentVolumeClaim{
						ObjectMeta: metav1.ObjectMeta{
							Name:      storageName,
							Namespace: ns.GetName(),
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &storageName,
							AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
							Resources: corev1.ResourceRequirements{
								Requests: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceStorage: resource.MustParse("3Gi"),
								},
							},
						},
					}

					return k8sClient.Create(context.Background(), p)
				}).Should(Succeed())
			}
		})
	})

	It("should mutate to default tenant StorageClass (class does not exists)", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		p := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pvc-default-sc",
				Namespace: ns.GetName(),
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
		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), p)
		}).Should(Succeed())
		Expect(*p.Spec.StorageClassName).To(Equal("tenant-default"))
	})

	It("should mutate to default tenant StorageClass (class exists)", func() {
		class := tenantDefault
		Expect(k8sClient.Create(context.TODO(), &class)).Should(Succeed())

		ns := NewNamespace("")
		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		p := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pvc-default-sc-present",
				Namespace: ns.GetName(),
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

		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), p)
		}).Should(Succeed())
		Expect(*p.Spec.StorageClassName).To(Equal(class.GetName()))
	})

	It("should mutate to default tenant StorageClass although cluster global ons is not allowed", func() {
		class := tenantDefault
		global := disallowedGlobalDefault

		Expect(k8sClient.Create(context.TODO(), &class)).Should(Succeed())
		Expect(k8sClient.Create(context.TODO(), &global)).Should(Succeed())

		ns := NewNamespace("")
		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		p := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pvc-default-sc-present",
				Namespace: ns.GetName(),
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
		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), p)
		}).Should(Succeed())
		Expect(*p.Spec.StorageClassName).To(Equal(class.GetName()))
	})

	It("should mutate to default tenant StorageClass although cluster global ons is allowed", func() {
		class := tenantDefault
		global := globalDefault

		Expect(k8sClient.Create(context.TODO(), &class)).Should(Succeed())
		Expect(k8sClient.Create(context.TODO(), &global)).Should(Succeed())

		ns := NewNamespace("")
		NamespaceCreation(ns, tntWithDefault.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tntWithDefault, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		p := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pvc-default-sc-present",
				Namespace: ns.GetName(),
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
		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), p)
		}).Should(Succeed())
		Expect(*p.Spec.StorageClassName).To(Equal(class.GetName()))
	})
})
