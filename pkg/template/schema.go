// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"text/template"

	"github.com/santhosh-tekuri/jsonschema/v5"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kube-openapi/pkg/validation/spec"
	"k8s.io/kube-openapi/pkg/validation/strfmt"
	"k8s.io/kube-openapi/pkg/validation/validate"

	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/template/functions"
)

func ValidateItems(schema k8sruntime.RawExtension, tis []k8sruntime.RawExtension) error {
	if _, err := ValidateSchema(schema.Raw); err != nil {
		return fmt.Errorf("paramSchema is invalid: %w", err)
	}

	for i, tpl := range tis {
		if _, err := validateTemplate(tpl.Raw); err != nil {
			return fmt.Errorf("template %d is invalid: %w", i, err)
		}
	}

	return nil
}

// ValidateResourceTemplates validates a parameter schema and the targets of
// reusable resource templates.
func ValidateResourceTemplates(schema *k8sruntime.RawExtension, resources []apiruntime.ResourceTemplate) error {
	var schemaBytes []byte
	if schema != nil {
		schemaBytes = schema.Raw
	}

	if _, err := ValidateSchema(schemaBytes); err != nil {
		return fmt.Errorf("paramSchema is invalid: %w", err)
	}

	for resourceIndex, resourceTemplate := range resources {
		if len(resourceTemplate.Targets) == 0 && resourceTemplate.Template == "" {
			return fmt.Errorf("resource %d must define at least one target or a template", resourceIndex)
		}

		for targetIndex, target := range resourceTemplate.Targets {
			targetData := target.Raw
			if len(targetData) == 0 && target.Object != nil {
				marshaled, err := json.Marshal(target.Object)
				if err != nil {
					return fmt.Errorf("resource %d target %d is invalid: %w", resourceIndex, targetIndex, err)
				}

				targetData = marshaled
			}

			if len(targetData) == 0 {
				return fmt.Errorf("resource %d target %d is empty", resourceIndex, targetIndex)
			}

			if _, err := validateTemplate(targetData); err != nil {
				return fmt.Errorf("resource %d target %d is invalid: %w", resourceIndex, targetIndex, err)
			}
		}

		if resourceTemplate.Template != "" {
			if _, err := validateTemplate([]byte(resourceTemplate.Template)); err != nil {
				return fmt.Errorf("resource %d template is invalid: %w", resourceIndex, err)
			}
		}
	}

	return nil
}

func validateTemplate(tpl []byte) (*template.Template, error) {
	return template.New("item").
		Option("missingkey=error").
		Funcs(functions.ExtraFuncMap()).
		Parse(string(tpl))
}

func Validate(schemaData []byte, params []byte) error {
	schema, err := ValidateSchema(schemaData)
	if err != nil || schema == nil {
		return err
	}

	// Create validator
	validator := validate.NewSchemaValidator(schema, nil, "", strfmt.Default)

	p := make(map[string]any)
	if len(params) != 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return err
		}
	}

	// Validate the data
	result := validator.Validate(p)
	if !result.IsValid() {
		var errors []string
		for _, err := range result.Errors {
			errors = append(errors, err.Error())
		}

		return fmt.Errorf("validation failed: %v", errors)
	}

	// The kube-openapi validator above preserves the validation behavior used by
	// existing templates, but it only understands the older OpenAPI schema
	// dialect. Validate the same parameters with the JSON Schema 2020-12
	// compiler as well so conditional and composition keywords such as if/then,
	// dependentRequired, and unevaluatedProperties are enforced at runtime.
	compiled, err := compileJSONSchema(schemaData)
	if err != nil {
		return fmt.Errorf("failed to compile JSON Schema: %w", err)
	}

	if err := compiled.Validate(p); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	kubernetesValidator, err := newKubernetesCELValidator(schemaData)
	if err != nil {
		return fmt.Errorf("failed to prepare x-kubernetes-validations: %w", err)
	}

	if err := kubernetesValidator.Validate(context.Background(), p, nil); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	return nil
}

// ValidateSchema prepares the validation schema. Returns nil if the schema is empty.
func ValidateSchema(schemaData []byte) (*spec.Schema, error) {
	if len(schemaData) == 0 {
		return nil, nil
	}

	err := metaValidateJSONSchema(schemaData)
	if err != nil {
		return nil, fmt.Errorf("failed to validate OpenAPI schemaData: %w", err)
	}

	if err := validateJSONSchemaFormExtensions(schemaData); err != nil {
		return nil, fmt.Errorf("failed to validate Capsule form extensions: %w", err)
	}

	if err := validateKubernetesValidationExtensions(schemaData); err != nil {
		return nil, fmt.Errorf("failed to validate x-kubernetes-validations: %w", err)
	}

	if _, err := newKubernetesCELValidator(schemaData); err != nil {
		return nil, fmt.Errorf("failed to prepare x-kubernetes-validations: %w", err)
	}

	// Convert to OpenAPI spec schemaData
	schema := &spec.Schema{}
	if err := schema.UnmarshalJSON(schemaData); err != nil {
		return nil, fmt.Errorf("failed to create OpenAPI schemaData: %w", err)
	}

	return schema, nil
}

func metaValidateJSONSchema(schemaBytes []byte) error {
	_, err := compileJSONSchema(schemaBytes)

	return err
}

func compileJSONSchema(schemaBytes []byte) (*jsonschema.Schema, error) {
	// For OAS 3.1: https://json-schema.org/draft/2020-12/schema
	meta := "https://json-schema.org/draft/2020-12/schema"

	c := jsonschema.NewCompiler()

	c.Draft = jsonschema.Draft2020

	if err := c.AddResource("meta.json", bytes.NewReader([]byte(`{"$ref":"`+meta+`"}`))); err != nil {
		return nil, err
	}
	// Compile the candidate schema using the chosen meta-schema
	if err := c.AddResource("candidate.json", bytes.NewReader(schemaBytes)); err != nil {
		return nil, err
	}

	compiled, err := c.Compile("candidate.json")
	if err != nil {
		return nil, fmt.Errorf("schema invalid: %w", err)
	}

	return compiled, nil
}
