// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

var _ = Describe("enforcing a Container Registry", Label("tenant", "rules", "images", "registry"), func() {
	originConfig := &capsulev1beta2.CapsuleConfiguration{}

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "container-registry",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "matt",
							Kind: "User",
						},
					},
				},
			},
			Rules: []*capsulev1beta2.NamespaceRule{
				{
					NamespaceRuleBody: capsulev1beta2.NamespaceRuleBody{
						Enforce: capsulev1beta2.NamespaceRuleEnforceBody{
							Registries: []api.OCIRegistry{
								// Global: allow any registry, but require PullPolicy Always (images+volumes)
								{
									Registry: ".*",
									Validation: []api.RegistryValidationTarget{
										api.ValidateImages,
										api.ValidateVolumes,
									},
									Policy: []corev1.PullPolicy{corev1.PullAlways},
								},
								// More specific harbor rule (no policy override => should NOT remove Always restriction)
								{
									Registry: "harbor/.*",
									Validation: []api.RegistryValidationTarget{
										api.ValidateImages,
										api.ValidateVolumes,
									},
								},
							},
						},
					},
				},
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"environment": "prod",
						},
					},
					NamespaceRuleBody: capsulev1beta2.NamespaceRuleBody{
						Enforce: capsulev1beta2.NamespaceRuleEnforceBody{
							Registries: []api.OCIRegistry{
								// Prod-only special-case
								{
									Registry: "harbor/production-image/.*",
									Validation: []api.RegistryValidationTarget{
										api.ValidateImages,
										api.ValidateVolumes,
									},
									Policy: []corev1.PullPolicy{corev1.PullAlways},
								},
							},
						},
					},
				},
			},
		},
	}

	// ---- Small local helpers (keep e2e readable) ----

	expectNamespaceStatusRegistries := func(nsName string, want []string) {
		Eventually(func(g Gomega) {
			nsStatus := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(
				context.Background(),
				client.ObjectKey{Name: meta.NameForManagedRuleStatus(), Namespace: nsName},
				nsStatus,
			)).To(Succeed())

			got := make([]string, 0, len(nsStatus.Status.Rule.Enforce.Registries))
			for _, r := range nsStatus.Status.Rule.Enforce.Registries {
				got = append(got, r.Registry)
			}

			g.Expect(got).To(Equal(want))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	createPodAndExpectDenied := func(cs kubernetes.Interface, nsName string, pod *corev1.Pod, substrings ...string) {
		base := pod.DeepCopy()
		baseName := base.Name
		if baseName == "" {
			baseName = "pod"
		}

		Eventually(func() error {
			// unique name per attempt to avoid AlreadyExists
			p := base.DeepCopy()
			p.Name = fmt.Sprintf("%s-%d", baseName, int(time.Now().UnixNano()%1e6))

			_, err := cs.CoreV1().Pods(nsName).Create(context.Background(), p, metav1.CreateOptions{})
			if err == nil {
				_ = cs.CoreV1().Pods(nsName).Delete(context.Background(), p.Name, metav1.DeleteOptions{})
				return fmt.Errorf("expected create to be denied, but it succeeded")
			}

			if apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("unexpected AlreadyExists: %v", err)
			}

			msg := err.Error()
			for _, s := range substrings {
				if !strings.Contains(msg, s) {
					return fmt.Errorf("expected error to contain %q, got: %s", s, msg)
				}
			}
			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	createPodAndExpectAllowed := func(cs kubernetes.Interface, nsName string, pod *corev1.Pod) {
		EventuallyCreation(func() error {
			_, err := cs.CoreV1().Pods(nsName).Create(context.Background(), pod, metav1.CreateOptions{})
			return err
		}).Should(Succeed())
	}

	JustBeforeEach(func() {
		Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: defaultConfigurationName}, originConfig)).To(Succeed())

		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
	})

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())

		// Restore Configuration
		Eventually(func() error {
			c := &capsulev1beta2.CapsuleConfiguration{}
			if err := k8sClient.Get(context.Background(), client.ObjectKey{Name: originConfig.Name}, c); err != nil {
				return err
			}
			c.Spec = originConfig.Spec
			return k8sClient.Update(context.Background(), c)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("aggregates enforcement rules into NamespaceStatus for a non-prod namespace", func() {
		ns := NewNamespace("")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		// Non-prod: should include only the global rule body (two registries in order)
		expectNamespaceStatusRegistries(ns.GetName(), []string{
			".*",
			"harbor/.*",
		})

		// Sanity: we can still create a trivial pod with explicit Always (since global allows all registries)
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "sanity"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "c", Image: "gcr.io/google_containers/pause-amd64:3.0", ImagePullPolicy: corev1.PullAlways},
				},
			},
		}
		createPodAndExpectAllowed(cs, ns.Name, pod)
	})

	It("aggregates enforcement rules into NamespaceStatus for a prod namespace", func() {
		ns := NewNamespace("", map[string]string{
			"environment": "prod",
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		// Prod: should include global + prod rule (3 registries in order)
		expectNamespaceStatusRegistries(ns.GetName(), []string{
			".*",
			"harbor/.*",
			"harbor/production-image/.*",
		})

		// Sanity allow with Always
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "prod-sanity"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "c", Image: "harbor/production-image/app:1", ImagePullPolicy: corev1.PullAlways},
				},
			},
		}
		createPodAndExpectAllowed(cs, ns.Name, pod)
	})

	It("denies a container image when pullPolicy is not explicitly set under restriction (dev)", func() {
		ns := NewNamespace("")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		// No ImagePullPolicy set => "" => should be denied because global rule restricts policy to Always
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "no-pullpolicy"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "c", Image: "gcr.io/google_containers/pause-amd64:3.0"},
				},
			},
		}

		createPodAndExpectDenied(cs, ns.Name, pod,
			"uses pullPolicy=IfNotPresent",
			"not allowed",
			"allowed: Always",
		)
	})

	It("denies a harbor image with pullPolicy IfNotPresent because global Always must still apply (dev)", func() {
		ns := NewNamespace("")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "harbor-wrong-policy"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:            "c",
						Image:           "harbor/some-team/app:1",
						ImagePullPolicy: corev1.PullIfNotPresent,
					},
				},
			},
		}

		createPodAndExpectDenied(cs, ns.Name, pod,
			"pullPolicy=IfNotPresent",
			"not allowed",
			"allowed:",
		)
	})

	It("allows a harbor image with pullPolicy Always (dev)", func() {
		ns := NewNamespace("")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "harbor-always"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:            "c",
						Image:           "harbor/some-team/app:1",
						ImagePullPolicy: corev1.PullAlways,
					},
				},
			},
		}

		createPodAndExpectAllowed(cs, ns.Name, pod)
	})

	It("denies initContainers when they violate policy (dev) and includes the correct location in the message", func() {
		ns := NewNamespace("")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "init-deny"},
			Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{
					{
						Name:            "init",
						Image:           "harbor/some-team/init:1",
						ImagePullPolicy: corev1.PullIfNotPresent, // should be denied
					},
				},
				Containers: []corev1.Container{
					{
						Name:            "c",
						Image:           "harbor/some-team/app:1",
						ImagePullPolicy: corev1.PullAlways,
					},
				},
			},
		}

		createPodAndExpectDenied(cs, ns.Name, pod,
			"initContainers[0]",
			"pullPolicy=IfNotPresent",
			"allowed:",
		)
	})

	It("denies volume image pullPolicy if not allowed (dev)", func() {
		ns := NewNamespace("")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "volume-deny"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					// main container must exist
					{Name: "c", Image: "harbor/some-team/app:1", ImagePullPolicy: corev1.PullAlways},
				},
				Volumes: []corev1.Volume{
					{
						Name: "imgvol",
						VolumeSource: corev1.VolumeSource{
							Image: &corev1.ImageVolumeSource{
								Reference:  "harbor/some-team/volimg:1",
								PullPolicy: corev1.PullIfNotPresent, // should be denied
							},
						},
					},
				},
			},
		}

		createPodAndExpectDenied(cs, ns.Name, pod,
			"volumes[0](imgvol)",
			"pullPolicy=IfNotPresent",
			"allowed:",
		)
	})

	It("allows prod-specific image only with Always, still enforcing global policy", func() {
		ns := NewNamespace("", map[string]string{
			"environment": "prod",
		})

		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		// Wrong policy => denied
		bad := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "prod-bad"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "c", Image: "harbor/production-image/app:1", ImagePullPolicy: corev1.PullNever},
				},
			},
		}
		createPodAndExpectDenied(cs, ns.Name, bad,
			"pullPolicy=Never",
			"allowed:",
		)

		// Correct policy => allowed
		good := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "prod-good"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "c", Image: "harbor/production-image/app:1", ImagePullPolicy: corev1.PullAlways},
				},
			},
		}
		createPodAndExpectAllowed(cs, ns.Name, good)
	})

	It("denies adding an ephemeral container with wrong pullPolicy on UPDATE", func() {
		ns := NewNamespace("")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
		expectNamespaceStatusRegistries(ns.GetName(), []string{".*", "harbor/.*"})

		cleanupRBAC := GrantEphemeralContainersUpdate(ns.Name, tnt.Spec.Owners[0].UserSpec.Name)
		defer cleanupRBAC()

		// Create an allowed pod
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "base"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "c", Image: "harbor/some-team/app:1", ImagePullPolicy: corev1.PullAlways},
				},
			},
		}
		createPodAndExpectAllowed(cs, ns.Name, pod)

		// Now attempt to add an ephemeral container with IfNotPresent (should be denied)
		ephem := corev1.EphemeralContainer{
			EphemeralContainerCommon: corev1.EphemeralContainerCommon{
				Name:            "debug",
				Image:           "harbor/some-team/debug:1",
				ImagePullPolicy: corev1.PullIfNotPresent,
			},
		}

		Eventually(func() error {
			// Must use the ephemeralcontainers subresource
			cur, err := cs.CoreV1().Pods(ns.Name).Get(context.Background(), pod.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			cur.Spec.EphemeralContainers = append(cur.Spec.EphemeralContainers, ephem)

			_, err = cs.CoreV1().Pods(ns.Name).UpdateEphemeralContainers(
				context.Background(),
				cur.Name,
				cur,
				metav1.UpdateOptions{},
			)
			if err == nil {
				return fmt.Errorf("expected UpdateEphemeralContainers to be denied, but it succeeded")
			}

			msg := err.Error()
			// Your webhook reports "ephemeralContainers[0]" location
			if !strings.Contains(msg, "ephemeralContainers") || !strings.Contains(msg, "pullPolicy=IfNotPresent") {
				return fmt.Errorf("unexpected error: %v", err)
			}
			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("denies a pod when volume image reference changes to a disallowed pullPolicy (recreate)", func() {
		ns := NewNamespace("")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
		expectNamespaceStatusRegistries(ns.GetName(), []string{".*", "harbor/.*"})

		pod1 := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "vol-ok"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "c", Image: "harbor/some-team/app:1", ImagePullPolicy: corev1.PullAlways},
				},
				Volumes: []corev1.Volume{
					{
						Name: "imgvol",
						VolumeSource: corev1.VolumeSource{
							Image: &corev1.ImageVolumeSource{
								Reference:  "harbor/some-team/volimg:1",
								PullPolicy: corev1.PullAlways,
							},
						},
					},
				},
			},
		}
		createPodAndExpectAllowed(cs, ns.Name, pod1)

		pod2 := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "vol-bad"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "c", Image: "harbor/some-team/app:1", ImagePullPolicy: corev1.PullAlways},
				},
				Volumes: []corev1.Volume{
					{
						Name: "imgvol",
						VolumeSource: corev1.VolumeSource{
							Image: &corev1.ImageVolumeSource{
								Reference:  "harbor/some-team/volimg:2",
								PullPolicy: corev1.PullIfNotPresent,
							},
						},
					},
				},
			},
		}

		createPodAndExpectDenied(cs, ns.Name, pod2,
			"volumes[0](imgvol)",
			"pullPolicy=IfNotPresent",
			"allowed:",
		)
	})

})
