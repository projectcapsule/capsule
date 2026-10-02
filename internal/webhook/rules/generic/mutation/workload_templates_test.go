// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/workloads"
)

func resourceTemplateFixture(target rules.WorkloadValidationTarget) *unstructured.Unstructured {
	gk, _ := target.GroupKind()
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": gk.WithVersion("v1").GroupVersion().String(), "kind": gk.Kind, "metadata": map[string]any{"name": "app", "namespace": "tenant-a"}}}
	data := map[string]any{"spec": map[string]any{"unknownPodField": "preserved", "containers": []any{map[string]any{"name": "app", "image": "example.com/app:v1", "unknownContainerField": "preserved", "resources": map[string]any{"unknownResourceField": "preserved"}}}, "initContainers": []any{map[string]any{"name": "init", "image": "example.com/init:v1"}}}}
	_ = unstructured.SetNestedMap(obj.Object, data, workloads.PodTemplatePath(obj.GroupVersionKind())...)
	return obj
}

func TestTemplateResourceMutation(t *testing.T) {
	for _, target := range []rules.WorkloadValidationTarget{rules.ValidateDeployment, rules.ValidateStatefulSet, rules.ValidateDaemonSet, rules.ValidateReplicaSet, rules.ValidateReplicationController, rules.ValidateJob, rules.ValidateCronJob} {
		t.Run(string(target), func(t *testing.T) {
			obj := resourceTemplateFixture(target)
			body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: []rules.WorkloadValidationTarget{target}, Resources: &rules.WorkloadResourceRules{Requests: map[corev1.ResourceName]rules.WorkloadResourceRequestPolicy{corev1.ResourceMemory: {Policy: rules.WorkloadResourceRequestPolicyDefault, Value: new(resource.MustParse("1Gi"))}}, Limits: map[corev1.ResourceName]rules.WorkloadResourceLimitPolicy{corev1.ResourceMemory: {Policy: rules.WorkloadResourceLimitPolicyRatio, Value: new(resource.MustParse("2"))}}}}}}
			before := body.DeepCopy()
			changed, err := MutateWorkloadResources(t.Context(), obj, obj.GroupVersionKind(), []*rules.NamespaceRuleBodyNamespace{body}, nil)
			require.NoError(t, err)
			require.True(t, changed)
			require.Equal(t, before, body)
			pod, err := workloads.PodFromTemplate(obj)
			require.NoError(t, err)
			for _, resources := range []corev1.ResourceRequirements{*pod.Spec.Resources, pod.Spec.Containers[0].Resources, pod.Spec.InitContainers[0].Resources} {
				require.Equal(t, "1Gi", resources.Requests.Memory().String())
				require.Equal(t, "2Gi", resources.Limits.Memory().String())
			}
			value, _, err := unstructured.NestedFieldNoCopy(obj.Object, append(workloads.PodTemplatePath(obj.GroupVersionKind()), "spec")...)
			require.NoError(t, err)
			spec := value.(map[string]any)
			require.Equal(t, "preserved", spec["unknownPodField"])
			container := spec["containers"].([]any)[0].(map[string]any)
			require.Equal(t, "preserved", container["unknownContainerField"])
			require.Equal(t, "preserved", container["resources"].(map[string]any)["unknownResourceField"])
			changed, err = MutateWorkloadResources(t.Context(), obj, obj.GroupVersionKind(), []*rules.NamespaceRuleBodyNamespace{body}, nil)
			require.NoError(t, err)
			require.False(t, changed, "defaults must converge")
			body.Enforce.Workloads.Targets = []rules.WorkloadValidationTarget{rules.WorkloadValidationTarget(string(target) + "/containers")}
			obj = resourceTemplateFixture(target)
			changed, err = MutateWorkloadResources(t.Context(), obj, obj.GroupVersionKind(), []*rules.NamespaceRuleBodyNamespace{body}, nil)
			require.NoError(t, err)
			require.True(t, changed)
			pod, err = workloads.PodFromTemplate(obj)
			require.NoError(t, err)
			require.Nil(t, pod.Spec.Resources)
			require.Empty(t, pod.Spec.InitContainers[0].Resources.Requests)
			body.Enforce.Workloads.Targets = nil
			obj = resourceTemplateFixture(target)
			changed, err = MutateWorkloadResources(t.Context(), obj, obj.GroupVersionKind(), []*rules.NamespaceRuleBodyNamespace{body}, nil)
			require.NoError(t, err)
			require.False(t, changed, "omitted targets remain Pod-only")
		})
	}
}

func TestTemplateResourceAdmissionScopeAndConditions(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	h := MetadataRules(compiler)
	body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Conditions: []rules.AdmissionCondition{{Expression: "object.kind == 'Deployment'"}}, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: []rules.WorkloadValidationTarget{rules.ValidateDeployment}, Resources: &rules.WorkloadResourceRules{Requests: map[corev1.ResourceName]rules.WorkloadResourceRequestPolicy{corev1.ResourceCPU: {Policy: rules.WorkloadResourceRequestPolicyDefault, Value: new(resource.MustParse("1"))}}}}}}
	for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
		obj := resourceTemplateFixture(rules.ValidateDeployment)
		req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: operation, Kind: metav1.GroupVersionKind(obj.GroupVersionKind())}}
		req.Object.Raw, err = json.Marshal(obj)
		require.NoError(t, err)
		var response *admission.Response
		if operation == admissionv1.Create {
			response = h.OnCreate(nil, nil, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
		} else {
			response = h.OnUpdate(nil, nil, obj.DeepCopy(), obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
		}
		require.NotNil(t, response)
		require.True(t, response.Allowed)
		pod, err := workloads.PodFromTemplate(obj)
		require.NoError(t, err)
		require.Equal(t, "1", pod.Spec.Containers[0].Resources.Requests.Cpu().String())
		for _, subresource := range []string{"status", "scale"} {
			req.SubResource = subresource
			fresh := resourceTemplateFixture(rules.ValidateDeployment)
			require.Nil(t, h.OnUpdate(nil, nil, fresh.DeepCopy(), fresh, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req))
		}
	}
	for _, expression := range []string{"false", "object.spec.missing == 'x'"} {
		obj := resourceTemplateFixture(rules.ValidateDeployment)
		before := obj.DeepCopy()
		body.Enforce.Conditions[0].Expression = expression
		changed, err := MutateWorkloadResources(t.Context(), obj, obj.GroupVersionKind(), []*rules.NamespaceRuleBodyNamespace{body}, ruleengine.NewConditionEvaluator(compiler, admissionv1.AdmissionRequest{}))
		require.False(t, changed)
		require.Equal(t, before, obj)
		if expression == "false" {
			require.NoError(t, err)
		} else {
			require.ErrorContains(t, err, "conditions[0]")
		}
	}
}

func BenchmarkTemplateResourceMutation(b *testing.B) {
	for _, count := range []int{1, 20} {
		b.Run(fmt.Sprintf("rules=%d", count), func(b *testing.B) {
			var bodies []*rules.NamespaceRuleBodyNamespace
			for range count {
				bodies = append(bodies, &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: []rules.WorkloadValidationTarget{rules.ValidateDeployment}, Resources: &rules.WorkloadResourceRules{Requests: map[corev1.ResourceName]rules.WorkloadResourceRequestPolicy{corev1.ResourceMemory: {Policy: rules.WorkloadResourceRequestPolicyDefault, Value: new(resource.MustParse("1Gi"))}}}}}})
			}
			original := resourceTemplateFixture(rules.ValidateDeployment)
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				obj := original.DeepCopy()
				b.StartTimer()
				changed, err := MutateWorkloadResources(b.Context(), obj, obj.GroupVersionKind(), bodies, nil)
				if err != nil || !changed {
					b.Fatalf("mutation failed: changed=%v err=%v", changed, err)
				}
			}
		})
	}
}
