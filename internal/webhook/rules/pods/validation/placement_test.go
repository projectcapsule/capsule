// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
)

func placementExact(values ...string) *rules.PlacementExpressionMatch {
	return &rules.PlacementExpressionMatch{Exact: values}
}
func placementRegex(expression string) *rules.PlacementExpressionMatch {
	return &rules.PlacementExpressionMatch{ExpressionRegex: runtime.ExpressionRegex{Expression: expression}}
}
func placementRule(action rules.ActionType, workloads rules.NamespaceRuleEnforceWorkloadsBody) *rules.NamespaceRuleEnforceBody {
	return &rules.NamespaceRuleEnforceBody{Action: action, Workloads: workloads}
}

func TestPlacementEmptyMatchersAndOrdering(t *testing.T) {
	h := &podRules{regexCache: cache.NewRegexCache()}
	pod := &corev1.Pod{Spec: corev1.PodSpec{
		NodeSelector:              map[string]string{"empty": ""},
		Tolerations:               []corev1.Toleration{{Operator: corev1.TolerationOpExists}},
		TopologySpreadConstraints: []corev1.TopologySpreadConstraint{{TopologyKey: "zone", MaxSkew: 1, WhenUnsatisfiable: corev1.DoNotSchedule}},
		Affinity:                  &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{}}}}},
	}}
	policy := rules.NamespaceRuleEnforceWorkloadsBody{NodeSelector: []rules.WorkloadNodeSelectorMatch{{}}, Tolerations: []rules.WorkloadTolerationMatch{{}}, TopologySpreadConstraints: []rules.WorkloadTopologySpreadMatch{{}}, Affinity: []rules.WorkloadAffinityMatch{{}}}
	for name, evaluate := range map[string]func(*corev1.Pod, []*rules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error){
		"nodeSelector": h.validateNodeSelectors, "tolerations": h.validateTolerations, "topologySpreadConstraints": h.validateTopologySpread, "affinity": h.validateAffinity,
	} {
		t.Run(name, func(t *testing.T) {
			deny := placementRule(rules.ActionTypeDeny, policy)
			allow := placementRule(rules.ActionTypeAllow, policy)
			audit := placementRule(rules.ActionTypeAudit, policy)
			for _, tc := range []struct {
				name   string
				bodies []*rules.NamespaceRuleEnforceBody
				denied bool
				audits int
			}{
				{"deny", []*rules.NamespaceRuleEnforceBody{deny}, true, 0},
				{"allow", []*rules.NamespaceRuleEnforceBody{allow}, false, 0},
				{"later allow", []*rules.NamespaceRuleEnforceBody{deny, allow}, false, 0},
				{"audit never overrides", []*rules.NamespaceRuleEnforceBody{deny, audit}, true, 1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					result, err := evaluate(pod, tc.bodies)
					if err != nil || result == nil {
						t.Fatalf("evaluate = %v, %v", result, err)
					}
					if (result.BlockingError() != nil) != tc.denied || len(result.Audits) != tc.audits {
						t.Fatalf("decision = %+v", result)
					}
					if tc.denied && !strings.Contains(result.BlockingError().Error(), "spec."+name) {
						t.Fatalf("missing field path: %v", result.BlockingError())
					}
				})
			}
			result, err := evaluate(&corev1.Pod{}, []*rules.NamespaceRuleEnforceBody{deny})
			if err != nil || result.BlockingError() != nil {
				t.Fatalf("absent field rejected: %v, %v", result, err)
			}
			deny.Workloads.Targets = []rules.WorkloadValidationTarget{rules.ValidateContainers}
			if result, err := evaluate(pod, []*rules.NamespaceRuleEnforceBody{deny}); err != nil || result != nil {
				t.Fatalf("container-only rule applied: %v, %v", result, err)
			}
		})
	}
}

func TestNodeSelectorRegexAndEveryEntry(t *testing.T) {
	h := &podRules{regexCache: cache.NewRegexCache()}
	bodies := []*rules.NamespaceRuleEnforceBody{placementRule(rules.ActionTypeAllow, rules.NamespaceRuleEnforceWorkloadsBody{NodeSelector: []rules.WorkloadNodeSelectorMatch{{Key: placementRegex(`^placement\.example\.com/`), Values: placementExact("shared")}}})}
	for _, tc := range []struct {
		key, value string
		denied     bool
	}{{"placement.example.com/pool", "shared", false}, {"placement.example.com/pool", "private", true}, {"other", "shared", true}, {"placement.example.com/pool", "", true}} {
		pod := &corev1.Pod{Spec: corev1.PodSpec{NodeSelector: map[string]string{tc.key: tc.value}}}
		result, err := h.validateNodeSelectors(pod, bodies)
		if err != nil || (result.BlockingError() != nil) != tc.denied {
			t.Fatalf("%+v: result=%v err=%v", tc, result, err)
		}
	}
	if h.regexCache.Stats() != 1 {
		t.Fatalf("regexes compiled = %d", h.regexCache.Stats())
	}
}

func TestTolerationMatchingSemantics(t *testing.T) {
	h := &podRules{regexCache: cache.NewRegexCache()}
	policy := rules.WorkloadTolerationMatch{WorkloadNodeSelectorMatch: rules.WorkloadNodeSelectorMatch{Key: placementExact("dedicated"), Values: placementExact("shared")}, Operators: []corev1.TolerationOperator{corev1.TolerationOpEqual}, Effects: []corev1.TaintEffect{corev1.TaintEffectNoExecute}, TolerationSeconds: &rules.TolerationDurationMatch{PlacementRange: rules.PlacementRange{Max: ptr.To(int64(300))}, AllowUnlimited: ptr.To(false)}}
	bodies := []*rules.NamespaceRuleEnforceBody{placementRule(rules.ActionTypeAllow, rules.NamespaceRuleEnforceWorkloadsBody{Tolerations: []rules.WorkloadTolerationMatch{policy}})}
	base := corev1.Toleration{Key: "dedicated", Value: "shared", Effect: corev1.TaintEffectNoExecute, TolerationSeconds: ptr.To(int64(300))}
	for _, tc := range []struct {
		name   string
		mutate func(*corev1.Toleration)
		denied bool
	}{
		{"default Equal", func(*corev1.Toleration) {}, false},
		{"unlimited", func(v *corev1.Toleration) { v.TolerationSeconds = nil }, true},
		{"too long", func(v *corev1.Toleration) { v.TolerationSeconds = ptr.To(int64(301)) }, true},
		{"all effects", func(v *corev1.Toleration) { v.Effect = "" }, true},
		{"all values", func(v *corev1.Toleration) { v.Operator = corev1.TolerationOpExists; v.Value = "" }, true},
		{"all keys", func(v *corev1.Toleration) { v.Key = ""; v.Operator = corev1.TolerationOpExists; v.Value = "" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := *base.DeepCopy()
			tc.mutate(&value)
			result, err := h.validateTolerations(&corev1.Pod{Spec: corev1.PodSpec{Tolerations: []corev1.Toleration{value}}}, bodies)
			if err != nil || (result.BlockingError() != nil) != tc.denied {
				t.Fatalf("result=%v err=%v", result, err)
			}
		})
	}
}

func TestTopologySpreadChecksEffectiveSelectors(t *testing.T) {
	h := &podRules{regexCache: cache.NewRegexCache()}
	policy := rules.WorkloadTopologySpreadMatch{TopologyKey: placementExact("zone"), MaxSkew: &rules.PlacementRange{Max: ptr.To(int64(2))}, LabelSelector: &rules.PlacementLabelSelectorMatch{Required: true, Requirements: []rules.PlacementRequirementMatch{{WorkloadNodeSelectorMatch: rules.WorkloadNodeSelectorMatch{Key: placementExact("app"), Values: placementExact("checkout")}, Operators: []corev1.NodeSelectorOperator{corev1.NodeSelectorOpIn}}}}}
	bodies := []*rules.NamespaceRuleEnforceBody{placementRule(rules.ActionTypeAllow, rules.NamespaceRuleEnforceWorkloadsBody{TopologySpreadConstraints: []rules.WorkloadTopologySpreadMatch{policy}})}
	for _, tc := range []struct {
		name     string
		selector *metav1.LabelSelector
		keys     []string
		labels   map[string]string
		skew     int32
		denied   bool
	}{
		{"labels", &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout"}}, nil, nil, 1, false},
		{"expressions", &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "app", Operator: metav1.LabelSelectorOpIn, Values: []string{"checkout"}}}}, nil, nil, 2, false},
		{"empty", &metav1.LabelSelector{}, nil, nil, 1, true},
		{"dynamic", &metav1.LabelSelector{}, []string{"app"}, map[string]string{"app": "checkout"}, 1, false},
		{"dynamic forbidden value", &metav1.LabelSelector{}, []string{"app"}, map[string]string{"app": "other"}, 1, true},
		{"dynamic absent", &metav1.LabelSelector{}, []string{"app"}, nil, 1, true},
		{"excessive skew", &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout"}}, nil, nil, 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: tc.labels}, Spec: corev1.PodSpec{TopologySpreadConstraints: []corev1.TopologySpreadConstraint{{TopologyKey: "zone", MaxSkew: tc.skew, WhenUnsatisfiable: corev1.DoNotSchedule, LabelSelector: tc.selector, MatchLabelKeys: tc.keys}}}}
			result, err := h.validateTopologySpread(pod, bodies)
			if err != nil || (result.BlockingError() != nil) != tc.denied {
				t.Fatalf("result=%v err=%v", result, err)
			}
		})
	}
}

func TestAffinityFlatMatchersAndTermBoundaries(t *testing.T) {
	h := &podRules{regexCache: cache.NewRegexCache()}
	requirement := func(key string) rules.PlacementRequirementMatch {
		return rules.PlacementRequirementMatch{WorkloadNodeSelectorMatch: rules.WorkloadNodeSelectorMatch{Key: placementExact(key)}, Operators: []corev1.NodeSelectorOperator{corev1.NodeSelectorOpIn}}
	}
	nodeRule := rules.WorkloadAffinityMatch{Types: []rules.PlacementAffinityType{rules.PlacementNodeAffinity}, Modes: []rules.PlacementAffinityMode{rules.PlacementAffinityRequired}, Requirements: []rules.PlacementRequirementMatch{requirement("zone")}}
	podRule := rules.WorkloadAffinityMatch{Types: []rules.PlacementAffinityType{rules.PlacementPodAffinity, rules.PlacementPodAntiAffinity}, Modes: []rules.PlacementAffinityMode{rules.PlacementAffinityPreferred}, NamespaceScope: rules.PlacementSameNamespace, TopologyKey: placementExact("host"), LabelSelector: &rules.PlacementLabelSelectorMatch{Required: true, Requirements: []rules.PlacementRequirementMatch{requirement("app")}}}
	bodies := []*rules.NamespaceRuleEnforceBody{placementRule(rules.ActionTypeAllow, rules.NamespaceRuleEnforceWorkloadsBody{Affinity: []rules.WorkloadAffinityMatch{nodeRule, podRule}})}
	term := corev1.PodAffinityTerm{TopologyKey: "host", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout"}}}
	base := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "team"}, Spec: corev1.PodSpec{Affinity: &corev1.Affinity{PodAffinity: &corev1.PodAffinity{PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{Weight: 50, PodAffinityTerm: term}}}, PodAntiAffinity: &corev1.PodAntiAffinity{PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{Weight: 100, PodAffinityTerm: term}}}}}}
	for _, tc := range []struct {
		name   string
		mutate func(*corev1.Pod)
		denied bool
	}{
		{"combined types", func(*corev1.Pod) {}, false},
		{"cross namespace", func(p *corev1.Pod) {
			p.Spec.Affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].PodAffinityTerm.Namespaces = []string{"other"}
		}, true},
		{"all namespaces selector", func(p *corev1.Pod) {
			p.Spec.Affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].PodAffinityTerm.NamespaceSelector = &metav1.LabelSelector{}
		}, true},
		{"required is not preferred", func(p *corev1.Pod) {
			p.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution = []corev1.PodAffinityTerm{term}
		}, true},
		{"mismatch operator checked", func(p *corev1.Pod) {
			p.Labels = map[string]string{"app": "checkout"}
			v := &p.Spec.Affinity.PodAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].PodAffinityTerm
			v.LabelSelector = &metav1.LabelSelector{}
			v.MismatchLabelKeys = []string{"app"}
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := base.DeepCopy()
			tc.mutate(p)
			result, err := h.validateAffinity(p, bodies)
			if err != nil || (result.BlockingError() != nil) != tc.denied {
				t.Fatalf("result=%v err=%v", result, err)
			}
		})
	}
	// Two alternatives cannot each authorize half of the same node selector term.
	nodeOther := nodeRule
	nodeOther.Requirements = []rules.PlacementRequirementMatch{requirement("disk")}
	bodies[0].Workloads.Affinity = append(bodies[0].Workloads.Affinity, nodeOther)
	pod := &corev1.Pod{Spec: corev1.PodSpec{Affinity: &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"a"}}, {Key: "disk", Operator: corev1.NodeSelectorOpIn, Values: []string{"ssd"}}}}}}}}}}
	result, err := h.validateAffinity(pod, bodies)
	if err != nil || result.BlockingError() == nil {
		t.Fatalf("split term was authorized: %v, %v", result, err)
	}
	pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0] = corev1.NodeSelectorTerm{MatchFields: []corev1.NodeSelectorRequirement{{Key: "metadata.name", Operator: corev1.NodeSelectorOpIn, Values: []string{"node-a"}}}}
	result, err = h.validateAffinity(pod, bodies)
	if err != nil || result.BlockingError() == nil {
		t.Fatalf("unchecked matchFields authorized: %v, %v", result, err)
	}
}

func TestPlacementValidationSkipsUnchangedUpdatesAndSubresources(t *testing.T) {
	h := PodRules(nil, nil, nil).(*podRules)
	pod := &corev1.Pod{Spec: corev1.PodSpec{Tolerations: []corev1.Toleration{{Operator: corev1.TolerationOpExists}}}}
	bodies := []*rules.NamespaceRuleEnforceBody{placementRule(rules.ActionTypeDeny, rules.NamespaceRuleEnforceWorkloadsBody{Tolerations: []rules.WorkloadTolerationMatch{{}}})}
	if err := h.validatePodRules(context.Background(), admission.Request{}, pod, nil, nil, bodies, pod.DeepCopy()); err != nil {
		t.Fatalf("unrelated update rejected: %v", err)
	}
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{SubResource: "ephemeralcontainers"}}
	if err := h.validatePodRules(context.Background(), req, pod, nil, nil, bodies); err != nil {
		t.Fatalf("subresource rejected: %v", err)
	}
}

func BenchmarkPlacementValidation(b *testing.B) {
	for _, count := range []int{0, 1, 20} {
		for _, denied := range []bool{false, true} {
			b.Run(fmt.Sprintf("rules=%d/deny=%t", count, denied), func(b *testing.B) {
				h := &podRules{regexCache: cache.NewRegexCache()}
				var bodies []*rules.NamespaceRuleEnforceBody
				for i := 0; i < count; i++ {
					bodies = append(bodies, placementRule(rules.ActionTypeAllow, rules.NamespaceRuleEnforceWorkloadsBody{NodeSelector: []rules.WorkloadNodeSelectorMatch{{Key: placementRegex(`^placement\.example\.com/`), Values: placementExact("shared")}}}))
				}
				value := "shared"
				if denied {
					value = "private"
				}
				pod := &corev1.Pod{Spec: corev1.PodSpec{NodeSelector: map[string]string{"placement.example.com/pool": value}}}
				if _, err := h.validateNodeSelectors(pod, bodies); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				for b.Loop() {
					result, err := h.validateNodeSelectors(pod, bodies)
					if err != nil {
						b.Fatal(err)
					}
					if count > 0 && (result.BlockingError() != nil) != denied {
						b.Fatal("incorrect decision")
					}
				}
			})
		}
	}
}

func BenchmarkPlacementValidationRegexCache(b *testing.B) {
	for _, mode := range []string{"cold", "warm", "invalidated", "parallel"} {
		b.Run(mode, func(b *testing.B) {
			h := &podRules{regexCache: cache.NewRegexCache()}
			bodies := []*rules.NamespaceRuleEnforceBody{placementRule(rules.ActionTypeAllow, rules.NamespaceRuleEnforceWorkloadsBody{
				NodeSelector: []rules.WorkloadNodeSelectorMatch{{Key: placementRegex(`^placement\.example\.com/`), Values: placementExact("shared")}},
			})}
			pod := &corev1.Pod{Spec: corev1.PodSpec{NodeSelector: map[string]string{"placement.example.com/pool": "shared"}}}
			check := func() {
				result, err := h.validateNodeSelectors(pod, bodies)
				if err != nil || result.BlockingError() != nil {
					b.Fatalf("unexpected decision: %+v, %v", result, err)
				}
			}
			check()
			b.ReportAllocs()
			b.ResetTimer()
			if mode == "parallel" {
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						check()
					}
				})
				return
			}
			for b.Loop() {
				switch mode {
				case "cold":
					h.regexCache = cache.NewRegexCache()
				case "invalidated":
					h.regexCache.Reset()
				}
				check()
			}
		})
	}
}
