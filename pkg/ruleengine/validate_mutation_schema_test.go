// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	crdvalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/yaml"
)

func TestWorkloadMutationGeneratedSchemas(t *testing.T) {
	for _, resource := range []string{"tenants", "rulestatuses"} {
		data, err := os.ReadFile("../../charts/capsule/crds/capsule.clastix.io_" + resource + ".yaml")
		require.NoError(t, err)
		var crd apiextensionsv1.CustomResourceDefinition
		require.NoError(t, yaml.UnmarshalStrict(data, &crd))
		found := 0
		var visit func(apiextensionsv1.JSONSchemaProps, string)
		visit = func(schema apiextensionsv1.JSONSchemaProps, path string) {
			if _, ok := schema.Properties["security"].Properties["readOnlyRootFilesystem"]; ok {
				found++
				for _, name := range []string{"scheduler", "nodeSelector", "tolerations", "topologySpreadConstraints", "affinity"} {
					require.Contains(t, schema.Properties["placement"].Properties, name)
					require.NotContains(t, schema.Properties, name)
				}
				for _, name := range []string{"hostUsers", "readOnlyRootFilesystem", "seccompProfile", "appArmorProfile"} {
					require.Contains(t, schema.Properties["security"].Properties, name)
					require.NotContains(t, schema.Properties, name)
				}
				require.Equal(t, "set", *schema.Properties["targets"].XListType)
				var internal apiextensions.JSONSchemaProps
				require.NoError(t, apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(&schema, &internal, nil))
				validator, _, err := crdvalidation.NewSchemaValidator(&internal)
				require.NoError(t, err)
				for _, tc := range []struct {
					object string
					valid  bool
				}{
					{`{}`, true}, {`{"security": {"readOnlyRootFilesystem": false}}`, true},
					{`{"targets": [], "security": {"readOnlyRootFilesystem": true}}`, true},
					{`{"targets": ["pod", "pod/containers", "pod/initcontainers", "pod/ephemeralcontainers"], "security": {"readOnlyRootFilesystem": true}}`, true},
					{`{"targets": ["deployment"], "security": {"readOnlyRootFilesystem": true}}`, false},
					{`{"targets": ["pod/volumes"], "security": {"readOnlyRootFilesystem": true}}`, false},
					{`{"targets": ["pod", "pod", "pod", "pod", "pod"], "security": {"readOnlyRootFilesystem": true}}`, false},
					{`{"security": {"readOnlyRootFilesystem": "false"}}`, false},
				} {
					var obj any
					require.NoError(t, json.Unmarshal([]byte(tc.object), &obj))
					errs := crdvalidation.ValidateCustomResource(field.NewPath(path), obj, validator)
					require.Equal(t, tc.valid, len(errs) == 0, "%s: %s: %v", path, tc.object, errs)
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
		if resource == "tenants" {
			require.Equal(t, 1, found)
		} else {
			require.Equal(t, 3, found)
		}
	}
}
