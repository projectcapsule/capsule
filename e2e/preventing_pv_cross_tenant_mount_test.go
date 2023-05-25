//go:build e2e

// Copyright 2020-2021 Clastix Labs
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
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

var _ = Describe("preventing PersistentVolume cross-tenant mount", func() {
	tnt1 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pv-one",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "jessica",
					Kind: "User",
				},
			},
		},
	}

	tnt2 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pv-two",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "leto",
					Kind: "User",
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
		NamespaceCreation(ns, tnt1.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
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
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
				StorageClassName: pointer.String("standard"),
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

		Expect(k8sClient.Delete(context.Background(), &pod, &client.DeleteOptions{GracePeriodSeconds: pointer.Int64(0)})).ToNot(HaveOccurred())

		ns2 := NewNamespace("")
		NamespaceCreation(ns2, tnt2.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
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
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Gi"),
						},
					},
					StorageClassName: pointer.String("standard"),
					VolumeName:       pv.Name,
				},
			}

			return k8sClient.Create(context.Background(), &pvc)
		}, defaultTimeoutInterval, defaultPollInterval).Should(HaveOccurred())
	})
})
