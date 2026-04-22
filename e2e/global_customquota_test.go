package e2e

import (
	"context"
	"fmt"
	"sort"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	capmeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

func expectLedgerSettled(ctx context.Context, namespace, name string) {
	Eventually(func(g Gomega) {
		obj := &capsulev1beta2.QuantityLedger{}
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		}, obj)).To(Succeed(),
			"failed to get QuantityLedger %s/%s",
			namespace,
			name,
		)

		g.Expect(obj.Status.Reserved.IsZero()).To(BeTrue(),
			"ledger %s/%s still has reserved=%q reservations=%+v pendingDeletes=%+v",
			namespace,
			name,
			obj.Status.Reserved.String(),
			obj.Status.Reservations,
			obj.Status.PendingDeletes,
		)

		g.Expect(obj.Status.PendingDeletes).To(BeEmpty(),
			"ledger %s/%s still has pendingDeletes=%+v reserved=%q reservations=%+v",
			namespace,
			name,
			obj.Status.PendingDeletes,
			obj.Status.Reserved.String(),
			obj.Status.Reservations,
		)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func expectGlobalQuotaUsedAndClaims(ctx context.Context, name string, used string, claims int) {
	expectedUsed := resource.MustParse(used)

	Eventually(func(g Gomega) {
		obj := &capsulev1beta2.GlobalCustomQuota{}
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, obj)).To(Succeed(),
			"failed to get GlobalCustomQuota %s", name)

		g.Expect(
			obj.Status.Usage.Used.Cmp(expectedUsed),
		).To(Equal(0),
			"unexpected used value for GlobalCustomQuota %s: used=%q expectedUsed=%q available=%q claims=%d expectedClaims=%d",
			name,
			obj.Status.Usage.Used.String(),
			used,
			obj.Status.Usage.Available.String(),
			len(obj.Status.Claims),
			claims,
		)

		g.Expect(obj.Status.Usage.Used.Sign()).To(BeNumerically(">=", 0),
			"usage went negative for GlobalCustomQuota %s: used=%q", name, obj.Status.Usage.Used.String())

		g.Expect(obj.Status.Usage.Available.Sign()).To(BeNumerically(">=", 0),
			"available went negative for GlobalCustomQuota %s: available=%q", name, obj.Status.Usage.Available.String())

		g.Expect(len(obj.Status.Claims)).To(Equal(claims),
			"unexpected claims for GlobalCustomQuota %s: used=%q expectedUsed=%q available=%q claims=%d expectedClaims=%d",
			name,
			obj.Status.Usage.Used.String(),
			used,
			obj.Status.Usage.Available.String(),
			len(obj.Status.Claims),
			claims,
		)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func expectCustomQuotaUsedAndClaims(ctx context.Context, namespace, name string, used string, claims int) {
	expectedUsed := resource.MustParse(used)

	Eventually(func(g Gomega) {
		obj := &capsulev1beta2.CustomQuota{}
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		}, obj)).To(Succeed(),
			"failed to get CustomQuota %s/%s", namespace, name)

		g.Expect(
			obj.Status.Usage.Used.Cmp(expectedUsed),
		).To(Equal(0),
			"unexpected used value for CustomQuota %s/%s: used=%q expectedUsed=%q available=%q claims=%d expectedClaims=%d",
			namespace,
			name,
			obj.Status.Usage.Used.String(),
			used,
			obj.Status.Usage.Available.String(),
			len(obj.Status.Claims),
			claims,
		)

		g.Expect(obj.Status.Usage.Used.Sign()).To(BeNumerically(">=", 0),
			"usage went negative for CustomQuota %s/%s: used=%q", namespace, name, obj.Status.Usage.Used.String())

		g.Expect(obj.Status.Usage.Available.Sign()).To(BeNumerically(">=", 0),
			"available went negative for CustomQuota %s/%s: available=%q", namespace, name, obj.Status.Usage.Available.String())

		g.Expect(len(obj.Status.Claims)).To(Equal(claims),
			"unexpected claims for CustomQuota %s/%s: used=%q expectedUsed=%q available=%q claims=%d expectedClaims=%d",
			namespace,
			name,
			obj.Status.Usage.Used.String(),
			used,
			obj.Status.Usage.Available.String(),
			len(obj.Status.Claims),
			claims,
		)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func awaitGlobalQuotaReady(ctx context.Context, name string) {
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

func awaitCustomQuotaReady(ctx context.Context, namespace, name string) {
	Eventually(func(g Gomega) {
		cq := &capsulev1beta2.CustomQuota{}
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		}, cq)).To(Succeed())

		g.Expect(cq.Status.Usage.Used.String()).NotTo(BeEmpty())
		g.Expect(cq.Status.Usage.Available.String()).NotTo(BeEmpty())

		ledger := &capsulev1beta2.QuantityLedger{}
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		}, ledger)).To(Succeed())

		g.Expect(ledger.Spec.TargetRef.Kind).To(Equal("CustomQuota"))
		g.Expect(ledger.Spec.TargetRef.Name).To(Equal(name))
		g.Expect(ledger.Spec.TargetRef.Namespace).To(Equal(namespace))

		g.Expect(ledger.Status.Reserved.IsZero()).To(BeTrue())
		g.Expect(ledger.Status.PendingDeletes).To(BeEmpty())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

func getGlobalQuota(ctx context.Context, name string) *capsulev1beta2.GlobalCustomQuota {
	obj := &capsulev1beta2.GlobalCustomQuota{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, obj)).To(Succeed())
	return obj
}

func getCustomQuota(ctx context.Context, namespace, name string) *capsulev1beta2.CustomQuota {
	obj := &capsulev1beta2.CustomQuota{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, obj)).To(Succeed())
	return obj
}

func getLedger(ctx context.Context, namespace, name string) *capsulev1beta2.QuantityLedger {
	obj := &capsulev1beta2.QuantityLedger{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, obj)).To(Succeed())
	return obj
}

var _ = Describe("when GlobalCustomQuota uses ledger-backed reconciliation", Label("global", "customquota", "ledger"), Ordered, func() {
	const (
		testNamespace = "global-custom-quota-e2e-test"
		tenantLabel   = "capsule.clastix.io/tenant"
		tenantValue   = "global-custom-quota-e2e"
	)

	var (
		ctx context.Context
		ns  *corev1.Namespace
	)

	awaitAllGlobalQuotasReady := func(names ...string) {
		for _, name := range names {
			awaitGlobalQuotaReady(ctx, name)
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
		ForceDeleteNamespace(ctx, testNamespace)

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
	})

	It("uses the smallest matching quota as authoritative while accounting successful pod count in both global and namespaced quotas", func() {
		gq := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-mixed-pod-count",
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
								tenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		cq := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-mixed-pod-count",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
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

		pod1 := MakePod(testNamespace, "mixed-count-1", nil, nil, "nginx:1.27.0", "", "")
		pod2 := MakePod(testNamespace, "mixed-count-2", nil, nil, "nginx:1.27.0", "", "")

		EventuallyCreation(func() error {
			pod1.ResourceVersion = ""
			return k8sClient.Create(ctx, pod1)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			pod2.ResourceVersion = ""
			return k8sClient.Create(ctx, pod2)
		}).Should(Succeed())

		expectGlobalQuotaUsedAndClaims(ctx, gq.GetName(), "2", 2)
		expectCustomQuotaUsedAndClaims(ctx, cq.GetNamespace(), cq.GetName(), "2", 2)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), gq.GetName())
		expectLedgerSettled(ctx, cq.GetNamespace(), cq.GetName())

		Eventually(func() error {
			pod3 := MakePod(testNamespace, "mixed-count-3", nil, nil, "nginx:1.27.0", "", "")
			return k8sClient.Create(ctx, pod3)
		}, defaultTimeoutInterval, defaultPollInterval).Should(
			MatchError(ContainSubstring(`CustomQuota "cq-mixed-pod-count"`)),
		)

		expectGlobalQuotaUsedAndClaims(ctx, gq.GetName(), "2", 2)
		expectCustomQuotaUsedAndClaims(ctx, cq.GetNamespace(), cq.GetName(), "2", 2)
	})

	It("uses the smallest matching quota as authoritative while accounting successful cpu usage in both global and namespaced quotas", func() {
		gq := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-mixed-pod-cpu",
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
								tenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		cq := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-mixed-pod-cpu",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
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

		pod1 := MakePod(testNamespace, "mixed-cpu-1", nil, nil, "nginx:1.27.0", "100m", "")
		pod2 := MakePod(testNamespace, "mixed-cpu-2", nil, nil, "nginx:1.27.0", "100m", "")

		EventuallyCreation(func() error {
			pod1.ResourceVersion = ""
			return k8sClient.Create(ctx, pod1)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			pod2.ResourceVersion = ""
			return k8sClient.Create(ctx, pod2)
		}).Should(Succeed())

		expectGlobalQuotaUsedAndClaims(ctx, gq.GetName(), "200m", 2)
		expectCustomQuotaUsedAndClaims(ctx, cq.GetNamespace(), cq.GetName(), "200m", 2)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), gq.GetName())
		expectLedgerSettled(ctx, cq.GetNamespace(), cq.GetName())

		Eventually(func() error {
			pod3 := MakePod(testNamespace, "mixed-cpu-3", nil, nil, "nginx:1.27.0", "100m", "")
			return k8sClient.Create(ctx, pod3)
		}, defaultTimeoutInterval, defaultPollInterval).Should(
			MatchError(ContainSubstring(`CustomQuota "cq-mixed-pod-cpu"`)),
		)
	})

	It("accounts only the matching subset for overlapping global and namespaced quota selectors", func() {
		gq := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-mixed-overlap",
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
								tenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		cq := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-mixed-overlap",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
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

		EventuallyCreation(func() error { return k8sClient.Create(ctx, gq) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, cq) }).Should(Succeed())

		awaitGlobalQuotaReady(ctx, gq.GetName())
		awaitCustomQuotaReady(ctx, testNamespace, cq.GetName())

		p1 := MakePod(testNamespace, "mixed-overlap-1", map[string]string{"track": "yes", "tier": "frontend"}, nil, "nginx:1.27.0", "", "")
		p2 := MakePod(testNamespace, "mixed-overlap-2", map[string]string{"track": "yes", "tier": "frontend"}, nil, "nginx:1.27.0", "", "")
		p3 := MakePod(testNamespace, "mixed-overlap-3", map[string]string{"track": "yes", "tier": "backend"}, nil, "nginx:1.27.0", "", "")

		EventuallyCreation(func() error { p1.ResourceVersion = ""; return k8sClient.Create(ctx, p1) }).Should(Succeed())
		EventuallyCreation(func() error { p2.ResourceVersion = ""; return k8sClient.Create(ctx, p2) }).Should(Succeed())
		EventuallyCreation(func() error { p3.ResourceVersion = ""; return k8sClient.Create(ctx, p3) }).Should(Succeed())

		expectGlobalQuotaUsedAndClaims(ctx, gq.GetName(), "3", 3)
		expectCustomQuotaUsedAndClaims(ctx, cq.GetNamespace(), cq.GetName(), "2", 2)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), gq.GetName())
		expectLedgerSettled(ctx, cq.GetNamespace(), cq.GetName())

		Eventually(func() error {
			p4 := MakePod(testNamespace, "mixed-overlap-4", map[string]string{"track": "yes", "tier": "frontend"}, nil, "nginx:1.27.0", "", "")
			return k8sClient.Create(ctx, p4)
		}, defaultTimeoutInterval, defaultPollInterval).Should(
			MatchError(ContainSubstring(`CustomQuota "cq-mixed-overlap"`)),
		)
	})

	It("tracks different paths independently when global and namespaced quotas match the same pod gvk", func() {
		gq := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-mixed-path-cpu",
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
								tenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		cq := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-mixed-path-emptydir",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
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

		p1 := MakePod(testNamespace, "mixed-path-1", nil, nil, "nginx:1.27.0", "100m", "1Gi")
		p2 := MakePod(testNamespace, "mixed-path-2", nil, nil, "nginx:1.27.0", "100m", "1Gi")

		EventuallyCreation(func() error { p1.ResourceVersion = ""; return k8sClient.Create(ctx, p1) }).Should(Succeed())
		EventuallyCreation(func() error { p2.ResourceVersion = ""; return k8sClient.Create(ctx, p2) }).Should(Succeed())

		expectGlobalQuotaUsedAndClaims(ctx, gq.GetName(), "200m", 2)
		expectCustomQuotaUsedAndClaims(ctx, cq.GetNamespace(), cq.GetName(), "2Gi", 2)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), gq.GetName())
		expectLedgerSettled(ctx, cq.GetNamespace(), cq.GetName())

		Eventually(func() error {
			p3 := MakePod(testNamespace, "mixed-path-3", nil, nil, "nginx:1.27.0", "100m", "1Gi")
			return k8sClient.Create(ctx, p3)
		}, defaultTimeoutInterval, defaultPollInterval).Should(
			MatchError(ContainSubstring(`CustomQuota "cq-mixed-path-emptydir"`)),
		)
	})

	It("accounts deployment scaling in both global and namespaced quotas and denies when the smaller namespaced quota is exceeded", func() {
		gq := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-mixed-scale",
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
								tenantLabel: tenantValue,
							},
						},
					},
				},
			},
		}

		cq := &capsulev1beta2.CustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cq-mixed-scale",
				Namespace: testNamespace,
				Labels: map[string]string{
					"e2e.capsule.dev/test-suite": "globalcustomquota-ledger",
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

		EventuallyCreation(func() error { return k8sClient.Create(ctx, gq) }).Should(Succeed())
		EventuallyCreation(func() error { return k8sClient.Create(ctx, cq) }).Should(Succeed())

		awaitGlobalQuotaReady(ctx, gq.GetName())
		awaitCustomQuotaReady(ctx, testNamespace, cq.GetName())

		dep := MakeDeployment(testNamespace, "mixed-scale", 1, nil, "")
		EventuallyCreation(func() error {
			dep.ResourceVersion = ""
			return k8sClient.Create(ctx, dep)
		}).Should(Succeed())

		expectGlobalQuotaUsedAndClaims(ctx, gq.GetName(), "1", 1)
		expectCustomQuotaUsedAndClaims(ctx, cq.GetNamespace(), cq.GetName(), "1", 1)

		ScaleDeployment(ctx, testNamespace, "mixed-scale", 3)
		expectGlobalQuotaUsedAndClaims(ctx, gq.GetName(), "3", 3)
		expectCustomQuotaUsedAndClaims(ctx, cq.GetNamespace(), cq.GetName(), "3", 3)

		ScaleDeployment(ctx, testNamespace, "mixed-scale", 4)

		Eventually(func(g Gomega) {
			obj := &capsulev1beta2.CustomQuota{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cq.GetName(), Namespace: testNamespace}, obj)).To(Succeed())
			g.Expect(obj.Status.Usage.Used.Cmp(resource.MustParse("3"))).To(Equal(0))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("clamps usage to zero for a pure subtraction source", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-sub-only-clamps-zero",
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
								Kind:    "PersistentVolumeClaim",
							},
							Operation: quota.OpSub,
							Path:      ".spec.resources.requests.storage",
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
		awaitGlobalQuotaReady(ctx, q.GetName())

		pvc := MakePVC(testNamespace, "sub-only-pvc", "2Gi")
		EventuallyCreation(func() error {
			pvc.ResourceVersion = ""
			return k8sClient.Create(ctx, pvc)
		}).Should(Succeed())

		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "0", 1)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())
	})

	It("subtracts matching pvc storage from added pod emptyDir storage", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-add-sub-storage",
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
		awaitGlobalQuotaReady(ctx, q.GetName())

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

		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "2Gi", 2)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())
	})

	It("clamps mixed add and subtraction result to zero when subtraction exceeds additions", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-add-sub-clamp-zero",
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
		awaitGlobalQuotaReady(ctx, q.GetName())

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

		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "0", 2)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())
	})

	It("supports subtraction with label selectors and removes the subtraction when the object no longer matches", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-sub-label-selector",
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
		awaitGlobalQuotaReady(ctx, q.GetName())

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

		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "2Gi", 2)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())

		Eventually(func() error {
			obj := &corev1.PersistentVolumeClaim{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, obj); err != nil {
				return err
			}
			obj.Labels = map[string]string{"discount": "no"}
			return k8sClient.Update(ctx, obj)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "3Gi", 1)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())
	})

	It("reconciles subtraction correctly when the subtracting resource is deleted", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-sub-delete-reconcile",
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
		awaitGlobalQuotaReady(ctx, q.GetName())

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

		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "2Gi", 2)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())

		EventuallyDeletion(pvc)
		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "3Gi", 1)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())
	})

	It("subtracts cpu requests from counted pod usage across multiple matching pods and clamps at zero", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-sub-cpu-clamp",
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
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpSub,
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

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitGlobalQuotaReady(ctx, q.GetName())

		pod1 := MakePod(testNamespace, "sub-cpu-pod-1", nil, nil, "nginx:1.27.0", "500m", "")
		pod2 := MakePod(testNamespace, "sub-cpu-pod-2", nil, nil, "nginx:1.27.0", "500m", "")

		EventuallyCreation(func() error {
			pod1.ResourceVersion = ""
			return k8sClient.Create(ctx, pod1)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			pod2.ResourceVersion = ""
			return k8sClient.Create(ctx, pod2)
		}).Should(Succeed())

		// 2 - 1.0 = 1
		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "1", 2)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())
	})

	It("applies subtraction while scaling a deployment", func() {
		q := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-sub-deployment-scale",
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
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpSub,
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

		EventuallyCreation(func() error {
			return k8sClient.Create(ctx, q)
		}).Should(Succeed())
		awaitGlobalQuotaReady(ctx, q.GetName())

		dep := MakeDeployment(testNamespace, "sub-scale", 2, nil, "250m")
		EventuallyCreation(func() error {
			dep.ResourceVersion = ""
			return k8sClient.Create(ctx, dep)
		}).Should(Succeed())

		// 2 - 0.5 = 1.5
		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "1500m", 2)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())

		ScaleDeployment(ctx, testNamespace, "sub-scale", 4)

		// 4 - 1.0 = 3
		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "3", 4)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())
	})

	It("uses the smallest matching quota as authoritative even when both quotas use subtraction", func() {
		small := &capsulev1beta2.GlobalCustomQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gq-sub-small",
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
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpSub,
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
				Name: "gq-sub-large",
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
						{
							GroupVersionKind: metav1.GroupVersionKind{
								Group:   "",
								Version: "v1",
								Kind:    "Pod",
							},
							Operation: quota.OpSub,
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
		awaitGlobalQuotaReady(ctx, small.GetName())
		awaitGlobalQuotaReady(ctx, large.GetName())

		pod1 := MakePod(testNamespace, "sub-auth-1", nil, nil, "nginx:1.27.0", "500m", "")
		pod2 := MakePod(testNamespace, "sub-auth-2", nil, nil, "nginx:1.27.0", "500m", "")
		pod3 := MakePod(testNamespace, "sub-auth-3", nil, nil, "nginx:1.27.0", "500m", "")

		EventuallyCreation(func() error {
			pod1.ResourceVersion = ""
			return k8sClient.Create(ctx, pod1)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			pod2.ResourceVersion = ""
			return k8sClient.Create(ctx, pod2)
		}).Should(Succeed())

		// 2 - 1.0 = 1
		expectGlobalQuotaUsedAndClaims(ctx, small.GetName(), "1", 2)
		expectGlobalQuotaUsedAndClaims(ctx, large.GetName(), "1", 2)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), small.GetName())
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), large.GetName())

		Eventually(func() error {
			pod3.ResourceVersion = ""
			return k8sClient.Create(ctx, pod3)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		// 3 - 1.5 = 1.5 still fits into both limits, so one more to push smaller first
		expectGlobalQuotaUsedAndClaims(ctx, small.GetName(), "1500m", 3)
		expectGlobalQuotaUsedAndClaims(ctx, large.GetName(), "1500m", 3)

		Eventually(func() error {
			p := MakePod(testNamespace, "sub-auth-4", nil, nil, "nginx:1.27.0", "500m", "")
			return k8sClient.Create(ctx, p)
		}, defaultTimeoutInterval, defaultPollInterval).Should(MatchError(ContainSubstring(`GlobalCustomQuota "gq-sub-small"`)))
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
			},
		}

		extraA := NewNamespace("gq-nsstatus-all-a", map[string]string{"purpose": "e2e"})
		extraB := NewNamespace("gq-nsstatus-all-b", map[string]string{"purpose": "e2e"})

		EventuallyCreation(func() error { return k8sClient.Create(ctx, quota) }).Should(Succeed())
		awaitGlobalQuotaReady(ctx, quota.GetName())

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

		NamespaceCreationAdmin(nsA1, defaultTimeoutInterval).Should(Succeed())
		NamespaceCreationAdmin(nsB1, defaultTimeoutInterval).Should(Succeed())
		NamespaceCreationAdmin(nsOther, defaultTimeoutInterval).Should(Succeed())

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
		awaitGlobalQuotaReady(ctx, quota.GetName())

		expectGlobalQuotaNamespaces(quota.GetName(),
			"gq-nsstatus-a1",
			"gq-nsstatus-b1",
		)

		NamespaceCreationAdmin(nsA2, defaultTimeoutInterval).Should(Succeed())
		expectGlobalQuotaNamespaces(quota.GetName(),
			"gq-nsstatus-a1",
			"gq-nsstatus-a2",
			"gq-nsstatus-b1",
		)

		NamespaceDeletionAdmin(nsB1, defaultTimeoutInterval).Should(Succeed())
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
		awaitGlobalQuotaReady(ctx, quota.GetName())

		expectGlobalQuotaNamespaces(quota.GetName())

		ns := NewNamespace("gq-nsstatus-empty-match", map[string]string{"team": "does-not-exist"})
		NamespaceCreationAdmin(ns, defaultTimeoutInterval).Should(Succeed())

		expectGlobalQuotaNamespaces(quota.GetName(), "gq-nsstatus-empty-match")

		NamespaceDeletionAdmin(ns, defaultTimeoutInterval).Should(Succeed())
		expectGlobalQuotaNamespaces(quota.GetName())
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
		awaitGlobalQuotaReady(ctx, q.GetName())

		dep := MakeDeployment(testNamespace, "cpu-requests", 2, map[string]string{
			"track": "yes",
		}, "100m")
		EventuallyCreation(func() error {
			dep.ResourceVersion = ""
			return k8sClient.Create(ctx, dep)
		}).Should(Succeed())

		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "200m", 2)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())

		ScaleDeployment(ctx, testNamespace, "cpu-requests", 4)
		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "400m", 4)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())

		ledger := getLedger(ctx, configuration.ControllerNamespace(), q.GetName())
		Expect(ledger.Spec.TargetRef.Kind).To(Equal("GlobalCustomQuota"))
		Expect(ledger.Spec.TargetRef.Name).To(Equal(q.GetName()))

		gq := getGlobalQuota(ctx, q.GetName())
		Expect(gq.Status.Usage.Used.Cmp(resource.MustParse("400m"))).To(Equal(0))
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
		awaitGlobalQuotaReady(ctx, q.GetName())

		pod := MakePod(testNamespace, "no-negative-on-relabel", map[string]string{"track": "yes"}, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "1", 1)

		UpdatePodLabels(ctx, testNamespace, "no-negative-on-relabel", map[string]string{"track": "no"})
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())

		Eventually(func(g Gomega) {
			obj := &capsulev1beta2.GlobalCustomQuota{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: q.GetName()}, obj)).To(Succeed())
			g.Expect(obj.Status.Usage.Used.Sign()).To(BeNumerically(">=", 0))
			g.Expect(obj.Status.Usage.Used.Cmp(resource.MustParse("0"))).To(Equal(0))
			g.Expect(len(obj.Status.Claims)).To(Equal(0))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
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
		awaitGlobalQuotaReady(ctx, q.GetName())

		dep := MakeDeployment(testNamespace, "counted", 1, nil, "")
		EventuallyCreation(func() error {
			dep.ResourceVersion = ""
			return k8sClient.Create(ctx, dep)
		}).Should(Succeed())

		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "1", 1)

		ScaleDeployment(ctx, testNamespace, "counted", 3)
		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "3", 3)

		ScaleDeployment(ctx, testNamespace, "counted", 2)
		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "2", 2)

		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())
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

		pod := MakePod(testNamespace, "single-matchlabel", map[string]string{"track": "yes"}, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "1", 1)

		UpdatePodLabels(ctx, testNamespace, "single-matchlabel", map[string]string{"track": "no"})
		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "0", 0)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())
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

		pod := MakePod(testNamespace, "multi-matchlabel", map[string]string{
			"track": "yes",
			"tier":  "frontend",
		}, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "1", 1)

		UpdatePodLabels(ctx, testNamespace, "multi-matchlabel", map[string]string{
			"track": "yes",
			"tier":  "backend",
		})
		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "0", 0)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())
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

		pod := MakePod(testNamespace, "single-fieldselector", nil, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "1", 1)

		UpdatePodImage(ctx, testNamespace, "single-fieldselector", "nginx:1.26.0")
		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "0", 0)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())
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

		pod := MakePod(testNamespace, "multi-fieldselector", nil, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "1", 1)

		UpdatePodImage(ctx, testNamespace, "multi-fieldselector", "nginx:1.26.0")
		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "0", 0)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())
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

		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "3Gi", 2)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())
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

		pod := MakePod(testNamespace, "multi-fieldselector", nil, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "1", 1)

		UpdatePodImage(ctx, testNamespace, "multi-fieldselector", "nginx:1.26.0")
		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "0", 0)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())
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

		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "3Gi", 2)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())
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

		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "3Gi", 2)

		UpdatePodLabels(ctx, testNamespace, "reconcile-emptydir", map[string]string{"track": "no"})
		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "2Gi", 1)

		EventuallyDeletion(pvc)
		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "0", 0)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())
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

		expectGlobalQuotaUsedAndClaims(ctx, q.GetName(), "0", 2)
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), q.GetName())
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

		pod := MakePod(testNamespace, "invalid-selector-pod", nil, nil, "nginx:1.27.0", "", "")

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

		pod1 := MakePod(testNamespace, "multi-gq-count-1", nil, nil, "nginx:1.27.0", "", "")
		pod2 := MakePod(testNamespace, "multi-gq-count-2", nil, nil, "nginx:1.27.0", "", "")

		EventuallyCreation(func() error {
			pod1.ResourceVersion = ""
			return k8sClient.Create(ctx, pod1)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			pod2.ResourceVersion = ""
			return k8sClient.Create(ctx, pod2)
		}).Should(Succeed())

		expectLedgerSettled(ctx, configuration.ControllerNamespace(), small.GetName())
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), large.GetName())
		expectGlobalQuotaUsedAndClaims(ctx, small.GetName(), "2", 2)
		expectGlobalQuotaUsedAndClaims(ctx, large.GetName(), "2", 2)

		expectPodCreationDeniedContaining(func(name string) *corev1.Pod {
			return MakePod(testNamespace, name, nil, nil, "nginx:1.27.0", "", "")
		}, `GlobalCustomQuota "gq-pod-count-small"`)

		expectGlobalQuotaUsedAndClaims(ctx, small.GetName(), "2", 2)
		expectGlobalQuotaUsedAndClaims(ctx, large.GetName(), "2", 2)
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

		pod1 := MakePod(testNamespace, "multi-gq-cpu-1", nil, nil, "nginx:1.27.0", "100m", "")
		pod2 := MakePod(testNamespace, "multi-gq-cpu-2", nil, nil, "nginx:1.27.0", "100m", "")

		EventuallyCreation(func() error {
			pod1.ResourceVersion = ""
			return k8sClient.Create(ctx, pod1)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			pod2.ResourceVersion = ""
			return k8sClient.Create(ctx, pod2)
		}).Should(Succeed())

		expectLedgerSettled(ctx, configuration.ControllerNamespace(), small.GetName())
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), large.GetName())
		expectGlobalQuotaUsedAndClaims(ctx, small.GetName(), "200m", 2)
		expectGlobalQuotaUsedAndClaims(ctx, large.GetName(), "200m", 2)

		expectPodCreationDeniedContaining(func(name string) *corev1.Pod {
			return MakePod(testNamespace, name, nil, nil, "nginx:1.27.0", "100m", "")
		}, `GlobalCustomQuota "gq-pod-cpu-small"`)

		expectGlobalQuotaUsedAndClaims(ctx, small.GetName(), "200m", 2)
		expectGlobalQuotaUsedAndClaims(ctx, large.GetName(), "200m", 2)
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

		pod1 := MakePod(testNamespace, "track-frontend-1", map[string]string{"track": "yes", "tier": "frontend"}, nil, "nginx:1.27.0", "", "")
		pod2 := MakePod(testNamespace, "track-frontend-2", map[string]string{"track": "yes", "tier": "frontend"}, nil, "nginx:1.27.0", "", "")
		pod3 := MakePod(testNamespace, "track-backend-1", map[string]string{"track": "yes", "tier": "backend"}, nil, "nginx:1.27.0", "", "")
		pod4 := MakePod(testNamespace, "track-backend-2", map[string]string{"track": "yes", "tier": "backend"}, nil, "nginx:1.27.0", "", "")

		EventuallyCreation(func() error { pod1.ResourceVersion = ""; return k8sClient.Create(ctx, pod1) }).Should(Succeed())
		EventuallyCreation(func() error { pod2.ResourceVersion = ""; return k8sClient.Create(ctx, pod2) }).Should(Succeed())
		EventuallyCreation(func() error { pod3.ResourceVersion = ""; return k8sClient.Create(ctx, pod3) }).Should(Succeed())
		EventuallyCreation(func() error { pod4.ResourceVersion = ""; return k8sClient.Create(ctx, pod4) }).Should(Succeed())

		expectLedgerSettled(ctx, configuration.ControllerNamespace(), broad.GetName())
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), narrow.GetName())
		expectGlobalQuotaUsedAndClaims(ctx, broad.GetName(), "4", 4)
		expectGlobalQuotaUsedAndClaims(ctx, narrow.GetName(), "2", 2)

		expectPodCreationDeniedContaining(func(name string) *corev1.Pod {
			return MakePod(testNamespace, name, map[string]string{"track": "yes", "tier": "frontend"}, nil, "nginx:1.27.0", "", "")
		}, `GlobalCustomQuota "gq-pod-track-frontend"`)

		expectGlobalQuotaUsedAndClaims(ctx, broad.GetName(), "4", 4)
		expectGlobalQuotaUsedAndClaims(ctx, narrow.GetName(), "2", 2)

		EventuallyCreation(func() error {
			pod := MakePod(testNamespace, "track-backend-3", map[string]string{"track": "yes", "tier": "backend"}, nil, "nginx:1.27.0", "", "")
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectLedgerSettled(ctx, configuration.ControllerNamespace(), broad.GetName())
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), narrow.GetName())
		expectGlobalQuotaUsedAndClaims(ctx, broad.GetName(), "5", 5)
		expectGlobalQuotaUsedAndClaims(ctx, narrow.GetName(), "2", 2)
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

		pod1 := MakePod(testNamespace, "path-pod-1", nil, nil, "nginx:1.27.0", "100m", "1Gi")
		pod2 := MakePod(testNamespace, "path-pod-2", nil, nil, "nginx:1.27.0", "100m", "1Gi")

		EventuallyCreation(func() error { pod1.ResourceVersion = ""; return k8sClient.Create(ctx, pod1) }).Should(Succeed())
		EventuallyCreation(func() error { pod2.ResourceVersion = ""; return k8sClient.Create(ctx, pod2) }).Should(Succeed())

		expectLedgerSettled(ctx, configuration.ControllerNamespace(), cpuQuota.GetName())
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), emptyDirQuota.GetName())
		expectGlobalQuotaUsedAndClaims(ctx, cpuQuota.GetName(), "200m", 2)
		expectGlobalQuotaUsedAndClaims(ctx, emptyDirQuota.GetName(), "2Gi", 2)

		expectPodCreationDeniedContaining(func(name string) *corev1.Pod {
			return MakePod(testNamespace, name, nil, nil, "nginx:1.27.0", "100m", "1Gi")
		}, `GlobalCustomQuota "gq-pod-path-emptydir"`)

		expectGlobalQuotaUsedAndClaims(ctx, cpuQuota.GetName(), "200m", 2)
		expectGlobalQuotaUsedAndClaims(ctx, emptyDirQuota.GetName(), "2Gi", 2)
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

		matchBoth := MakePod(testNamespace, "subset-both", map[string]string{"track": "yes"}, nil, "nginx:1.27.0", "", "")
		matchLabelOnly := MakePod(testNamespace, "subset-label-only", map[string]string{"track": "yes"}, nil, "busybox:1.36.1", "", "")
		matchFieldOnly := MakePod(testNamespace, "subset-field-only", nil, nil, "nginx:1.27.0", "", "")

		EventuallyCreation(func() error { matchBoth.ResourceVersion = ""; return k8sClient.Create(ctx, matchBoth) }).Should(Succeed())
		EventuallyCreation(func() error { matchLabelOnly.ResourceVersion = ""; return k8sClient.Create(ctx, matchLabelOnly) }).Should(Succeed())
		EventuallyCreation(func() error { matchFieldOnly.ResourceVersion = ""; return k8sClient.Create(ctx, matchFieldOnly) }).Should(Succeed())

		expectLedgerSettled(ctx, configuration.ControllerNamespace(), labelQuota.GetName())
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), fieldQuota.GetName())
		expectGlobalQuotaUsedAndClaims(ctx, labelQuota.GetName(), "2", 2)
		expectGlobalQuotaUsedAndClaims(ctx, fieldQuota.GetName(), "2", 2)
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
		pod1 := MakePod(testNamespace, "tie-1", nil, nil, "nginx:1.27.0", "", "")
		pod2 := MakePod(testNamespace, "tie-2", nil, nil, "nginx:1.27.0", "", "")

		EventuallyCreation(func() error { pod1.ResourceVersion = ""; return k8sClient.Create(ctx, pod1) }).Should(Succeed())
		EventuallyCreation(func() error { pod2.ResourceVersion = ""; return k8sClient.Create(ctx, pod2) }).Should(Succeed())

		expectLedgerSettled(ctx, configuration.ControllerNamespace(), quotaA.GetName())
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), quotaB.GetName())
		expectGlobalQuotaUsedAndClaims(ctx, quotaA.GetName(), "2", 2)
		expectGlobalQuotaUsedAndClaims(ctx, quotaB.GetName(), "2", 2)

		// Next pod is still allowed, because both have 1 and 2 available respectively after pod3.
		pod3 := MakePod(testNamespace, "tie-3", nil, nil, "nginx:1.27.0", "", "")
		EventuallyCreation(func() error { pod3.ResourceVersion = ""; return k8sClient.Create(ctx, pod3) }).Should(Succeed())

		expectLedgerSettled(ctx, configuration.ControllerNamespace(), quotaA.GetName())
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), quotaB.GetName())
		expectGlobalQuotaUsedAndClaims(ctx, quotaA.GetName(), "3", 3)
		expectGlobalQuotaUsedAndClaims(ctx, quotaB.GetName(), "3", 3)

		// Now gq-tie-a is exhausted first and should be authoritative.
		expectPodCreationDeniedContaining(func(name string) *corev1.Pod {
			return MakePod(testNamespace, name, nil, nil, "nginx:1.27.0", "", "")
		}, `GlobalCustomQuota "gq-tie-a"`)

		expectGlobalQuotaUsedAndClaims(ctx, quotaA.GetName(), "3", 3)
		expectGlobalQuotaUsedAndClaims(ctx, quotaB.GetName(), "3", 3)
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

		pod := MakePod(testNamespace, "multi-path-shared-1", nil, nil, "nginx:1.27.0", "100m", "1Gi")
		EventuallyCreation(func() error {
			pod.ResourceVersion = ""
			return k8sClient.Create(ctx, pod)
		}).Should(Succeed())

		expectLedgerSettled(ctx, configuration.ControllerNamespace(), cpuQuota.GetName())
		expectLedgerSettled(ctx, configuration.ControllerNamespace(), emptyDirQuota.GetName())
		expectGlobalQuotaUsedAndClaims(ctx, cpuQuota.GetName(), "100m", 1)
		expectGlobalQuotaUsedAndClaims(ctx, emptyDirQuota.GetName(), "1Gi", 1)

		pod2 := MakePod(testNamespace, "multi-path-shared-2", nil, nil, "nginx:1.27.0", "100m", "1Gi")
		EventuallyCreation(func() error {
			pod2.ResourceVersion = ""
			return k8sClient.Create(ctx, pod2)
		}).Should(Succeed())

		expectGlobalQuotaUsedAndClaims(ctx, cpuQuota.GetName(), "200m", 2)
		expectGlobalQuotaUsedAndClaims(ctx, emptyDirQuota.GetName(), "2Gi", 2)
	})
})
