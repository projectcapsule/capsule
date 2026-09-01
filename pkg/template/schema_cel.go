// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	schemacel "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	"k8s.io/apimachinery/pkg/util/validation/field"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"k8s.io/apiserver/pkg/cel/environment"

	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	celruntime "github.com/projectcapsule/capsule/pkg/runtime/cel"
)

// kubernetesCELValidator delegates runtime behavior to the same structural
// schema CEL implementation used for Kubernetes CRDs. Param schemas which use
// x-kubernetes-validations therefore get Kubernetes scoping, nested rule,
// fieldPath, reason, messageExpression, and cost-budget behavior.
type kubernetesCELValidator struct {
	schema    *structuralschema.Structural
	validator *schemacel.Validator
}

// validateKubernetesValidationExtensions validates the portable rule shape
// and expression syntax at template admission. Kubernetes' typed structural
// compiler is then used for the actual parameter value at render time.
func validateKubernetesValidationExtensions(schemaData []byte) error {
	var root any
	if err := json.Unmarshal(schemaData, &root); err != nil {
		return err
	}

	compiler, err := celruntime.NewCompiler()
	if err != nil {
		return err
	}

	return walkJSONSchemaNodes(root, "$", func(schemaNode map[string]any, path string) error {
		raw, found := schemaNode[apiruntime.JSONSchemaKubernetesValidationsKey]
		if !found {
			return nil
		}

		data, err := json.Marshal(raw)
		if err != nil {
			return fmt.Errorf("%s.%s is invalid: %w", path, apiruntime.JSONSchemaKubernetesValidationsKey, err)
		}

		var rules apiextensionsv1.ValidationRules
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()

		if err := decoder.Decode(&rules); err != nil {
			return fmt.Errorf("%s.%s is invalid: decode rules: %w", path, apiruntime.JSONSchemaKubernetesValidationsKey, err)
		}

		if len(rules) == 0 {
			return fmt.Errorf("%s.%s must contain at least one rule", path, apiruntime.JSONSchemaKubernetesValidationsKey)
		}

		for index, rule := range rules {
			rulePath := fmt.Sprintf("%s.%s[%d]", path, apiruntime.JSONSchemaKubernetesValidationsKey, index)
			if err := validateKubernetesValidationRule(compiler, rule); err != nil {
				return fmt.Errorf("%s is invalid: %w", rulePath, err)
			}
		}

		return nil
	})
}

func validateKubernetesValidationRule(
	compiler *celruntime.Compiler,
	rule apiextensionsv1.ValidationRule,
) error {
	trimmedRule := strings.TrimSpace(rule.Rule)
	if trimmedRule == "" {
		return fmt.Errorf("rule must not be empty")
	}

	trimmedMessage := strings.TrimSpace(rule.Message)
	if rule.Message != "" && trimmedMessage == "" {
		return fmt.Errorf("message must not be blank")
	}

	if strings.ContainsAny(rule.Message, "\r\n") {
		return fmt.Errorf("message must not contain line breaks")
	}

	if strings.ContainsAny(rule.Rule, "\r\n") && trimmedMessage == "" {
		return fmt.Errorf("message is required when rule contains line breaks")
	}

	if rule.FieldPath != "" {
		if strings.TrimSpace(rule.FieldPath) == "" {
			return fmt.Errorf("fieldPath must not be blank")
		}

		if strings.ContainsAny(rule.FieldPath, "\r\n") {
			return fmt.Errorf("fieldPath must not contain line breaks")
		}
	}

	variables := []string{"self"}
	if strings.Contains(rule.Rule, "oldSelf") || strings.Contains(rule.MessageExpression, "oldSelf") {
		variables = append(variables, "oldSelf")
	}

	if _, err := compiler.CompileBooleanWithVariables(
		rule.Rule,
		environment.NewExpressions,
		variables...,
	); err != nil {
		return fmt.Errorf("compile rule: %w", err)
	}

	if rule.MessageExpression != "" {
		if strings.TrimSpace(rule.MessageExpression) == "" {
			return fmt.Errorf("messageExpression must not be blank")
		}

		if _, err := compiler.CompileStringWithVariables(
			rule.MessageExpression,
			environment.NewExpressions,
			variables...,
		); err != nil {
			return fmt.Errorf("compile messageExpression: %w", err)
		}
	}

	return nil
}

func newKubernetesCELValidator(schemaData []byte) (*kubernetesCELValidator, error) {
	containsRules, err := containsJSONSchemaKeyword(schemaData, apiruntime.JSONSchemaKubernetesValidationsKey)
	if err != nil || !containsRules {
		return nil, err
	}

	versioned := &apiextensionsv1.JSONSchemaProps{}
	if err := json.Unmarshal(schemaData, versioned); err != nil {
		return nil, fmt.Errorf("decode Kubernetes structural schema: %w", err)
	}

	internal := &apiextensions.JSONSchemaProps{}
	if err := apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(
		versioned,
		internal,
		nil,
	); err != nil {
		return nil, fmt.Errorf("convert Kubernetes structural schema: %w", err)
	}

	structural, err := structuralschema.NewStructural(internal)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes structural schema: %w", err)
	}

	rawRuleCount, err := countRawKubernetesValidationRules(schemaData)
	if err != nil {
		return nil, err
	}

	if structuralRuleCount := countStructuralKubernetesValidationRules(structural); structuralRuleCount != rawRuleCount {
		return nil, fmt.Errorf(
			"x-kubernetes-validations rules must be placed on Kubernetes structural schema nodes: decoded %d of %d rules",
			structuralRuleCount,
			rawRuleCount,
		)
	}

	// Validate the parameter schema as a child of a synthetic root. Kubernetes'
	// structural validation reserves a few rules for the root of a CRD, while a
	// parameter object is not itself a Kubernetes API resource.
	wrapper := &structuralschema.Structural{
		Generic: structuralschema.Generic{Type: "object"},
		Properties: map[string]structuralschema.Structural{
			"params": *structural,
		},
	}
	if errs := structuralschema.ValidateStructural(field.NewPath("paramSchema"), wrapper); len(errs) > 0 {
		return nil, fmt.Errorf("x-kubernetes-validations requires a structural schema: %w", errs.ToAggregate())
	}

	return &kubernetesCELValidator{
		schema:    structural,
		validator: schemacel.NewValidator(structural, false, celconfig.PerCallLimit),
	}, nil
}

func (v *kubernetesCELValidator) Validate(ctx context.Context, value, oldValue any) error {
	if v == nil || v.validator == nil {
		return nil
	}

	errs, _ := v.validator.Validate(
		ctx,
		field.NewPath("params"),
		v.schema,
		value,
		oldValue,
		celconfig.RuntimeCELCostBudget,
	)
	if len(errs) > 0 {
		return errs.ToAggregate()
	}

	return nil
}

func containsJSONSchemaKeyword(schemaData []byte, keyword string) (bool, error) {
	var root any
	if err := json.Unmarshal(schemaData, &root); err != nil {
		return false, err
	}

	found := false
	err := walkJSONSchemaNodes(root, "$", func(schemaNode map[string]any, _ string) error {
		if _, exists := schemaNode[keyword]; exists {
			found = true
		}

		return nil
	})

	return found, err
}

func countRawKubernetesValidationRules(schemaData []byte) (int, error) {
	var root any
	if err := json.Unmarshal(schemaData, &root); err != nil {
		return 0, err
	}

	count := 0
	err := walkJSONSchemaNodes(root, "$", func(schemaNode map[string]any, _ string) error {
		raw, found := schemaNode[apiruntime.JSONSchemaKubernetesValidationsKey]
		if !found {
			return nil
		}

		rules, ok := raw.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", apiruntime.JSONSchemaKubernetesValidationsKey)
		}

		count += len(rules)

		return nil
	})

	return count, err
}

func countStructuralKubernetesValidationRules(schema *structuralschema.Structural) int {
	if schema == nil {
		return 0
	}

	count := len(schema.XValidations)
	count += countStructuralKubernetesValidationRules(schema.Items)

	if schema.AdditionalProperties != nil {
		count += countStructuralKubernetesValidationRules(schema.AdditionalProperties.Structural)
	}

	for _, property := range schema.Properties {
		property := property
		count += countStructuralKubernetesValidationRules(&property)
	}

	if schema.ValueValidation != nil {
		count += countNestedKubernetesValidationRules(schema.ValueValidation.AllOf)
		count += countNestedKubernetesValidationRules(schema.ValueValidation.AnyOf)
		count += countNestedKubernetesValidationRules(schema.ValueValidation.OneOf)
		count += countNestedKubernetesValidationRule(schema.ValueValidation.Not)
	}

	return count
}

func countNestedKubernetesValidationRules(validations []structuralschema.NestedValueValidation) int {
	count := 0
	for index := range validations {
		count += countNestedKubernetesValidationRule(&validations[index])
	}

	return count
}

func countNestedKubernetesValidationRule(validation *structuralschema.NestedValueValidation) int {
	if validation == nil {
		return 0
	}

	count := len(validation.XValidations)
	count += countNestedKubernetesValidationRule(validation.Items)
	count += countNestedKubernetesValidationRule(validation.AdditionalProperties)

	for name := range validation.Properties {
		property := validation.Properties[name]
		count += countNestedKubernetesValidationRule(&property)
	}

	count += countNestedKubernetesValidationRules(validation.AllOf)
	count += countNestedKubernetesValidationRules(validation.AnyOf)
	count += countNestedKubernetesValidationRules(validation.OneOf)
	count += countNestedKubernetesValidationRule(validation.Not)

	return count
}
