// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("preventing PersistentVolume cross-tenant mount", Label("tenant", "storage", "persistentvolumeclaim"), func() {
	tnt1 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pv-one",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "jessica",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	tnt2 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pv-two",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "leto",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	JustBeforeEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{tnt1, tnt2} {
			EventuallyCreation(func() error {
				tnt.ResourceVersion = ""

				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
		}
	})

	JustAfterEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{tnt1, tnt2} {
			Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
		}
	})

	It("should add labels to PersistentVolume and prevent cross-Tenant mount", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tnt1.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt1, defaultTimeoutInterval).Should(ContainElement(ns.Name))

		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "arrakis",
				Namespace: ns.Name,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
				StorageClassName: ptr.To("standard"),
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), &pvc)
		}).Should(Succeed())

		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "arrakis-pod",
				Namespace: ns.Name,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:            "container",
						Image:           "gcr.io/google_containers/pause-amd64:3.0",
						ImagePullPolicy: corev1.PullAlways,
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "data",
								MountPath: "/tmp",
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "data",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: pvc.Name,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), &pod)
		}).Should(Succeed())

		Eventually(func() int {
			nsName := types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}

			if err := k8sClient.Get(context.Background(), nsName, &pvc); err != nil {
				return 0
			}

			return len(pvc.Spec.VolumeName)
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeNumerically(">", 0))

		pv := corev1.PersistentVolume{}
		defer func() {
			_ = k8sClient.Delete(context.Background(), &pv)
		}()

		Eventually(func() string {
			if err := k8sClient.Get(context.Background(), types.NamespacedName{Name: pvc.Spec.VolumeName}, &pv); err != nil {
				return "not-found"
			}

			if pv.GetLabels() == nil {
				return "no-labels"
			}

			return pv.GetLabels()["capsule.clastix.io/tenant"]
		}, defaultTimeoutInterval, defaultPollInterval).Should(Equal(tnt1.Name))

		Eventually(func() error {
			nsName := types.NamespacedName{Name: pv.Name}

			if err := k8sClient.Get(context.Background(), nsName, &pv); err != nil {
				return err
			}

			pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRecycle

			return k8sClient.Update(context.Background(), &pv)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		Expect(k8sClient.Delete(context.Background(), &pod, &client.DeleteOptions{GracePeriodSeconds: ptr.To(int64(0))})).ToNot(HaveOccurred())

		ns2 := NewNamespace("")
		NamespaceCreation(ns2, tnt2.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt2, defaultTimeoutInterval).Should(ContainElement(ns2.Name))

		Consistently(func() error {
			pvc := corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "caladan",
					Namespace: ns2.Name,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Gi"),
						},
					},
					StorageClassName: ptr.To("standard"),
					VolumeName:       pv.Name,
				},
			}

			return k8sClient.Create(context.Background(), &pvc)
		}, defaultTimeoutInterval, defaultPollInterval).Should(HaveOccurred())
	})

	It("should not add a selector when updating an already-bound dynamic PVC without selector", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tnt1.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt1, defaultTimeoutInterval).Should(ContainElement(ns.Name))

		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dynamic-pvc",
				Namespace: ns.Name,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
				StorageClassName: ptr.To("standard"),
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), &pvc)
		}).Should(Succeed())

		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(
				context.Background(),
				types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace},
				&pvc,
			)).To(Succeed())

			g.Expect(pvc.Spec.Selector).To(BeNil())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		pvc.Labels = map[string]string{
			"updated": "true",
		}

		Eventually(func() error {
			return k8sClient.Update(context.Background(), &pvc)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		Eventually(func(g Gomega) {
			updated := corev1.PersistentVolumeClaim{}

			g.Expect(k8sClient.Get(
				context.Background(),
				types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace},
				&updated,
			)).To(Succeed())

			g.Expect(updated.Labels).To(HaveKeyWithValue("updated", "true"))
			g.Expect(updated.Spec.Selector).To(BeNil())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("should add the Tenant selector to a PVC with an existing selector", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tnt1.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt1, defaultTimeoutInterval).Should(ContainElement(ns.Name))

		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "selector-pvc",
				Namespace: ns.Name,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
				StorageClassName: ptr.To("standard"),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"storage-tier": "gold",
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), &pvc)
		}).Should(Succeed())

		Eventually(func(g Gomega) {
			created := corev1.PersistentVolumeClaim{}

			g.Expect(k8sClient.Get(
				context.Background(),
				types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace},
				&created,
			)).To(Succeed())

			g.Expect(created.Spec.Selector).ToNot(BeNil())
			g.Expect(created.Spec.Selector.MatchLabels).To(HaveKeyWithValue("storage-tier", "gold"))
			g.Expect(created.Spec.Selector.MatchLabels).ToNot(HaveKey(meta.TenantLabel))
			g.Expect(created.Spec.Selector.MatchExpressions).To(ContainElement(metav1.LabelSelectorRequirement{
				Key:      meta.TenantLabel,
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{tnt1.Name},
			}))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("should overwrite conflicting Tenant selectors on PVC creation", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tnt1.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt1, defaultTimeoutInterval).Should(ContainElement(ns.Name))

		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "conflicting-selector-pvc",
				Namespace: ns.Name,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
				StorageClassName: ptr.To("standard"),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						meta.TenantLabel: tnt2.Name,
						"storage-tier":   "gold",
					},
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      meta.TenantLabel,
							Operator: metav1.LabelSelectorOpNotIn,
							Values:   []string{tnt1.Name},
						},
						{
							Key:      "environment",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"test"},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), &pvc)
		}).Should(Succeed())

		Eventually(func(g Gomega) {
			created := corev1.PersistentVolumeClaim{}

			g.Expect(k8sClient.Get(
				context.Background(),
				types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace},
				&created,
			)).To(Succeed())

			g.Expect(created.Spec.Selector).ToNot(BeNil())

			// The mutating webhook must preserve unrelated selector requirements.
			g.Expect(created.Spec.Selector.MatchLabels).To(HaveKeyWithValue("storage-tier", "gold"))
			g.Expect(created.Spec.Selector.MatchExpressions).To(ContainElement(metav1.LabelSelectorRequirement{
				Key:      "environment",
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"test"},
			}))

			// The tenant selector must be canonicalized.
			g.Expect(created.Spec.Selector.MatchLabels).ToNot(HaveKey(meta.TenantLabel))

			tenantExpressions := 0
			for _, expression := range created.Spec.Selector.MatchExpressions {
				if expression.Key != meta.TenantLabel {
					continue
				}

				tenantExpressions++

				g.Expect(expression.Operator).To(Equal(metav1.LabelSelectorOpIn))
				g.Expect(expression.Values).To(Equal([]string{tnt1.Name}))
			}

			g.Expect(tenantExpressions).To(Equal(1))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("should add the Tenant selector to a pre-bound PVC", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tnt1.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt1, defaultTimeoutInterval).Should(ContainElement(ns.Name))

		pv := corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{
				Name: "prebound-pv",
				Labels: map[string]string{
					meta.TenantLabel: tnt1.Name,
				},
			},
			Spec: corev1.PersistentVolumeSpec{
				Capacity: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
				StorageClassName:              "manual",
				PersistentVolumeSource: corev1.PersistentVolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: "/tmp/capsule-e2e-prebound-pv",
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), &pv)
		}).Should(Succeed())

		defer func() {
			_ = k8sClient.Delete(context.Background(), &pv)
		}()

		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "prebound-pvc",
				Namespace: ns.Name,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
				StorageClassName: ptr.To("manual"),
				VolumeName:       pv.Name,
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.Background(), &pvc)
		}).Should(Succeed())

		Eventually(func(g Gomega) {
			created := corev1.PersistentVolumeClaim{}

			g.Expect(k8sClient.Get(
				context.Background(),
				types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace},
				&created,
			)).To(Succeed())

			g.Expect(created.Spec.VolumeName).To(Equal(pv.Name))
			g.Expect(created.Spec.Selector).ToNot(BeNil())
			g.Expect(created.Spec.Selector.MatchExpressions).To(ContainElement(metav1.LabelSelectorRequirement{
				Key:      meta.TenantLabel,
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{tnt1.Name},
			}))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

})
