// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/runtime/workloads"
)

const disruptionBudgetPageSize = 500

type disruptionBudgetRules struct {
	selectors *cache.LabelSelectorCache
	compiler  ruleengine.ConditionCompiler
}

type selectedBudget struct {
	name     string
	selector labels.Selector
	spec     policyv1.PodDisruptionBudgetSpec
}

type budgetWorkload struct {
	gvk      schema.GroupVersionKind
	name     string
	labels   map[string]string
	replicas *int64
}

func (h *disruptionBudgetRules) OnCreate(_ client.Client, reader client.Reader, obj genericObject, decoder admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.validate(reader, nil, obj, decoder, recorder, tnt, bodies)
}

func (h *disruptionBudgetRules) OnUpdate(_ client.Client, reader client.Reader, old, obj genericObject, decoder admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.validate(reader, old, obj, decoder, recorder, tnt, bodies)
}

func (*disruptionBudgetRules) OnDelete(client.Client, client.Reader, genericObject, admission.Decoder, events.EventRecorder, *capsulev1beta2.Tenant, []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { return nil }
}

func isDisruptionBudget(gvk schema.GroupVersionKind) bool {
	return gvk.Group == policyv1.GroupName && gvk.Kind == "PodDisruptionBudget" && gvk.Version == "v1"
}

// Pod status writes can change labels. Compare only labels before resolving any
// tenant/ruleset so ordinary high-volume kubelet status updates perform no reads.
func matchesBudgetPodStatus(req admission.Request) bool {
	if !isBudgetPodStatus(req) {
		return false
	}

	type podLabels struct {
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
	}

	var obj, old podLabels
	if json.Unmarshal(req.Object.Raw, &obj) != nil || json.Unmarshal(req.OldObject.Raw, &old) != nil {
		// Let the normal decoder report malformed admission objects.
		return true
	}

	return !maps.Equal(obj.Metadata.Labels, old.Metadata.Labels)
}

func isBudgetPodStatus(req admission.Request) bool {
	return req.SubResource == "status" && req.Kind.Group == "" && req.Kind.Kind == "Pod" && req.Operation == admissionv1.Update
}

func budgetTargets(w rules.NamespaceRuleEnforceWorkloadsBody, gvk schema.GroupVersionKind) bool {
	target, ok := rules.WorkloadTargetForGVK(gvk)
	if !ok {
		return false
	}

	if len(w.Targets) == 0 {
		return target == rules.ValidatePod
	}
	// Container/volume part targets cannot select Pod metadata.
	return slices.Contains(w.Targets, target)
}

func budgetPolicy(body *rules.NamespaceRuleEnforceBody) *bool {
	if body == nil || body.Workloads.DisruptionBudgets == nil {
		return nil
	}

	return body.Workloads.DisruptionBudgets.AllowOverlap
}

func budgetRuleApplies(body *rules.NamespaceRuleEnforceBody, gvk schema.GroupVersionKind) bool {
	if body == nil || body.Workloads.DisruptionBudgets == nil {
		return false
	}

	if !isDisruptionBudget(gvk) {
		return budgetTargets(body.Workloads, gvk) && budgetConstraintApplies(body.Workloads.DisruptionBudgets, gvk)
	}

	if len(body.Workloads.Targets) == 0 {
		return true
	}

	for _, target := range body.Workloads.Targets {
		groupKind, ok := target.GroupKind()
		if ok && budgetTargets(body.Workloads, groupKind.WithVersion("v1")) && budgetConstraintApplies(body.Workloads.DisruptionBudgets, groupKind.WithVersion("v1")) {
			return true
		}
	}

	return false
}

func (h *disruptionBudgetRules) validate(reader client.Reader, old, obj genericObject, decoder admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	if !slices.ContainsFunc(bodies, func(body *rules.NamespaceRuleBodyNamespace) bool {
		return body != nil && hasBudgetConstraints(body.Enforce)
	}) {
		return func(context.Context, admission.Request) *admission.Response { return nil }
	}

	return func(ctx context.Context, req admission.Request) *admission.Response {
		gvk := budgetRequestGVK(req)

		if obj == nil || req.Namespace == "" || (req.SubResource != "" && !isBudgetPodStatus(req) && !isBudgetScale(req)) {
			return nil
		}

		if !isDisruptionBudget(gvk) {
			if _, ok := rules.WorkloadTargetForGVK(gvk); !ok {
				return nil
			}
		}
		// No decoding, expression evaluation or API reads without an applicable constraint.
		if !slices.ContainsFunc(bodies, func(body *rules.NamespaceRuleBodyNamespace) bool {
			return body != nil && budgetRuleApplies(body.Enforce, gvk) && hasBudgetConstraints(body.Enforce)
		}) {
			return nil
		}

		enforce := ruleengine.EnforceBodiesFromNamespaceRules(bodies)
		if isBudgetScale(req) && !hasBudgetReplicaConstraints(enforce, gvk) {
			return nil
		}

		enforce, err := ruleengine.FilterEnforcementConditions(ctx, ruleengine.NewConditionEvaluator(h.compiler, req.AdmissionRequest), nil, enforce,
			func(body *rules.NamespaceRuleEnforceBody) bool { return budgetRuleApplies(body, gvk) })
		if err != nil {
			return ad.Deny(fmt.Sprintf("disruption budgets: %v", err))
		}

		if !slices.ContainsFunc(enforce, func(body *rules.NamespaceRuleEnforceBody) bool {
			return budgetRuleApplies(body, gvk) && hasBudgetConstraints(body)
		}) {
			return nil
		}

		if isBudgetScale(req) && !hasBudgetReplicaConstraints(enforce, gvk) {
			return nil
		}

		audited := map[string]bool{}
		report := func(workload budgetWorkload, names []string, spec *policyv1.PodDisruptionBudgetSpec) error {
			evaluation, err := evaluateBudgetPolicies(workload, names, spec, enforce)
			if err != nil {
				return err
			}

			for _, audit := range evaluation.Audits {
				key := workload.gvk.Kind + "/" + audit.SetName + "/" + audit.MatchedRule
				if audited[key] {
					continue
				}

				audited[key] = true

				recorder.LabeledEvent(obj, corev1.EventTypeNormal, events.ReasonNamespaceRuleAudit, events.ActionRuleAudit, audit.Message).
					WithRelated(tnt).WithTenantLabel(tnt).WithRequestAnnotations(req).Emit(ctx)
			}

			if err := evaluation.BlockingError(); err != nil {
				recorder.LabeledEvent(obj, corev1.EventTypeWarning, events.ReasonForbiddenDisruptionBudget, events.ActionValidationDenied, err.Error()).
					WithRelated(tnt).WithTenantLabel(tnt).WithRequestAnnotations(req).Emit(ctx)

				return err
			}

			return nil
		}

		if isDisruptionBudget(gvk) {
			err = h.checkBudget(ctx, reader, decoder, req, enforce, report)
		} else {
			err = h.checkWorkload(ctx, reader, decoder, req, old, obj, enforce, report)
		}

		if err != nil {
			return ad.Deny(fmt.Sprintf("disruption budgets: %v", err))
		}

		return nil
	}
}

// Lists use the authoritative reader and namespace scope. A cached reverse index
// can omit a newly committed PDB/workload; re-reading candidates cannot repair that
// unsafe allow. Lists are paginated; no cluster-wide scans or per-item GETs occur.
func (h *disruptionBudgetRules) readBudgets(ctx context.Context, reader client.Reader, namespace, exclude string) ([]selectedBudget, error) {
	var out []selectedBudget

	opts := &client.ListOptions{Namespace: namespace, Limit: disruptionBudgetPageSize}

	for {
		list := &policyv1.PodDisruptionBudgetList{}
		if err := reader.List(ctx, list, opts); err != nil {
			return nil, fmt.Errorf("list PDBs: %w", err)
		}

		for i := range list.Items {
			budget := &list.Items[i]
			if budget.Name == exclude {
				continue
			}

			selector, err := h.selectors.GetOrCompile(budget.Spec.Selector)
			if err != nil {
				return nil, fmt.Errorf("PDB %s selector: %w", budget.Name, err)
			}

			out = append(out, selectedBudget{name: budget.Name, selector: selector, spec: budget.Spec})
		}

		if list.Continue == "" {
			return out, nil
		}

		opts.Continue = list.Continue
	}
}

type budgetReporter func(budgetWorkload, []string, *policyv1.PodDisruptionBudgetSpec) error

func (h *disruptionBudgetRules) checkWorkload(ctx context.Context, reader client.Reader, decoder admission.Decoder, req admission.Request, old, obj genericObject, bodies []*rules.NamespaceRuleEnforceBody, report budgetReporter) error {
	value, changed, err := budgetRequestWorkload(ctx, reader, decoder, req, old, obj, bodies)
	if err != nil || !changed {
		return err
	}

	budgets, err := h.readBudgets(ctx, reader, req.Namespace, "")
	if err != nil {
		return err
	}

	if names := matchingBudgets(budgets, value.labels); len(names) > 1 {
		if err := report(value, names, nil); err != nil {
			return err
		}
	}

	if !hasBudgetPropertiesForKind(bodies, value.gvk) {
		return nil
	}

	for _, budget := range budgets {
		if budget.selector.Matches(labels.Set(value.labels)) {
			if err := report(value, []string{budget.name}, &budget.spec); err != nil {
				return err
			}
		}
	}

	return nil
}

func budgetTemplateLabels(obj *unstructured.Unstructured) (map[string]string, error) {
	path := workloads.PodTemplatePath(obj.GroupVersionKind())
	if len(path) == 0 {
		return nil, fmt.Errorf("unsupported workload %s", obj.GroupVersionKind())
	}

	value, _, err := unstructured.NestedStringMap(obj.Object, append(path, "metadata", "labels")...)

	return value, err
}

func matchingBudgets(budgets []selectedBudget, podLabels map[string]string) []string {
	var names []string

	for _, budget := range budgets {
		if budget.selector.Matches(labels.Set(podLabels)) {
			names = append(names, budget.name)
			// Two names prove overlap and keep diagnostics bounded.
			if len(names) == 2 {
				break
			}
		}
	}

	slices.Sort(names)

	return names
}

func (h *disruptionBudgetRules) checkBudget(ctx context.Context, reader client.Reader, decoder admission.Decoder, req admission.Request, bodies []*rules.NamespaceRuleEnforceBody, report budgetReporter) error {
	obj := &policyv1.PodDisruptionBudget{}
	if err := decoder.Decode(req, obj); err != nil {
		return err
	}

	selectorChanged := true

	if req.Operation == admissionv1.Update {
		old := &policyv1.PodDisruptionBudget{}
		if err := decoder.DecodeRaw(req.OldObject, old); err != nil {
			return err
		}

		selectorChanged = !apiequality.Semantic.DeepEqual(old.Spec.Selector, obj.Spec.Selector)

		propertiesChanged := !apiequality.Semantic.DeepEqual(old.Spec, obj.Spec)

		if !selectorChanged && (!propertiesChanged || !hasBudgetProperties(bodies)) {
			return nil
		}
	}

	// In policy/v1 a null selector selects no Pods; an empty selector selects all.
	if obj.Spec.Selector == nil {
		return nil
	}

	selector, err := h.selectors.GetOrCompile(obj.Spec.Selector)
	if err != nil {
		return err
	}

	var budgets []selectedBudget
	if selectorChanged && hasOverlapConstraints(bodies) {
		budgets, err = h.readBudgets(ctx, reader, req.Namespace, obj.Name)
		if err != nil {
			return err
		}
	}

	if len(budgets) == 0 && !hasBudgetProperties(bodies) {
		return nil
	}

	check := func(value budgetWorkload) (bool, error) {
		if !selector.Matches(labels.Set(value.labels)) {
			return false, nil
		}

		names := matchingBudgets(budgets, value.labels)
		if len(names) > 0 {
			names = append(names, obj.Name)
			slices.Sort(names)

			if err := report(value, names, nil); err != nil {
				return true, err
			}
		}

		if hasBudgetPropertiesForKind(bodies, value.gvk) {
			// Replica counts differ between templates, so examine every matching
			// controller even after an allowed or audited overlap.
			return !hasBudgetReplicaConstraints(bodies, value.gvk), report(value, []string{obj.Name}, &obj.Spec)
		}

		return len(names) > 1, nil
	}
	// Stable order keeps diagnostics deterministic. Only whole kinds constrained by
	// an applicable rule are read, including zero-replica controller templates.
	for _, target := range []rules.WorkloadValidationTarget{rules.ValidatePod, rules.ValidateDeployment, rules.ValidateStatefulSet, rules.ValidateDaemonSet, rules.ValidateReplicaSet, rules.ValidateReplicationController, rules.ValidateJob, rules.ValidateCronJob} {
		gk, _ := target.GroupKind()
		gvk := gk.WithVersion("v1")

		if !slices.ContainsFunc(bodies, func(body *rules.NamespaceRuleEnforceBody) bool {
			return budgetRuleApplies(body, gvk) && ((len(budgets) > 0 && budgetPolicy(body) != nil && !*budgetPolicy(body)) || hasBudgetPropertiesForKind([]*rules.NamespaceRuleEnforceBody{body}, gvk))
		}) {
			continue
		}

		if err := visitBudgetWorkloads(ctx, reader, req.Namespace, gvk, selector, check); err != nil {
			return err
		}
	}

	return nil
}

func visitBudgetWorkloads(ctx context.Context, reader client.Reader, namespace string, gvk schema.GroupVersionKind, selector labels.Selector, visit func(budgetWorkload) (bool, error)) error {
	opts := &client.ListOptions{Namespace: namespace, Limit: disruptionBudgetPageSize}
	if gvk.Kind == "Pod" {
		opts.LabelSelector = selector
	}

	for {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk.GroupVersion().WithKind(gvk.Kind + "List"))

		if err := reader.List(ctx, list, opts); err != nil {
			return fmt.Errorf("list %s: %w", gvk.Kind, err)
		}

		for i := range list.Items {
			item := &list.Items[i]
			item.SetGroupVersionKind(gvk)

			value := budgetWorkload{gvk: gvk, name: item.GetName(), labels: item.GetLabels()}

			if gvk.Kind != "Pod" {
				var err error

				value, err = budgetTemplateWorkload(item)
				if err != nil {
					return err
				}
			}

			if done, err := visit(value); done || err != nil {
				return err
			}
		}

		if list.GetContinue() == "" {
			return nil
		}

		opts.Continue = list.GetContinue()
	}
}

type budgetConstraint struct {
	allow  bool
	action rules.ActionType
}

func evaluateBudgetOverlap(workload budgetWorkload, names []string, bodies []*rules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error) {
	// Like resource constraints, allow matches compliance, while deny and audit
	// match violations. Reuse rule ordering, allow-list misses and audit handling.
	return ruleengine.EvaluateEnforce(workload, bodies, ruleengine.Set[budgetConstraint, budgetWorkload]{
		Name: "PDB overlap", EventReason: events.ReasonForbiddenDisruptionBudget,
		Values: func(budgetWorkload) []ruleengine.Value {
			return []ruleengine.Value{{Value: strings.Join(names, ", "), Path: workload.gvk.Kind + "/" + workload.name}}
		},
		Rules: func(body *rules.NamespaceRuleEnforceBody) []budgetConstraint {
			if budgetPolicy(body) == nil || !budgetTargets(body.Workloads, workload.gvk) {
				return nil
			}

			return []budgetConstraint{{allow: *budgetPolicy(body), action: body.Action.OrDefault()}}
		},
		Matches: func(constraint budgetConstraint, _ ruleengine.Value) (ruleengine.Match, error) {
			matched := constraint.allow
			if constraint.action != rules.ActionTypeAllow {
				matched = !matched
			}

			return ruleengine.Match{Matched: matched}, nil
		},
		RuleDescription: func(constraint budgetConstraint) string { return fmt.Sprintf("allowOverlap: %t", constraint.allow) },
		Message: func(action rules.ActionType, value ruleengine.Value, _ any) string {
			return fmt.Sprintf("overlapping PDBs [%s] select %s (%s namespace rule)", value.Value, value.Path, action)
		},
	})
}
