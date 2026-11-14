// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package replication

import (
	"fmt"
	"slices"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type ConditionValidator interface{ ValidateTriggerCondition(string) error }

func ValidateTriggers(spec *capsulev1beta2.TenantResourceCommonSpec, namespaced bool, compiler ConditionValidator) error {
	return validateTriggers(spec, namespaced, compiler, true)
}

// ValidateTriggerStructure checks the bounded watch shape before preparation.
// Controllers validate CEL through shared compiled/prepared caches.
func ValidateTriggerStructure(spec *capsulev1beta2.TenantResourceCommonSpec, namespaced bool) error {
	return validateTriggers(spec, namespaced, nil, false)
}

func validateTriggers(spec *capsulev1beta2.TenantResourceCommonSpec, namespaced bool, compiler ConditionValidator, compile bool) error {
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

		//nolint:staticcheck // Explicitly reject the legacy admission-only field.
		if len(t.Audience) != 0 {
			return fmt.Errorf("%s.audience is unsupported: watches do not carry request identities", path)
		}

		if len(t.MatchConditions) > 60 {
			return fmt.Errorf("%s exceeds match condition limit", path)
		}

		if err := validateTriggerConditions(t, path, compiler, compile); err != nil {
			return err
		}
	}

	return nil
}

func validateTriggerRules(t *capsulev1beta2.TenantResourceTrigger, path string) error {
	if len(t.Operations) == 0 || len(t.APIGroups) == 0 || len(t.APIVersions) == 0 || len(t.Resources) == 0 {
		return fmt.Errorf("%s requires operations, apiGroups, apiVersions, and resources", path)
	}

	if len(t.Operations) > 3 {
		return fmt.Errorf("%s.operations must contain at most three entries", path)
	}

	for _, op := range t.Operations {
		if !slices.Contains([]admissionv1.OperationType{admissionv1.Create, admissionv1.Update, admissionv1.Delete, admissionv1.OperationAll}, op) {
			return fmt.Errorf("%s.operations contains unsupported operation %q", path, op)
		}
	}

	if slices.Contains(t.Operations, admissionv1.OperationAll) && len(t.Operations) > 1 {
		return fmt.Errorf("%s.operations wildcard must be the only operation", path)
	}

	if len(t.APIGroups) > 8 || len(t.APIVersions) > 8 || len(t.Resources) > 8 || len(t.APIGroups)*len(t.APIVersions)*len(t.Resources) > MaxTriggerResources {
		return fmt.Errorf("%s may select at most %d explicit group/version/resource combinations", path, MaxTriggerResources)
	}

	for _, group := range t.APIGroups {
		if group != "" && len(validation.IsDNS1123Subdomain(group)) > 0 {
			return fmt.Errorf("%s.apiGroups requires explicit valid groups", path)
		}
	}

	for _, version := range t.APIVersions {
		if version == "" || len(validation.IsDNS1035Label(version)) > 0 {
			return fmt.Errorf("%s.apiVersions requires explicit valid versions", path)
		}
	}

	return validateTriggerResources(t.Resources, path)
}

// Informers watch explicit top-level resources, not admission subresources.
func validateTriggerResources(resources []string, path string) error {
	for _, resource := range resources {
		if len(validation.IsDNS1035Label(resource)) > 0 {
			return fmt.Errorf("%s.resources requires explicit top-level resource names; wildcards and subresources are unsupported", path)
		}
	}

	return nil
}

func validateTriggerConditions(t *capsulev1beta2.TenantResourceTrigger, path string, compiler ConditionValidator, compile bool) error {
	conditionNames := make(map[string]bool)

	for _, condition := range t.MatchConditions {
		if len(condition.Expression) == 0 || len(condition.Expression) > 4096 {
			return fmt.Errorf("%s.matchConditions expression must contain 1 to 4096 bytes", path)
		}

		if condition.Name == "" || len(validation.IsQualifiedName(condition.Name)) > 0 || conditionNames[condition.Name] {
			return fmt.Errorf("%s.matchConditions requires unique valid names", path)
		}

		conditionNames[condition.Name] = true

		if !compile {
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
