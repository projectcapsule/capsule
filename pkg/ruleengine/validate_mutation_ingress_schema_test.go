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
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting"
	crdvalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/yaml"
)

func TestListenerSetMutationGeneratedAPISchema(t *testing.T) {
	for _, resource := range []string{"tenants", "rulestatuses"} {
		data, err := os.ReadFile("../../charts/capsule/crds/capsule.clastix.io_" + resource + ".yaml")
		require.NoError(t, err)
		var crd apiextensionsv1.CustomResourceDefinition
		require.NoError(t, yaml.UnmarshalStrict(data, &crd))
		found := 0
		var visit func(apiextensionsv1.JSONSchemaProps)
		visit = func(schema apiextensionsv1.JSONSchemaProps) {
			if ref, ok := schema.Properties["listenerSets"].Properties["parentRef"]; ok {
				found++
				var internal apiextensions.JSONSchemaProps
				require.NoError(t, apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(&ref, &internal, nil))
				structural, err := structuralschema.NewStructural(&internal)
				require.NoError(t, err)
				validator, _, err := crdvalidation.NewSchemaValidator(&internal)
				require.NoError(t, err)
				for _, tc := range []struct {
					source string
					valid  bool
				}{
					{`{"name":"edge"}`, true},
					{`{"name":"{{ .tenant.metadata.name }}","namespace":"{{ .namespace.metadata.name }}"}`, true},
					{`{}`, false},
					{`{"name":""}`, false},
					{`{"name":"edge","namespace":""}`, false},
					{`{"name":"edge","namespace":"{{ index .tenant.metadata.labels \"networking.example.com/default-gateway-namespace\" }}"}`, true},
				} {
					var obj map[string]any
					require.NoError(t, json.Unmarshal([]byte(tc.source), &obj))
					defaulting.Default(obj, structural)
					require.Equal(t, "gateway.networking.k8s.io", obj["group"])
					require.Equal(t, "Gateway", obj["kind"])
					errs := crdvalidation.ValidateCustomResource(field.NewPath("parentRef"), obj, validator)
					require.Equal(t, tc.valid, len(errs) == 0, "%s: %v", tc.source, errs)
				}
				for _, name := range []string{"name", "namespace", "group", "kind"} {
					for _, length := range []int{4096, 4097} {
						template := "{{" + strings.Repeat(" ", length-6) + ". }}"
						obj := map[string]any{"name": "edge", name: template}
						errs := crdvalidation.ValidateCustomResource(field.NewPath("parentRef"), obj, validator)
						require.Equal(t, length == 4096, len(errs) == 0, "%s/%s length %d: %v", resource, name, length, errs)
					}
				}
			}
			for _, child := range schema.Properties {
				visit(child)
			}
			if schema.Items != nil && schema.Items.Schema != nil {
				visit(*schema.Items.Schema)
			}
		}
		for _, version := range crd.Spec.Versions {
			visit(*version.Schema.OpenAPIV3Schema)
		}
		require.Positive(t, found, "%s must expose the ingress mutation API", resource)
	}
}
