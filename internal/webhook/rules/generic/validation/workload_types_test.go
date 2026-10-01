// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

func typePolicy(action rules.ActionType, types ...rules.WorkloadValidationTarget) *rules.NamespaceRuleEnforceBody {
	return &rules.NamespaceRuleEnforceBody{Action: action, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: types}}
}

func TestWorkloadTypesMatchNativeGroups(t *testing.T) {
	h := GenericRules(nil).(*genericRules)
	for _, tc := range []struct{ group, kind string }{
		{"", "Pod"}, {"", "ReplicationController"}, {"apps", "Deployment"},
		{"apps", "StatefulSet"}, {"apps", "DaemonSet"}, {"apps", "ReplicaSet"},
		{"batch", "Job"}, {"batch", "CronJob"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			for _, version := range []string{"v1", "v1beta1"} {
				gvk := schema.GroupVersionKind{Group: tc.group, Version: version, Kind: tc.kind}
				bodies := []*rules.NamespaceRuleEnforceBody{typePolicy(rules.ActionTypeDeny, rules.WorkloadValidationTarget(strings.ToLower(tc.kind)))}
				before := bodies[0].DeepCopy()
				result, err := h.validateWorkloadTypes(nil, &metav1.PartialObjectMetadata{}, gvk, bodies)
				require.NoError(t, err)
				require.ErrorContains(t, result.BlockingError(), tc.kind)
				require.Equal(t, before, bodies[0])
				gvk.Group = "custom.example.com"
				result, err = h.validateWorkloadTypes(nil, &metav1.PartialObjectMetadata{}, gvk, bodies)
				require.NoError(t, err)
				require.Nil(t, result, "a CRD sharing a kind name is not a native workload")
			}
		})
	}
}

func TestWorkloadTypeDecisions(t *testing.T) {
	h := GenericRules(nil).(*genericRules)
	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DaemonSet"}
	for _, tc := range []struct {
		name   string
		bodies []*rules.NamespaceRuleEnforceBody
		denied bool
		audits int
	}{
		{"omitted", nil, false, 0},
		{"empty", []*rules.NamespaceRuleEnforceBody{nil, typePolicy(rules.ActionTypeAllow)}, false, 0},
		{"deny", []*rules.NamespaceRuleEnforceBody{typePolicy(rules.ActionTypeDeny, rules.ValidateDaemonSet)}, true, 0},
		{"default deny", []*rules.NamespaceRuleEnforceBody{typePolicy("", rules.ValidateDaemonSet)}, true, 0},
		{"deny different type", []*rules.NamespaceRuleEnforceBody{typePolicy(rules.ActionTypeDeny, rules.ValidateJob)}, false, 0},
		{"allow", []*rules.NamespaceRuleEnforceBody{typePolicy(rules.ActionTypeAllow, rules.ValidateDaemonSet)}, false, 0},
		{"allow miss", []*rules.NamespaceRuleEnforceBody{typePolicy(rules.ActionTypeAllow, rules.ValidateDeployment)}, true, 0},
		{"audit", []*rules.NamespaceRuleEnforceBody{typePolicy(rules.ActionTypeAudit, rules.ValidateDaemonSet)}, false, 1},
		{"later allow", []*rules.NamespaceRuleEnforceBody{typePolicy(rules.ActionTypeDeny, rules.ValidateDaemonSet), typePolicy(rules.ActionTypeAllow, rules.ValidateDaemonSet)}, false, 0},
		{"later deny with audit", []*rules.NamespaceRuleEnforceBody{typePolicy(rules.ActionTypeAllow, rules.ValidateDaemonSet), typePolicy(rules.ActionTypeDeny, rules.ValidateDaemonSet), typePolicy(rules.ActionTypeAudit, rules.ValidateDaemonSet)}, true, 1},
		{"audit cannot satisfy allow list", []*rules.NamespaceRuleEnforceBody{typePolicy(rules.ActionTypeAllow, rules.ValidateDeployment), typePolicy(rules.ActionTypeAudit, rules.ValidateDaemonSet)}, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {

			result, err := h.validateWorkloadTypes(nil, &metav1.PartialObjectMetadata{}, gvk, tc.bodies)
			require.NoError(t, err)
			require.Equal(t, tc.denied, result.BlockingError() != nil)
			if result != nil {
				require.Len(t, result.Audits, tc.audits)
			}
		})
	}
	for _, kind := range []string{"Pod", "ReplicaSet"} {
		group := "apps"
		if kind == "Pod" {
			group = ""
		}
		result, err := h.validateWorkloadTypes(nil, &metav1.PartialObjectMetadata{}, schema.GroupVersionKind{Group: group, Version: "v1", Kind: kind}, []*rules.NamespaceRuleEnforceBody{typePolicy(rules.ActionTypeAllow, rules.ValidateDeployment)})
		require.NoError(t, err)
		require.ErrorContains(t, result.BlockingError(), "not allowed", "allowing Deployment does not allow children")
	}
}

func TestWorkloadTypeConditionsAndOperations(t *testing.T) {
	for _, operation := range []admissionv1.Operation{admissionv1.Create, admissionv1.Update} {
		for _, tc := range []struct{ name, expression, failure string }{
			{"true", "true", "workload type"},
			{"false", "false", ""},
			{"full object", "object.spec.template.spec.containers[0].image == 'example/app:v1'", "workload type"},
			{"request", "request.namespace == 'tenant-a'", "workload type"},
			{"error", "object.spec.missing == 'x'", "conditions[0]"},
			{"false dominates error", "false", ""},
		} {
			t.Run(string(operation)+"/"+tc.name, func(t *testing.T) {
				compiler, err := cache.NewCELCache()
				require.NoError(t, err)
				h := GenericRules(nil, compiler)
				body := &rules.NamespaceRuleBodyNamespace{Enforce: typePolicy(rules.ActionTypeDeny, rules.ValidateDaemonSet)}
				body.Enforce.Conditions = []rules.AdmissionCondition{{Expression: tc.expression}}
				if tc.name == "false dominates error" {
					body.Enforce.Conditions = append([]rules.AdmissionCondition{{Expression: "object.missing == 'x'"}}, body.Enforce.Conditions...)
				}
				obj := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "tenant-a"}}
				req := requestWithKind("apps", "DaemonSet")
				req.Namespace, req.Operation = obj.Namespace, operation
				req.Object.Raw = []byte(`{"metadata":{"name":"example"},"spec":{"template":{"spec":{"containers":[{"image":"example/app:v1"}]}}}}`)
				bodies := []*rules.NamespaceRuleBodyNamespace{body}
				var response *admission.Response
				if operation == admissionv1.Create {
					response = h.OnCreate(nil, nil, obj, nil, testEventRecorder{}, nil, bodies)(t.Context(), req)
				} else {
					response = h.OnUpdate(nil, nil, obj.DeepCopy(), obj, nil, testEventRecorder{}, nil, bodies)(t.Context(), req)
				}
				if tc.failure == "" {
					require.Nil(t, response)
				} else {
					require.NotNil(t, response)
					require.False(t, response.Allowed)
					require.Contains(t, response.Result.Message, tc.failure)
				}
			})
		}
	}
}

func TestWorkloadTypeScopeAndMetadataIndependence(t *testing.T) {
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	h := GenericRules(nil, compiler)
	obj := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{meta.NewManagedByCapsuleLabel: meta.ValueControllerResources}}}
	deny := &rules.NamespaceRuleBodyNamespace{Enforce: typePolicy(rules.ActionTypeDeny, rules.ValidateDaemonSet)}
	bodies := []*rules.NamespaceRuleBodyNamespace{deny}
	req := requestWithKind("apps", "DaemonSet")
	response := h.OnCreate(nil, nil, obj, nil, testEventRecorder{}, nil, bodies)(t.Context(), req)
	require.NotNil(t, response)
	require.Contains(t, response.Result.Message, "workload type")
	for _, subresource := range []string{"status", "scale", "ephemeralcontainers"} {
		req.SubResource = subresource
		require.Nil(t, h.OnUpdate(nil, nil, obj, obj, nil, nil, nil, bodies)(t.Context(), req))
	}
	req.SubResource = ""
	require.Nil(t, h.OnDelete(nil, nil, obj, nil, nil, nil, bodies)(t.Context(), req))
	deny.Enforce.Conditions = []rules.AdmissionCondition{{Expression: "object.spec.missing == 'x'"}}
	for _, gvk := range []schema.GroupVersionKind{{Version: "v1", Kind: "ConfigMap"}, {Group: "other.example", Version: "v1", Kind: "DaemonSet"}, {Version: "v1", Kind: "Pod"}, {Group: "batch", Version: "v1", Kind: "Job"}} {
		req.Kind = metav1.GroupVersionKind(gvk)
		require.Nil(t, h.OnCreate(nil, nil, obj, nil, nil, nil, bodies)(t.Context(), req))
	}
	require.Zero(t, compiler.Stats(), "irrelevant conditions must not compile")
	// A matching allow is a nil response, so other metadata and Pod policies still run.
	allow := &rules.NamespaceRuleBodyNamespace{Enforce: typePolicy(rules.ActionTypeAllow, rules.ValidateDaemonSet)}
	metadata := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny, Metadata: []rules.MetadataRule{{VersionKinds: apiruntime.VersionKinds{APIGroups: []string{"apps"}, Kinds: []string{"DaemonSet"}}, Labels: map[string]rules.MetadataValueRule{"blocked": {}}}}}}
	obj.Labels = map[string]string{"blocked": "true"}
	req = requestWithKind("apps", "DaemonSet")
	response = h.OnCreate(nil, nil, obj, nil, testEventRecorder{}, nil, []*rules.NamespaceRuleBodyNamespace{allow, metadata})(t.Context(), req)
	require.NotNil(t, response)
	require.Contains(t, response.Result.Message, "metadata label")
	require.Nil(t, h.OnCreate(nil, nil, obj, nil, nil, nil, []*rules.NamespaceRuleBodyNamespace{allow})(t.Context(), req))
}

func BenchmarkWorkloadTypeAdmission(b *testing.B) {
	for _, tenants := range []int{1, 8} {
		for _, count := range []int{1, 20} {
			for _, mode := range []string{"allow", "deny", "audit", "skip", "conditional", "false", "unrelated"} {
				b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s", tenants, count, mode), func(b *testing.B) {
					compiler, err := cache.NewCELCache()
					require.NoError(b, err)
					h := GenericRules(nil, compiler)
					bodies := make([][]*rules.NamespaceRuleBodyNamespace, tenants)
					objects := make([]*metav1.PartialObjectMetadata, tenants)
					tnts := make([]*capsulev1beta2.Tenant, tenants)
					reqs := make([]admission.Request, tenants)
					for tenant := range tenants {
						tnts[tenant] = &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("tenant-%d", tenant)}}
						objects[tenant] = &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: tnts[tenant].Name}}
						reqs[tenant] = requestWithKind("apps", "DaemonSet")
						reqs[tenant].Namespace = tnts[tenant].Name
						reqs[tenant].Operation = admissionv1.Create
						if mode == "unrelated" {
							reqs[tenant] = requestWithKind("", "ConfigMap")
						}
						reqs[tenant].Object.Raw, err = json.Marshal(map[string]any{"metadata": map[string]string{"namespace": tnts[tenant].Name}, "spec": map[string]bool{"enabled": true}})
						require.NoError(b, err)
						for range count {
							action := rules.ActionTypeAllow
							if mode == "deny" {
								action = rules.ActionTypeDeny
							}
							if mode == "audit" {
								action = rules.ActionTypeAudit
							}
							body := &rules.NamespaceRuleBodyNamespace{Enforce: typePolicy(action, rules.ValidateDaemonSet)}
							if mode == "skip" {
								body.Enforce.Workloads.Targets = nil
							}
							if mode == "conditional" || mode == "false" || mode == "unrelated" {
								exp := "object.spec.enabled && object.metadata.namespace == request.namespace"
								if mode == "false" {
									exp = "false"
								}
								body.Enforce.Conditions = []rules.AdmissionCondition{{Expression: exp}}
							}
							bodies[tenant] = append(bodies[tenant], body)
						}
					}
					// Warm only immutable compiled expressions; each call has a fresh evaluator.
					h.OnCreate(nil, nil, objects[0], nil, testEventRecorder{}, tnts[0], bodies[0])(b.Context(), reqs[0])
					b.ReportAllocs()
					for i := 0; b.Loop(); i++ {
						index := i % tenants
						response := h.OnCreate(nil, nil, objects[index], nil, testEventRecorder{}, tnts[index], bodies[index])(b.Context(), reqs[index])
						if mode == "deny" {
							if response == nil || response.Allowed {
								b.Fatal("expected denial")
							}
						} else if response != nil {
							b.Fatalf("unexpected response: %+v", response)
						}
					}
				})
			}
		}
	}
}
