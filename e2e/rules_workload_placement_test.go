// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

var _ = Describe("workload placement namespace profiles", Label("tenant", "rules", "workloads", "placement"), func() {
	exact := func(values ...string) *rules.PlacementExpressionMatch {
		return &rules.PlacementExpressionMatch{Exact: values}
	}
	profileSelector := func() *metav1.LabelSelector {
		return &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "placement-profile", Operator: metav1.LabelSelectorOpIn, Values: []string{"placement", "strict"}}}}
	}
	newTenant := func(suffix string) *capsulev1beta2.Tenant {
		suffix = fmt.Sprintf("%s-%d", suffix, time.Now().UnixNano())
		return &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "e2e-placement-" + suffix, Labels: map[string]string{"env": "e2e"}}, Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{{CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Name: "e2e-placement-owner-" + suffix, Kind: "User"}}}},
			Rules: []*rules.NamespaceRuleBodyTenant{
				{NamespaceSelector: profileSelector(), NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{NodeSelector: map[string]string{"placement.example.com/pool": "{{ .tenant.metadata.name }}"},
					Tolerations:               []corev1.Toleration{{Key: "placement.example.com/pool", Operator: corev1.TolerationOpEqual, Value: "{{ .tenant.metadata.name }}", Effect: corev1.TaintEffectNoSchedule}},
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{{TopologyKey: "topology.kubernetes.io/zone", MaxSkew: 1, WhenUnsatisfiable: corev1.DoNotSchedule, LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout"}}}},
					Affinity: &corev1.Affinity{
						NodeAffinity:    &corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "topology.kubernetes.io/zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"zone-a", "zone-b"}}}}}}},
						PodAffinity:     &corev1.PodAffinity{PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{Weight: 50, PodAffinityTerm: corev1.PodAffinityTerm{TopologyKey: "topology.kubernetes.io/zone", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "cache"}}}}}},
						PodAntiAffinity: &corev1.PodAntiAffinity{PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{Weight: 100, PodAffinityTerm: corev1.PodAffinityTerm{TopologyKey: "kubernetes.io/hostname", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout"}}}}}},
					}},
				}}}}},
				{NamespaceSelector: profileSelector(), NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Placement: rules.WorkloadPlacementEnforcement{NodeSelector: []rules.WorkloadNodeSelectorMatch{{Key: &rules.PlacementExpressionMatch{ExpressionRegex: apiruntime.ExpressionRegex{Expression: `^forbidden\.example\.com/`}}}},
					Tolerations:               []rules.WorkloadTolerationMatch{{WorkloadNodeSelectorMatch: rules.WorkloadNodeSelectorMatch{Key: exact("forbidden.example.com/pool")}}},
					TopologySpreadConstraints: []rules.WorkloadTopologySpreadMatch{{TopologyKey: exact("forbidden.example.com/rack")}},
					Affinity:                  []rules.WorkloadAffinityMatch{{Types: []rules.PlacementAffinityType{rules.PlacementPodAntiAffinity}, Modes: []rules.PlacementAffinityMode{rules.PlacementAffinityRequired}}}},
				}}}},
				{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"placement-profile": "strict"}}, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Placement: rules.WorkloadPlacementEnforcement{NodeSelector: []rules.WorkloadNodeSelectorMatch{{Key: exact("placement.example.com/pool"), Values: exact("{{ .tenant.metadata.name }}")}}}}}}},
				{NamespaceSelector: profileSelector(), NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Audience: []rules.Audience{{Kind: rules.AudienceKindUser, Name: "not-the-placement-owner"}}, Mutate: []rules.NamespaceRuleMutation{{Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{NodeSelector: map[string]string{"audience": "unmatched"}}}}}}},
			},
		}}
	}
	newPod := func(name string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"app": "checkout"}}, Spec: corev1.PodSpec{
			SchedulingGates: []corev1.PodSchedulingGate{{Name: "example.com/e2e-placement"}},
			SecurityContext: nobodyPodSecurityContext(), Containers: []corev1.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.9", SecurityContext: restrictedContainerSecurityContext()}},
		}}
	}
	createNamespace := func(tnt *capsulev1beta2.Tenant, profile string) *corev1.Namespace {
		ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name, "placement-profile": profile})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
		want := 0
		if profile == "placement" {
			want = 3
		}
		if profile == "strict" {
			want = 4
		}
		Eventually(func(g Gomega) {
			status := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
			g.Expect(status.Status.Rules).To(HaveLen(want))
			if want > 0 {
				g.Expect(status.Status.Rules[0].Mutate).NotTo(BeNil())
				g.Expect(status.Status.Rules[0].Mutate[0].Workloads.Placement.NodeSelector["placement.example.com/pool"]).To(Equal(tnt.Name))
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		return ns
	}
	createPod := func(cs kubernetes.Interface, ns string, pod *corev1.Pod) *corev1.Pod {
		var result *corev1.Pod
		EventuallyCreation(func() error {
			var err error
			result, err = cs.CoreV1().Pods(ns).Create(context.Background(), pod, metav1.CreateOptions{})
			return err
		}).Should(Succeed())
		return result
	}
	expectDenied := func(cs kubernetes.Interface, ns string, pod *corev1.Pod, path string) {
		_, err := cs.CoreV1().Pods(ns).Create(context.Background(), pod, metav1.CreateOptions{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(path))
		_, err = cs.CoreV1().Pods(ns).Get(context.Background(), pod.Name, metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	}
	updatePlacementTenant := func(tnt *capsulev1beta2.Tenant, change func(*capsulev1beta2.Tenant)) (*capsulev1beta2.Tenant, error) {
		var before *capsulev1beta2.Tenant
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			before = &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(tnt), before); err != nil {
				return err
			}
			updated := before.DeepCopy()
			change(updated)
			return k8sClient.Update(context.Background(), updated)
		})
		return before, err
	}
	var tenants []*capsulev1beta2.Tenant
	BeforeEach(func() {
		for _, suffix := range []string{"a", "b"} {
			tnt := newTenant(suffix)
			EventuallyCreation(func() error { return k8sClient.Create(context.Background(), tnt) }).Should(Succeed())
			tenants = append(tenants, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
	})
	AfterEach(func() {
		for _, tnt := range tenants {
			EventuallyDeletion(tnt)
		}
		tenants = nil
	})

	It("ensures all placement properties with independent namespace profiles and tenant isolation", func() {
		a, b := tenants[0], tenants[1]
		selected, strict, plain, other := createNamespace(a, "placement"), createNamespace(a, "strict"), createNamespace(a, "plain"), createNamespace(b, "placement")
		ownerA, ownerB := ownerClient(a.Spec.Owners[0].UserSpec), ownerClient(b.Spec.Owners[0].UserSpec)
		pod := newPod("all-properties")
		pod.Spec.NodeSelector = map[string]string{"placement.example.com/pool": "user-choice", "other": "kept"}
		created := createPod(ownerA, selected.Name, pod)
		Expect(created.Spec.NodeSelector).To(HaveKeyWithValue("placement.example.com/pool", a.Name))
		Expect(created.Spec.NodeSelector).To(HaveKeyWithValue("other", "kept"))
		Expect(created.Spec.NodeSelector).NotTo(HaveKey("audience"))
		Expect(created.Spec.Tolerations).To(ContainElement(corev1.Toleration{Key: "placement.example.com/pool", Operator: corev1.TolerationOpEqual, Value: a.Name, Effect: corev1.TaintEffectNoSchedule}))
		Expect(created.Spec.TopologySpreadConstraints).To(HaveLen(1))
		Expect(created.Spec.TopologySpreadConstraints[0].MaxSkew).To(Equal(int32(1)))
		Expect(created.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms).To(HaveLen(1))
		Expect(created.Spec.Affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution).To(HaveLen(1))
		Expect(created.Spec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution).To(HaveLen(1))
		unselected := createPod(ownerA, plain.Name, newPod("unselected"))
		Expect(unselected.Spec.NodeSelector).To(BeEmpty())
		Expect(unselected.Spec.Affinity).To(BeNil())
		Expect(unselected.Spec.TopologySpreadConstraints).To(BeEmpty())
		isolated := createPod(ownerB, other.Name, newPod("isolated"))
		Expect(isolated.Spec.NodeSelector["placement.example.com/pool"]).To(Equal(b.Name))
		createPod(ownerA, strict.Name, newPod("strict-allowed"))
		invalid := newPod("strict-denied")
		invalid.Spec.NodeSelector = map[string]string{"other": "disallowed"}
		expectDenied(ownerA, strict.Name, invalid, "spec.nodeSelector")
		_, err := ownerA.CoreV1().Pods(other.Name).Create(context.Background(), newPod("cross-tenant"), metav1.CreateOptions{})
		Expect(apierrors.IsForbidden(err)).To(BeTrue())
	})

	It("rejects each forbidden property after mutation and does not persist denied Pods", func() {
		tnt := tenants[0]
		ns := createNamespace(tnt, "placement")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		for _, tc := range []struct {
			name, path string
			mutate     func(*corev1.Pod)
		}{
			{"selector", "spec.nodeSelector", func(p *corev1.Pod) { p.Spec.NodeSelector = map[string]string{"forbidden.example.com/pool": "private"} }},
			{"toleration", "spec.tolerations", func(p *corev1.Pod) {
				p.Spec.Tolerations = []corev1.Toleration{{Key: "forbidden.example.com/pool", Operator: corev1.TolerationOpExists}}
			}},
			{"spread", "spec.topologySpreadConstraints", func(p *corev1.Pod) {
				p.Spec.TopologySpreadConstraints = []corev1.TopologySpreadConstraint{{TopologyKey: "forbidden.example.com/rack", MaxSkew: 1, WhenUnsatisfiable: corev1.ScheduleAnyway, LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout"}}}}
			}},
			{"affinity", "spec.affinity", func(p *corev1.Pod) {
				p.Spec.Affinity = &corev1.Affinity{PodAntiAffinity: &corev1.PodAntiAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{TopologyKey: "kubernetes.io/hostname", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout"}}}}}}
			}},
		} {
			p := newPod(tc.name)
			tc.mutate(p)
			expectDenied(cs, ns.Name, p, tc.path)
		}
		created := createPod(cs, ns.Name, newPod("update-tolerations"))
		created.Spec.Tolerations = append(created.Spec.Tolerations, corev1.Toleration{Key: "forbidden.example.com/pool", Operator: corev1.TolerationOpExists})
		_, err := cs.CoreV1().Pods(ns.Name).Update(context.Background(), created, metav1.UpdateOptions{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.tolerations"))
		stored, err := cs.CoreV1().Pods(ns.Name).Get(context.Background(), created.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		for _, tol := range stored.Spec.Tolerations {
			Expect(tol.Key).NotTo(Equal("forbidden.example.com/pool"))
		}
	})

	It("updates effective placement when rules and namespace labels change", func() {
		tnt := tenants[0]
		ns := createNamespace(tnt, "placement")
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		old := createPod(cs, ns.Name, newPod("before-policy-change"))
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(tnt), current); err != nil {
				return err
			}
			current.Spec.Rules[0].Mutate[0].Workloads.Placement.NodeSelector["placement.example.com/pool"] = "updated"
			return k8sClient.Update(context.Background(), current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			status := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
			g.Expect(status.Status.Rules[0].Mutate[0].Workloads.Placement.NodeSelector["placement.example.com/pool"]).To(Equal("updated"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		updated := createPod(cs, ns.Name, newPod("after-policy-change"))
		Expect(updated.Spec.NodeSelector["placement.example.com/pool"]).To(Equal("updated"))
		stored, err := cs.CoreV1().Pods(ns.Name).Get(context.Background(), old.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(stored.Spec.NodeSelector["placement.example.com/pool"]).To(Equal(tnt.Name))
		Eventually(func() error {
			current := &corev1.Namespace{}
			if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(ns), current); err != nil {
				return err
			}
			current.Labels["placement-profile"] = "plain"
			return k8sClient.Update(context.Background(), current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func() error {
			status := &capsulev1beta2.RuleStatus{}
			if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status); err != nil {
				return err
			}
			if len(status.Status.Rules) != 0 {
				return fmt.Errorf("still has %d rules", len(status.Status.Rules))
			}
			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		plain := createPod(cs, ns.Name, newPod("after-label-change"))
		Expect(plain.Spec.NodeSelector).To(BeEmpty())
	})

	It("rejects malformed placement rules on a real Tenant", func() {
		current := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
		for _, expression := range []*rules.PlacementExpressionMatch{
			{ExpressionRegex: apiruntime.ExpressionRegex{Expression: "["}},
			{},
		} {
			before, err := updatePlacementTenant(current, func(invalid *capsulev1beta2.Tenant) {
				invalid.Spec.Rules[1].Enforce.Workloads.Placement.NodeSelector[0].Key = expression
			})
			Expect(err).To(HaveOccurred())
			Expect(strings.Contains(err.Error(), "nodeSelector") && strings.Contains(err.Error(), "exp")).To(BeTrue(), err.Error())
			stored := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(current), stored)).To(Succeed())
			Expect(stored.Spec).To(Equal(before.Spec))
		}
	})
	It("applies ordered conditional merge and replace mutations without leaking tenant profiles", func() {
		a, b := tenants[0], tenants[1]
		ns, plain, other := createNamespace(a, "placement"), createNamespace(a, "plain"), createNamespace(b, "placement")
		ownerA, ownerB := ownerClient(a.Spec.Owners[0].UserSpec), ownerClient(b.Spec.Owners[0].UserSpec)
		mode := func(value string) []rules.AdmissionCondition {
			return []rules.AdmissionCondition{{Name: "mode", Expression: fmt.Sprintf("has(object.metadata.labels) && 'mode' in object.metadata.labels && object.metadata.labels['mode'] == '%s' && request.operation == 'CREATE'", value)}}
		}
		_, updateErr := updatePlacementTenant(a, func(current *capsulev1beta2.Tenant) {
			current.Spec.Rules[0].Mutate = append(current.Spec.Rules[0].Mutate,
				rules.NamespaceRuleMutation{
					Conditions: mode("replace"), Action: rules.MutationActionReplace, Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{NodeSelector: map[string]string{"replacement": "yes"},
						Tolerations:               []corev1.Toleration{{Key: "replacement", Operator: corev1.TolerationOpExists}},
						TopologySpreadConstraints: []corev1.TopologySpreadConstraint{{TopologyKey: "kubernetes.io/hostname", MaxSkew: 2, WhenUnsatisfiable: corev1.ScheduleAnyway, LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout"}}}},
						Affinity:                  &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "disk", Operator: corev1.NodeSelectorOpIn, Values: []string{"ssd"}}}}}}}}},
					}},
				rules.NamespaceRuleMutation{Conditions: []rules.AdmissionCondition{{Name: "after-replace", Expression: "has(object.spec.nodeSelector) && 'replacement' in object.spec.nodeSelector"}}, Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{NodeSelector: map[string]string{"ordered": "yes"}}}},
				rules.NamespaceRuleMutation{Conditions: mode("clear"), Action: rules.MutationActionReplace, Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{NodeSelector: map[string]string{}, Tolerations: []corev1.Toleration{}, TopologySpreadConstraints: []corev1.TopologySpreadConstraint{}, Affinity: &corev1.Affinity{}}}},
				rules.NamespaceRuleMutation{Conditions: append(mode("error"), rules.AdmissionCondition{Name: "runtime-error", Expression: "object.spec.missing == 'x'"}), Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{NodeSelector: map[string]string{"error": "never"}}}},
			)
		})
		Expect(updateErr).To(Succeed())
		Eventually(func(g Gomega) {
			status := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
			g.Expect(status.Status.Rules[0].Mutate).To(HaveLen(5))
			g.Expect(status.Status.Rules[0].Mutate[3].Workloads.Placement.Tolerations).NotTo(BeNil())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		podWithMode := func(value string) *corev1.Pod {
			pod := newPod("conditional-" + value)
			pod.Labels["mode"] = value
			return pod
		}
		replaced := createPod(ownerA, ns.Name, podWithMode("replace"))
		Expect(replaced.Spec.NodeSelector).To(Equal(map[string]string{"replacement": "yes", "ordered": "yes"}))
		Expect(replaced.Spec.Tolerations).To(ContainElement(corev1.Toleration{Key: "replacement", Operator: corev1.TolerationOpExists}))
		for _, tolerance := range replaced.Spec.Tolerations {
			Expect(tolerance.Key).NotTo(Equal("placement.example.com/pool"))
		}
		Expect(replaced.Spec.TopologySpreadConstraints).To(HaveLen(1))
		Expect(replaced.Spec.TopologySpreadConstraints[0].TopologyKey).To(Equal("kubernetes.io/hostname"))
		Expect(replaced.Spec.Affinity.PodAffinity).To(BeNil())
		Expect(replaced.Spec.Affinity.PodAntiAffinity).To(BeNil())
		Expect(replaced.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Key).To(Equal("disk"))
		cleared := createPod(ownerA, ns.Name, podWithMode("clear"))
		Expect(cleared.Spec.NodeSelector).To(BeEmpty())
		Expect(cleared.Spec.TopologySpreadConstraints).To(BeEmpty())
		Expect(cleared.Spec.Affinity == nil || *cleared.Spec.Affinity == (corev1.Affinity{})).To(BeTrue())
		for _, tolerance := range cleared.Spec.Tolerations {
			Expect(tolerance.Key).NotTo(Equal("placement.example.com/pool"))
		}
		skipped := createPod(ownerA, ns.Name, podWithMode("skip"))
		Expect(skipped.Spec.NodeSelector["placement.example.com/pool"]).To(Equal(a.Name))
		expectDenied(ownerA, ns.Name, podWithMode("error"), `rules[0].mutate[4]: conditions[1] ("runtime-error")`)
		unselected := createPod(ownerA, plain.Name, podWithMode("replace"))
		Expect(unselected.Spec.NodeSelector).To(BeEmpty())
		isolated := createPod(ownerB, other.Name, podWithMode("replace"))
		Expect(isolated.Spec.NodeSelector["placement.example.com/pool"]).To(Equal(b.Name))
		Expect(isolated.Spec.NodeSelector).NotTo(HaveKey("replacement"))
	})

	It("admits the playground placement examples and rejects an explicit forbidden selector", func() {
		a, b := tenants[0], tenants[1]
		ns, plain, other := createNamespace(a, "placement"), createNamespace(a, "plain"), createNamespace(b, "placement")
		ownerA, ownerB := ownerClient(a.Spec.Owners[0].UserSpec), ownerClient(b.Spec.Owners[0].UserSpec)
		data, err := os.ReadFile("../playground/platform/tenants/solar.yaml")
		Expect(err).NotTo(HaveOccurred())
		fixture := &capsulev1beta2.Tenant{}
		Expect(yaml.UnmarshalStrict(data, fixture)).To(Succeed())
		_, err = updatePlacementTenant(a, func(current *capsulev1beta2.Tenant) {
			current.Spec.Rules[0].NamespaceRuleBodyNamespace = fixture.Spec.Rules[0].NamespaceRuleBodyNamespace.DeepCopy()
		})
		Expect(err).NotTo(HaveOccurred())
		Eventually(func(g Gomega) {
			status := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
			g.Expect(status.Status.Rules[0].Mutate[0].Conditions).To(HaveLen(1))
			g.Expect(status.Status.Rules[0].Mutate[0].Conditions[0].Name).To(Equal("linux-default"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		for _, name := range []string{"default", "shared"} {
			data, err := os.ReadFile("../playground/user/solar/placement/" + name + ".yaml")
			Expect(err).NotTo(HaveOccurred())
			pod := &corev1.Pod{}
			Expect(yaml.UnmarshalStrict(data, pod)).To(Succeed())
			pod.Namespace = ""
			created := createPod(ownerA, ns.Name, pod)
			Eventually(func(g Gomega) {
				stored := &corev1.Pod{}
				g.Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(created), stored)).To(Succeed())
				wantScheduler := corev1.DefaultSchedulerName
				if name == "shared" {
					wantScheduler = "solar-shared-scheduler"
				}
				g.Expect(stored.Spec.SchedulerName).To(Equal(wantScheduler))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(created.Spec.NodeSelector).To(HaveKeyWithValue("kubernetes.io/os", "linux"))
			if name == "shared" {
				Expect(created.Spec.NodeSelector).To(HaveKeyWithValue("placement.example.com/pool", "shared"))
				Expect(created.Spec.Tolerations).To(ContainElement(corev1.Toleration{Key: "infrastructure.example.com/dedicated", Operator: corev1.TolerationOpEqual, Value: "shared", Effect: corev1.TaintEffectNoSchedule}))
			}
		}
		bad := newPod("playground-denied")
		bad.Spec.NodeSelector = map[string]string{"kubernetes.io/os": "windows"}
		expectDenied(ownerA, ns.Name, bad, "spec.nodeSelector")
		Expect(createPod(ownerA, plain.Name, bad.DeepCopy()).Spec.NodeSelector).To(HaveKeyWithValue("kubernetes.io/os", "windows"))
		Expect(createPod(ownerB, other.Name, bad.DeepCopy()).Spec.NodeSelector).To(HaveKeyWithValue("kubernetes.io/os", "windows"))
	})

	It("gates workload and service enforcement together and rechecks conditions on updates", func() {
		a := tenants[0]
		ns := createNamespace(a, "placement")
		owner := ownerClient(a.Spec.Owners[0].UserSpec)
		_, updateErr := updatePlacementTenant(a, func(current *capsulev1beta2.Tenant) {
			current.Spec.Rules[1].Enforce.Conditions = []rules.AdmissionCondition{{Name: "restricted", Expression: "has(object.metadata.labels) && 'restricted' in object.metadata.labels && object.metadata.labels['restricted'] == 'yes'"}}
			current.Spec.Rules[1].Enforce.Services = rules.NamespaceRuleEnforceServicesBody{Types: []rules.ServiceType{rules.ServiceTypeNodePort}}
		})
		Expect(updateErr).To(Succeed())
		Eventually(func(g Gomega) {
			status := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
			g.Expect(status.Status.Rules[1].Enforce.Conditions).To(HaveLen(1))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		pod := newPod("gated-enforcement")
		pod.Spec.NodeSelector = map[string]string{"forbidden.example.com/pool": "private"}
		created := createPod(owner, ns.Name, pod)
		Eventually(func(g Gomega) {
			fresh, err := owner.CoreV1().Pods(ns.Name).Get(context.Background(), created.Name, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			fresh.Labels["restricted"] = "yes"
			_, err = owner.CoreV1().Pods(ns.Name).Update(context.Background(), fresh, metav1.UpdateOptions{})
			if err == nil {
				Fail("restricted Pod update unexpectedly succeeded")
			}
			g.Expect(err).To(MatchError(ContainSubstring("spec.nodeSelector")))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		stored, err := owner.CoreV1().Pods(ns.Name).Get(context.Background(), created.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(stored.Labels).NotTo(HaveKey("restricted"))
		pod.Name = "gated-create-denied"
		pod.Labels["restricted"] = "yes"
		expectDenied(owner, ns.Name, pod, "spec.nodeSelector")
		service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "conditional-service"}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort, Ports: []corev1.ServicePort{{Port: 80}}}}
		allowed, err := owner.CoreV1().Services(ns.Name).Create(context.Background(), service, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		Eventually(func(g Gomega) {
			fresh, err := owner.CoreV1().Services(ns.Name).Get(context.Background(), allowed.Name, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			fresh.Labels = map[string]string{"restricted": "yes"}
			_, err = owner.CoreV1().Services(ns.Name).Update(context.Background(), fresh, metav1.UpdateOptions{})
			if err == nil {
				Fail("restricted Service update unexpectedly succeeded")
			}
			g.Expect(err).To(MatchError(ContainSubstring("spec.type")))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		storedService, err := owner.CoreV1().Services(ns.Name).Get(context.Background(), allowed.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(storedService.Spec.Type).To(Equal(corev1.ServiceTypeNodePort))
		Expect(storedService.Labels).NotTo(HaveKey("restricted"))
	})

	It("gates metadata mutation, metadata validation and ingress across namespace profiles", func() {
		a, b := tenants[0], tenants[1]
		ns, plain, other := createNamespace(a, "placement"), createNamespace(a, "plain"), createNamespace(b, "placement")
		ownerA, ownerB := ownerClient(a.Spec.Owners[0].UserSpec), ownerClient(b.Spec.Owners[0].UserSpec)
		newConfig := func(name string, restricted bool) *corev1.ConfigMap {
			labels := map[string]string{"env": "e2e", "blocked": "yes"}
			if restricted {
				labels["restricted"] = "yes"
			}
			return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
		}
		_, err := ownerA.CoreV1().ConfigMaps(ns.Name).Create(context.Background(), newConfig("before-policy", false), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, err = updatePlacementTenant(a, func(current *capsulev1beta2.Tenant) {
			conditions := []rules.AdmissionCondition{{Name: "restricted", Expression: "has(object.metadata.labels) && 'restricted' in object.metadata.labels && object.metadata.labels['restricted'] == 'yes'"}}
			current.Spec.Rules[0].Enforce = &rules.NamespaceRuleEnforceBody{Conditions: conditions, Action: rules.ActionTypeAllow, Metadata: []rules.MetadataRule{{VersionKinds: apiruntime.VersionKinds{APIGroups: []string{"v1"}, Kinds: []string{"ConfigMap"}}, Labels: map[string]rules.MetadataValueRule{"conditional": {Managed: ptr.To("yes")}}}}}
			current.Spec.Rules[1].Enforce.Conditions = conditions
			current.Spec.Rules[1].Enforce.Metadata = []rules.MetadataRule{{VersionKinds: apiruntime.VersionKinds{APIGroups: []string{"v1"}, Kinds: []string{"ConfigMap"}}, Labels: map[string]rules.MetadataValueRule{"blocked": {}}}}
			current.Spec.Rules[1].Enforce.Ingress = rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeIngress}, Hostnames: []apiruntime.ExpressionMatch{{Exact: []string{"blocked.example.com"}}}}
		})
		Expect(err).NotTo(HaveOccurred())
		Eventually(func(g Gomega) {
			status := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
			g.Expect(status.Status.Rules[0].Enforce).NotTo(BeNil())
			g.Expect(status.Status.Rules[1].Enforce.Ingress.Types).To(ContainElement(rules.IngressTypeIngress))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		before, err := ownerA.CoreV1().ConfigMaps(ns.Name).Get(context.Background(), "before-policy", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(before.Labels).NotTo(HaveKey("conditional"))
		good := newConfig("restricted-valid", true)
		delete(good.Labels, "blocked")
		created, err := ownerA.CoreV1().ConfigMaps(ns.Name).Create(context.Background(), good, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(created.Labels).To(HaveKeyWithValue("conditional", "yes"))
		_, err = ownerA.CoreV1().ConfigMaps(ns.Name).Create(context.Background(), newConfig("restricted-denied", true), metav1.CreateOptions{})
		Expect(err).To(MatchError(ContainSubstring("blocked")))
		_, err = ownerA.CoreV1().ConfigMaps(ns.Name).Get(context.Background(), "restricted-denied", metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
		for _, scope := range []struct {
			owner      kubernetes.Interface
			namespace  string
			restricted bool
		}{{ownerA, ns.Name, false}, {ownerA, plain.Name, true}, {ownerB, other.Name, true}} {
			created, err := scope.owner.CoreV1().ConfigMaps(scope.namespace).Create(context.Background(), newConfig("unaffected", scope.restricted), metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(created.Labels).NotTo(HaveKey("conditional"))
		}
		for _, restricted := range []bool{false, true} {
			labels := map[string]string{"env": "e2e"}
			if restricted {
				labels["restricted"] = "yes"
			}
			ingress := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("conditional-%t", restricted), Labels: labels}, Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{Host: "blocked.example.com", IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{Path: "/", PathType: ptr.To(networkingv1.PathTypePrefix), Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "backend", Port: networkingv1.ServiceBackendPort{Number: 80}}}}}}}}}}}
			_, err := ownerA.NetworkingV1().Ingresses(ns.Name).Create(context.Background(), ingress, metav1.CreateOptions{})
			if restricted {
				Expect(err).To(MatchError(ContainSubstring("blocked.example.com")))
				_, err = ownerA.NetworkingV1().Ingresses(ns.Name).Get(context.Background(), ingress.Name, metav1.GetOptions{})
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			} else {
				Expect(err).NotTo(HaveOccurred())
			}
			_, err = ownerB.NetworkingV1().Ingresses(other.Name).Create(context.Background(), ingress.DeepCopy(), metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
		}
	})

	It("mutates hostUsers with explicit Booleans and preserves namespace and tenant isolation", Label("hostusers"), func() {
		a, b := tenants[0], tenants[1]
		ns, plain, other := createNamespace(a, "placement"), createNamespace(a, "plain"), createNamespace(b, "placement")
		ownerA, ownerB := ownerClient(a.Spec.Owners[0].UserSpec), ownerClient(b.Spec.Owners[0].UserSpec)
		_, err := updatePlacementTenant(a, func(current *capsulev1beta2.Tenant) {
			current.Spec.Rules[0].Mutate = append(current.Spec.Rules[0].Mutate,
				rules.NamespaceRuleMutation{Workloads: rules.WorkloadMutation{Security: rules.WorkloadSecurityMutation{HostUsers: ptr.To(false)}}},
				rules.NamespaceRuleMutation{
					Conditions: []rules.AdmissionCondition{{Name: "host-users", Expression: "'host-users' in object.metadata.labels && object.metadata.labels['host-users'] == 'true'"}}, Action: rules.MutationActionReplace, Workloads: rules.WorkloadMutation{Security: rules.WorkloadSecurityMutation{HostUsers: ptr.To(true)}}},
				rules.NamespaceRuleMutation{
					Conditions: []rules.AdmissionCondition{{Name: "after-host-users", Expression: "object.spec.hostUsers == false"}}, Action: rules.MutationActionReplace, Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{NodeSelector: map[string]string{"user-namespace": "yes"}}}},
			)
		})
		Expect(err).NotTo(HaveOccurred())
		Eventually(func(g Gomega) {
			status := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
			g.Expect(status.Status.Rules[0].Mutate).To(HaveLen(4))
			g.Expect(status.Status.Rules[0].Mutate[1].Action).To(Equal(rules.MutationActionMerge))
			g.Expect(status.Status.Rules[0].Mutate[1].Workloads.Security.HostUsers).To(Equal(ptr.To(false)))
			g.Expect(status.Status.Rules[0].Mutate[2].Workloads.Security.HostUsers).To(Equal(ptr.To(true)))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		for i, initial := range []*bool{nil, ptr.To(true), ptr.To(false)} {
			pod := newPod(fmt.Sprintf("host-users-false-%d", i))
			pod.Spec.HostUsers = initial
			created := createPod(ownerA, ns.Name, pod)
			Expect(created.Spec.HostUsers).To(Equal(ptr.To(false)))
			Expect(created.Spec.NodeSelector).To(Equal(map[string]string{"user-namespace": "yes"}))
		}
		hostPod := newPod("host-users-true")
		hostPod.Labels["host-users"] = "true"
		hostPod.Spec.HostUsers = ptr.To(false)
		created := createPod(ownerA, ns.Name, hostPod)
		Expect(created.Spec.HostUsers).To(Equal(ptr.To(true)))
		Expect(created.Spec.NodeSelector).To(HaveKeyWithValue("placement.example.com/pool", a.Name))
		for _, scope := range []struct {
			owner kubernetes.Interface
			ns    string
		}{{ownerA, plain.Name}, {ownerB, other.Name}} {
			pod := newPod("host-users-isolation")
			pod.Spec.HostUsers = ptr.To(true)
			untouched := createPod(scope.owner, scope.ns, pod)
			Expect(untouched.Spec.HostUsers).To(Equal(ptr.To(true)))
			Expect(untouched.Spec.NodeSelector).NotTo(HaveKey("user-namespace"))
		}
		forbidden := newPod("host-users-forbidden")
		forbidden.Spec.Tolerations = []corev1.Toleration{{Key: "forbidden.example.com/pool", Operator: corev1.TolerationOpExists}}
		expectDenied(ownerA, ns.Name, forbidden, "spec.tolerations")
		_, err = ownerA.CoreV1().Pods(other.Name).Create(context.Background(), hostPod.DeepCopy(), metav1.CreateOptions{})
		Expect(apierrors.IsForbidden(err)).To(BeTrue())
		_, err = ownerB.CoreV1().Pods(other.Name).Get(context.Background(), hostPod.Name, metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())

		// Removing the setting affects new Pods, without reconciling existing ones.
		_, err = updatePlacementTenant(a, func(current *capsulev1beta2.Tenant) {
			current.Spec.Rules[0].Mutate = current.Spec.Rules[0].Mutate[:1]
		})
		Expect(err).NotTo(HaveOccurred())
		Eventually(func(g Gomega) {
			status := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
			g.Expect(status.Status.Rules[0].Mutate).To(HaveLen(1))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		pod := newPod("host-users-after-policy-removal")
		pod.Spec.HostUsers = ptr.To(true)
		Expect(createPod(ownerA, ns.Name, pod).Spec.HostUsers).To(Equal(ptr.To(true)))
		stored, err := ownerA.CoreV1().Pods(ns.Name).Get(context.Background(), "host-users-false-0", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(stored.Spec.HostUsers).To(Equal(ptr.To(false)))

		invalidTenant := newTenant("invalid-host-users")
		values, err := runtime.DefaultUnstructuredConverter.ToUnstructured(invalidTenant)
		Expect(err).NotTo(HaveOccurred())
		invalid := &unstructured.Unstructured{Object: values}
		invalid.SetAPIVersion(capsulev1beta2.GroupVersion.String())
		invalid.SetKind("Tenant")
		Expect(unstructured.SetNestedSlice(invalid.Object, []any{map[string]any{"mutate": []any{map[string]any{"workloads": map[string]any{"security": map[string]any{"hostUsers": "false"}}}}}}, "spec", "rules")).To(Succeed())
		err = k8sClient.Create(context.Background(), invalid)
		// The typed mutation webhook decodes the request before CRD validation.
		Expect(err).To(MatchError(And(ContainSubstring("hostUsers"), ContainSubstring("cannot unmarshal string"), ContainSubstring("type bool"))))
		Expect(apierrors.IsNotFound(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(invalidTenant), &capsulev1beta2.Tenant{}))).To(BeTrue())
	})

	It("rejects invalid mutation actions and Boolean conditions without changing a Tenant", func() {
		current := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
		for _, test := range []struct {
			expression string
			action     rules.MutationAction
		}{
			{expression: "object.spec."},
			{expression: "'not-a-boolean'"},
			{expression: "now >= timestamp('2026-01-01T00:00:00Z')"},
			{expression: "requestor.name == 'alice'"},
			{expression: "true", action: "append"},
		} {
			before, err := updatePlacementTenant(current, func(invalid *capsulev1beta2.Tenant) {
				invalid.Spec.Rules[0].Mutate[0].Action = test.action
				invalid.Spec.Rules[0].Mutate[0].Conditions = []rules.AdmissionCondition{{Name: "invalid", Expression: test.expression}}
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("mutate"))
			stored := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(current), stored)).To(Succeed())
			Expect(stored.Spec).To(Equal(before.Spec))
		}
	})

})
