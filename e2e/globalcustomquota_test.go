package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

var _ = Describe("when GlobalCustomQuota uses ledger-backed reconciliation", Label("e2e", "globalcustomquota", "ledger"), Ordered, func() {
	const (
		testNamespace = "global-custom-quota-e2e-test"
		tenantLabel   = "capsule.clastix.io/tenant"
		tenantValue   = "global-custom-quota-e2e"
	)

	var (
		ctx context.Context

		ns *corev1.Namespace
	)

	makeQuota := func(name string, spec map[string]any) *unstructured.Unstructured {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   capsulev1beta2.GroupVersion.Group,
			Version: capsulev1beta2.GroupVersion.Version,
			Kind:    "GlobalCustomQuota",
		})
		obj.SetName(name)
		obj.Object["spec"] = spec
		return obj
	}

	makePod := func(namespace, name string, labels map[string]string, annotations map[string]string, image string, cpuRequest string, emptyDirSize string) *corev1.Pod {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   namespace,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "main",
						Image: image,
					},
				},
				RestartPolicy: corev1.RestartPolicyAlways,
			},
		}

		if cpuRequest != "" {
			pod.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse(cpuRequest),
			}
		}

		if emptyDirSize != "" {
			pod.Spec.Volumes = []corev1.Volume{
				{
					Name: "cache",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{
							SizeLimit: ptr.To(resource.MustParse(emptyDirSize)),
						},
					},
				},
			}
			pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
				{
					Name:      "cache",
					MountPath: "/cache",
				},
			}
		}

		return pod
	}

	makeDeployment := func(namespace, name string, replicas int32, labels map[string]string, cpuRequest string) *appsv1.Deployment {
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To(replicas),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": name,
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: mergeMaps(map[string]string{"app": name}, labels),
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "nginx",
								Image: "nginx:1.27.0",
							},
						},
					},
				},
			},
		}

		if cpuRequest != "" {
			dep.Spec.Template.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse(cpuRequest),
			}
		}

		return dep
	}

	makePVC := func(namespace, name, size string) *corev1.PersistentVolumeClaim {
		return &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse(size),
					},
				},
			},
		}
	}

	getGlobalQuota := func(name string) *capsulev1beta2.GlobalCustomQuota {
		obj := &capsulev1beta2.GlobalCustomQuota{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, obj)).To(Succeed())
		return obj
	}

	getLedger := func(name string) *capsulev1beta2.QuantityLedger {
		obj := &capsulev1beta2.QuantityLedger{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: configuration.ControllerNamespace(),
		}, obj)).To(Succeed())
		return obj
	}

	expectQuotaUsedAndClaims := func(name string, used string, claims int) {
		expectedUsed := resource.MustParse(used)

		Eventually(func(g Gomega) {
			obj := &capsulev1beta2.GlobalCustomQuota{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, obj)).To(Succeed())
			g.Expect(obj.Status.Usage.Used.Cmp(expectedUsed)).To(Equal(0))
			g.Expect(obj.Status.Usage.Available.Sign()).To(BeNumerically(">=", 0))
			g.Expect(len(obj.Status.Claims)).To(Equal(claims))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	expectLedgerSettled := func(name string) {
		Eventually(func(g Gomega) {
			obj := &capsulev1beta2.QuantityLedger{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: configuration.ControllerNamespace(),
			}, obj)).To(Succeed())
			g.Expect(obj.Status.Reserved.IsZero()).To(BeTrue())
			g.Expect(obj.Status.PendingDeletes).To(BeEmpty())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	scaleDeployment := func(namespace, name string, replicas int32) {
		Eventually(func() error {
			dep := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, dep); err != nil {
				return err
			}
			dep.Spec.Replicas = ptr.To(replicas)
			return k8sClient.Update(ctx, dep)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	updatePodLabels := func(namespace, name string, labels map[string]string) {
		Eventually(func() error {
			pod := &corev1.Pod{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, pod); err != nil {
				return err
			}
			pod.Labels = labels
			return k8sClient.Update(ctx, pod)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	updatePodImage := func(namespace, name, image string) {
		Eventually(func() error {
			pod := &corev1.Pod{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, pod); err != nil {
				return err
			}
			pod.Spec.Containers[0].Image = image
			return k8sClient.Update(ctx, pod)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	BeforeAll(func() {
		ctx = context.Background()
		utilruntime.Must(capsulev1beta2.AddToScheme(scheme.Scheme))
	})

	BeforeEach(func() {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
				Labels: map[string]string{
					tenantLabel: tenantValue,
				},
			},
		}

		EventuallyCreation(func() error {
			ns.ResourceVersion = ""
			return k8sClient.Create(ctx, ns)
		}).Should(Succeed())
	})

	AfterEach(func() {
		// delete all test resources in the namespace
		_ = k8sClient.DeleteAllOf(ctx, &appsv1.Deployment{}, client.InNamespace(testNamespace))
		_ = k8sClient.DeleteAllOf(ctx, &corev1.Pod{}, client.InNamespace(testNamespace))
		_ = k8sClient.DeleteAllOf(ctx, &corev1.PersistentVolumeClaim{}, client.InNamespace(testNamespace))

		// delete all global quotas used by tests
		quotaList := &capsulev1beta2.GlobalCustomQuotaList{}
		if err := k8sClient.List(ctx, quotaList); err == nil {
			for i := range quotaList.Items {
				item := quotaList.Items[i]
				if item.Name == "" {
					continue
				}
				if item.Labels["e2e.capsule.dev/test-suite"] == "globalcustomquota-ledger" {
					EventuallyDeletion(&item)
				}
			}
		}

		EventuallyDeletion(ns)
	})

	It("aggregates a custom pod quantity path and settles the corresponding ledger", func() {
		quota := makeQuota("gq-pod-cpu-requests", map[string]any{
			"limit": "500m",
			"namespaceSelectors": []any{
				map[string]any{
					"matchLabels": map[string]any{
						tenantLabel: tenantValue,
					},
				},
			},
			"sources": []any{
				map[string]any{
					"group":   "",
					"version": "v1",
					"kind":    "Pod",
					"op":      "add",
					"path":    ".spec.containers[*].resources.requests.cpu",
				},
			},
		})
		quota.SetLabels(map[string]string{"e2e.capsule.dev/test-suite": "globalcustomquota-ledger"})

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, quota)
		}).Should(Succeed())

		dep := makeDeployment(testNamespace, "cpu-requests", 2, nil, "100m")
		EventuallyCreation(func() error {
			dep.ResourceVersion = ""
			return k8sClient.Create(ctx, dep)
		}).Should(Succeed())

		expectQuotaUsedAndClaims("gq-pod-cpu-requests", "200m", 2)
		expectLedgerSettled("gq-pod-cpu-requests")

		scaleDeployment(testNamespace, "cpu-requests", 4)
		expectQuotaUsedAndClaims("gq-pod-cpu-requests", "400m", 4)
		expectLedgerSettled("gq-pod-cpu-requests")

		ledger := getLedger("gq-pod-cpu-requests")
		Expect(ledger.Spec.TargetRef.Kind).To(Equal("GlobalCustomQuota"))
		Expect(ledger.Spec.TargetRef.Name).To(Equal("gq-pod-cpu-requests"))

		gq := getGlobalQuota("gq-pod-cpu-requests")
		Expect(gq.Status.Usage.Used.Cmp(resource.MustParse("400m"))).To(Equal(0))
	})

	It("counts pods correctly while scaling a deployment", func() {
		quota := makeQuota("gq-pod-count", map[string]any{
			"limit": "5",
			"namespaceSelectors": []any{
				map[string]any{
					"matchLabels": map[string]any{
						tenantLabel: tenantValue,
					},
				},
			},
			"sources": []any{
				map[string]any{
					"group":   "",
					"version": "v1",
					"kind":    "Pod",
					"op":      "count",
				},
			},
		})
		quota.SetLabels(map[string]string{"e2e.capsule.dev/test-suite": "globalcustomquota-ledger"})

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, quota)
		}).Should(Succeed())

		dep := makeDeployment(testNamespace, "counted", 1, nil, "")
		EventuallyCreation(func() error {
			dep.ResourceVersion = ""
			return k8sClient.Create(ctx, dep)
		}).Should(Succeed())

		expectQuotaUsedAndClaims("gq-pod-count", "1", 1)

		scaleDeployment(testNamespace, "counted", 3)
		expectQuotaUsedAndClaims("gq-pod-count", "3", 3)

		scaleDeployment(testNamespace, "counted", 2)
		expectQuotaUsedAndClaims("gq-pod-count", "2", 2)

		expectLedgerSettled("gq-pod-count")
	})

	It("tracks count with a single MatchLabels selector and updates when the pod no longer matches", func() {
		quota := makeQuota("gq-count-single-matchlabel", map[string]any{
			"limit": "10",
			"namespaceSelectors": []any{
				map[string]any{
					"matchLabels": map[string]any{
						tenantLabel: tenantValue,
					},
				},
			},
			"sources": []any{
				map[string]any{
					"group":   "",
					"version": "v1",
					"kind":    "Pod",
					"op":      "count",
					"selectors": []any{
						map[string]any{
							"matchLabels": map[string]any{
								"track": "yes",
							},
						},
					},
				},
			},
		})
		quota.SetLabels(map[string]string{"e2e.capsule.dev/test-suite": "globalcustomquota-ledger"})

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, quota)
		}).Should(Succeed())

		pod := makePod(testNamespace, "single-matchlabel", map[string]string{"track": "yes"}, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectQuotaUsedAndClaims("gq-count-single-matchlabel", "1", 1)

		updatePodLabels(testNamespace, "single-matchlabel", map[string]string{"track": "no"})
		expectQuotaUsedAndClaims("gq-count-single-matchlabel", "0", 0)
		expectLedgerSettled("gq-count-single-matchlabel")
	})

	It("tracks count with multiple MatchLabels and updates when the pod no longer matches", func() {
		quota := makeQuota("gq-count-multi-matchlabel", map[string]any{
			"limit": "10",
			"namespaceSelectors": []any{
				map[string]any{
					"matchLabels": map[string]any{
						tenantLabel: tenantValue,
					},
				},
			},
			"sources": []any{
				map[string]any{
					"group":   "",
					"version": "v1",
					"kind":    "Pod",
					"op":      "count",
					"selectors": []any{
						map[string]any{
							"matchLabels": map[string]any{
								"track": "yes",
								"tier":  "frontend",
							},
						},
					},
				},
			},
		})
		quota.SetLabels(map[string]string{"e2e.capsule.dev/test-suite": "globalcustomquota-ledger"})

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, quota)
		}).Should(Succeed())

		pod := makePod(testNamespace, "multi-matchlabel", map[string]string{
			"track": "yes",
			"tier":  "frontend",
		}, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectQuotaUsedAndClaims("gq-count-multi-matchlabel", "1", 1)

		updatePodLabels(testNamespace, "multi-matchlabel", map[string]string{
			"track": "yes",
			"tier":  "backend",
		})
		expectQuotaUsedAndClaims("gq-count-multi-matchlabel", "0", 0)
		expectLedgerSettled("gq-count-multi-matchlabel")
	})

	It("tracks count with a single field selector and updates when the pod no longer matches", func() {
		quota := makeQuota("gq-count-single-fieldselector", map[string]any{
			"limit": "10",
			"namespaceSelectors": []any{
				map[string]any{
					"matchLabels": map[string]any{
						tenantLabel: tenantValue,
					},
				},
			},
			"sources": []any{
				map[string]any{
					"group":   "",
					"version": "v1",
					"kind":    "Pod",
					"op":      "count",
					"selectors": []any{
						map[string]any{
							"fieldSelectors": []any{
								`.spec.containers[?(@.image=="nginx:1.27.0")]`,
							},
						},
					},
				},
			},
		})
		quota.SetLabels(map[string]string{"e2e.capsule.dev/test-suite": "globalcustomquota-ledger"})

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, quota)
		}).Should(Succeed())

		pod := makePod(testNamespace, "single-fieldselector", nil, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectQuotaUsedAndClaims("gq-count-single-fieldselector", "1", 1)

		updatePodImage(testNamespace, "single-fieldselector", "nginx:1.26.0")
		expectQuotaUsedAndClaims("gq-count-single-fieldselector", "0", 0)
		expectLedgerSettled("gq-count-single-fieldselector")
	})

	It("tracks count with multiple field selectors and updates when the pod no longer matches", func() {
		quota := makeQuota("gq-count-multi-fieldselector", map[string]any{
			"limit": "10",
			"namespaceSelectors": []any{
				map[string]any{
					"matchLabels": map[string]any{
						tenantLabel: tenantValue,
					},
				},
			},
			"sources": []any{
				map[string]any{
					"group":   "",
					"version": "v1",
					"kind":    "Pod",
					"op":      "count",
					"selectors": []any{
						map[string]any{
							"fieldSelectors": []any{
								`.spec.containers[?(@.image=="nginx:1.27.0")]`,
								`.spec.restartPolicy[?(@=="Always")]`,
							},
						},
					},
				},
			},
		})
		quota.SetLabels(map[string]string{"e2e.capsule.dev/test-suite": "globalcustomquota-ledger"})

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, quota)
		}).Should(Succeed())

		pod := makePod(testNamespace, "multi-fieldselector", nil, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectQuotaUsedAndClaims("gq-count-multi-fieldselector", "1", 1)

		updatePodImage(testNamespace, "multi-fieldselector", "nginx:1.26.0")
		expectQuotaUsedAndClaims("gq-count-multi-fieldselector", "0", 0)
		expectLedgerSettled("gq-count-multi-fieldselector")
	})

	It("aggregates multiple sources across pod emptyDir size and pvc storage size", func() {
		quota := makeQuota("gq-multi-source-storage", map[string]any{
			"limit": "3Gi",
			"namespaceSelectors": []any{
				map[string]any{
					"matchLabels": map[string]any{
						tenantLabel: tenantValue,
					},
				},
			},
			"sources": []any{
				map[string]any{
					"group":   "",
					"version": "v1",
					"kind":    "Pod",
					"op":      "add",
					"path":    ".spec.volumes[*].emptyDir.sizeLimit",
				},
				map[string]any{
					"group":   "",
					"version": "v1",
					"kind":    "PersistentVolumeClaim",
					"op":      "add",
					"path":    ".spec.resources.requests.storage",
				},
			},
		})
		quota.SetLabels(map[string]string{"e2e.capsule.dev/test-suite": "globalcustomquota-ledger"})

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, quota)
		}).Should(Succeed())

		pod := makePod(testNamespace, "multi-source-pod", nil, nil, "nginx:1.27.0", "", "1Gi")
		pvc := makePVC(testNamespace, "multi-source-pvc", "2Gi")

		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			pvc.ResourceVersion = ""
			return k8sClient.Create(ctx, pvc)
		}).Should(Succeed())

		expectQuotaUsedAndClaims("gq-multi-source-storage", "3Gi", 2)
		expectLedgerSettled("gq-multi-source-storage")
	})
})

func mergeMaps(base map[string]string, extra map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}
