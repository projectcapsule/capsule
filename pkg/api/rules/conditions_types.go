// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import "fmt"

// AdmissionCondition is a Boolean CEL gate for a typed resource block. It can
// inspect object and request metadata, but cannot generate mutation values.
// +kubebuilder:object:generate=true
type AdmissionCondition struct {
	// Name identifies a condition in admission errors. Names must be unique within a block.
	// +optional
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name,omitempty"`

	// Expression must evaluate to bool. Any false condition skips this block.
	// An evaluation error rejects the request unless another condition is false.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=4096
	Expression string `json:"expression"`
}

// VisitConditions traverses only the resource blocks that support conditions.
func (r *NamespaceRuleBodyNamespace) VisitConditions(visit func(string, []AdmissionCondition) error) error {
	if r == nil {
		return nil
	}

	for i := range r.Mutate {
		if len(r.Mutate[i].Workloads.Conditions) == 0 {
			continue
		}

		if err := visit(fmt.Sprintf("mutate[%d].workloads.conditions", i), r.Mutate[i].Workloads.Conditions); err != nil {
			return err
		}
	}

	if r.Enforce == nil {
		return nil
	}

	if len(r.Enforce.Workloads.Conditions) > 0 {
		if err := visit("enforce.workloads.conditions", r.Enforce.Workloads.Conditions); err != nil {
			return err
		}
	}

	if len(r.Enforce.Services.Conditions) > 0 {
		return visit("enforce.services.conditions", r.Enforce.Services.Conditions)
	}

	return nil
}
