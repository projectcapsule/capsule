// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting"
	crdvalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/yaml"
)

func TestOptionalRuleBooleansGeneratedSchema(t *testing.T) {
	for _, resource := range []string{"tenants", "rulestatuses"} {
		data, err := os.ReadFile("../../charts/capsule/crds/capsule.clastix.io_" + resource + ".yaml")
		require.NoError(t, err)
		var crd apiextensionsv1.CustomResourceDefinition
		require.NoError(t, yaml.UnmarshalStrict(data, &crd))
		found := map[string]int{}
		var visit func(apiextensionsv1.JSONSchemaProps, string)
		visit = func(schema apiextensionsv1.JSONSchemaProps, path string) {
			for name, property := range schema.Properties {
				if (name == "negate" || name == "required") && property.Type == "boolean" {
					found[name]++
					t.Run(resource+"/"+path+"/"+name, func(t *testing.T) {
						require.Nil(t, property.Default, "optional booleans must not be materialized by defaulting")
						require.False(t, slices.Contains(schema.Required, name))
						wrapper := apiextensionsv1.JSONSchemaProps{Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{name: property}}
						var internal apiextensions.JSONSchemaProps
						require.NoError(t, apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(&wrapper, &internal, nil))
						structural, err := structuralschema.NewStructural(&internal)
						require.NoError(t, err)
						validator, _, err := crdvalidation.NewSchemaValidator(&internal)
						require.NoError(t, err)
						for _, source := range []string{`{}`, `{"` + name + `":null}`, `{"` + name + `":false}`, `{"` + name + `":true}`} {
							var obj map[string]any
							require.NoError(t, json.Unmarshal([]byte(source), &obj))
							want := map[string]any{}
							if value, present := obj[name]; present && value != nil {
								want[name] = value
							}
							defaulting.PruneNonNullableNullsWithoutDefaults(obj, structural)
							defaulting.Default(obj, structural)
							require.Equal(t, want, obj, source)
							require.Empty(t, crdvalidation.ValidateCustomResource(field.NewPath("policy"), obj, validator))
						}
						for _, value := range []any{"false", float64(0), []any{false}, map[string]any{}} {
							require.NotEmpty(t, crdvalidation.ValidateCustomResource(field.NewPath("policy"), map[string]any{name: value}, validator))
						}
					})
				}
				visit(property, path+"/"+name)
			}
			if schema.Items != nil && schema.Items.Schema != nil {
				visit(*schema.Items.Schema, path+"/items")
			}
			if schema.AdditionalProperties != nil && schema.AdditionalProperties.Schema != nil {
				visit(*schema.AdditionalProperties.Schema, path+"/values")
			}
		}
		for _, version := range crd.Spec.Versions {
			visit(*version.Schema.OpenAPIV3Schema, version.Name)
		}
		require.Positive(t, found["negate"], resource)
		require.Positive(t, found["required"], resource)
	}
}
