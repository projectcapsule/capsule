package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

var _ = Describe("when CustomQuota uses ledger-backed reconciliation", Label("namespaced", "namespacedcustomquota", "customquota", "ledger"), Ordered, func() {
	const (
		testNamespace = "custom-quota-e2e-test"
		tenantLabel   = "capsule.clastix.io/tenant"
		tenantValue   = "custom-quota-e2e"
	)

	var (
		ctx context.Context
		ns  *corev1.Namespace
	)

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
		ForceDeleteNamespace(ctx, testNamespace)
		// delete all global quotas used by tests
		quotaList := &capsulev1beta2.CustomQuotaList{}
		if err := k8sClient.List(ctx, quotaList); err == nil {
			for i := range quotaList.Items {
				item := quotaList.Items[i]
				if item.Name == "" {
					continue
				}
				if item.Labels["e2e.capsule.dev/test-suite"] == "customquota-ledger" {
					EventuallyDeletion(&item)
				}
			}
		}

		// delete all global quotas used by tests
		gquotaList := &capsulev1beta2.GlobalCustomQuotaList{}
		if err := k8sClient.List(ctx, gquotaList); err == nil {
			for i := range gquotaList.Items {
				item := gquotaList.Items[i]
				if item.Name == "" {
					continue
				}
				if item.Labels["e2e.capsule.dev/test-suite"] == "customquota-ledger" {
					EventuallyDeletion(&item)
				}
			}
		}
	})

	It("remains consistent under concurrent pod creations for a CustomQuota", func() {
		q := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-concurrent-pod-count",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitCustomQuotaReady(ctx, testNamespace, q.GetName())

		const total = 100
		type result struct {
			name string
			err  error
		}

		results := make(chan result, total)

		for i := 0; i < total; i++ {
			i := i
			go func() {
				name := fmt.Sprintf("cq-concurrent-pod-%02d", i)
				pod := MakePod(
					testNamespace,
					name,
					map[string]string{"track": "yes"},
					nil,
					"nginx:1.27.0",
					"",
					"",
				)

				err := k8sClient.Create(ctx, pod)
				results <- result{name: name, err: err}
			}()
		}

		var succeeded, failed int
		for i := 0; i < total; i++ {
			res := <-results
			if res.err == nil {
				succeeded++
			} else {
				failed++
			}
		}

		Expect(succeeded).To(Equal(10))
		Expect(failed).To(Equal(90))

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "10", 10)
		expectLedgerSettled(ctx, testNamespace, q.GetName())
	})

	It("tracks different paths independently when global and namespaced quotas match the same pod gvk", func() {
		gq := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-mixed-path-cpu-from-cq-suite",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
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
								tenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		cq := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-mixed-path-emptydir-from-cq-suite",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, gq) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, cq) }).Should(Succeed())

		awaitGlobalQuotaReady(ctx, gq.GetName())
		awaitCustomQuotaReady(ctx, testNamespace, cq.GetName())

		p1 := MakePod(testNamespace, "mixed-path-cq-suite-1", nil, nil, "nginx:1.27.0", "100m", "1Gi")
		p2 := MakePod(testNamespace, "mixed-path-cq-suite-2", nil, nil, "nginx:1.27.0", "100m", "1Gi")

		EventuallyCreation(func() error { p1.ResourceVersion = ""; return k8sClient.Create(ctx, p1) }).Should(Succeed())
		EventuallyCreation(func() error { p2.ResourceVersion = ""; return k8sClient.Create(ctx, p2) }).Should(Succeed())

		expectGlobalQuotaUsedAndClaims(ctx, gq.GetName(), "200m", 2)
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, cq.GetName(), "2Gi", 2)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), gq.GetName())
		expectLedgerSettled(ctx, testNamespace, cq.GetName())

		Eventually(func() error {
			p3 := MakePod(testNamespace, "mixed-path-cq-suite-3", nil, nil, "nginx:1.27.0", "100m", "1Gi")
			return k8sClient.Create(ctx, p3)
		}, defaultTimeoutInterval, defaultPollInterval).Should(
			MatchError(ContainSubstring(`CustomQuota "cq-mixed-path-emptydir-from-cq-suite"`)),
		)
	})

	It("treats missing quantity paths as zero contribution", func() {
		q := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-wrong-path-zero",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
				Limit: resource.MustParse("10Gi"),
				Sources: []capsulev1beta2.CustomQuotaSpecSource{
					{
						GroupVersionKind: metav1.GroupVersionKind{
							Group:   "",
							Version: "v1",
							Kind:    "Pod",
						},
						Operation: quota.OpAdd,
						Path:      ".spec.volumes[*].doesNotExist.sizeLimit",
					},
					{
						GroupVersionKind: metav1.GroupVersionKind{
							Group:   "",
							Version: "v1",
							Kind:    "PersistentVolumeClaim",
						},
						Operation: quota.OpAdd,
						Path:      ".spec.resources.requests.thisDoesNotExist",
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitCustomQuotaReady(ctx, testNamespace, q.GetName())

		pod := MakePod(testNamespace, "wrong-path-pod", nil, nil, "nginx:1.27.0", "", "1Gi")
		pvc := MakePVC(testNamespace, "wrong-path-pvc", "2Gi")

		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			pvc.ResourceVersion = ""
			return k8sClient.Create(ctx, pvc)
		}).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "0", 2)
		expectLedgerSettled(ctx, testNamespace, q.GetName())
	})

	It("aggregates a custom pod quantity path and settles the corresponding ledger", func() {
		q := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-pod-cpu-requests",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitCustomQuotaReady(ctx, testNamespace, q.GetName())

		dep := MakeDeployment(testNamespace, "cpu-requests", 2, map[string]string{
			"track": "yes",
		}, "100m")
		EventuallyCreation(func() error {
			dep.ResourceVersion = ""
			return k8sClient.Create(ctx, dep)
		}).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "200m", 2)
		expectLedgerSettled(ctx, testNamespace, q.GetName())

		ScaleDeployment(ctx, testNamespace, "cpu-requests", 4)
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "400m", 4)
		expectLedgerSettled(ctx, testNamespace, q.GetName())

		ledger := getLedger(ctx, testNamespace, q.GetName())
		Expect(ledger.Spec.TargetRef.Kind).To(Equal("CustomQuota"))
		Expect(ledger.Spec.TargetRef.Name).To(Equal(q.GetName()))
		Expect(ledger.Spec.TargetRef.Namespace).To(Equal(testNamespace))
	})

	It("counts pods correctly while scaling a deployment", func() {
		q := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-pod-count",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitCustomQuotaReady(ctx, testNamespace, q.GetName())

		dep := MakeDeployment(testNamespace, "counted", 1, nil, "")
		EventuallyCreation(func() error {
			dep.ResourceVersion = ""
			return k8sClient.Create(ctx, dep)
		}).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "1", 1)

		ScaleDeployment(ctx, testNamespace, "counted", 3)
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "3", 3)

		ScaleDeployment(ctx, testNamespace, "counted", 2)
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "2", 2)

		expectLedgerSettled(ctx, testNamespace, q.GetName())
	})

	It("tracks count with a single MatchLabels selector and updates when the pod no longer matches", func() {
		q := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-count-single-matchlabel",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitCustomQuotaReady(ctx, testNamespace, q.GetName())

		pod := MakePod(testNamespace, "single-matchlabel", map[string]string{"track": "yes"}, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "1", 1)

		UpdatePodLabels(ctx, testNamespace, "single-matchlabel", map[string]string{"track": "no"})
		expectLedgerSettled(ctx, testNamespace, q.GetName())
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "0", 0)
	})

	It("tracks count with multiple MatchLabels and updates when the pod no longer matches", func() {
		q := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-count-multi-matchlabel",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitCustomQuotaReady(ctx, testNamespace, q.GetName())

		pod := MakePod(testNamespace, "multi-matchlabel", map[string]string{
			"track": "yes",
			"tier":  "frontend",
		}, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "1", 1)

		UpdatePodLabels(ctx, testNamespace, "multi-matchlabel", map[string]string{
			"track": "yes",
			"tier":  "backend",
		})
		expectLedgerSettled(ctx, testNamespace, q.GetName())
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "0", 0)
	})

	It("tracks count with a single field selector and updates when the pod no longer matches", func() {
		q := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-count-single-fieldselector",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitCustomQuotaReady(ctx, testNamespace, q.GetName())

		pod := MakePod(testNamespace, "single-fieldselector", nil, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "1", 1)

		UpdatePodImage(ctx, testNamespace, "single-fieldselector", "nginx:1.26.0")
		expectLedgerSettled(ctx, testNamespace, q.GetName())
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "0", 0)
	})

	It("tracks count with multiple field selectors and updates when the pod no longer matches", func() {
		q := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-count-multi-fieldselector",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitCustomQuotaReady(ctx, testNamespace, q.GetName())

		pod := MakePod(testNamespace, "multi-fieldselector", nil, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "1", 1)

		UpdatePodImage(ctx, testNamespace, "multi-fieldselector", "nginx:1.26.0")
		expectLedgerSettled(ctx, testNamespace, q.GetName())
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "0", 0)
	})

	It("aggregates multiple sources across pod emptyDir size and pvc storage size", func() {
		q := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-multi-source-storage",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
					{
						GroupVersionKind: metav1.GroupVersionKind{
							Group:   "",
							Version: "v1",
							Kind:    "PersistentVolumeClaim",
						},
						Operation: quota.OpAdd,
						Path:      ".spec.resources.requests.storage",
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitCustomQuotaReady(ctx, testNamespace, q.GetName())

		pod := MakePod(testNamespace, "multi-source-pod", nil, nil, "nginx:1.27.0", "", "1Gi")
		pvc := MakePVC(testNamespace, "multi-source-pvc", "2Gi")

		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			pvc.ResourceVersion = ""
			return k8sClient.Create(ctx, pvc)
		}).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "3Gi", 2)
		expectLedgerSettled(ctx, testNamespace, q.GetName())
	})

	It("aggregates multiple sources with selectors across pod emptyDir and pvc storage", func() {
		q := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-multi-source-selectors-storage",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitCustomQuotaReady(ctx, testNamespace, q.GetName())

		matchingPod := MakePod(testNamespace, "matching-emptydir", map[string]string{"track": "yes"}, nil, "nginx:1.27.0", "", "1Gi")
		nonMatchingPod := MakePod(testNamespace, "ignored-emptydir", map[string]string{"track": "no"}, nil, "nginx:1.27.0", "", "5Gi")

		matchingPVC := MakePVC(testNamespace, "matching-pvc", "2Gi")
		nonMatchingPVC := MakePVC(testNamespace, "ignored-pvc", "4Gi")
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

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "3Gi", 2)
		expectLedgerSettled(ctx, testNamespace, q.GetName())
	})

	It("clamps usage to zero for a pure subtraction source", func() {
		q := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-sub-only-clamps-zero",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
				Limit: resource.MustParse("10Gi"),
				Sources: []capsulev1beta2.CustomQuotaSpecSource{
					{
						GroupVersionKind: metav1.GroupVersionKind{
							Group:   "",
							Version: "v1",
							Kind:    "PersistentVolumeClaim",
						},
						Operation: quota.OpSub,
						Path:      ".spec.resources.requests.storage",
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitCustomQuotaReady(ctx, testNamespace, q.GetName())

		pvc := MakePVC(testNamespace, "sub-only-pvc", "2Gi")
		EventuallyCreation(func() error {
			pvc.ResourceVersion = ""
			return k8sClient.Create(ctx, pvc)
		}).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "0", 1)
		expectLedgerSettled(ctx, testNamespace, q.GetName())
	})

	It("subtracts matching pvc storage from added pod emptyDir storage", func() {
		q := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-add-sub-storage",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
				Limit: resource.MustParse("10Gi"),
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
					{
						GroupVersionKind: metav1.GroupVersionKind{
							Group:   "",
							Version: "v1",
							Kind:    "PersistentVolumeClaim",
						},
						Operation: quota.OpSub,
						Path:      ".spec.resources.requests.storage",
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitCustomQuotaReady(ctx, testNamespace, q.GetName())

		pod := MakePod(testNamespace, "add-sub-pod", nil, nil, "nginx:1.27.0", "", "3Gi")
		pvc := MakePVC(testNamespace, "add-sub-pvc", "1Gi")

		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			pvc.ResourceVersion = ""
			return k8sClient.Create(ctx, pvc)
		}).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "2Gi", 2)
		expectLedgerSettled(ctx, testNamespace, q.GetName())
	})

	It("clamps mixed add and subtraction result to zero when subtraction exceeds additions", func() {
		q := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-add-sub-clamp-zero",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
				Limit: resource.MustParse("10Gi"),
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
					{
						GroupVersionKind: metav1.GroupVersionKind{
							Group:   "",
							Version: "v1",
							Kind:    "PersistentVolumeClaim",
						},
						Operation: quota.OpSub,
						Path:      ".spec.resources.requests.storage",
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitCustomQuotaReady(ctx, testNamespace, q.GetName())

		pod := MakePod(testNamespace, "add-sub-clamp-pod", nil, nil, "nginx:1.27.0", "", "1Gi")
		pvc := MakePVC(testNamespace, "add-sub-clamp-pvc", "2Gi")

		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			pvc.ResourceVersion = ""
			return k8sClient.Create(ctx, pvc)
		}).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "0", 2)
		expectLedgerSettled(ctx, testNamespace, q.GetName())
	})

	It("supports subtraction with label selectors and removes the subtraction when the object no longer matches", func() {
		q := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-sub-label-selector",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
				Limit: resource.MustParse("10Gi"),
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
					{
						GroupVersionKind: metav1.GroupVersionKind{
							Group:   "",
							Version: "v1",
							Kind:    "PersistentVolumeClaim",
						},
						Operation: quota.OpSub,
						Path:      ".spec.resources.requests.storage",
						Selectors: []selectors.SelectorWithFields{
							{
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"discount": "yes",
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
		awaitCustomQuotaReady(ctx, testNamespace, q.GetName())

		pod := MakePod(testNamespace, "sub-label-pod", nil, nil, "nginx:1.27.0", "", "3Gi")
		pvc := MakePVC(testNamespace, "sub-label-pvc", "1Gi")
		pvc.Labels = map[string]string{"discount": "yes"}

		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			pvc.ResourceVersion = ""
			return k8sClient.Create(ctx, pvc)
		}).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "2Gi", 2)
		expectLedgerSettled(ctx, testNamespace, q.GetName())

		Eventually(func() error {
			obj := &corev1.PersistentVolumeClaim{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, obj); err != nil {
				return err
			}
			obj.Labels = map[string]string{"discount": "no"}
			return k8sClient.Update(ctx, obj)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		expectLedgerSettled(ctx, testNamespace, q.GetName())
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "3Gi", 1)
	})

	It("reconciles subtraction correctly when the subtracting resource is deleted", func() {
		q := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-sub-delete-reconcile",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
				Limit: resource.MustParse("10Gi"),
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
					{
						GroupVersionKind: metav1.GroupVersionKind{
							Group:   "",
							Version: "v1",
							Kind:    "PersistentVolumeClaim",
						},
						Operation: quota.OpSub,
						Path:      ".spec.resources.requests.storage",
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitCustomQuotaReady(ctx, testNamespace, q.GetName())

		pod := MakePod(testNamespace, "sub-delete-pod", nil, nil, "nginx:1.27.0", "", "3Gi")
		pvc := MakePVC(testNamespace, "sub-delete-pvc", "1Gi")

		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			pvc.ResourceVersion = ""
			return k8sClient.Create(ctx, pvc)
		}).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "2Gi", 2)
		expectLedgerSettled(ctx, testNamespace, q.GetName())

		EventuallyDeletion(pvc)
		expectLedgerSettled(ctx, testNamespace, q.GetName())
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "3Gi", 1)
	})

	It("uses the smallest matching custom quota as authoritative while accounting successful pod count in both quotas", func() {
		small := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-pod-count-small",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		large := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-pod-count-large",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, small) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, large) }).Should(Succeed())

		awaitCustomQuotaReady(ctx, testNamespace, small.GetName())
		awaitCustomQuotaReady(ctx, testNamespace, large.GetName())

		pod1 := MakePod(testNamespace, "multi-cq-count-1", nil, nil, "nginx:1.27.0", "", "")
		pod2 := MakePod(testNamespace, "multi-cq-count-2", nil, nil, "nginx:1.27.0", "", "")

		EventuallyCreation(func() error {
			pod1.ResourceVersion = ""
			return k8sClient.Create(ctx, pod1)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			pod2.ResourceVersion = ""
			return k8sClient.Create(ctx, pod2)
		}).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, small.GetName(), "2", 2)
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, large.GetName(), "2", 2)
		expectLedgerSettled(ctx, testNamespace, small.GetName())
		expectLedgerSettled(ctx, testNamespace, large.GetName())

		Eventually(func() error {
			pod3 := MakePod(testNamespace, "multi-cq-count-3", nil, nil, "nginx:1.27.0", "", "")
			return k8sClient.Create(ctx, pod3)
		}, defaultTimeoutInterval, defaultPollInterval).Should(
			MatchError(ContainSubstring(`CustomQuota "cq-pod-count-small"`)),
		)
	})

	It("uses the smallest matching custom quota as authoritative while accounting successful cpu usage in both quotas", func() {
		small := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-pod-cpu-small",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		large := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-pod-cpu-large",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, small) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, large) }).Should(Succeed())

		awaitCustomQuotaReady(ctx, testNamespace, small.GetName())
		awaitCustomQuotaReady(ctx, testNamespace, large.GetName())

		pod1 := MakePod(testNamespace, "multi-cq-cpu-1", nil, nil, "nginx:1.27.0", "100m", "")
		pod2 := MakePod(testNamespace, "multi-cq-cpu-2", nil, nil, "nginx:1.27.0", "100m", "")

		EventuallyCreation(func() error {
			pod1.ResourceVersion = ""
			return k8sClient.Create(ctx, pod1)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			pod2.ResourceVersion = ""
			return k8sClient.Create(ctx, pod2)
		}).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, small.GetName(), "200m", 2)
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, large.GetName(), "200m", 2)
		expectLedgerSettled(ctx, testNamespace, small.GetName())
		expectLedgerSettled(ctx, testNamespace, large.GetName())

		Eventually(func() error {
			pod3 := MakePod(testNamespace, "multi-cq-cpu-3", nil, nil, "nginx:1.27.0", "100m", "")
			return k8sClient.Create(ctx, pod3)
		}, defaultTimeoutInterval, defaultPollInterval).Should(
			MatchError(ContainSubstring(`CustomQuota "cq-pod-cpu-small"`)),
		)
	})

	It("accounts only the matching subset for overlapping selectors on the same pod gvk", func() {
		broad := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-pod-track-broad",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		narrow := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-pod-track-frontend",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, broad) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, narrow) }).Should(Succeed())

		awaitCustomQuotaReady(ctx, testNamespace, broad.GetName())
		awaitCustomQuotaReady(ctx, testNamespace, narrow.GetName())

		p1 := MakePod(testNamespace, "cq-overlap-1", map[string]string{"track": "yes", "tier": "frontend"}, nil, "nginx:1.27.0", "", "")
		p2 := MakePod(testNamespace, "cq-overlap-2", map[string]string{"track": "yes", "tier": "frontend"}, nil, "nginx:1.27.0", "", "")
		p3 := MakePod(testNamespace, "cq-overlap-3", map[string]string{"track": "yes", "tier": "backend"}, nil, "nginx:1.27.0", "", "")
		p4 := MakePod(testNamespace, "cq-overlap-4", map[string]string{"track": "yes", "tier": "backend"}, nil, "nginx:1.27.0", "", "")

		EventuallyCreation(func() error { p1.ResourceVersion = ""; return k8sClient.Create(ctx, p1) }).Should(Succeed())
		EventuallyCreation(func() error { p2.ResourceVersion = ""; return k8sClient.Create(ctx, p2) }).Should(Succeed())
		EventuallyCreation(func() error { p3.ResourceVersion = ""; return k8sClient.Create(ctx, p3) }).Should(Succeed())
		EventuallyCreation(func() error { p4.ResourceVersion = ""; return k8sClient.Create(ctx, p4) }).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, broad.GetName(), "4", 4)
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, narrow.GetName(), "2", 2)
		expectLedgerSettled(ctx, testNamespace, broad.GetName())
		expectLedgerSettled(ctx, testNamespace, narrow.GetName())

		Eventually(func() error {
			p5 := MakePod(testNamespace, "cq-overlap-5", map[string]string{"track": "yes", "tier": "frontend"}, nil, "nginx:1.27.0", "", "")
			return k8sClient.Create(ctx, p5)
		}, defaultTimeoutInterval, defaultPollInterval).Should(
			MatchError(ContainSubstring(`CustomQuota "cq-pod-track-frontend"`)),
		)

		EventuallyCreation(func() error {
			p6 := MakePod(testNamespace, "cq-overlap-6", map[string]string{"track": "yes", "tier": "backend"}, nil, "nginx:1.27.0", "", "")
			return k8sClient.Create(ctx, p6)
		}).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, broad.GetName(), "5", 5)
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, narrow.GetName(), "2", 2)
	})

	It("tracks different paths independently when multiple custom quotas match the same pod gvk", func() {
		cpuQuota := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-pod-path-cpu",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		emptyDirQuota := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-pod-path-emptydir",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, cpuQuota) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, emptyDirQuota) }).Should(Succeed())

		awaitCustomQuotaReady(ctx, testNamespace, cpuQuota.GetName())
		awaitCustomQuotaReady(ctx, testNamespace, emptyDirQuota.GetName())

		p1 := MakePod(testNamespace, "cq-path-pod-1", nil, nil, "nginx:1.27.0", "100m", "1Gi")
		p2 := MakePod(testNamespace, "cq-path-pod-2", nil, nil, "nginx:1.27.0", "100m", "1Gi")

		EventuallyCreation(func() error { p1.ResourceVersion = ""; return k8sClient.Create(ctx, p1) }).Should(Succeed())
		EventuallyCreation(func() error { p2.ResourceVersion = ""; return k8sClient.Create(ctx, p2) }).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, cpuQuota.GetName(), "200m", 2)
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, emptyDirQuota.GetName(), "2Gi", 2)
		expectLedgerSettled(ctx, testNamespace, cpuQuota.GetName())
		expectLedgerSettled(ctx, testNamespace, emptyDirQuota.GetName())

		Eventually(func() error {
			p3 := MakePod(testNamespace, "cq-path-pod-3", nil, nil, "nginx:1.27.0", "100m", "1Gi")
			return k8sClient.Create(ctx, p3)
		}, defaultTimeoutInterval, defaultPollInterval).Should(
			MatchError(ContainSubstring(`CustomQuota "cq-pod-path-emptydir"`)),
		)
	})

	It("accounts deployment scaling in multiple custom quotas and denies when the smaller quota is exceeded", func() {
		small := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-scale-small",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		large := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-scale-large",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, small) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, large) }).Should(Succeed())

		awaitCustomQuotaReady(ctx, testNamespace, small.GetName())
		awaitCustomQuotaReady(ctx, testNamespace, large.GetName())

		dep := MakeDeployment(testNamespace, "cq-scale", 1, nil, "")
		EventuallyCreation(func() error {
			dep.ResourceVersion = ""
			return k8sClient.Create(ctx, dep)
		}).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, small.GetName(), "1", 1)
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, large.GetName(), "1", 1)

		ScaleDeployment(ctx, testNamespace, "cq-scale", 3)
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, small.GetName(), "3", 3)
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, large.GetName(), "3", 3)

		ScaleDeployment(ctx, testNamespace, "cq-scale", 4)

		Eventually(func(g Gomega) {
			obj := &capsulev1beta2.CustomQuota{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      small.GetName(),
				Namespace: testNamespace,
			}, obj)).To(Succeed())
			g.Expect(obj.Status.Usage.Used.Cmp(resource.MustParse("3"))).To(Equal(0))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, large.GetName(), "3", 3)
	})

	It("uses the smallest matching quota as authoritative while accounting successful pod count in both global and namespaced quotas", func() {
		gq := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-mixed-pod-count-from-cq-suite",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
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
								tenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		cq := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-mixed-pod-count-from-cq-suite",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, gq) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, cq) }).Should(Succeed())

		awaitGlobalQuotaReady(ctx, gq.GetName())
		awaitCustomQuotaReady(ctx, testNamespace, cq.GetName())

		p1 := MakePod(testNamespace, "mixed-cq-suite-1", nil, nil, "nginx:1.27.0", "", "")
		p2 := MakePod(testNamespace, "mixed-cq-suite-2", nil, nil, "nginx:1.27.0", "", "")

		EventuallyCreation(func() error { p1.ResourceVersion = ""; return k8sClient.Create(ctx, p1) }).Should(Succeed())
		EventuallyCreation(func() error { p2.ResourceVersion = ""; return k8sClient.Create(ctx, p2) }).Should(Succeed())

		expectGlobalQuotaUsedAndClaims(ctx, gq.GetName(), "2", 2)
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, cq.GetName(), "2", 2)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), gq.GetName())
		expectLedgerSettled(ctx, testNamespace, cq.GetName())

		Eventually(func() error {
			p3 := MakePod(testNamespace, "mixed-cq-suite-3", nil, nil, "nginx:1.27.0", "", "")
			return k8sClient.Create(ctx, p3)
		}, defaultTimeoutInterval, defaultPollInterval).Should(
			MatchError(ContainSubstring(`CustomQuota "cq-mixed-pod-count-from-cq-suite"`)),
		)
	})

	It("uses the smallest matching quota as authoritative while accounting successful cpu usage in both global and namespaced quotas", func() {
		gq := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-mixed-pod-cpu-from-cq-suite",
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
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
								tenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		cq := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-mixed-pod-cpu-from-cq-suite",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, gq) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, cq) }).Should(Succeed())

		awaitGlobalQuotaReady(ctx, gq.GetName())
		awaitCustomQuotaReady(ctx, testNamespace, cq.GetName())

		p1 := MakePod(testNamespace, "mixed-cpu-cq-suite-1", nil, nil, "nginx:1.27.0", "100m", "")
		p2 := MakePod(testNamespace, "mixed-cpu-cq-suite-2", nil, nil, "nginx:1.27.0", "100m", "")

		EventuallyCreation(func() error { p1.ResourceVersion = ""; return k8sClient.Create(ctx, p1) }).Should(Succeed())
		EventuallyCreation(func() error { p2.ResourceVersion = ""; return k8sClient.Create(ctx, p2) }).Should(Succeed())

		expectGlobalQuotaUsedAndClaims(ctx, gq.GetName(), "200m", 2)
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, cq.GetName(), "200m", 2)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), gq.GetName())
		expectLedgerSettled(ctx, testNamespace, cq.GetName())

		Eventually(func() error {
			p3 := MakePod(testNamespace, "mixed-cpu-cq-suite-3", nil, nil, "nginx:1.27.0", "100m", "")
			return k8sClient.Create(ctx, p3)
		}, defaultTimeoutInterval, defaultPollInterval).Should(
			MatchError(ContainSubstring(`CustomQuota "cq-mixed-pod-cpu-from-cq-suite"`)),
		)
	})

	It("reconciles multiple sources when objects are deleted", func() {
		q := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-multi-source-reconcile",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
				Limit: resource.MustParse("10Gi"),
				Sources: []capsulev1beta2.CustomQuotaSpecSource{
					{
						GroupVersionKind: metav1.GroupVersionKind{
							Group:   "",
							Version: "v1",
							Kind:    "Pod",
						},
						Operation: quota.OpAdd,
						Path:      ".spec.volumes[*].emptyDir.sizeLimit",
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
						Operation: quota.OpAdd,
						Path:      ".spec.resources.requests.storage",
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitCustomQuotaReady(ctx, testNamespace, q.GetName())

		pod := MakePod(testNamespace, "reconcile-emptydir", map[string]string{"track": "yes"}, nil, "nginx:1.27.0", "", "1Gi")
		pvc := MakePVC(testNamespace, "reconcile-pvc", "2Gi")

		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			pvc.ResourceVersion = ""
			return k8sClient.Create(ctx, pvc)
		}).Should(Succeed())

		expectLedgerSettled(ctx, testNamespace, q.GetName())
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "3Gi", 2)

		EventuallyDeletion(pod)
		expectLedgerSettled(ctx, testNamespace, q.GetName())
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "2Gi", 1)

		EventuallyDeletion(pvc)
		expectLedgerSettled(ctx, testNamespace, q.GetName())
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "0", 0)
	})

	It("does not produce negative usage when a matching pod is relabeled to no longer match", func() {
		q := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-no-negative-on-relabel",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitCustomQuotaReady(ctx, testNamespace, q.GetName())

		pod := MakePod(testNamespace, "no-negative-on-relabel", map[string]string{"track": "yes"}, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "1", 1)

		UpdatePodLabels(ctx, testNamespace, "no-negative-on-relabel", map[string]string{"track": "no"})
		expectLedgerSettled(ctx, testNamespace, q.GetName())
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "0", 0)
	})

	It("retracts emptyDir usage when a pod no longer matches source selectors", func() {
		q := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-emptydir-relabel",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
				Limit: resource.MustParse("10Gi"),
				Sources: []capsulev1beta2.CustomQuotaSpecSource{
					{
						GroupVersionKind: metav1.GroupVersionKind{
							Group:   "",
							Version: "v1",
							Kind:    "Pod",
						},
						Operation: quota.OpAdd,
						Path:      ".spec.volumes[*].emptyDir.sizeLimit",
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
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitCustomQuotaReady(ctx, testNamespace, q.GetName())

		pod := MakePod(testNamespace, "emptydir-relabel", map[string]string{"track": "yes"}, nil, "nginx:1.27.0", "", "1Gi")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "1Gi", 1)

		UpdatePodLabels(ctx, testNamespace, "emptydir-relabel", map[string]string{"track": "no"})
		expectLedgerSettled(ctx, testNamespace, q.GetName())
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, q.GetName(), "0", 0)
	})

	It("accounts only the quotas that actually match when multiple custom quotas share the same gvk", func() {
		labelQuota := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-track-only",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		fieldQuota := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-nginx-only",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, labelQuota) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, fieldQuota) }).Should(Succeed())

		awaitCustomQuotaReady(ctx, testNamespace, labelQuota.GetName())
		awaitCustomQuotaReady(ctx, testNamespace, fieldQuota.GetName())

		matchBoth := MakePod(testNamespace, "subset-both", map[string]string{"track": "yes"}, nil, "nginx:1.27.0", "", "")
		matchLabelOnly := MakePod(testNamespace, "subset-label-only", map[string]string{"track": "yes"}, nil, "busybox:1.36.1", "", "")
		matchFieldOnly := MakePod(testNamespace, "subset-field-only", nil, nil, "nginx:1.27.0", "", "")

		EventuallyCreation(func() error { matchBoth.ResourceVersion = ""; return k8sClient.Create(ctx, matchBoth) }).Should(Succeed())
		EventuallyCreation(func() error { matchLabelOnly.ResourceVersion = ""; return k8sClient.Create(ctx, matchLabelOnly) }).Should(Succeed())
		EventuallyCreation(func() error { matchFieldOnly.ResourceVersion = ""; return k8sClient.Create(ctx, matchFieldOnly) }).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, labelQuota.GetName(), "2", 2)
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, fieldQuota.GetName(), "2", 2)
		expectLedgerSettled(ctx, testNamespace, labelQuota.GetName())
		expectLedgerSettled(ctx, testNamespace, fieldQuota.GetName())
	})

	It("uses deterministic tie-breaking when multiple custom quotas have the same remaining availability", func() {
		quotaA := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-tie-a",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		quotaB := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-tie-b",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, quotaA) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, quotaB) }).Should(Succeed())

		awaitCustomQuotaReady(ctx, testNamespace, quotaA.GetName())
		awaitCustomQuotaReady(ctx, testNamespace, quotaB.GetName())

		p1 := MakePod(testNamespace, "tie-1", nil, nil, "nginx:1.27.0", "", "")
		p2 := MakePod(testNamespace, "tie-2", nil, nil, "nginx:1.27.0", "", "")
		p3 := MakePod(testNamespace, "tie-3", nil, nil, "nginx:1.27.0", "", "")

		EventuallyCreation(func() error { p1.ResourceVersion = ""; return k8sClient.Create(ctx, p1) }).Should(Succeed())
		EventuallyCreation(func() error { p2.ResourceVersion = ""; return k8sClient.Create(ctx, p2) }).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, quotaA.GetName(), "2", 2)
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, quotaB.GetName(), "2", 2)

		EventuallyCreation(func() error { p3.ResourceVersion = ""; return k8sClient.Create(ctx, p3) }).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, quotaA.GetName(), "3", 3)
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, quotaB.GetName(), "3", 3)

		Eventually(func() error {
			p4 := MakePod(testNamespace, "tie-4", nil, nil, "nginx:1.27.0", "", "")
			return k8sClient.Create(ctx, p4)
		}, defaultTimeoutInterval, defaultPollInterval).Should(
			MatchError(ContainSubstring(`CustomQuota "cq-tie-a"`)),
		)
	})

	It("aggregates the same successful pod into multiple custom quotas with different paths on the same gvk", func() {
		cpuQuota := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-multi-path-cpu",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		emptyDirQuota := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-multi-path-emptydir",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error { return k8sClient.Create(ctx, cpuQuota) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, emptyDirQuota) }).Should(Succeed())

		awaitCustomQuotaReady(ctx, testNamespace, cpuQuota.GetName())
		awaitCustomQuotaReady(ctx, testNamespace, emptyDirQuota.GetName())

		p1 := MakePod(testNamespace, "multi-path-shared-1", nil, nil, "nginx:1.27.0", "100m", "1Gi")
		p2 := MakePod(testNamespace, "multi-path-shared-2", nil, nil, "nginx:1.27.0", "100m", "1Gi")

		EventuallyCreation(func() error { p1.ResourceVersion = ""; return k8sClient.Create(ctx, p1) }).Should(Succeed())
		EventuallyCreation(func() error { p2.ResourceVersion = ""; return k8sClient.Create(ctx, p2) }).Should(Succeed())

		expectCustomQuotaUsedAndClaims(ctx, testNamespace, cpuQuota.GetName(), "200m", 2)
		expectCustomQuotaUsedAndClaims(ctx, testNamespace, emptyDirQuota.GetName(), "2Gi", 2)
		expectLedgerSettled(ctx, testNamespace, cpuQuota.GetName())
		expectLedgerSettled(ctx, testNamespace, emptyDirQuota.GetName())
	})

	It("rejects admission when a field selector uses an invalid jsonpath filter on a scalar", func() {
		q := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-invalid-fieldselector",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "customquota-ledger",
				},
			},
			Spec: capsulev1beta2.CustomQuotaSpec{
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
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitCustomQuotaReady(ctx, testNamespace, q.GetName())

		Eventually(func() error {
			pod := MakePod(testNamespace, "invalid-selector-pod", nil, nil, "nginx:1.27.0", "", "")
			return k8sClient.Create(ctx, pod)
		}, defaultTimeoutInterval, defaultPollInterval).Should(
			MatchError(ContainSubstring("is not array or slice and cannot be filtered")),
		)
	})

})
