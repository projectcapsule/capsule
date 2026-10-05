// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"encoding/json"
	"fmt"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

func conditionalMetadataBody(expression string) *rules.NamespaceRuleBodyNamespace {
	return &rules.NamespaceRuleBodyNamespace{
		Mutate: []rules.NamespaceRuleMutation{{Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{NodeSelector: map[string]string{"independent": "yes"}}}}},
		Enforce: &rules.NamespaceRuleEnforceBody{
			Conditions: []rules.AdmissionCondition{{Expression: expression}},
			Metadata:   []rules.MetadataRule{{VersionKinds: apiruntime.VersionKinds{APIGroups: []string{"v1"}, Kinds: []string{"Pod"}}, Labels: map[string]rules.MetadataValueRule{"applied": {Default: ptr.To("yes")}}}},
			Workloads:  rules.NamespaceRuleEnforceWorkloadsBody{Targets: []rules.WorkloadValidationTarget{rules.ValidateContainers}, Resources: &rules.WorkloadResourceRules{Requests: map[corev1.ResourceName]rules.WorkloadResourceRequestPolicy{corev1.ResourceCPU: {Policy: rules.WorkloadResourceRequestPolicyDefault, Value: ptr.To(resource.MustParse("100m"))}}}},
		},
	}
}

func conditionPod() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{"name": "example"}, "spec": map[string]any{"containers": []any{map[string]any{"name": "main", "image": "example.com/main"}}}}}
}

func TestMetadataAndResourcesShareEnforcementGate(t *testing.T) {
	compiler, err := cache.NewCELCache()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, expression string
		mutated, failed  bool
	}{
		{"false preserves independent mutation", "false", false, false},
		{"true mutates metadata and resources", "true", true, false},
		{"same snapshot before metadata default", "!has(object.metadata.labels)", true, false},
		{"runtime error fails closed", "object.spec.missing == 'x'", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := conditionalMetadataBody(tc.expression)
			before := body.DeepCopy()
			obj := conditionPod()
			raw, err := json.Marshal(obj)
			if err != nil {
				t.Fatal(err)
			}
			req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: admissionv1.Create, Kind: metav1.GroupVersionKind{Version: "v1", Kind: "Pod"}, Object: runtime.RawExtension{Raw: raw}}}
			response := MetadataRules(compiler).OnCreate(nil, nil, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
			if response == nil || response.Allowed == tc.failed {
				t.Fatalf("response=%#v", response)
			}
			if !equality.Semantic.DeepEqual(before, body) {
				t.Fatal("mutated cached rule")
			}
			if tc.failed {
				return
			}
			pod := &corev1.Pod{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, pod); err != nil {
				t.Fatal(err)
			}
			if (pod.Labels["applied"] == "yes") != tc.mutated || (pod.Spec.Containers[0].Resources.Requests.Cpu().Sign() > 0) != tc.mutated {
				t.Fatalf("mismatched effects: %#v", pod)
			}
			if pod.Spec.NodeSelector["independent"] != "yes" {
				t.Fatal("enforcement gate skipped independent mutation")
			}
		})
	}
}

func BenchmarkConditionalMetadataMutation(b *testing.B) {
	compiler, err := cache.NewCELCache()
	if err != nil {
		b.Fatal(err)
	}
	for _, tenants := range []int{1, 8} {
		for _, count := range []int{1, 20} {
			for _, expression := range []string{"true", "false", "object.spec.missing == 'x'"} {
				b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s", tenants, count, expression), func(b *testing.B) {
					bodies := make([]*rules.NamespaceRuleBodyNamespace, count)
					for i := range bodies {
						bodies[i] = conditionalMetadataBody(expression)
					}
					handler := MetadataRules(compiler)
					req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: admissionv1.Create, Kind: metav1.GroupVersionKind{Version: "v1", Kind: "Pod"}, Object: runtime.RawExtension{Raw: []byte(`{}`)}}}
					template := conditionPod()
					requests := make([]admission.Request, tenants)
					for i := range requests {
						obj := template.DeepCopy()
						obj.SetNamespace(fmt.Sprintf("tenant-%d-profile", i))
						raw, err := json.Marshal(obj)
						if err != nil {
							b.Fatal(err)
						}
						requests[i] = req
						requests[i].Namespace = obj.GetNamespace()
						requests[i].Object.Raw = raw
					}
					handler.OnCreate(nil, nil, template.DeepCopy(), nil, nil, nil, bodies)(b.Context(), requests[0])
					b.ReportAllocs()
					b.ResetTimer()
					iteration := 0
					for b.Loop() {
						b.StopTimer()
						obj := template.DeepCopy()
						req = requests[iteration%tenants]
						obj.SetNamespace(req.Namespace)
						iteration++
						b.StartTimer()
						response := handler.OnCreate(nil, nil, obj, nil, nil, nil, bodies)(b.Context(), req)
						if response == nil || response.Allowed == (expression == "object.spec.missing == 'x'") {
							b.Fatal("unexpected decision")
						}
					}
				})
			}
		}
	}
}
