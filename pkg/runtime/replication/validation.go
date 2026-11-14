// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package replication

import (
	"fmt"
	"slices"
	"strings"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
)

type ConditionValidator interface{ ValidateTriggerCondition(string) error }

func ValidateTriggers(spec *capsulev1beta2.TenantResourceCommonSpec, namespaced bool, compiler ConditionValidator) error {
	return validateTriggers(spec, namespaced, compiler, true)
}

// ValidateTriggerStructure checks the candidate shape without compiling CEL.
// Registration uses this before applying its serialized publication budget;
// admission and resource controllers must continue using ValidateTriggers.
func ValidateTriggerStructure(spec *capsulev1beta2.TenantResourceCommonSpec, namespaced bool) error {
	return validateTriggers(spec, namespaced, nil, false)
}

func validateTriggers(spec *capsulev1beta2.TenantResourceCommonSpec, namespaced bool, compiler ConditionValidator, compileConditions bool) error {
	if spec == nil {
		return fmt.Errorf("replication specification is required")
	}

	if d := spec.TriggerMinInterval.Duration; d != 0 && (d < time.Second || d > 24*time.Hour) {
		return fmt.Errorf("spec.triggerMinInterval must be between 1s and 24h")
	}

	if len(spec.Triggers) > 32 {
		return fmt.Errorf("spec.triggers must contain at most 32 entries")
	}

	names := make(map[string]bool, len(spec.Triggers))

	for i := range spec.Triggers {
		t := &spec.Triggers[i]

		path := fmt.Sprintf("spec.triggers[%d]", i)
		if t.Name == "" || len(validation.IsDNS1123Label(t.Name)) > 0 || names[t.Name] {
			return fmt.Errorf("%s.name must be a unique DNS label", path)
		}

		names[t.Name] = true

		if namespaced && t.NamespaceSelector != nil {
			return fmt.Errorf("%s.namespaceSelector is only supported by GlobalTenantResource", path)
		}

		if t.Scope != nil && *t.Scope != admissionv1.NamespacedScope {
			return fmt.Errorf("%s.scope must be Namespaced", path)
		}

		if err := validateTriggerRules(t, path); err != nil {
			return err
		}

		for _, selector := range []*metav1.LabelSelector{t.NamespaceSelector, t.ObjectSelector} {
			if selector != nil {
				if _, err := metav1.LabelSelectorAsSelector(selector); err != nil {
					return fmt.Errorf("%s selector: %w", path, err)
				}
			}
		}

		if err := ruleengine.ValidateAudience(path+".audience", t.Audience); err != nil {
			return err
		}

		if len(t.Audience) > 64 || len(t.MatchConditions) > 60 {
			return fmt.Errorf("%s exceeds audience or match condition limit", path)
		}

		if err := validateTriggerConditions(t, path, compiler, compileConditions); err != nil {
			return err
		}
	}

	return nil
}

func validateTriggerRules(t *capsulev1beta2.TenantResourceTrigger, path string) error {
	if len(t.Operations) == 0 || len(t.APIGroups) == 0 || len(t.APIVersions) == 0 || len(t.Resources) == 0 {
		return fmt.Errorf("%s requires operations, apiGroups, apiVersions, and resources", path)
	}

	for _, op := range t.Operations {
		if !slices.Contains([]admissionv1.OperationType{admissionv1.Create, admissionv1.Update, admissionv1.Delete, admissionv1.OperationAll}, op) {
			return fmt.Errorf("%s.operations contains unsupported operation %q", path, op)
		}
	}

	if slices.Contains(t.Operations, admissionv1.OperationAll) && len(t.Operations) > 1 {
		return fmt.Errorf("%s.operations wildcard must be the only operation", path)
	}

	for _, values := range [][]string{t.APIGroups, t.APIVersions} {
		if slices.Contains(values, "*") && len(values) > 1 {
			return fmt.Errorf("%s wildcard must be the only group or version", path)
		}
	}

	if slices.Contains(t.APIVersions, "") {
		return fmt.Errorf("%s.apiVersions must not contain empty values", path)
	}

	for _, group := range t.APIGroups {
		if group != "" && group != "*" && len(validation.IsDNS1123Subdomain(group)) > 0 {
			return fmt.Errorf("%s.apiGroups contains invalid group %q", path, group)
		}
	}

	return validateTriggerResources(t.Resources, path)
}

// Reject overlapping wildcards before publishing the shared webhook object.
// Otherwise one invalid subscription can block unrelated enforcement updates.
func validateTriggerResources(resources []string, path string) error {
	wildcardResources := make(map[string]int)
	wildcardSubresources := make(map[string]int)
	hasAllResources := slices.Contains(resources, "*")

	for i, resource := range resources {
		if resource == "" || strings.Count(resource, "/") > 1 {
			return fmt.Errorf("%s.resources contains invalid resource %q", path, resource)
		}

		for part := range strings.SplitSeq(resource, "/") {
			if part != "*" && len(validation.IsDNS1123Subdomain(part)) > 0 {
				return fmt.Errorf("%s.resources contains invalid resource %q", path, resource)
			}
		}

		res, sub, found := strings.Cut(resource, "/")
		if found && sub == "*" {
			wildcardSubresources[res] = i
		}

		if found && res == "*" {
			wildcardResources[sub] = i
		}
	}

	if slices.Contains(resources, "*/*") && len(resources) > 1 {
		return fmt.Errorf("%s.resources */* must be the only resource", path)
	}

	for i, resource := range resources {
		res, sub, found := strings.Cut(resource, "/")
		if !found {
			if res != "*" && hasAllResources {
				return fmt.Errorf("%s.resources wildcard overlaps resource %q", path, resource)
			}

			continue
		}

		if index, exists := wildcardSubresources[res]; exists && index != i {
			return fmt.Errorf("%s.resources wildcard overlaps resource %q", path, resource)
		}

		if index, exists := wildcardResources[sub]; exists && index != i {
			return fmt.Errorf("%s.resources wildcard overlaps resource %q", path, resource)
		}
	}

	return nil
}

func validateTriggerConditions(t *capsulev1beta2.TenantResourceTrigger, path string, compiler ConditionValidator, compileConditions bool) error {
	conditionNames := make(map[string]bool)

	for _, condition := range t.MatchConditions {
		if len(condition.Expression) == 0 || len(condition.Expression) > 4096 {
			return fmt.Errorf("%s.matchConditions expression must contain 1 to 4096 bytes", path)
		}

		if condition.Name == "" || len(validation.IsQualifiedName(condition.Name)) > 0 || conditionNames[condition.Name] || strings.HasPrefix(condition.Name, "capsule-trigger-") {
			return fmt.Errorf("%s.matchConditions requires unique valid names outside capsule-trigger-", path)
		}

		conditionNames[condition.Name] = true

		if !compileConditions {
			continue
		}

		if compiler == nil {
			return fmt.Errorf("%s match condition compiler is unavailable", path)
		}

		if err := compiler.ValidateTriggerCondition(condition.Expression); err != nil {
			return fmt.Errorf("%s.matchConditions[%s]: %w", path, condition.Name, err)
		}
	}

	return nil
}
