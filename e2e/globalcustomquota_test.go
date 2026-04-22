package e2e

import (
	"context"
	"fmt"
	"sort"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	capmeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

var _ = Describe("when GlobalCustomQuota uses ledger-backed reconciliation", Label("e2e", "globalcustomquota", "ledger"), Ordered, func() {
	const (
		testNamespace = "global-custom-quota-e2e-test"
		tenantLabel   = "capsule.clastix.io/tenant"
		tenantValue   = "global-custom-quota-e2e"
	)

	var (
		ctx context.Context
		ns  *corev1.Namespace
	)

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
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, obj)).To(Succeed(),
				"failed to get GlobalCustomQuota %s", name)

			g.Expect(
				obj.Status.Usage.Used.Cmp(expectedUsed),
			).To(Equal(0),
				"unexpected used value for %s: used=%q expectedUsed=%q available=%q claims=%d expectedClaims=%d",
				name,
				obj.Status.Usage.Used.String(),
				used,
				obj.Status.Usage.Available.String(),
				len(obj.Status.Claims),
				claims,
			)

			g.Expect(obj.Status.Usage.Used.Sign()).To(BeNumerically(">=", 0),
				"usage went negative for %s: used=%q", name, obj.Status.Usage.Used.String())

			g.Expect(obj.Status.Usage.Available.Sign()).To(BeNumerically(">=", 0),
				"available went negative for %s: available=%q", name, obj.Status.Usage.Available.String())

			g.Expect(len(obj.Status.Claims)).To(Equal(claims),
				"unexpected claims for %s: used=%q expectedUsed=%q available=%q claims=%d expectedClaims=%d",
				name,
				obj.Status.Usage.Used.String(),
				used,
				obj.Status.Usage.Available.String(),
				len(obj.Status.Claims),
				claims,
			)
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

	awaitGlobalQuotaReady := func(name string) {
		Eventually(func(g Gomega) {
			gq := &capsulev1beta2.GlobalCustomQuota{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, gq)).To(Succeed())

			g.Expect(gq.Status.Targets).NotTo(Equal(0))

			// status should be initialized by the controller
			g.Expect(gq.Status.Usage.Used.String()).NotTo(BeEmpty())
			g.Expect(gq.Status.Usage.Available.String()).NotTo(BeEmpty())

			ledger := &capsulev1beta2.QuantityLedger{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: configuration.ControllerNamespace(),
			}, ledger)).To(Succeed())

			g.Expect(capmeta.IsStatusConditionTrue(gq.Status.Conditions, capmeta.ReadyCondition)).To(BeTrue())

			g.Expect(ledger.Spec.TargetRef.Kind).To(Equal("GlobalCustomQuota"))
			g.Expect(ledger.Spec.TargetRef.Name).To(Equal(name))

			// initial ledger should be settled before test objects are created
			g.Expect(ledger.Status.Reserved.IsZero()).To(BeTrue())
			g.Expect(ledger.Status.PendingDeletes).To(BeEmpty())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	awaitAllGlobalQuotasReady := func(names ...string) {
		for _, name := range names {
			awaitGlobalQuotaReady(name)
		}
	}

	expectPodCreationDeniedContaining := func(build func(name string) *corev1.Pod, expected string) {
		Eventually(func() string {
			name := fmt.Sprintf("denied-%d", time.Now().UnixNano())
			obj := build(name)

			err := k8sClient.Create(ctx, obj)
			if err == nil {
				_ = k8sClient.Delete(ctx, obj)
				return ""
			}

			return err.Error()
		}, defaultTimeoutInterval, defaultPollInterval).Should(ContainSubstring(expected))
	}

	expectGlobalQuotaWildcardNamespaces := func(name string) {
		Eventually(func(g Gomega) {
			obj := &capsulev1beta2.GlobalCustomQuota{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, obj)).To(Succeed())

			g.Expect(obj.Status.Namespaces).To(ContainElement("*"),
				"expected wildcard namespace status for %s, got=%v",
				name, obj.Status.Namespaces,
			)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}

	expectGlobalQuotaNamespaces := func(name string, expected ...string) {
		Eventually(func(g Gomega) {
			obj := &capsulev1beta2.GlobalCustomQuota{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, obj)).To(Succeed())

			actual := append([]string(nil), obj.Status.Namespaces...)
			sort.Strings(actual)

			want := append([]string(nil), expected...)
			sort.Strings(want)

			g.Expect(actual).To(Equal(want),
				"unexpected status.namespaces for %s: got=%v want=%v",
				name, actual, want,
			)
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

	It("does not produce negative usage when a matching pod is relabeled to no longer match", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-no-negative-on-relabel",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("10"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpCount,
							Selectors: []selectors.SelectorWithFields{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"track": "yes",
										},
									},
								},
							},
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitGlobalQuotaReady(q.GetName())

		pod := makePod(testNamespace, "no-negative-on-relabel", map[string]string{"track": "yes"}, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectQuotaUsedAndClaims(q.GetName(), "1", 1)

		updatePodLabels(testNamespace, "no-negative-on-relabel", map[string]string{"track": "no"})
		expectLedgerSettled(q.GetName())

		Eventually(func(g Gomega) {
			obj := &capsulev1beta2.GlobalCustomQuota{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: q.GetName()}, obj)).To(Succeed())
			g.Expect(obj.Status.Usage.Used.Sign()).To(BeNumerically(">=", 0))
			g.Expect(obj.Status.Usage.Used.Cmp(resource.MustParse("0"))).To(Equal(0))
			g.Expect(len(obj.Status.Claims)).To(Equal(0))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("aggregates a custom pod quantity path and settles the corresponding ledger", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-pod-cpu-requests",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("500m"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpAdd,
							Path:      ".spec.containers[*].resources.requests.cpu",
							Selectors: []selectors.SelectorWithFields{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"track": "yes",
										},
									},
								},
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitGlobalQuotaReady(q.GetName())

		dep := makeDeployment(testNamespace, "cpu-requests", 2, map[string]string{
			"track": "yes",
		}, "100m")
		EventuallyCreation(func() error {
			dep.ResourceVersion = ""
			return k8sClient.Create(ctx, dep)
		}).Should(Succeed())

		expectQuotaUsedAndClaims(q.GetName(), "200m", 2)
		expectLedgerSettled(q.GetName())

		scaleDeployment(testNamespace, "cpu-requests", 4)
		expectQuotaUsedAndClaims(q.GetName(), "400m", 4)
		expectLedgerSettled(q.GetName())

		ledger := getLedger(q.GetName())
		Expect(ledger.Spec.TargetRef.Kind).To(Equal("GlobalCustomQuota"))
		Expect(ledger.Spec.TargetRef.Name).To(Equal(q.GetName()))

		gq := getGlobalQuota(q.GetName())
		Expect(gq.Status.Usage.Used.Cmp(resource.MustParse("400m"))).To(Equal(0))
	})

	It("counts pods correctly while scaling a deployment", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-pod-count",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("5"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpCount,
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitGlobalQuotaReady(q.GetName())

		dep := makeDeployment(testNamespace, "counted", 1, nil, "")
		EventuallyCreation(func() error {
			dep.ResourceVersion = ""
			return k8sClient.Create(ctx, dep)
		}).Should(Succeed())

		expectQuotaUsedAndClaims(q.GetName(), "1", 1)

		scaleDeployment(testNamespace, "counted", 3)
		expectQuotaUsedAndClaims(q.GetName(), "3", 3)

		scaleDeployment(testNamespace, "counted", 2)
		expectQuotaUsedAndClaims(q.GetName(), "2", 2)

		expectLedgerSettled(q.GetName())
	})

	It("tracks count with a single MatchLabels selector and updates when the pod no longer matches", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-count-single-matchlabel",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("10"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpCount,
							Selectors: []selectors.SelectorWithFields{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"track": "yes",
										},
									},
								},
							},
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())

		pod := makePod(testNamespace, "single-matchlabel", map[string]string{"track": "yes"}, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectQuotaUsedAndClaims(q.GetName(), "1", 1)

		updatePodLabels(testNamespace, "single-matchlabel", map[string]string{"track": "no"})
		expectQuotaUsedAndClaims(q.GetName(), "0", 0)
		expectLedgerSettled(q.GetName())
	})

	It("tracks count with multiple MatchLabels and updates when the pod no longer matches", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-count-multi-matchlabel",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("10"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpCount,
							Selectors: []selectors.SelectorWithFields{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"track": "yes",
											"tier":  "frontend",
										},
									},
								},
							},
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())

		pod := makePod(testNamespace, "multi-matchlabel", map[string]string{
			"track": "yes",
			"tier":  "frontend",
		}, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectQuotaUsedAndClaims(q.GetName(), "1", 1)

		updatePodLabels(testNamespace, "multi-matchlabel", map[string]string{
			"track": "yes",
			"tier":  "backend",
		})
		expectQuotaUsedAndClaims(q.GetName(), "0", 0)
		expectLedgerSettled(q.GetName())
	})

	It("tracks count with a single field selector and updates when the pod no longer matches", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-count-single-fieldselector",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("10"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpCount,
							Selectors: []selectors.SelectorWithFields{
								{
									FieldSelectors: []string{
										`.spec.containers[?(@.image=="nginx:1.27.0")]`,
									},
								},
							},
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())

		pod := makePod(testNamespace, "single-fieldselector", nil, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectQuotaUsedAndClaims(q.GetName(), "1", 1)

		updatePodImage(testNamespace, "single-fieldselector", "nginx:1.26.0")
		expectQuotaUsedAndClaims(q.GetName(), "0", 0)
		expectLedgerSettled(q.GetName())
	})

	It("tracks count with multiple field selectors and updates when the pod no longer matches", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-count-multi-fieldselector",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("10"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpCount,
							Selectors: []selectors.SelectorWithFields{
								{
									FieldSelectors: []string{
										`.spec.containers[?(@.image=="nginx:1.27.0")]`,
										`.spec.containers[?(@.name=="main")]`,
									},
								},
							},
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())

		pod := makePod(testNamespace, "multi-fieldselector", nil, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectQuotaUsedAndClaims(q.GetName(), "1", 1)

		updatePodImage(testNamespace, "multi-fieldselector", "nginx:1.26.0")
		expectQuotaUsedAndClaims(q.GetName(), "0", 0)
		expectLedgerSettled(q.GetName())
	})

	It("aggregates multiple sources across pod emptyDir size and pvc storage size", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-multi-source-storage",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("3Gi"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Path:      ".spec.volumes[*].emptyDir.sizeLimit",
							Operation: quota.OpAdd,
						},
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "PersistentVolumeClaim",
							},
							Path:      ".spec.resources.requests.storage",
							Operation: quota.OpAdd,
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
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

		expectQuotaUsedAndClaims(q.GetName(), "3Gi", 2)
		expectLedgerSettled(q.GetName())
	})

	It("tracks count with multiple field selectors and updates when the pod no longer matches", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-count-multi-fieldselector",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("10"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpCount,
							Selectors: []selectors.SelectorWithFields{
								{
									FieldSelectors: []string{
										`.spec.containers[?(@.image=="nginx:1.27.0")]`,
										`.spec.containers[?(@.name=="main")]`,
									},
								},
							},
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())

		pod := makePod(testNamespace, "multi-fieldselector", nil, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectQuotaUsedAndClaims(q.GetName(), "1", 1)

		updatePodImage(testNamespace, "multi-fieldselector", "nginx:1.26.0")
		expectQuotaUsedAndClaims(q.GetName(), "0", 0)
		expectLedgerSettled(q.GetName())
	})

	It("aggregates multiple sources with selectors across pod emptyDir and pvc storage", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-multi-source-selectors-storage",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("10Gi"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Path:      ".spec.volumes[*].emptyDir.sizeLimit",
							Operation: quota.OpAdd,
							Selectors: []selectors.SelectorWithFields{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"track": "yes",
										},
									},
								},
							},
						},
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "PersistentVolumeClaim",
							},
							Path:      ".spec.resources.requests.storage",
							Operation: quota.OpAdd,
							Selectors: []selectors.SelectorWithFields{
								{
									FieldSelectors: []string{
										`.spec.accessModes[?(@=="ReadWriteOnce")]`,
									},
								},
							},
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())

		matchingPod := makePod(testNamespace, "matching-emptydir", map[string]string{"track": "yes"}, nil, "nginx:1.27.0", "", "1Gi")
		nonMatchingPod := makePod(testNamespace, "ignored-emptydir", map[string]string{"track": "no"}, nil, "nginx:1.27.0", "", "5Gi")

		matchingPVC := makePVC(testNamespace, "matching-pvc", "2Gi")
		nonMatchingPVC := makePVC(testNamespace, "ignored-pvc", "4Gi")
		nonMatchingPVC.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadOnlyMany}

		EventuallyCreation(func() error {
			matchingPod.ResourceVersion = ""
			return k8sClient.Create(ctx, matchingPod)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			nonMatchingPod.ResourceVersion = ""
			return k8sClient.Create(ctx, nonMatchingPod)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			matchingPVC.ResourceVersion = ""
			return k8sClient.Create(ctx, matchingPVC)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			nonMatchingPVC.ResourceVersion = ""
			return k8sClient.Create(ctx, nonMatchingPVC)
		}).Should(Succeed())

		expectQuotaUsedAndClaims(q.GetName(), "3Gi", 2)
		expectLedgerSettled(q.GetName())
	})

	It("reconciles multiple sources when objects stop matching or are deleted", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-multi-source-reconcile",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("10Gi"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Path:      ".spec.volumes[*].emptyDir.sizeLimit",
							Operation: quota.OpAdd,
							Selectors: []selectors.SelectorWithFields{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"track": "yes",
										},
									},
								},
							},
						},
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "PersistentVolumeClaim",
							},
							Path:      ".spec.resources.requests.storage",
							Operation: quota.OpAdd,
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())

		pod := makePod(testNamespace, "reconcile-emptydir", map[string]string{"track": "yes"}, nil, "nginx:1.27.0", "", "1Gi")
		pvc := makePVC(testNamespace, "reconcile-pvc", "2Gi")

		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			pvc.ResourceVersion = ""
			return k8sClient.Create(ctx, pvc)
		}).Should(Succeed())

		expectQuotaUsedAndClaims(q.GetName(), "3Gi", 2)

		updatePodLabels(testNamespace, "reconcile-emptydir", map[string]string{"track": "no"})
		expectQuotaUsedAndClaims(q.GetName(), "2Gi", 1)

		EventuallyDeletion(pvc)
		expectQuotaUsedAndClaims(q.GetName(), "0", 0)
		expectLedgerSettled(q.GetName())
	})

	It("treats a missing quantity path as zero contribution", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-wrong-path-zero",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("10"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Path:      ".spec.volumes[*].doesNotExist.sizeLimit",
							Operation: quota.OpAdd,
						},
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "PersistentVolumeClaim",
							},
							Path:      ".spec.resources.requests.thisDoesNotExist",
							Operation: quota.OpAdd,
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())

		pod := makePod(testNamespace, "wrong-path-pod", nil, nil, "nginx:1.27.0", "", "1Gi")
		pvc := makePVC(testNamespace, "wrong-path-pvc", "2Gi")

		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			pvc.ResourceVersion = ""
			return k8sClient.Create(ctx, pvc)
		}).Should(Succeed())

		expectQuotaUsedAndClaims(q.GetName(), "0", 2)
		expectLedgerSettled(q.GetName())
	})

	It("rejects admission when a field selector uses an invalid jsonpath filter on a scalar", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-invalid-fieldselector",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("10"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpCount,
							Selectors: []selectors.SelectorWithFields{
								{
									FieldSelectors: []string{
										`.spec.restartPolicy[?(@=="Always")]`,
									},
								},
							},
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())

		pod := makePod(testNamespace, "invalid-selector-pod", nil, nil, "nginx:1.27.0", "", "")

		Eventually(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}, defaultTimeoutInterval, defaultPollInterval).Should(
			MatchError(ContainSubstring("is not array or slice and cannot be filtered")),
		)
	})

	It("uses the smallest matching global quota as authoritative while accounting usage in all matching quotas for pod count", func() {
		small := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-pod-count-small",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("2"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpCount,
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		large := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-pod-count-large",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("5"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpCount,
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, small) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, large) }).Should(Succeed())

		awaitAllGlobalQuotasReady(small.GetName(), large.GetName())

		pod1 := makePod(testNamespace, "multi-gq-count-1", nil, nil, "nginx:1.27.0", "", "")
		pod2 := makePod(testNamespace, "multi-gq-count-2", nil, nil, "nginx:1.27.0", "", "")

		EventuallyCreation(func() error {
			pod1.ResourceVersion = ""
			return k8sClient.Create(ctx, pod1)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			pod2.ResourceVersion = ""
			return k8sClient.Create(ctx, pod2)
		}).Should(Succeed())

		expectLedgerSettled(small.GetName())
		expectLedgerSettled("gq-pod-count-large")
		expectQuotaUsedAndClaims(small.GetName(), "2", 2)
		expectQuotaUsedAndClaims("gq-pod-count-large", "2", 2)

		expectPodCreationDeniedContaining(func(name string) *corev1.Pod {
			return makePod(testNamespace, name, nil, nil, "nginx:1.27.0", "", "")
		}, `GlobalCustomQuota "gq-pod-count-small"`)

		expectQuotaUsedAndClaims(small.GetName(), "2", 2)
		expectQuotaUsedAndClaims(large.GetName(), "2", 2)
	})

	It("uses the smallest matching global quota as authoritative while accounting usage in all matching quotas for pod cpu requests", func() {
		small := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-pod-cpu-small",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("200m"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpAdd,
							Path:      ".spec.containers[*].resources.requests.cpu",
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		large := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-pod-cpu-large",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("500m"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpAdd,
							Path:      ".spec.containers[*].resources.requests.cpu",
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, small) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, large) }).Should(Succeed())

		awaitAllGlobalQuotasReady(small.GetName(), large.GetName())

		pod1 := makePod(testNamespace, "multi-gq-cpu-1", nil, nil, "nginx:1.27.0", "100m", "")
		pod2 := makePod(testNamespace, "multi-gq-cpu-2", nil, nil, "nginx:1.27.0", "100m", "")

		EventuallyCreation(func() error {
			pod1.ResourceVersion = ""
			return k8sClient.Create(ctx, pod1)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			pod2.ResourceVersion = ""
			return k8sClient.Create(ctx, pod2)
		}).Should(Succeed())

		expectLedgerSettled(small.GetName())
		expectLedgerSettled(large.GetName())
		expectQuotaUsedAndClaims(small.GetName(), "200m", 2)
		expectQuotaUsedAndClaims(large.GetName(), "200m", 2)

		expectPodCreationDeniedContaining(func(name string) *corev1.Pod {
			return makePod(testNamespace, name, nil, nil, "nginx:1.27.0", "100m", "")
		}, `GlobalCustomQuota "gq-pod-cpu-small"`)

		expectQuotaUsedAndClaims(small.GetName(), "200m", 2)
		expectQuotaUsedAndClaims(large.GetName(), "200m", 2)
	})

	It("accounts only the matching subset for overlapping selectors on the same pod gvk", func() {

		broad := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-pod-track-broad",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("5"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpCount,
							Selectors: []selectors.SelectorWithFields{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"track": "yes",
										},
									},
								},
							},
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		narrow := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-pod-track-frontend",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("2"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpCount,
							Selectors: []selectors.SelectorWithFields{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"track": "yes",
											"tier":  "frontend",
										},
									},
								},
							},
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, broad) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, narrow) }).Should(Succeed())

		awaitAllGlobalQuotasReady(narrow.GetName(), broad.GetName())

		pod1 := makePod(testNamespace, "track-frontend-1", map[string]string{"track": "yes", "tier": "frontend"}, nil, "nginx:1.27.0", "", "")
		pod2 := makePod(testNamespace, "track-frontend-2", map[string]string{"track": "yes", "tier": "frontend"}, nil, "nginx:1.27.0", "", "")
		pod3 := makePod(testNamespace, "track-backend-1", map[string]string{"track": "yes", "tier": "backend"}, nil, "nginx:1.27.0", "", "")
		pod4 := makePod(testNamespace, "track-backend-2", map[string]string{"track": "yes", "tier": "backend"}, nil, "nginx:1.27.0", "", "")

		EventuallyCreation(func() error { pod1.ResourceVersion = ""; return k8sClient.Create(ctx, pod1) }).Should(Succeed())
		EventuallyCreation(func() error { pod2.ResourceVersion = ""; return k8sClient.Create(ctx, pod2) }).Should(Succeed())
		EventuallyCreation(func() error { pod3.ResourceVersion = ""; return k8sClient.Create(ctx, pod3) }).Should(Succeed())
		EventuallyCreation(func() error { pod4.ResourceVersion = ""; return k8sClient.Create(ctx, pod4) }).Should(Succeed())

		expectLedgerSettled(broad.GetName())
		expectLedgerSettled(narrow.GetName())
		expectQuotaUsedAndClaims(broad.GetName(), "4", 4)
		expectQuotaUsedAndClaims(narrow.GetName(), "2", 2)

		expectPodCreationDeniedContaining(func(name string) *corev1.Pod {
			return makePod(testNamespace, name, map[string]string{"track": "yes", "tier": "frontend"}, nil, "nginx:1.27.0", "", "")
		}, `GlobalCustomQuota "gq-pod-track-frontend"`)

		expectQuotaUsedAndClaims(broad.GetName(), "4", 4)
		expectQuotaUsedAndClaims(narrow.GetName(), "2", 2)

		EventuallyCreation(func() error {
			pod := makePod(testNamespace, "track-backend-3", map[string]string{"track": "yes", "tier": "backend"}, nil, "nginx:1.27.0", "", "")
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectLedgerSettled(broad.GetName())
		expectLedgerSettled(narrow.GetName())
		expectQuotaUsedAndClaims(broad.GetName(), "5", 5)
		expectQuotaUsedAndClaims(narrow.GetName(), "2", 2)
	})

	It("tracks different paths independently when multiple global quotas match the same pod gvk", func() {
		cpuQuota := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-pod-path-cpu",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("400m"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpAdd,
							Path:      ".spec.containers[*].resources.requests.cpu",
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		emptyDirQuota := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-pod-path-emptydir",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("2Gi"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpAdd,
							Path:      ".spec.volumes[*].emptyDir.sizeLimit",
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, cpuQuota) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, emptyDirQuota) }).Should(Succeed())

		awaitAllGlobalQuotasReady(cpuQuota.GetName(), emptyDirQuota.GetName())

		pod1 := makePod(testNamespace, "path-pod-1", nil, nil, "nginx:1.27.0", "100m", "1Gi")
		pod2 := makePod(testNamespace, "path-pod-2", nil, nil, "nginx:1.27.0", "100m", "1Gi")

		EventuallyCreation(func() error { pod1.ResourceVersion = ""; return k8sClient.Create(ctx, pod1) }).Should(Succeed())
		EventuallyCreation(func() error { pod2.ResourceVersion = ""; return k8sClient.Create(ctx, pod2) }).Should(Succeed())

		expectLedgerSettled(cpuQuota.GetName())
		expectLedgerSettled(emptyDirQuota.GetName())
		expectQuotaUsedAndClaims(cpuQuota.GetName(), "200m", 2)
		expectQuotaUsedAndClaims(emptyDirQuota.GetName(), "2Gi", 2)

		expectPodCreationDeniedContaining(func(name string) *corev1.Pod {
			return makePod(testNamespace, name, nil, nil, "nginx:1.27.0", "100m", "1Gi")
		}, `GlobalCustomQuota "gq-pod-path-emptydir"`)

		expectQuotaUsedAndClaims(cpuQuota.GetName(), "200m", 2)
		expectQuotaUsedAndClaims(emptyDirQuota.GetName(), "2Gi", 2)
	})

	It("accounts only the quotas that actually match when multiple global quotas share the same gvk", func() {
		labelQuota := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-pod-track-only",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("10"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpCount,
							Selectors: []selectors.SelectorWithFields{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"track": "yes",
										},
									},
								},
							},
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		fieldQuota := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-pod-nginx-only",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("10"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpCount,
							Selectors: []selectors.SelectorWithFields{
								{
									FieldSelectors: []string{
										`.spec.containers[?(@.image=="nginx:1.27.0")]`,
									},
								},
							},
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, labelQuota) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, fieldQuota) }).Should(Succeed())

		awaitAllGlobalQuotasReady(labelQuota.GetName(), fieldQuota.GetName())

		matchBoth := makePod(testNamespace, "subset-both", map[string]string{"track": "yes"}, nil, "nginx:1.27.0", "", "")
		matchLabelOnly := makePod(testNamespace, "subset-label-only", map[string]string{"track": "yes"}, nil, "busybox:1.36.1", "", "")
		matchFieldOnly := makePod(testNamespace, "subset-field-only", nil, nil, "nginx:1.27.0", "", "")

		EventuallyCreation(func() error { matchBoth.ResourceVersion = ""; return k8sClient.Create(ctx, matchBoth) }).Should(Succeed())
		EventuallyCreation(func() error { matchLabelOnly.ResourceVersion = ""; return k8sClient.Create(ctx, matchLabelOnly) }).Should(Succeed())
		EventuallyCreation(func() error { matchFieldOnly.ResourceVersion = ""; return k8sClient.Create(ctx, matchFieldOnly) }).Should(Succeed())

		expectLedgerSettled(labelQuota.GetName())
		expectLedgerSettled(fieldQuota.GetName())
		expectQuotaUsedAndClaims(labelQuota.GetName(), "2", 2)
		expectQuotaUsedAndClaims(fieldQuota.GetName(), "2", 2)
	})

	It("uses deterministic tie-breaking when multiple global quotas have the same remaining availability", func() {
		quotaA := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-tie-a",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("3"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpCount,
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		quotaB := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-tie-b",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("4"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpCount,
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, quotaA) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, quotaB) }).Should(Succeed())

		awaitAllGlobalQuotasReady(quotaA.GetName(), quotaB.GetName())

		// Drive them to equal remaining availability:
		// gq-tie-a limit 3, used 1 => available 2
		// gq-tie-b limit 4, used 2 => available 2
		pod1 := makePod(testNamespace, "tie-1", nil, nil, "nginx:1.27.0", "", "")
		pod2 := makePod(testNamespace, "tie-2", nil, nil, "nginx:1.27.0", "", "")

		EventuallyCreation(func() error { pod1.ResourceVersion = ""; return k8sClient.Create(ctx, pod1) }).Should(Succeed())
		EventuallyCreation(func() error { pod2.ResourceVersion = ""; return k8sClient.Create(ctx, pod2) }).Should(Succeed())

		expectLedgerSettled(quotaA.GetName())
		expectLedgerSettled(quotaB.GetName())
		expectQuotaUsedAndClaims(quotaA.GetName(), "2", 2)
		expectQuotaUsedAndClaims(quotaB.GetName(), "2", 2)

		// Next pod is still allowed, because both have 1 and 2 available respectively after pod3.
		pod3 := makePod(testNamespace, "tie-3", nil, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error { pod3.ResourceVersion = ""; return k8sClient.Create(ctx, pod3) }).Should(Succeed())

		expectLedgerSettled(quotaA.GetName())
		expectLedgerSettled(quotaB.GetName())
		expectQuotaUsedAndClaims(quotaA.GetName(), "3", 3)
		expectQuotaUsedAndClaims(quotaB.GetName(), "3", 3)

		// Now gq-tie-a is exhausted first and should be authoritative.
		expectPodCreationDeniedContaining(func(name string) *corev1.Pod {
			return makePod(testNamespace, name, nil, nil, "nginx:1.27.0", "", "")
		}, `GlobalCustomQuota "gq-tie-a"`)

		expectQuotaUsedAndClaims(quotaA.GetName(), "3", 3)
		expectQuotaUsedAndClaims(quotaB.GetName(), "3", 3)
	})

	It("aggregates the same successful pod into multiple quotas with different paths on the same gvk", func() {
		cpuQuota := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-multi-path-cpu",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("300m"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpAdd,
							Path:      ".spec.containers[*].resources.requests.cpu",
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		emptyDirQuota := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-multi-path-emptydir",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("3Gi"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpAdd,
							Path:      ".spec.volumes[*].emptyDir.sizeLimit",
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, cpuQuota) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, emptyDirQuota) }).Should(Succeed())

		awaitAllGlobalQuotasReady(cpuQuota.GetName(), emptyDirQuota.GetName())

		pod := makePod(testNamespace, "multi-path-shared-1", nil, nil, "nginx:1.27.0", "100m", "1Gi")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectLedgerSettled(cpuQuota.GetName())
		expectLedgerSettled(emptyDirQuota.GetName())
		expectQuotaUsedAndClaims(cpuQuota.GetName(), "100m", 1)
		expectQuotaUsedAndClaims(emptyDirQuota.GetName(), "1Gi", 1)

		pod2 := makePod(testNamespace, "multi-path-shared-2", nil, nil, "nginx:1.27.0", "100m", "1Gi")
		EventuallyCreation(func() error {
			pod2.ResourceVersion = ""
			return k8sClient.Create(ctx, pod2)
		}).Should(Succeed())

		expectQuotaUsedAndClaims(cpuQuota.GetName(), "200m", 2)
		expectQuotaUsedAndClaims(emptyDirQuota.GetName(), "2Gi", 2)
	})

	It("posts wildcard namespace status when no namespaceSelectors are configured and keeps it stable as namespaces change", func() {
		quota := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-nsstatus-all",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("10"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpCount,
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								meta.TenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		extraA := NewNamespace("gq-nsstatus-all-a", map[string]string{"purpose": "e2e"})
		extraB := NewNamespace("gq-nsstatus-all-b", map[string]string{"purpose": "e2e"})

		EventuallyCreation(func() error { return k8sClient.Create(ctx, quota) }).Should(Succeed())
		awaitGlobalQuotaReady(quota.GetName())

		expectGlobalQuotaWildcardNamespaces(quota.GetName())

		NamespaceDeletionAdmin(extraA, defaultTimeoutInterval)
		expectGlobalQuotaWildcardNamespaces(quota.GetName())

		NamespaceDeletionAdmin(extraB, defaultTimeoutInterval)
		expectGlobalQuotaWildcardNamespaces(quota.GetName())

		NamespaceDeletionAdmin(extraA, defaultTimeoutInterval)
		expectGlobalQuotaWildcardNamespaces(quota.GetName())

		NamespaceDeletionAdmin(extraB, defaultTimeoutInterval)
		expectGlobalQuotaWildcardNamespaces(quota.GetName())
	})

	It("posts all namespaces matched by multiple namespaceSelectors and updates status on namespace create and delete", func() {
		nsA1 := NewNamespace("gq-nsstatus-a1", map[string]string{"team": "a"})
		nsA2 := NewNamespace("gq-nsstatus-a2", map[string]string{"team": "a"})
		nsB1 := NewNamespace("gq-nsstatus-b1", map[string]string{"team": "b"})
		nsOther := NewNamespace("gq-nsstatus-other", map[string]string{"team": "other"})

		NamespaceCreationAdmin(nsA1, defaultTimeoutInterval)
		NamespaceCreationAdmin(nsB1, defaultTimeoutInterval)
		NamespaceCreationAdmin(nsOther, defaultTimeoutInterval)

		quota := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-nsstatus-multi-selectors",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("10"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpCount,
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"team": "a",
							},
						},
					},
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"team": "b",
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, quota) }).Should(Succeed())
		awaitGlobalQuotaReady(quota.GetName())

		expectGlobalQuotaNamespaces(quota.GetName(),
			"gq-nsstatus-a1",
			"gq-nsstatus-b1",
		)

		NamespaceCreationAdmin(nsA2, defaultTimeoutInterval)
		expectGlobalQuotaNamespaces(quota.GetName(),
			"gq-nsstatus-a1",
			"gq-nsstatus-a2",
			"gq-nsstatus-b1",
		)

		NamespaceDeletionAdmin(nsB1, defaultTimeoutInterval)
		expectGlobalQuotaNamespaces(quota.GetName(),
			"gq-nsstatus-a1",
			"gq-nsstatus-a2",
		)
	})

	It("posts an empty namespace status when namespaceSelectors match no namespaces and updates when matches appear or disappear", func() {
		quota := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-nsstatus-empty",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
				},
			},
			Spec: capsulev1beta2.GlobalCustomQuotaSpec{
				CustomQuotaSpec: capsulev1beta2.CustomQuotaSpec{
					Limit: resource.MustParse("10"),
					Sources: []capsulev1beta2.CustomQuotaSpecSource{
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpCount,
						},
					},
				},
				NamespaceSelectors: []selectors.NamespaceSelector{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"team": "does-not-exist",
							},
						},
					},
				},
			},
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, quota) }).Should(Succeed())
		awaitGlobalQuotaReady(quota.GetName())

		expectGlobalQuotaNamespaces(quota.GetName())

		ns := NewNamespace("gq-nsstatus-empty-match", map[string]string{"team": "does-not-exist"})
		NamespaceCreationAdmin(ns, defaultTimeoutInterval)

		expectGlobalQuotaNamespaces(quota.GetName(), "gq-nsstatus-empty-match")

		NamespaceDeletionAdmin(ns, defaultTimeoutInterval)
		expectGlobalQuotaNamespaces(quota.GetName())
	})

})
