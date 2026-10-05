// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func overlapRule(action rules.ActionType, allow bool, targets ...rules.WorkloadValidationTarget) *rules.NamespaceRuleBodyNamespace {
	return &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: action, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: targets, DisruptionBudgets: &rules.WorkloadDisruptionBudgetRules{AllowOverlap: new(allow)}}}}
}

func overlapPDB(namespace, name string, selector *metav1.LabelSelector) *policyv1.PodDisruptionBudget {
	return &policyv1.PodDisruptionBudget{TypeMeta: metav1.TypeMeta{APIVersion: "policy/v1", Kind: "PodDisruptionBudget"}, ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}, Spec: policyv1.PodDisruptionBudgetSpec{Selector: selector}}
}

func overlapObject(kind, name, namespace string, labels map[string]string) *unstructured.Unstructured {
	target := rules.WorkloadValidationTarget(kind)
	gk, ok := target.GroupKind()
	if !ok {
		panic(kind)
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gk.WithVersion("v1"))
	obj.SetName(name)
	obj.SetNamespace(namespace)
	path := []string{"metadata", "labels"}
	if kind != "pod" {
		path = []string{"spec", "template", "metadata", "labels"}
	}
	if kind == "cronjob" {
		path = []string{"spec", "jobTemplate", "spec", "template", "metadata", "labels"}
	}
	if err := unstructured.SetNestedStringMap(obj.Object, labels, path...); err != nil {
		panic(err)
	}
	return obj
}

func overlapScheme(t testing.TB) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, appsv1.AddToScheme, batchv1.AddToScheme, policyv1.AddToScheme, capsulev1beta2.AddToScheme} {
		require.NoError(t, add(s))
	}
	return s
}

type overlapReader struct {
	client.Reader
	lists int
	fail  bool
}

func (r *overlapReader) List(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
	r.lists++
	options := (&client.ListOptions{}).ApplyOptions(opts)
	if options.Namespace == "" {
		return errors.New("unscoped list")
	}
	if options.Limit != disruptionBudgetPageSize {
		return errors.New("unpaginated list")
	}
	if r.fail {
		return errors.New("injected list failure")
	}
	return r.Reader.List(ctx, obj, opts...)
}

func overlapRequest(t testing.TB, obj client.Object, old client.Object) (admission.Request, genericObject, genericObject) {
	t.Helper()
	raw, err := json.Marshal(obj)
	require.NoError(t, err)
	gvk := obj.GetObjectKind().GroupVersionKind()
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Namespace: obj.GetNamespace(), Name: obj.GetName(), Kind: metav1.GroupVersionKind{Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind}, Operation: admissionv1.Create, Object: runtime.RawExtension{Raw: raw}}}
	meta := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: obj.GetName(), Namespace: obj.GetNamespace(), Labels: obj.GetLabels()}}
	var oldMeta genericObject
	if old != nil {
		req.Operation = admissionv1.Update
		req.OldObject.Raw, err = json.Marshal(old)
		require.NoError(t, err)
		oldMeta = &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: old.GetName(), Namespace: old.GetNamespace(), Labels: old.GetLabels()}}
	}
	return req, meta, oldMeta
}

func TestDisruptionBudgetAdmission(t *testing.T) {
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}
	base := []client.Object{overlapPDB("a", "first", selector), overlapPDB("a", "second", &metav1.LabelSelector{})}
	for _, tc := range []struct {
		name        string
		object, old client.Object
		bodies      []*rules.NamespaceRuleBodyNamespace
		objects     []client.Object
		want        string
		lists       int
	}{
		{name: "pod overlap", object: overlapObject("pod", "web", "a", map[string]string{"app": "web"}), bodies: []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false)}, want: "PDB overlap", lists: 1},
		{name: "single match", object: overlapObject("pod", "worker", "a", map[string]string{"app": "worker"}), bodies: []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false)}, lists: 1},
		{name: "no match", object: overlapObject("pod", "web", "b", map[string]string{"app": "web"}), bodies: []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false)}, lists: 1},
		{name: "unselected kind", object: overlapObject("deployment", "web", "a", map[string]string{"app": "web"}), bodies: []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false)}, lists: 0},
		{name: "container part ignored", object: overlapObject("pod", "web", "a", map[string]string{"app": "web"}), bodies: []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false, rules.ValidateContainers)}, lists: 0},
		{name: "unchanged labels", object: overlapObject("pod", "web", "a", map[string]string{"app": "web"}), old: overlapObject("pod", "web", "a", map[string]string{"app": "web"}), bodies: []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false)}, lists: 0},
		{name: "label update", object: overlapObject("pod", "web", "a", map[string]string{"app": "web"}), old: overlapObject("pod", "web", "a", map[string]string{"app": "worker"}), bodies: []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false)}, want: "PDB overlap", lists: 1},
		{name: "label repair", object: overlapObject("pod", "web", "a", map[string]string{"app": "worker"}), old: overlapObject("pod", "web", "a", map[string]string{"app": "web"}), bodies: []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false)}, lists: 1},
		{name: "explicit true", object: overlapObject("pod", "web", "a", map[string]string{"app": "web"}), bodies: []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, true)}, lists: 0},
		{name: "no rules", object: overlapObject("pod", "web", "a", map[string]string{"app": "web"}), lists: 0},
		{name: "PDB update replaces itself", object: overlapPDB("b", "existing", selector), old: overlapPDB("b", "existing", nil), objects: []client.Object{overlapPDB("b", "existing", nil), overlapObject("pod", "web", "b", map[string]string{"app": "web"})}, bodies: []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false)}, lists: 1},
		{name: "PDB overlap on pod", object: overlapPDB("a", "third", selector), objects: []client.Object{overlapObject("pod", "web", "a", map[string]string{"app": "web"})}, bodies: []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false)}, want: "PDB overlap", lists: 2},
		{name: "PDB overlap on zero template", object: overlapPDB("a", "third", selector), objects: []client.Object{overlapObject("deployment", "web", "a", map[string]string{"app": "web"})}, bodies: []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false, rules.ValidateDeployment)}, want: "Deployment/web", lists: 2},
		{name: "PDB unselected template", object: overlapPDB("a", "third", selector), objects: []client.Object{overlapObject("deployment", "web", "a", map[string]string{"app": "web"})}, bodies: []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false)}, lists: 2},
		{name: "PDB before workloads", object: overlapPDB("a", "third", selector), bodies: []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false)}, lists: 2},
		{name: "PDB null", object: overlapPDB("a", "third", nil), bodies: []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false)}, lists: 0},
		{name: "PDB unchanged selector", object: overlapPDB("a", "first", selector), old: overlapPDB("a", "first", selector), bodies: []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false)}, lists: 0},
		{name: "PDB unrelated overlap", object: overlapPDB("a", "third", &metav1.LabelSelector{MatchLabels: map[string]string{"app": "other"}}), objects: []client.Object{overlapObject("pod", "web", "a", map[string]string{"app": "web"})}, bodies: []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false)}, lists: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scheme := overlapScheme(t)
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(append(slicesCloneObjects(base), tc.objects...)...).Build()
			reader := &overlapReader{Reader: cl}
			compiler, err := cache.NewCELCache()
			require.NoError(t, err)
			h := &disruptionBudgetRules{selectors: cache.NewLabelSelectorCache(), compiler: compiler}
			req, obj, old := overlapRequest(t, tc.object, tc.old)
			before := make([]*rules.NamespaceRuleBodyNamespace, len(tc.bodies))
			for i, b := range tc.bodies {
				before[i] = b.DeepCopy()
			}
			response := h.validate(reader, old, obj, admission.NewDecoder(scheme), testEventRecorder{}, &capsulev1beta2.Tenant{}, tc.bodies)(t.Context(), req)
			if tc.want != "" {
				require.NotNil(t, response)
				require.False(t, response.Allowed)
				require.Contains(t, response.Result.Message, tc.want)
			} else {
				require.Nil(t, response)
			}
			require.Equal(t, tc.lists, reader.lists)
			if len(tc.bodies) > 0 {
				require.Equal(t, before, tc.bodies)
			}
		})
	}
}

func slicesCloneObjects(objects []client.Object) []client.Object {
	out := make([]client.Object, len(objects))
	for i, obj := range objects {
		out[i] = obj.DeepCopyObject().(client.Object)
	}
	return out
}

func TestDisruptionBudgetTemplateKinds(t *testing.T) {
	for _, kind := range []rules.WorkloadValidationTarget{rules.ValidateDeployment, rules.ValidateStatefulSet, rules.ValidateDaemonSet, rules.ValidateReplicaSet, rules.ValidateReplicationController, rules.ValidateJob, rules.ValidateCronJob} {
		t.Run(string(kind), func(t *testing.T) {
			scheme := overlapScheme(t)
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(overlapPDB("a", "one", &metav1.LabelSelector{}), overlapPDB("a", "two", &metav1.LabelSelector{})).Build()
			h := &disruptionBudgetRules{selectors: cache.NewLabelSelectorCache()}
			body := []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false, kind)}
			object := overlapObject(string(kind), "app", "a", map[string]string{"app": "web"})
			req, obj, _ := overlapRequest(t, object, nil)
			resp := h.OnCreate(cl, cl, obj, admission.NewDecoder(scheme), testEventRecorder{}, &capsulev1beta2.Tenant{}, body)(t.Context(), req)
			require.NotNil(t, resp)
			require.Contains(t, resp.Result.Message, "one, two")
			old := object.DeepCopy()
			require.NoError(t, unstructured.SetNestedField(old.Object, int64(0), "spec", "replicas"))
			req, obj, oldMeta := overlapRequest(t, object, old)
			reader := &overlapReader{Reader: cl, fail: true}
			require.Nil(t, h.OnUpdate(cl, reader, oldMeta, obj, admission.NewDecoder(scheme), nil, nil, body)(t.Context(), req))
			require.Zero(t, reader.lists)
		})
	}
}

func TestDisruptionBudgetActions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		bodies []*rules.NamespaceRuleBodyNamespace
		deny   bool
		audits int
	}{
		{"allow rejects", []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, false)}, true, 0},
		{"deny rejects", []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeDeny, false)}, true, 0},
		{"audit observes", []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAudit, false)}, false, 1},
		{"allow overrides deny", []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeDeny, false), overlapRule(rules.ActionTypeAllow, true)}, false, 0},
		{"deny overrides allow", []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeAllow, true), overlapRule(rules.ActionTypeDeny, false)}, true, 0},
		{"audit never overrides", []*rules.NamespaceRuleBodyNamespace{overlapRule(rules.ActionTypeDeny, false), overlapRule(rules.ActionTypeAudit, false)}, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var bodies []*rules.NamespaceRuleEnforceBody
			for _, b := range tc.bodies {
				bodies = append(bodies, b.Enforce)
			}
			ev, err := evaluateBudgetOverlap(budgetWorkload{gvk: schema.GroupVersionKind{Version: "v1", Kind: "Pod"}, name: "app"}, []string{"one", "two"}, bodies)
			require.NoError(t, err)
			require.Equal(t, tc.deny, ev.BlockingError() != nil)
			require.Len(t, ev.Audits, tc.audits)
		})
	}
}

func TestDisruptionBudgetConditionsAndErrors(t *testing.T) {
	scheme := overlapScheme(t)
	compiler, err := cache.NewCELCache()
	require.NoError(t, err)
	h := &disruptionBudgetRules{selectors: cache.NewLabelSelectorCache(), compiler: compiler}
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	reader := &overlapReader{Reader: cl, fail: true}
	req, obj, _ := overlapRequest(t, overlapObject("pod", "app", "a", nil), nil)
	rule := overlapRule(rules.ActionTypeAllow, false)
	rule.Enforce.Conditions = []rules.AdmissionCondition{{Expression: "false"}}
	body := []*rules.NamespaceRuleBodyNamespace{rule}
	require.Nil(t, h.OnCreate(cl, reader, obj, admission.NewDecoder(scheme), nil, nil, body)(t.Context(), req))
	require.Zero(t, reader.lists)
	rule.Enforce.Conditions = []rules.AdmissionCondition{{Expression: "request.operation == 'CREATE'"}}
	resp := h.OnCreate(cl, reader, obj, admission.NewDecoder(scheme), nil, nil, body)(t.Context(), req)
	require.NotNil(t, resp)
	require.Contains(t, resp.Result.Message, "injected list failure")
	rule.Enforce.Conditions = []rules.AdmissionCondition{{Expression: "object.spec.missing == 'x'"}}
	resp = h.OnCreate(cl, reader, obj, admission.NewDecoder(scheme), nil, nil, body)(t.Context(), req)
	require.NotNil(t, resp)
	require.Contains(t, resp.Result.Message, "condition")
	require.Equal(t, 1, reader.lists)
	req.SubResource = "resize"
	require.Nil(t, h.OnCreate(cl, reader, obj, nil, nil, nil, body)(t.Context(), req))
	require.Nil(t, h.OnDelete(cl, reader, obj, nil, nil, nil, body)(t.Context(), req))
	require.Equal(t, 1, reader.lists)
}

func TestDisruptionBudgetEmptyBodyIsNotKindPolicy(t *testing.T) {
	body := rules.NamespaceRuleEnforceWorkloadsBody{Targets: []rules.WorkloadValidationTarget{rules.ValidateDeployment}, DisruptionBudgets: &rules.WorkloadDisruptionBudgetRules{}}
	require.True(t, body.HasPolicies())
	require.False(t, body.TargetsOnly())
	require.False(t, body.HasPodSpecPolicies())
	require.False(t, matchesTemplatePolicies(requestWithKind("apps", "Deployment"), []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: body}}}))
	for _, policy := range []struct {
		name string
		body rules.NamespaceRuleEnforceWorkloadsBody
	}{
		{"placement", rules.NamespaceRuleEnforceWorkloadsBody{Placement: rules.WorkloadPlacementEnforcement{NodeSelector: []rules.WorkloadNodeSelectorMatch{{}}}}},
		{"security", rules.NamespaceRuleEnforceWorkloadsBody{Security: rules.WorkloadSecurityEnforcement{SeccompProfiles: []rules.WorkloadSecurityProfileMatch{{Types: []rules.SecurityProfileType{rules.SecurityProfileRuntimeDefault}}}}}},
	} {
		t.Run(policy.name, func(t *testing.T) {
			policy.body.Targets = body.Targets
			policy.body.DisruptionBudgets = body.DisruptionBudgets
			require.True(t, policy.body.HasPodSpecPolicies())
			require.False(t, policy.body.TargetsOnly())
			bodies := []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: policy.body}}}
			require.True(t, matchesTemplatePolicies(requestWithKind("apps", "Deployment"), bodies))
			require.False(t, matchesTemplatePolicies(requestWithKind("apps", "StatefulSet"), bodies))
		})
	}
	raw, err := json.Marshal(overlapRule(rules.ActionTypeAllow, false))
	require.NoError(t, err)
	require.Contains(t, string(raw), `"allowOverlap":false`)
}

type budgetListReader struct {
	client.Reader
	list func(client.ObjectList, *client.ListOptions) error
}

func (r budgetListReader) List(_ context.Context, obj client.ObjectList, opts ...client.ListOption) error {
	return r.list(obj, (&client.ListOptions{}).ApplyOptions(opts))
}

func TestDisruptionBudgetPaginationAndReadErrors(t *testing.T) {
	h := &disruptionBudgetRules{selectors: cache.NewLabelSelectorCache()}
	calls := 0
	reader := budgetListReader{list: func(obj client.ObjectList, opts *client.ListOptions) error {
		require.Equal(t, "a", opts.Namespace)
		require.Equal(t, int64(disruptionBudgetPageSize), opts.Limit)
		list := obj.(*policyv1.PodDisruptionBudgetList)
		calls++
		if opts.Continue == "" {
			list.Items = []policyv1.PodDisruptionBudget{*overlapPDB("a", "one", &metav1.LabelSelector{})}
			list.Continue = "next"
		} else {
			require.Equal(t, "next", opts.Continue)
			list.Items = []policyv1.PodDisruptionBudget{*overlapPDB("a", "two", &metav1.LabelSelector{})}
		}
		return nil
	}}
	budgets, err := h.readBudgets(t.Context(), reader, "a", "")
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Equal(t, []string{"one", "two"}, matchingBudgets(budgets, nil))
	for _, kind := range []string{"Pod", "Deployment"} {
		t.Run(kind, func(t *testing.T) {
			calls, visited := 0, 0
			gvk := schema.GroupVersionKind{Version: "v1", Kind: kind}
			if kind == "Deployment" {
				gvk.Group = "apps"
			}
			reader := budgetListReader{list: func(obj client.ObjectList, opts *client.ListOptions) error {
				calls++
				require.Equal(t, "a", opts.Namespace)
				if kind == "Pod" {
					require.NotNil(t, opts.LabelSelector)
				} else {
					require.Nil(t, opts.LabelSelector, "controller metadata is not its template metadata")
				}
				list := obj.(*unstructured.UnstructuredList)
				if opts.Continue == "" {
					list.SetContinue("next")
					return nil
				}
				require.Equal(t, "next", opts.Continue)
				resource := "pod"
				if kind == "Deployment" {
					resource = "deployment"
				}
				list.Items = []unstructured.Unstructured{*overlapObject(resource, "web", "a", map[string]string{"app": "web"})}
				return nil
			}}
			selector, err := h.selectors.GetOrCompile(&metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}})
			require.NoError(t, err)
			err = visitBudgetWorkloads(t.Context(), reader, "a", gvk, selector, func(value budgetWorkload) (bool, error) {
				visited++
				require.Equal(t, map[string]string{"app": "web"}, value.labels)
				return false, nil
			})
			require.NoError(t, err)
			require.Equal(t, 2, calls)
			require.Equal(t, 1, visited)
		})
	}
	reader.list = func(obj client.ObjectList, opts *client.ListOptions) error {
		if opts.Continue != "" {
			return errors.New("second page failed")
		}
		obj.(*policyv1.PodDisruptionBudgetList).Continue = "next"
		return nil
	}
	_, err = h.readBudgets(t.Context(), reader, "a", "")
	require.ErrorContains(t, err, "second page failed")
	reader.list = func(client.ObjectList, *client.ListOptions) error { return errors.New("workload list failed") }
	sel, err := h.selectors.GetOrCompile(&metav1.LabelSelector{})
	require.NoError(t, err)
	err = visitBudgetWorkloads(t.Context(), reader, "a", schema.GroupVersionKind{Version: "v1", Kind: "Pod"}, sel, func(budgetWorkload) (bool, error) {
		t.Fatal("must not evaluate after a failed read")
		return false, nil
	})
	require.ErrorContains(t, err, "workload list failed")
}

func TestDisruptionBudgetSelectorsAndLifecycle(t *testing.T) {
	scheme := overlapScheme(t)
	h := &disruptionBudgetRules{selectors: cache.NewLabelSelectorCache()}
	for _, operator := range []metav1.LabelSelectorOperator{metav1.LabelSelectorOpIn, metav1.LabelSelectorOpNotIn, metav1.LabelSelectorOpExists, metav1.LabelSelectorOpDoesNotExist} {
		t.Run(string(operator), func(t *testing.T) {
			req := metav1.LabelSelectorRequirement{Key: "app", Operator: operator}
			value := map[string]string{"app": "web"}
			switch operator {
			case metav1.LabelSelectorOpIn:
				req.Values = []string{"web"}
			case metav1.LabelSelectorOpNotIn:
				req.Values = []string{"other"}
			case metav1.LabelSelectorOpDoesNotExist:
				value = nil
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(overlapPDB("a", "one", &metav1.LabelSelector{}), overlapPDB("a", "two", &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{req}})).Build()
			budgets, err := h.readBudgets(t.Context(), cl, "a", "")
			require.NoError(t, err)
			require.Len(t, matchingBudgets(budgets, value), 2)
			current := &policyv1.PodDisruptionBudget{}
			require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: "a", Name: "two"}, current))
			current.Spec.Selector = nil
			require.NoError(t, cl.Update(t.Context(), current))
			budgets, err = h.readBudgets(t.Context(), cl, "a", "")
			require.NoError(t, err)
			require.Equal(t, []string{"one"}, matchingBudgets(budgets, value))
			require.NoError(t, cl.Delete(t.Context(), current))
			require.NoError(t, cl.Create(t.Context(), overlapPDB("a", "two", &metav1.LabelSelector{})))
			budgets, err = h.readBudgets(t.Context(), cl, "a", "")
			require.NoError(t, err)
			require.Len(t, matchingBudgets(budgets, value), 2)
		})
	}
	bad := overlapPDB("a", "bad", &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "app", Operator: "invalid"}}})
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(bad).Build()
	_, err := h.readBudgets(t.Context(), cl, "a", "")
	require.ErrorContains(t, err, "PDB bad selector")
}

func TestDisruptionBudgetReportsOneExamplePerKind(t *testing.T) {
	scheme := overlapScheme(t)
	objects := []client.Object{overlapPDB("a", "first", &metav1.LabelSelector{})}
	for _, kind := range []string{"pod", "deployment"} {
		for _, name := range []string{"one", "two"} {
			objects = append(objects, overlapObject(kind, name, "a", nil))
		}
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	h := &disruptionBudgetRules{selectors: cache.NewLabelSelectorCache()}
	req, _, _ := overlapRequest(t, overlapPDB("a", "second", &metav1.LabelSelector{}), nil)
	body := overlapRule(rules.ActionTypeAudit, false, rules.ValidatePod, rules.ValidateDeployment)
	reports := map[string]int{}
	err := h.checkBudget(t.Context(), cl, admission.NewDecoder(scheme), req, []*rules.NamespaceRuleEnforceBody{body.Enforce}, func(value budgetWorkload, names []string, _ *policyv1.PodDisruptionBudgetSpec) error {
		reports[value.gvk.Kind]++
		require.Equal(t, []string{"first", "second"}, names)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, map[string]int{"Pod": 1, "Deployment": 1}, reports)
}
