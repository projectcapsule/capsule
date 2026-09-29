// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
	"k8s.io/utils/ptr"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func placementTerm(key string, values ...string) corev1.NodeSelectorTerm {
	return corev1.NodeSelectorTerm{MatchExpressions: []corev1.NodeSelectorRequirement{{Key: key, Operator: corev1.NodeSelectorOpIn, Values: values}}}
}

func placementBaseline() []*rules.NamespaceRuleBodyNamespace {
	return []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{{Workloads: rules.WorkloadMutation{
		NodeSelector:              map[string]string{"pool": "shared"},
		Tolerations:               []corev1.Toleration{{Key: "dedicated", Value: "shared", Effect: corev1.TaintEffectNoExecute, TolerationSeconds: ptr.To(int64(60))}},
		TopologySpreadConstraints: []corev1.TopologySpreadConstraint{{TopologyKey: "zone", WhenUnsatisfiable: corev1.DoNotSchedule, MaxSkew: 1, LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout"}}}},
		Affinity: &corev1.Affinity{
			NodeAffinity:    &corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{placementTerm("zone", "a", "b")}}},
			PodAffinity:     &corev1.PodAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{TopologyKey: "zone", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "cache"}}}}},
			PodAntiAffinity: &corev1.PodAntiAffinity{PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{Weight: 50, PodAffinityTerm: corev1.PodAffinityTerm{TopologyKey: "host", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout"}}}}}},
		},
	}}}}}
}

func TestMutatePlacementMergesAllPropertiesAndIsIdempotent(t *testing.T) {
	bodies := placementBaseline()
	rulesBefore := bodies[0].DeepCopy()
	pod := &corev1.Pod{Spec: corev1.PodSpec{
		NodeSelector:              map[string]string{"pool": "private", "arch": "arm64"},
		Tolerations:               []corev1.Toleration{{Key: "dedicated", Value: "shared", Effect: corev1.TaintEffectNoExecute}, {Key: "other", Operator: corev1.TolerationOpExists}},
		TopologySpreadConstraints: []corev1.TopologySpreadConstraint{{TopologyKey: "zone", WhenUnsatisfiable: corev1.DoNotSchedule, MaxSkew: 4}, {TopologyKey: "host", WhenUnsatisfiable: corev1.ScheduleAnyway, MaxSkew: 3}},
		Affinity:                  &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{placementTerm("disk", "ssd"), placementTerm("arch", "arm64")}}}},
	}}
	changed, err := MutatePodPlacement(context.Background(), pod, bodies, nil)
	if err != nil || !changed {
		t.Fatalf("mutation = %t, %v", changed, err)
	}
	if pod.Spec.NodeSelector["pool"] != "shared" || pod.Spec.NodeSelector["arch"] != "arm64" {
		t.Fatalf("selectors = %v", pod.Spec.NodeSelector)
	}
	if len(pod.Spec.Tolerations) != 2 || *pod.Spec.Tolerations[0].TolerationSeconds != 60 {
		t.Fatalf("tolerations = %v", pod.Spec.Tolerations)
	}
	if len(pod.Spec.TopologySpreadConstraints) != 2 || pod.Spec.TopologySpreadConstraints[0].MaxSkew != 1 {
		t.Fatalf("spread = %v", pod.Spec.TopologySpreadConstraints)
	}
	terms := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 2 || len(terms[0].MatchExpressions) != 2 || len(terms[1].MatchExpressions) != 2 {
		t.Fatalf("required restrictions did not reach every alternative: %v", terms)
	}
	if pod.Spec.Affinity.PodAffinity == nil || pod.Spec.Affinity.PodAntiAffinity == nil {
		t.Fatal("missing Pod affinity mutation")
	}
	if !equality.Semantic.DeepEqual(rulesBefore, bodies[0]) {
		t.Fatal("mutation modified cached rules")
	}
	first := pod.DeepCopy()
	changed, err = MutatePodPlacement(context.Background(), pod, bodies, nil)
	if err != nil || changed || !equality.Semantic.DeepEqual(first, pod) {
		t.Fatalf("reinvocation changed the Pod: %t, %v", changed, err)
	}
	// Neither the Pod nor its nested maps may alias the rule baseline.
	pod.Spec.TopologySpreadConstraints[0].LabelSelector.MatchLabels["app"] = "other"
	if !equality.Semantic.DeepEqual(rulesBefore, bodies[0]) {
		t.Fatal("injected selector aliases the rule")
	}
}

func TestMutatePlacementOrderedOverrides(t *testing.T) {
	bodies := placementBaseline()
	later := bodies[0].DeepCopy()
	later.Mutate[0].Workloads.NodeSelector["pool"] = "batch"
	later.Mutate[0].Workloads.Tolerations[0].TolerationSeconds = nil
	later.Mutate[0].Workloads.TopologySpreadConstraints[0].MaxSkew = 2
	later.Mutate[0].Workloads.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].Weight = 80
	bodies = append(bodies, later)
	pod := &corev1.Pod{}
	if _, err := MutatePodPlacement(context.Background(), pod, bodies, nil); err != nil {
		t.Fatal(err)
	}
	if pod.Spec.NodeSelector["pool"] != "batch" || pod.Spec.Tolerations[0].TolerationSeconds != nil || pod.Spec.TopologySpreadConstraints[0].MaxSkew != 2 || pod.Spec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].Weight != 80 {
		t.Fatalf("later rules did not override: %+v", pod.Spec)
	}
	if changed, err := MutatePodPlacement(context.Background(), pod, bodies, nil); err != nil || changed {
		t.Fatalf("ordered reinvocation = %t, %v", changed, err)
	}
}

func TestRequiredNodeAffinityConjunctionTruthTable(t *testing.T) {
	existing := []corev1.NodeSelectorTerm{placementTerm("disk", "ssd"), placementTerm("arch", "arm64")}
	baseline := []corev1.NodeSelectorTerm{placementTerm("zone", "a"), placementTerm("pool", "shared")}
	combined, err := conjoinNodeTerms(existing, baseline)
	if err != nil {
		t.Fatal(err)
	}
	compile := func(terms []corev1.NodeSelectorTerm) *nodeaffinity.NodeSelector {
		selector, err := nodeaffinity.NewNodeSelector(&corev1.NodeSelector{NodeSelectorTerms: terms})
		if err != nil {
			t.Fatal(err)
		}
		return selector
	}
	left, right, result := compile(existing), compile(baseline), compile(combined)
	for bits := 0; bits < 16; bits++ {
		labels := map[string]string{}
		for i, label := range [][2]string{{"disk", "ssd"}, {"arch", "arm64"}, {"zone", "a"}, {"pool", "shared"}} {
			if bits&(1<<i) != 0 {
				labels[label[0]] = label[1]
			}
		}
		node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Labels: labels}}
		if got, want := result.Match(node), left.Match(node) && right.Match(node); got != want {
			t.Fatalf("labels %v: matched=%t want %t", labels, got, want)
		}
	}
	second, err := conjoinNodeTerms(combined, baseline)
	if err != nil || !equality.Semantic.DeepEqual(combined, second) {
		t.Fatalf("conjunction not idempotent: %v", err)
	}
	for _, empty := range [][]corev1.NodeSelectorTerm{nil, {{}}} {
		result, err := conjoinNodeTerms(empty, baseline)
		if err != nil {
			t.Fatal(err)
		}
		if compile(result).Match(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"zone": "a"}}}) {
			t.Fatal("empty required affinity was widened")
		}
	}
}

func TestRequiredNodeAffinityBoundsExpansion(t *testing.T) {
	var left, right []corev1.NodeSelectorTerm
	for i := 0; i < 17; i++ {
		left = append(left, placementTerm("left", fmt.Sprint(i)))
		right = append(right, placementTerm("right", fmt.Sprint(i)))
	}
	if _, err := conjoinNodeTerms(left, right); err == nil {
		t.Fatal("unbounded node affinity expansion accepted")
	}
}

func BenchmarkMutatePodPlacement(b *testing.B) {
	for _, size := range []int{0, 1, 10} {
		b.Run(fmt.Sprintf("rules=%d", size), func(b *testing.B) {
			var bodies []*rules.NamespaceRuleBodyNamespace
			for i := 0; i < size; i++ {
				bodies = append(bodies, placementBaseline()...)
			}
			b.ReportAllocs()
			for b.Loop() {
				pod := &corev1.Pod{}
				if _, err := MutatePodPlacement(context.Background(), pod, bodies, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
