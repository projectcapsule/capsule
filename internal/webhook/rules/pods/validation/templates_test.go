// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/workloads"
)

func templateFixture(t testing.TB, target rules.WorkloadValidationTarget) (*unstructured.Unstructured, admission.Request) {
	t.Helper()
	gk, ok := target.GroupKind()
	require.True(t, ok)
	gvk := gk.WithVersion("v1")
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": gvk.GroupVersion().String(), "kind": gvk.Kind, "metadata": map[string]any{"name": "example", "namespace": "tenant-a", "labels": map[string]any{"blocked": "yes"}}, "spec": map[string]any{}}}
	data := map[string]any{"metadata": map[string]any{"labels": map[string]any{"app": "example"}}, "spec": map[string]any{
		"schedulerName": "batch", "nodeSelector": map[string]any{"pool": "batch"},
		"containers":     []any{map[string]any{"name": "app", "image": "example.com/team/app:v1", "resources": map[string]any{"requests": map[string]any{"memory": "1Gi"}, "limits": map[string]any{"memory": "2Gi"}}}},
		"initContainers": []any{map[string]any{"name": "init", "image": "example.com/team/init:v1"}},
	}}
	require.NoError(t, unstructured.SetNestedMap(obj.Object, data, workloads.PodTemplatePath(gvk)...))
	return obj, admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Kind: metav1.GroupVersionKind(gvk), Operation: admissionv1.Create, Namespace: obj.GetNamespace()}}
}

type templateRecorder struct {
	events.EventRecorder
	regarding runtime.Object
}

func (r *templateRecorder) LabeledEvent(obj runtime.Object, typ, reason, action, note string) events.LabeledEvent {
	r.regarding = obj
	return r.EventRecorder.LabeledEvent(obj, typ, reason, action, note)
}

func TestTemplateWorkloadPolicies(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	h := TemplateRules(nil, nil, compiler)
	for _, target := range []rules.WorkloadValidationTarget{rules.ValidateDeployment, rules.ValidateStatefulSet, rules.ValidateDaemonSet, rules.ValidateReplicaSet, rules.ValidateReplicationController, rules.ValidateJob, rules.ValidateCronJob} {
		for _, policy := range []struct {
			name, message string
			body          rules.NamespaceRuleEnforceWorkloadsBody
		}{
			{"legacy scheduler", "scheduler", rules.NamespaceRuleEnforceWorkloadsBody{Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{"batch"}}}}},
			{"scheduler", "scheduler", rules.NamespaceRuleEnforceWorkloadsBody{Placement: rules.WorkloadPlacementEnforcement{Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{"batch"}}}}}},
			{"registry", "registry", rules.NamespaceRuleEnforceWorkloadsBody{Registries: []rules.OCIRegistry{{ExpressionMatch: apiruntime.ExpressionMatch{Exact: []string{"example.com/team/app:v1"}}}}}},
			{"resources", "resource", rules.NamespaceRuleEnforceWorkloadsBody{Resources: &rules.WorkloadResourceRules{Limits: map[corev1.ResourceName]rules.WorkloadResourceLimitPolicy{corev1.ResourceMemory: {Policy: rules.WorkloadResourceLimitPolicyRatio, Value: new(resource.MustParse("1.5"))}}}}},
			{"qos", "QoS", rules.NamespaceRuleEnforceWorkloadsBody{QoSClasses: []corev1.PodQOSClass{corev1.PodQOSBurstable}}},
			{"placement", "nodeSelector", rules.NamespaceRuleEnforceWorkloadsBody{Placement: rules.WorkloadPlacementEnforcement{NodeSelector: []rules.WorkloadNodeSelectorMatch{{}}}}},
		} {
			t.Run(string(target)+"/"+policy.name, func(t *testing.T) {
				obj, req := templateFixture(t, target)
				workload := policy.body
				workload.Targets = []rules.WorkloadValidationTarget{target}
				body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny, Workloads: workload}}
				before := body.DeepCopy()
				original := obj.DeepCopy()
				recorder := &templateRecorder{EventRecorder: events.NewEventRecorder(nil, logr.Discard(), nil, nil)}
				tenant := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"}}
				for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
					req.Operation = operation
					var response *admission.Response
					if operation == admissionv1.Create {
						response = h.OnCreate(nil, nil, obj, nil, recorder, tenant, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
					} else {
						old := obj.DeepCopy()
						// Force placement re-evaluation on a changed spec.
						require.NoError(t, unstructured.SetNestedStringMap(old.Object, map[string]string{}, append(workloads.PodTemplatePath(old.GroupVersionKind()), "spec", "nodeSelector")...))
						response = h.OnUpdate(nil, nil, old, obj, nil, recorder, tenant, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
					}
					require.NotNil(t, response)
					require.False(t, response.Allowed)
					require.Contains(t, response.Result.Message, policy.message)
					require.Contains(t, response.Result.Message, strings.Join(workloads.PodTemplatePath(obj.GroupVersionKind()), "."))
					require.Same(t, obj, recorder.regarding, "events must concern the controller, not a synthetic Pod")
				}
				require.Equal(t, before, body)
				require.Equal(t, original, obj)
				body.Enforce.Action = rules.ActionTypeAudit
				require.Nil(t, h.OnCreate(nil, nil, obj, nil, recorder, tenant, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req))
				if policy.message == "scheduler" {
					body.Enforce.Action = rules.ActionTypeAllow
					req.Operation = admissionv1.Create
					require.Nil(t, h.OnCreate(nil, nil, obj, nil, recorder, tenant, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req))

					updated := obj.DeepCopy()
					path := workloads.PodTemplatePath(obj.GroupVersionKind())
					require.NoError(t, unstructured.SetNestedField(updated.Object, "unlisted", append(path, "spec", "schedulerName")...))
					req.Operation = admissionv1.Update
					response := h.OnUpdate(nil, nil, obj, updated, nil, recorder, tenant, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
					require.NotNil(t, response)
					require.False(t, response.Allowed)
					require.Contains(t, response.Result.Message, strings.Join(path, ".")+`: scheduler "unlisted" at spec.schedulerName is not allowed by namespace rule`)
				}
				// Omitting targets and selecting a different kind must not opt this controller in.
				body.Enforce.Action = rules.ActionTypeDeny
				for _, targets := range [][]rules.WorkloadValidationTarget{nil, {rules.ValidatePod}} {
					body.Enforce.Workloads.Targets = targets
					require.Nil(t, h.OnCreate(nil, nil, obj, nil, recorder, tenant, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req))
				}
			})
		}
	}
}

func TestTemplateConditionsPartsAndScope(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	h := TemplateRules(nil, nil, compiler)
	obj, req := templateFixture(t, rules.ValidateDeployment)
	recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
	body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: []rules.WorkloadValidationTarget{"deployment/initcontainers"}, Registries: []rules.OCIRegistry{{ExpressionMatch: apiruntime.ExpressionMatch{Exact: []string{"example.com/team/app:v1"}}}}}}}
	call := func() *admission.Response {
		return h.OnCreate(nil, nil, obj, nil, recorder, &capsulev1beta2.Tenant{}, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
	}
	require.Nil(t, call(), "container image is outside selected init containers")
	body.Enforce.Workloads.Targets = []rules.WorkloadValidationTarget{"deployment/containers"}
	require.Contains(t, call().Result.Message, "registry")
	for _, tc := range []struct {
		expression string
		denied     bool
		message    string
	}{
		{`object.metadata.labels['blocked'] == 'yes' && object.spec.template.spec.schedulerName == 'batch'`, true, "registry"},
		{`object.metadata.labels['blocked'] == 'no'`, false, ""},
		{`object.spec.missing == 'x'`, true, "conditions[0]"},
	} {
		body.Enforce.Conditions = []rules.AdmissionCondition{{Expression: tc.expression}}
		got := call()
		if tc.denied {
			require.NotNil(t, got)
			require.Contains(t, got.Result.Message, tc.message)
		} else {
			require.Nil(t, got)
		}
	}
	for _, subresource := range []string{"status", "scale", "ephemeralcontainers"} {
		req.SubResource = subresource
		require.Nil(t, call())
	}
	req.SubResource = ""
	req.Kind.Group = "custom.example.com"
	require.Nil(t, call())
	req.Kind.Group = "apps"
	require.Nil(t, h.OnDelete(nil, nil, obj, nil, recorder, nil, nil)(t.Context(), req))
	delete(obj.Object, "spec")
	body.Enforce.Conditions = nil
	require.Contains(t, call().Result.Message, "template is missing")
	// Controller-only scheduler/QoS policies cannot affect Pods.
	pod := &corev1.Pod{Spec: corev1.PodSpec{SchedulerName: "batch"}}
	podBody := &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: []rules.WorkloadValidationTarget{rules.ValidateDeployment}, Placement: rules.WorkloadPlacementEnforcement{Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{"batch"}}}}}}
	require.NoError(t, PodRules(nil, nil, compiler).(*podRules).validatePodRules(t.Context(), admission.Request{}, pod, nil, nil, []*rules.NamespaceRuleEnforceBody{podBody}))
}

func BenchmarkTemplateAdmission(b *testing.B) {
	for _, count := range []int{1, 20} {
		for _, mode := range []string{"skip", "allow", "deny", "conditional", "cold"} {
			b.Run(fmt.Sprintf("rules=%d/%s", count, mode), func(b *testing.B) {
				compiler, err := cache.NewCELCache()
				require.NoError(b, err)
				h := TemplateRules(nil, nil, compiler)
				obj, req := templateFixture(b, rules.ValidateDeployment)
				var bodies []*rules.NamespaceRuleBodyNamespace
				for range count {
					body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: []rules.WorkloadValidationTarget{rules.ValidateDeployment}, Placement: rules.WorkloadPlacementEnforcement{Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{"other"}}}}}}}
					if mode == "skip" {
						body.Enforce.Workloads.Targets = []rules.WorkloadValidationTarget{rules.ValidateJob}
					}
					if mode == "deny" {
						body.Enforce.Workloads.Placement.Schedulers[0].Exact[0] = "batch"
					}
					if mode == "conditional" || mode == "cold" {
						body.Enforce.Conditions = []rules.AdmissionCondition{{Expression: `object.spec.template.spec.schedulerName == 'batch'`}}
					}
					bodies = append(bodies, body)
				}
				recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
				tenant := &capsulev1beta2.Tenant{}
				call := h.OnCreate(nil, nil, obj, nil, recorder, tenant, bodies)
				call(b.Context(), req)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if mode == "cold" {
						compiler.Reset()
					}
					got := call(b.Context(), req)
					if (got != nil) != (mode == "deny") {
						b.Fatalf("unexpected response: %v", got)
					}
				}
			})
		}
	}
}

func BenchmarkTemplateSizeAndConcurrency(b *testing.B) {
	for _, tenants := range []int{1, 8} {
		for _, containers := range []int{1, 20} {
			for _, parallel := range []bool{false, true} {
				b.Run(fmt.Sprintf("tenants=%d/containers=%d/parallel=%v", tenants, containers, parallel), func(b *testing.B) {
					h := TemplateRules(nil, nil, nil)
					obj, req := templateFixture(b, rules.ValidateDeployment)
					data := make([]any, containers)
					for i := range data {
						data[i] = map[string]any{"name": fmt.Sprintf("app-%d", i), "image": "example.com/team/app:v1"}
					}
					require.NoError(b, unstructured.SetNestedSlice(obj.Object, data, "spec", "template", "spec", "containers"))
					profiles := make([][]*rules.NamespaceRuleBodyNamespace, tenants)
					for tenant := range tenants {
						profiles[tenant] = []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: []rules.WorkloadValidationTarget{rules.ValidateDeployment}, Registries: []rules.OCIRegistry{{ExpressionMatch: apiruntime.ExpressionMatch{ExpressionRegex: apiruntime.ExpressionRegex{Expression: "^example.com/team/"}}}}}}}}
					}
					recorder := events.NewEventRecorder(nil, logr.Discard(), nil, nil)
					run := func(object *unstructured.Unstructured, index int) {
						body := profiles[index%tenants]
						if response := h.OnCreate(nil, nil, object, nil, recorder, nil, body)(b.Context(), req); response != nil {
							b.Fatalf("unexpected denial: %v", response)
						}
					}
					run(obj, 0)
					b.ReportAllocs()
					b.ResetTimer()
					if parallel {
						b.RunParallel(func(pb *testing.PB) {
							object := obj.DeepCopy()
							i := 0
							for pb.Next() {
								run(object, i)
								i++
							}
						})
					} else {
						for i := 0; b.Loop(); i++ {
							run(obj, i)
						}
					}
				})
			}
		}
	}
}
