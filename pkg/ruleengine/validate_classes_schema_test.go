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

	for _, resource := range []string{"tenants", "rulestatuses"} {
		data, err := os.ReadFile("../../charts/capsule/crds/capsule.clastix.io_" + resource + ".yaml")
		require.NoError(t, err)

		var crd apiextensionsv1.CustomResourceDefinition
		require.NoError(t, yaml.UnmarshalStrict(data, &crd))

		enforcement, mutation := 0, 0

		var visit func(apiextensionsv1.JSONSchemaProps, string)

		visit = func(schema apiextensionsv1.JSONSchemaProps, path string) {
			// Tenant status keeps a deprecated classes.priority list of plain names.
			priority, ok := schema.Properties["classes"].Properties["priority"]
			if ok && (priority.Type == "string" || priority.Items.Schema.Type == "object") {
				cases := []struct {
					object string
					valid  bool
				}{
					{`{}`, true},
					{`{"classes":{}}`, true},
					{`{"classes":{"priority":"tenant-default"}}`, true},
					{`{"classes":{"priority":"{{ .tenant.metadata.name }}-default"}}`, true},
					{`{"classes":{"priority":"` + strings.Repeat("a", 4096) + `"}}`, true},
					{`{"classes":{"priority":"` + strings.Repeat("a", 4097) + `"}}`, false},
					{`{"classes":{"priority":""}}`, false},
					{`{"classes":{"priority":["tenant-default"]}}`, false},
					{`{"classes":"tenant-default"}`, false},
				}

				if priority.Type == "array" {
					enforcement++

					require.Equal(t, "atomic", *priority.XListType)
					require.EqualValues(t, 64, *priority.MaxItems)

					for _, name := range []string{"exact", "exp", "matchLabels", "matchExpressions"} {
						require.Contains(t, priority.Items.Schema.Properties, name)
					}

					cases = []struct {
						object string
						valid  bool
					}{
						{`{}`, true},
						{`{"classes":{}}`, true},
						{`{"classes":{"priority":[{"exact":["internal.example.com"],"exp":"^[a-z0-9-]+\\.example\\.com$","matchLabels":{"env":"customer"}}]}}`, true},
						{`{"classes":{"priority":[{"matchExpressions":[{"key":"env","operator":"Exists"}]}]}}`, true},
						{`{"classes":{"priority":[{"exact":` + names(64) + `}]}}`, true},
						{`{"classes":{"priority":[{"exact":` + names(65) + `}]}}`, false},
						{`{"classes":{"priority":[]}}`, false},
						{`{"classes":{"priority":[{"exact":[]}]}}`, false},
						{`{"classes":{"priority":[{"exact":[""]}]}}`, false},
						{`{"classes":{"priority":[{"exp":""}]}}`, false},
						{`{"classes":{"priority":[{"matchLabels":["env"]}]}}`, false},
						{`{"classes":{"priority":["gold"]}}`, false},
						{`{"classes":{"priority":"gold"}}`, false},
					}
				} else {
					mutation++

					require.Equal(t, "string", priority.Type)
				}

				var internal apiextensions.JSONSchemaProps
				require.NoError(t, apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(&schema, &internal, nil))

				validator, _, err := crdvalidation.NewSchemaValidator(&internal)
				require.NoError(t, err)

				for _, tc := range cases {
					var obj any
					require.NoError(t, json.Unmarshal([]byte(tc.object), &obj))

					errs := crdvalidation.ValidateCustomResource(field.NewPath(path), obj, validator)
					require.Equal(t, tc.valid, len(errs) == 0, "%s: %.120s: %v", path, tc.object, errs)
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
		require.Positive(t, enforcement)
		require.Equal(t, enforcement, mutation)

		if resource == "tenants" {
			require.Equal(t, 1, enforcement)
		}
	}
}
