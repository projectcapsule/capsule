// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	crdvalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/yaml"
)

func TestWorkloadClassGeneratedSchemas(t *testing.T) {
	names := func(n int) string {
		return `["` + strings.Join(strings.Split(strings.Repeat("gold ", n)[:5*n-1], " "), `","`) + `"]`
	}

	type schemaCase struct {
		object string
		valid  bool
	}

	for _, resource := range []string{"tenants", "rulestatuses"} {
		data, err := os.ReadFile("../../charts/capsule/crds/capsule.clastix.io_" + resource + ".yaml")
		require.NoError(t, err)

		var crd apiextensionsv1.CustomResourceDefinition
		require.NoError(t, yaml.UnmarshalStrict(data, &crd))

		for _, class := range workloadClassFields {
			enforcement, mutation := 0, 0

			var visit func(apiextensionsv1.JSONSchemaProps, string)

			visit = func(schema apiextensionsv1.JSONSchemaProps, path string) {
				// Tenant status keeps deprecated classes lists of plain names.
				property, ok := schema.Properties["classes"].Properties[class.name]
				if ok && (property.Type == "string" || property.Items.Schema.Type == "object") {
					cases := []schemaCase{
						{`{}`, true},
						{`{"classes":{}}`, true},
						{`{"classes":{"NAME":"tenant-default"}}`, true},
						{`{"classes":{"NAME":"{{ .tenant.metadata.name }}-default"}}`, true},
						{`{"classes":{"NAME":"` + strings.Repeat("a", 4096) + `"}}`, true},
						{`{"classes":{"NAME":"` + strings.Repeat("a", 4097) + `"}}`, false},
						{`{"classes":{"NAME":""}}`, false},
						{`{"classes":{"NAME":["tenant-default"]}}`, false},
						{`{"classes":"tenant-default"}`, false},
					}

					if property.Type == "array" {
						enforcement++

						require.Equal(t, "atomic", *property.XListType)
						require.EqualValues(t, 64, *property.MaxItems)

						for _, name := range []string{"exact", "exp", "matchLabels", "matchExpressions"} {
							require.Contains(t, property.Items.Schema.Properties, name)
						}

						cases = []schemaCase{
							{`{}`, true},
							{`{"classes":{}}`, true},
							{`{"classes":{"NAME":[{"exact":["internal.example.com"],"exp":"^[a-z0-9-]+\\.example\\.com$","matchLabels":{"env":"customer"}}]}}`, true},
							{`{"classes":{"NAME":[{"matchExpressions":[{"key":"env","operator":"Exists"}]}]}}`, true},
							{`{"classes":{"NAME":[{"exact":` + names(64) + `}]}}`, true},
							{`{"classes":{"NAME":[{"exact":` + names(65) + `}]}}`, false},
							{`{"classes":{"NAME":[]}}`, false},
							{`{"classes":{"NAME":[{"exact":[]}]}}`, false},
							{`{"classes":{"NAME":[{"exact":[""]}]}}`, false},
							{`{"classes":{"NAME":[{"exp":""}]}}`, false},
							{`{"classes":{"NAME":[{"matchLabels":["env"]}]}}`, false},
							{`{"classes":{"NAME":["gold"]}}`, false},
							{`{"classes":{"NAME":"gold"}}`, false},
						}
					} else {
						mutation++

						require.Equal(t, "string", property.Type)
					}

					var internal apiextensions.JSONSchemaProps
					require.NoError(t, apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(&schema, &internal, nil))

					validator, _, err := crdvalidation.NewSchemaValidator(&internal)
					require.NoError(t, err)

					for _, tc := range cases {
						object := strings.ReplaceAll(tc.object, "NAME", class.name)

						var obj any
						require.NoError(t, json.Unmarshal([]byte(object), &obj))

						errs := crdvalidation.ValidateCustomResource(field.NewPath(path), obj, validator)
						require.Equal(t, tc.valid, len(errs) == 0, "%s: %.120s: %v", path, object, errs)
					}
				}

				for name, property := range schema.Properties {
					visit(property, path+"/"+name)
				}

				if schema.Items != nil && schema.Items.Schema != nil {
					visit(*schema.Items.Schema, path+"/*")
				}
			}

			for _, version := range crd.Spec.Versions {
				visit(*version.Schema.OpenAPIV3Schema, resource+"/"+version.Name)
			}

			// A class rule and a class default are available wherever rules are stored.
			require.Positive(t, enforcement, class.name)
			require.Equal(t, enforcement, mutation, class.name)

			if resource == "tenants" {
				require.Equal(t, 1, enforcement, class.name)
			}
		}
	}
}
