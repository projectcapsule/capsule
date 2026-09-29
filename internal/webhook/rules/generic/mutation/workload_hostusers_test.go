// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestHostUsersMutationPresenceAndActions(t *testing.T) {
	values := map[string]*bool{"omitted": nil, "true": ptr.To(true), "false": ptr.To(false)}
	for _, action := range []rules.MutationAction{"", rules.MutationActionMerge, rules.MutationActionReplace} {
		for inputName, input := range values {
			for desiredName, desired := range values {
				t.Run(fmt.Sprintf("%s/%s-to-%s", action, inputName, desiredName), func(t *testing.T) {
					pod := &corev1.Pod{Spec: corev1.PodSpec{HostUsers: input, NodeSelector: map[string]string{"keep": "yes"}}}
					bodies := []*rules.NamespaceRuleBodyNamespace{{Mutate: []rules.NamespaceRuleMutation{{Action: action, Workloads: rules.WorkloadMutation{HostUsers: desired}}}}}
					before := bodies[0].DeepCopy()
					want := input
					if desired != nil {
						want = desired
					}
					for pass := 0; pass < 2; pass++ {
						changed, err := MutatePodPlacement(context.Background(), pod, bodies, nil)
						wantChanged := pass == 0 && !equality.Semantic.DeepEqual(input, want)
						if err != nil || changed != wantChanged || !equality.Semantic.DeepEqual(pod.Spec.HostUsers, want) {
							t.Fatalf("pass %d: changed=%v error=%v hostUsers=%v want=%v", pass, changed, err, pod.Spec.HostUsers, want)
						}
					}
					if pod.Spec.NodeSelector["keep"] != "yes" {
						t.Fatal("changed omitted property")
					}
					if desired != nil {
						*pod.Spec.HostUsers = !*pod.Spec.HostUsers
						if !equality.Semantic.DeepEqual(before, bodies[0]) {
							t.Fatal("Pod aliases mutation rule")
						}
					}
				})
			}
		}
	}
}

func TestHostUsersOrderedConditionsAndUnstructuredOutput(t *testing.T) {
	for _, action := range []rules.MutationAction{rules.MutationActionMerge, rules.MutationActionReplace} {
		for _, desired := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", action, desired), func(t *testing.T) {
				body := &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{
					{Action: action, Workloads: rules.WorkloadMutation{HostUsers: ptr.To(desired)}},
					{Conditions: []rules.AdmissionCondition{{Name: "after-host-users", Expression: fmt.Sprintf("object.spec.hostUsers == %v", desired)}}, Workloads: rules.WorkloadMutation{NodeSelector: map[string]string{"observed": "yes"}}},
					{Conditions: []rules.AdmissionCondition{{Expression: "false"}}, Workloads: rules.WorkloadMutation{HostUsers: ptr.To(!desired)}},
				}}
				obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Pod", "spec": map[string]any{"hostUsers": !desired}}}
				for pass := 0; pass < 2; pass++ {
					changed, err := MutateWorkloadResources(context.Background(), obj, corev1.SchemeGroupVersion.WithKind("Pod"), []*rules.NamespaceRuleBodyNamespace{body}, mutationConditions(t))
					if err != nil || changed != (pass == 0) {
						t.Fatalf("pass %d: changed=%v error=%v", pass, changed, err)
					}
				}
				got, found, err := unstructured.NestedBool(obj.Object, "spec", "hostUsers")
				if err != nil || !found || got != desired {
					t.Fatalf("hostUsers=%v found=%v error=%v", got, found, err)
				}
				observed, _, err := unstructured.NestedString(obj.Object, "spec", "nodeSelector", "observed")
				if err != nil || observed != "yes" {
					t.Fatalf("later condition did not see hostUsers: %s, %v", observed, err)
				}
			})
		}
	}
}

func TestMutationAffinityErrorLocation(t *testing.T) {
	var existing, configured []corev1.NodeSelectorTerm
	for i := 0; i < 17; i++ {
		existing = append(existing, placementTerm("existing", fmt.Sprint(i)))
		configured = append(configured, placementTerm("configured", fmt.Sprint(i)))
	}
	pod := &corev1.Pod{Spec: corev1.PodSpec{Affinity: &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: existing}}}}}
	bodies := []*rules.NamespaceRuleBodyNamespace{nil, {Mutate: []rules.NamespaceRuleMutation{
		{Workloads: rules.WorkloadMutation{HostUsers: ptr.To(false)}},
		{Workloads: rules.WorkloadMutation{Affinity: &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: configured}}}}},
	}}}
	_, err := MutatePodPlacement(context.Background(), pod, bodies, nil)
	if err == nil || !strings.Contains(err.Error(), "rules[1].mutate[1].workloads: affinity: nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution:") {
		t.Fatalf("missing mutation or property location: %v", err)
	}
}

func BenchmarkHostUsersMutation(b *testing.B) {
	for _, action := range []rules.MutationAction{rules.MutationActionMerge, rules.MutationActionReplace} {
		for _, count := range []int{1, 20} {
			b.Run(fmt.Sprintf("%s/rules=%d", action, count), func(b *testing.B) {
				bodies := make([]*rules.NamespaceRuleBodyNamespace, count)
				for i := range bodies {
					bodies[i] = &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Action: action, Workloads: rules.WorkloadMutation{HostUsers: ptr.To(i%2 == 1)}}}}
				}
				b.ReportAllocs()
				for b.Loop() {
					pod := &corev1.Pod{}
					changed, err := MutatePodPlacement(context.Background(), pod, bodies, nil)
					if err != nil || !changed || pod.Spec.HostUsers == nil || *pod.Spec.HostUsers != ((count-1)%2 == 1) {
						b.Fatalf("changed=%v error=%v hostUsers=%v", changed, err, pod.Spec.HostUsers)
					}
				}
			})
		}
	}
}
