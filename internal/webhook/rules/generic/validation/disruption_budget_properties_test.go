// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	webhookutils "github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
)

func propertyBudget(namespace, name string, minAvailable, maxUnavailable *intstr.IntOrString, policy *policyv1.UnhealthyPodEvictionPolicyType) *policyv1.PodDisruptionBudget {
	pdb := overlapPDB(namespace, name, &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}})
	pdb.Spec.MinAvailable, pdb.Spec.MaxUnavailable, pdb.Spec.UnhealthyPodEvictionPolicy = minAvailable, maxUnavailable, policy
	return pdb
}

func replicaObject(kind, namespace string, replicas int64) *unstructured.Unstructured {
	obj := overlapObject(kind, "web", namespace, map[string]string{"app": "web"})
	if err := unstructured.SetNestedField(obj.Object, replicas, "spec", "replicas"); err != nil {
		panic(err)
	}
	return obj
}

func replicaRule(action rules.ActionType, min, max *int64, targets ...rules.WorkloadValidationTarget) *rules.NamespaceRuleBodyNamespace {
	if len(targets) == 0 {
		targets = []rules.WorkloadValidationTarget{rules.ValidateDeployment}
	}
	return &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: action, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: targets, DisruptionBudgets: &rules.WorkloadDisruptionBudgetRules{EvictableReplicas: &rules.PlacementRange{Min: min, Max: max}}}}}
}

func TestConfiguredEvictableReplicas(t *testing.T) {
	for _, tc := range []struct {
		name     string
		replicas int64
		min, max *intstr.IntOrString
		want     int64
		err      bool
	}{
		{"min integer", 3, new(intstr.FromInt32(2)), nil, 1, false},
		{"min exceeds scale", 1, new(intstr.FromInt32(2)), nil, 0, false},
		{"max integer", 3, nil, new(intstr.FromInt32(1)), 1, false},
		{"max bounded by scale", 3, nil, new(intstr.FromInt32(5)), 3, false},
		{"min percentage rounds up", 3, new(intstr.FromString("75%")), nil, 0, false},
		{"min percentage exact", 4, new(intstr.FromString("75%")), nil, 1, false},
		{"min percentage seven", 7, new(intstr.FromString("50%")), nil, 3, false},
		{"max percentage rounds up", 3, nil, new(intstr.FromString("25%")), 1, false},
		{"singleton percentage", 1, nil, new(intstr.FromString("25%")), 1, false},
		{"zero scale", 0, nil, new(intstr.FromString("25%")), 0, false},
		{"zero unavailable", 3, nil, new(intstr.FromInt32(0)), 0, false},
		{"max scale", 2147483647, new(intstr.FromString("100%")), nil, 0, false},
		{"malformed", 3, nil, new(intstr.FromString("bad")), 0, true},
		{"missing", 3, nil, nil, 0, false},
		{"both", 3, new(intstr.FromInt32(1)), new(intstr.FromInt32(1)), 0, true},
		{"negative budget", 3, new(intstr.FromInt32(-1)), nil, 0, true},
		{"negative scale", -1, nil, new(intstr.FromInt32(1)), 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := configuredEvictableReplicas(&policyv1.PodDisruptionBudgetSpec{MinAvailable: tc.min, MaxUnavailable: tc.max}, tc.replicas)
			if tc.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
			}
		})
	}
}

func TestBudgetPropertyComposition(t *testing.T) {
	strict := replicaRule(rules.ActionTypeAllow, new(int64(1)), new(int64(2)))
	deny := replicaRule(rules.ActionTypeDeny, new(int64(1)), nil)
	audit := replicaRule(rules.ActionTypeAudit, new(int64(1)), nil)
	exception := replicaRule(rules.ActionTypeAllow, new(int64(0)), nil)
	unhealthy := replicaRule(rules.ActionTypeAllow, nil, nil)
	unhealthy.Enforce.Workloads.DisruptionBudgets.UnhealthyPodEvictionPolicies = []policyv1.UnhealthyPodEvictionPolicyType{policyv1.AlwaysAllow}
	unhealthyDeny := unhealthy.DeepCopy()
	unhealthyDeny.Enforce.Action = rules.ActionTypeDeny
	unhealthyDeny.Enforce.Workloads.DisruptionBudgets.UnhealthyPodEvictionPolicies = []policyv1.UnhealthyPodEvictionPolicyType{policyv1.IfHealthyBudget}
	for _, tc := range []struct {
		name     string
		replicas int64
		pdb      *policyv1.PodDisruptionBudget
		bodies   []*rules.NamespaceRuleBodyNamespace
		want     string
		audits   int
	}{
		{"compliant", 3, propertyBudget("a", "one", nil, new(intstr.FromInt32(1)), nil), []*rules.NamespaceRuleBodyNamespace{strict}, "", 0},
		{"min allow miss", 3, propertyBudget("a", "one", new(intstr.FromString("75%")), nil, nil), []*rules.NamespaceRuleBodyNamespace{strict}, "evictable replicas", 0},
		{"max allow miss", 4, propertyBudget("a", "one", nil, new(intstr.FromString("100%")), nil), []*rules.NamespaceRuleBodyNamespace{strict}, "evictable replicas", 0},
		{"deny violation", 3, propertyBudget("a", "one", nil, new(intstr.FromInt32(0)), nil), []*rules.NamespaceRuleBodyNamespace{deny}, "evictable replicas", 0},
		{"later exception", 3, propertyBudget("a", "one", nil, new(intstr.FromInt32(0)), nil), []*rules.NamespaceRuleBodyNamespace{deny, exception}, "", 0},
		{"later deny", 3, propertyBudget("a", "one", nil, new(intstr.FromInt32(0)), nil), []*rules.NamespaceRuleBodyNamespace{exception, deny}, "evictable replicas", 0},
		{"audit empty budget", 3, propertyBudget("a", "one", nil, nil, nil), []*rules.NamespaceRuleBodyNamespace{audit}, "", 1},
		{"audit only", 3, propertyBudget("a", "one", nil, new(intstr.FromInt32(0)), nil), []*rules.NamespaceRuleBodyNamespace{audit}, "", 1},
		{"zero scale exempt", 0, propertyBudget("a", "one", nil, new(intstr.FromInt32(0)), nil), []*rules.NamespaceRuleBodyNamespace{strict}, "", 0},
		{"unhealthy omitted defaults", 3, propertyBudget("a", "one", nil, new(intstr.FromInt32(1)), nil), []*rules.NamespaceRuleBodyNamespace{unhealthy}, "IfHealthyBudget", 0},
		{"unhealthy explicit allow", 3, propertyBudget("a", "one", nil, new(intstr.FromInt32(1)), new(policyv1.AlwaysAllow)), []*rules.NamespaceRuleBodyNamespace{unhealthy}, "", 0},
		{"unhealthy deny list", 3, propertyBudget("a", "one", nil, new(intstr.FromInt32(1)), nil), []*rules.NamespaceRuleBodyNamespace{unhealthyDeny}, "IfHealthyBudget", 0},
		{"unhealthy does not override replicas", 3, propertyBudget("a", "one", nil, new(intstr.FromInt32(0)), new(policyv1.AlwaysAllow)), []*rules.NamespaceRuleBodyNamespace{strict, unhealthy}, "evictable replicas", 0},
		{"zero still checks unhealthy", 0, propertyBudget("a", "one", nil, new(intstr.FromInt32(0)), nil), []*rules.NamespaceRuleBodyNamespace{strict, unhealthy}, "IfHealthyBudget", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := budgetWorkload{gvk: schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, name: "web", replicas: &tc.replicas}
			evaluation, err := evaluateBudgetPolicies(value, []string{"one"}, &tc.pdb.Spec, ruleengine.EnforceBodiesFromNamespaceRules(tc.bodies))
			require.NoError(t, err)
			if tc.want == "" {
				require.NoError(t, evaluation.BlockingError())
			} else {
				require.ErrorContains(t, evaluation.BlockingError(), tc.want)
			}
			require.Len(t, evaluation.Audits, tc.audits)
		})
	}
}

func TestBudgetPropertyAdmission(t *testing.T) {
	basePDB := propertyBudget("a", "one", new(intstr.FromString("75%")), nil, new(policyv1.AlwaysAllow))
	changedPDB := basePDB.DeepCopy()
	changedPDB.Spec.MinAvailable = new(intstr.FromString("100%"))
	badPolicy := basePDB.DeepCopy()
	badPolicy.Spec.UnhealthyPodEvictionPolicy = nil
	strict := replicaRule(rules.ActionTypeAllow, new(int64(1)), nil)
	both := strict.DeepCopy()
	both.Enforce.Workloads.DisruptionBudgets.UnhealthyPodEvictionPolicies = []policyv1.UnhealthyPodEvictionPolicyType{policyv1.AlwaysAllow}
	for _, tc := range []struct {
		name        string
		object, old client.Object
		objects     []client.Object
		body        *rules.NamespaceRuleBodyNamespace
		want        string
		lists       int
	}{
		{"controller create denies", replicaObject("deployment", "a", 3), nil, []client.Object{basePDB}, strict, "evictable replicas", 1},
		{"controller create allows", replicaObject("deployment", "a", 4), nil, []client.Object{basePDB}, strict, "", 1},
		{"controller scale down", replicaObject("deployment", "a", 3), replicaObject("deployment", "a", 4), []client.Object{basePDB}, strict, "evictable replicas", 1},
		{"controller scale repair", replicaObject("deployment", "a", 4), replicaObject("deployment", "a", 3), []client.Object{basePDB}, strict, "", 1},
		{"zero scale", replicaObject("deployment", "a", 0), replicaObject("deployment", "a", 4), []client.Object{basePDB}, strict, "", 1},
		{"unchanged", replicaObject("deployment", "a", 3), replicaObject("deployment", "a", 3), []client.Object{basePDB}, strict, "", 0},
		{"no PDB required", replicaObject("deployment", "b", 1), nil, []client.Object{basePDB}, strict, "", 1},
		{"PDB create denies", basePDB, nil, []client.Object{replicaObject("deployment", "a", 3)}, strict, "evictable replicas", 1},
		{"PDB create allows", basePDB, nil, []client.Object{replicaObject("deployment", "a", 4)}, strict, "", 1},
		{"PDB before controller", basePDB, nil, nil, strict, "", 1},
		{"PDB changes budget only", changedPDB, basePDB, []client.Object{replicaObject("deployment", "a", 4)}, strict, "evictable replicas", 1},
		{"PDB budget repair", basePDB, changedPDB, []client.Object{replicaObject("deployment", "a", 4)}, strict, "", 1},
		{"PDB unchanged spec", changedPDB, changedPDB, []client.Object{replicaObject("deployment", "a", 4)}, strict, "", 0},
		{"PDB policy only", badPolicy, basePDB, []client.Object{replicaObject("deployment", "a", 4)}, both, "IfHealthyBudget", 1},
		{"PDB policy repair", basePDB, badPolicy, []client.Object{replicaObject("deployment", "a", 4)}, both, "", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheme := overlapScheme(t)
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.objects...).Build()
			reader := &overlapReader{Reader: cl}
			h := &disruptionBudgetRules{selectors: cache.NewLabelSelectorCache()}
			req, obj, old := overlapRequest(t, tc.object, tc.old)
			before := tc.body.DeepCopy()
			response := h.validate(reader, old, obj, admission.NewDecoder(scheme), testEventRecorder{}, &capsulev1beta2.Tenant{}, []*rules.NamespaceRuleBodyNamespace{tc.body})(t.Context(), req)
			if tc.want == "" {
				require.Nil(t, response)
			} else {
				require.NotNil(t, response)
				require.False(t, response.Allowed)
				require.Contains(t, response.Result.Message, tc.want)
			}
			require.Equal(t, tc.lists, reader.lists)
			require.Equal(t, before, tc.body)
		})
	}
}

func scaleRequest(t testing.TB, kind, namespace string, old, replicas int64) admission.Request {
	t.Helper()
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "autoscaling/v1", "kind": "Scale", "metadata": map[string]any{"name": "web", "namespace": namespace}, "spec": map[string]any{"replicas": replicas}}}
	previous := obj.DeepCopy()
	require.NoError(t, unstructured.SetNestedField(previous.Object, old, "spec", "replicas"))
	req, _, _ := overlapRequest(t, obj, previous)
	req.SubResource = "scale"
	group := "apps"
	if kind == "replicationcontroller" {
		group = ""
	}
	req.Resource = metav1.GroupVersionResource{Group: group, Version: "v1", Resource: kind + "s"}
	return req
}

func TestBudgetScaleAdmissionChain(t *testing.T) {
	for _, kind := range []string{"deployment", "statefulset", "replicaset", "replicationcontroller"} {
		for _, tc := range []struct {
			name        string
			replicas    int64
			profile     bool
			condition   string
			want        string
			gets, lists int64
		}{
			{"deny down", 3, true, "", "evictable replicas", 4, 1},
			{"allow up", 8, true, "", "", 4, 1},
			{"zero", 0, true, "", "", 4, 1},
			{"unchanged", 4, true, "", "", 0, 0},
			{"other namespace profile", 3, false, "", "", 3, 0},
			{"scale condition true", 3, true, "request.subResource == 'scale' && object.spec.replicas == 3", "evictable replicas", 4, 1},
			{"scale condition false", 3, true, "request.subResource == ''", "", 3, 0},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				scheme := overlapScheme(t)
				tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "a", UID: "a"}}
				rs := &capsulev1beta2.RuleStatus{ObjectMeta: metav1.ObjectMeta{Name: meta.NameForManagedRuleStatus(), Namespace: "a"}}
				body := replicaRule(rules.ActionTypeAllow, new(int64(1)), nil, rules.WorkloadValidationTarget(kind))
				if tc.condition != "" {
					body.Enforce.Conditions = []rules.AdmissionCondition{{Name: "scope", Expression: tc.condition}}
				}
				if tc.profile {
					rs.Status.Rules = []*rules.NamespaceRuleBodyNamespace{body}
				}
				cl := &typeAdmissionClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tnt, overlapNamespace("a", "a", true), rs, replicaObject(kind, "a", 4), propertyBudget("a", "one", new(intstr.FromString("75%")), nil, nil)).Build()}
				compiler, err := cache.NewCELCache()
				require.NoError(t, err)
				chain := Register(nil, nil, nil, compiler, nil).GetHandlers()
				req := scaleRequest(t, kind, "a", 4, tc.replicas)
				var response *admission.Response
				reader := webhookutils.NewRequestCachingReader(cl)
				for _, h := range chain {
					response = h.OnUpdate(cl, reader, admission.NewDecoder(scheme), testEventRecorder{})(t.Context(), req)
					if response != nil {
						break
					}
				}
				if tc.want == "" {
					require.Nil(t, response)
				} else {
					require.NotNil(t, response)
					require.Contains(t, response.Result.Message, tc.want)
				}
				require.Equal(t, tc.gets, cl.gets.Load())
				require.Equal(t, tc.lists, cl.lists.Load())
			})
		}
	}
}

func BenchmarkDisruptionBudgetProperties(b *testing.B) {
	for _, tenants := range []int{1, 8} {
		for _, count := range []int{1, 20} {
			for _, mode := range []string{"allow", "deny", "pdb", "scale", "skip", "scale-skip", "scale-unchanged", "scale-baseline"} {
				b.Run(fmt.Sprintf("tenants=%d/rules=%d/%s", tenants, count, mode), func(b *testing.B) {
					scheme := overlapScheme(b)
					var objects []client.Object
					requests := make([]admission.Request, tenants)
					for i := range tenants {
						ns := fmt.Sprintf("tenant-%d", i)
						tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: ns, UID: types.UID(ns)}}
						rs := &capsulev1beta2.RuleStatus{ObjectMeta: metav1.ObjectMeta{Name: meta.NameForManagedRuleStatus(), Namespace: ns}}
						for range count {
							rs.Status.Rules = append(rs.Status.Rules, replicaRule(rules.ActionTypeAllow, new(int64(1)), nil))
						}
						if mode == "skip" || mode == "scale-skip" || mode == "scale-baseline" {
							rs.Status.Rules = nil
						}
						pdb := propertyBudget(ns, "one", new(intstr.FromString("75%")), nil, nil)
						objects = append(objects, tnt, overlapNamespace(ns, ns, true), rs, pdb)
						replicas := int64(4)
						if mode == "deny" {
							replicas = 3
						}
						requests[i], _, _ = overlapRequest(b, replicaObject("deployment", ns, replicas), nil)
						if mode == "pdb" || mode == "scale" {
							objects = append(objects, replicaObject("deployment", ns, 4))
						}
						if mode == "pdb" {
							requests[i], _, _ = overlapRequest(b, pdb, nil)
						}
						if strings.HasPrefix(mode, "scale") {
							replicas := int64(3)
							if mode == "scale-unchanged" {
								replicas = 4
							}
							requests[i] = scaleRequest(b, "deployment", ns, 4, replicas)
						}
					}
					cl := &typeAdmissionClient{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()}
					chain := Register(nil, nil, nil, nil, nil).GetHandlers()
					if mode == "scale-baseline" {
						chain[0].(*matchingHandler).predicate = func(req admission.Request) bool {
							return matchesGenericMetadataRequest(req) || matchesBudgetPodStatus(req)
						}
					}
					decoder := admission.NewDecoder(scheme)
					run := func(req admission.Request) {
						reader := webhookutils.NewRequestCachingReader(cl)
						var response *admission.Response
						for _, h := range chain {
							handle := h.OnCreate(cl, reader, decoder, testEventRecorder{})
							if req.Operation == admissionv1.Update {
								handle = h.OnUpdate(cl, reader, decoder, testEventRecorder{})
							}
							response = handle(b.Context(), req)
							if response != nil {
								break
							}
						}
						if (response != nil) != (mode == "deny" || mode == "scale") {
							b.Fatalf("unexpected response %v", response)
						}
					}
					for _, req := range requests {
						run(req)
					}
					cl.gets.Store(0)
					cl.lists.Store(0)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; b.Loop(); i++ {
						run(requests[i%tenants])
					}
					b.ReportMetric(float64(cl.gets.Load())/float64(b.N), "GET/op")
					b.ReportMetric(float64(cl.lists.Load())/float64(b.N), "LIST/op")
				})
			}
		}
	}
}

func TestBudgetPropertiesDeepCopy(t *testing.T) {
	body := replicaRule(rules.ActionTypeAllow, new(int64(1)), new(int64(2)))
	body.Enforce.Workloads.DisruptionBudgets.UnhealthyPodEvictionPolicies = []policyv1.UnhealthyPodEvictionPolicyType{policyv1.AlwaysAllow}
	copy := body.DeepCopy()
	*copy.Enforce.Workloads.DisruptionBudgets.EvictableReplicas.Min = 0
	copy.Enforce.Workloads.DisruptionBudgets.UnhealthyPodEvictionPolicies[0] = policyv1.IfHealthyBudget
	require.Equal(t, int64(1), *body.Enforce.Workloads.DisruptionBudgets.EvictableReplicas.Min)
	require.Equal(t, policyv1.AlwaysAllow, body.Enforce.Workloads.DisruptionBudgets.UnhealthyPodEvictionPolicies[0])
	data, err := json.Marshal(body)
	require.NoError(t, err)
	var decoded rules.NamespaceRuleBodyNamespace
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, body, &decoded)
}

func TestUnhealthyBudgetWholeTargets(t *testing.T) {
	for _, target := range []rules.WorkloadValidationTarget{rules.ValidatePod, rules.ValidateDeployment, rules.ValidateStatefulSet, rules.ValidateReplicaSet, rules.ValidateReplicationController, rules.ValidateDaemonSet, rules.ValidateJob, rules.ValidateCronJob} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverse=%v", target, reverse), func(t *testing.T) {
				body := overlapRule(rules.ActionTypeAllow, true, target)
				body.Enforce.Workloads.DisruptionBudgets.AllowOverlap = nil
				body.Enforce.Workloads.DisruptionBudgets.UnhealthyPodEvictionPolicies = []policyv1.UnhealthyPodEvictionPolicyType{policyv1.AlwaysAllow}
				object := overlapObject(string(target), "web", "a", map[string]string{"app": "web"})
				pdb := propertyBudget("a", "one", new(intstr.FromInt32(0)), nil, nil)
				var request, stored client.Object = object, pdb
				if reverse {
					request, stored = pdb, object
				}
				scheme := overlapScheme(t)
				cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(stored).Build()
				h := &disruptionBudgetRules{selectors: cache.NewLabelSelectorCache()}
				req, obj, old := overlapRequest(t, request, nil)
				response := h.validate(cl, old, obj, admission.NewDecoder(scheme), testEventRecorder{}, &capsulev1beta2.Tenant{}, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
				require.NotNil(t, response)
				require.Contains(t, response.Result.Message, "IfHealthyBudget")
			})
		}
	}
}

func TestBudgetPDBChecksEveryController(t *testing.T) {
	scheme := overlapScheme(t)
	first := replicaObject("deployment", "a", 4)
	first.SetName("first-compliant")
	second := replicaObject("deployment", "a", 3)
	second.SetName("second-noncompliant")
	pdb := propertyBudget("a", "one", new(intstr.FromString("75%")), nil, nil)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(first, second).Build()
	body := replicaRule(rules.ActionTypeAllow, new(int64(1)), nil)
	req, obj, old := overlapRequest(t, pdb, nil)
	h := &disruptionBudgetRules{selectors: cache.NewLabelSelectorCache()}
	response := h.validate(cl, old, obj, admission.NewDecoder(scheme), testEventRecorder{}, &capsulev1beta2.Tenant{}, []*rules.NamespaceRuleBodyNamespace{body})(t.Context(), req)
	require.NotNil(t, response)
	require.Contains(t, response.Result.Message, "second-noncompliant")
}

func TestBudgetScaleParentFailures(t *testing.T) {
	for _, mode := range []string{"missing", "recreated", "reader failure"} {
		t.Run(mode, func(t *testing.T) {
			scheme := overlapScheme(t)
			parent := replicaObject("deployment", "a", 4)
			parent.SetUID("new")
			cl := fake.NewClientBuilder().WithScheme(scheme).Build()
			if mode != "missing" {
				require.NoError(t, cl.Create(t.Context(), parent))
			}
			var reader client.Reader = cl
			if mode == "reader failure" {
				reader = &budgetGetFailure{Reader: cl}
			}
			scale := &unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"uid": "old"}, "spec": map[string]any{"replicas": int64(3)}}}
			_, _, err := budgetScaledWorkload(t.Context(), reader, scaleRequest(t, "deployment", "a", 4, 3), scale)
			require.Error(t, err)
			if mode == "recreated" {
				require.ErrorContains(t, err, "UID changed")
			} else {
				require.ErrorContains(t, err, "read scale parent")
			}
		})
	}
}

type budgetGetFailure struct{ client.Reader }

func (r *budgetGetFailure) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return fmt.Errorf("injected GET failure")
}
