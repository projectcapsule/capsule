// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

func placementTenant(name string) *capsulev1beta2.Tenant {
	return &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: capsulev1beta2.TenantSpec{Rules: []*rules.NamespaceRuleBodyTenant{{
		NamespaceSelector:          &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "placement"}},
		NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Workloads: rules.WorkloadMutation{NodeSelector: map[string]string{"pool": "{{ .tenant.metadata.name }}"}}}}},
	}}}}
}

func TestMutationOnlyNamespaceProjectionAndTemplating(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tenant-a", "tenant-b"} {
		tnt := placementTenant(name)
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name + "-work", Labels: map[string]string{"profile": "placement"}}}
		result, err := tenant.BuildNamespaceRuleBodyStatus(scheme, ns, tnt)
		if err != nil || len(result) != 1 {
			t.Fatalf("projection=%v error=%v", result, err)
		}
		if result[0].Enforce != nil || result[0].Mutate[0].Workloads.NodeSelector["pool"] != name {
			t.Fatalf("wrong mutation-only projection: %+v", result[0])
		}
		result[0].Mutate[0].Workloads.NodeSelector["pool"] = "mutated"
		again, err := tenant.BuildNamespaceRuleBodyStatus(scheme, ns, tnt)
		if err != nil || again[0].Mutate[0].Workloads.NodeSelector["pool"] != name {
			t.Fatal("projection aliases cached/template values")
		}
		ns.Labels["profile"] = "other"
		result, err = tenant.BuildNamespaceRuleBodyStatus(scheme, ns, tnt)
		if err != nil || len(result) != 0 {
			t.Fatalf("non-selected namespace got rules: %v %v", result, err)
		}
	}
}

func BenchmarkNamespacePlacementProjection(b *testing.B) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		b.Fatal(err)
	}
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		b.Fatal(err)
	}
	for _, count := range []int{1, 20} {
		b.Run(fmt.Sprintf("tenants=%d", count), func(b *testing.B) {
			tenants := make([]*capsulev1beta2.Tenant, count)
			for i := range tenants {
				tenants[i] = placementTenant(fmt.Sprintf("tenant-%d", i))
			}
			namespaces := []*corev1.Namespace{{ObjectMeta: metav1.ObjectMeta{Name: "selected", Labels: map[string]string{"profile": "placement"}}}, {ObjectMeta: metav1.ObjectMeta{Name: "other"}}}
			b.ReportAllocs()
			for b.Loop() {
				for _, tnt := range tenants {
					for i, ns := range namespaces {
						bodies, err := tenant.BuildNamespaceRuleBodyStatus(scheme, ns, tnt)
						if err != nil || len(bodies) != 1-i {
							b.Fatalf("projection error: %v", err)
						}
					}
				}
			}
		})
	}
}

func TestMutationProjectionRetainsActionsConditionsAndEmptyProperties(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	tnt := placementTenant("tenant-a")
	tnt.Spec.Rules[0].Mutate = append(tnt.Spec.Rules[0].Mutate, rules.NamespaceRuleMutation{Conditions: []rules.AdmissionCondition{{Name: "tenant", Expression: `request.namespace == '{{ .namespace.metadata.name }}'`}}, Action: rules.MutationActionReplace, Workloads: rules.WorkloadMutation{HostUsers: ptr.To(false), NodeSelector: map[string]string{}, Tolerations: []corev1.Toleration{}, TopologySpreadConstraints: []corev1.TopologySpreadConstraint{}, Affinity: &corev1.Affinity{}}})
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "work-a", Labels: map[string]string{"profile": "placement"}}}
	result, err := tenant.BuildNamespaceRuleBodyStatus(scheme, ns, tnt)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || len(result[0].Mutate) != 2 {
		t.Fatalf("wrong ordered projection: %+v", result)
	}
	block := result[0].Mutate[1]
	if block.Workloads.HostUsers == nil || *block.Workloads.HostUsers || block.Action != rules.MutationActionReplace || block.Conditions[0].Expression != `request.namespace == 'work-a'` || block.Workloads.NodeSelector == nil || block.Workloads.Tolerations == nil || block.Workloads.TopologySpreadConstraints == nil || block.Workloads.Affinity == nil {
		t.Fatalf("lost fields: %+v", block)
	}
	*block.Workloads.HostUsers = true
	if *tnt.Spec.Rules[0].Mutate[1].Workloads.HostUsers {
		t.Fatal("hostUsers aliases Tenant")
	}
	block.Conditions[0].Expression = "false"
	if tnt.Spec.Rules[0].Mutate[1].Conditions[0].Expression == "false" {
		t.Fatal("conditions alias Tenant")
	}
}
