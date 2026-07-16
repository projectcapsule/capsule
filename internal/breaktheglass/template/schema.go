// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kube-openapi/pkg/validation/spec"
	"k8s.io/kube-openapi/pkg/validation/strfmt"
	"k8s.io/kube-openapi/pkg/validation/validate"
)

func ValidateItems(schema runtime.RawExtension, tis []runtime.RawExtension) error {
	if _, err := ValidateSchema(schema.Raw); err != nil {
		return fmt.Errorf("paramSchema is invalid: %w", err)
	}

	for i, tpl := range tis {
		if _, err := ValidateTemplate(tpl.Raw); err != nil {
			return fmt.Errorf("template %d is invalid: %w", i, err)
		}
	}

	return nil
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

	// Convert to OpenAPI spec schemaData
	schema := &spec.Schema{}
	if err := schema.UnmarshalJSON(schemaData); err != nil {
		return nil, fmt.Errorf("failed to create OpenAPI schemaData: %w", err)
	}

	return schema, nil
}

func metaValidateJSONSchema(schemaBytes []byte) error {
	// For OAS 3.1: https://json-schema.org/draft/2020-12/schema
	meta := "https://json-schema.org/draft/2020-12/schema"

	c := jsonschema.NewCompiler()
	if err := c.AddResource(
		"meta.json",
		bytes.NewReader([]byte(`{"$ref":"`+meta+`"}`)),
	); err != nil {
		return err
	}
	// Compile the candidate schema using the chosen meta-schema
	if err := c.AddResource("candidate.json", bytes.NewReader(schemaBytes)); err != nil {
		return err
	}

	if _, err := c.Compile("candidate.json"); err != nil {
		return fmt.Errorf("schema invalid: %w", err)
	}

	return nil
}
