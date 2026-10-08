// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	crdvalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/yaml"

	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

func TestSecurityProfileLimitsInGeneratedSchemas(t *testing.T) {
	for _, resource := range []struct {
		name  string
		paths []string
	}{
		{"tenants", []string{"spec/rules/*/enforce/workloads"}},
		{"rulestatuses", []string{"spec/*/enforce/workloads", "status/rule/enforce/workloads", "status/rules/*/enforce/workloads"}},
	} {
		data, err := os.ReadFile("../../charts/capsule/crds/capsule.clastix.io_" + resource.name + ".yaml")
		require.NoError(t, err)
		var crd apiextensionsv1.CustomResourceDefinition
		require.NoError(t, yaml.UnmarshalStrict(data, &crd))
		var root *apiextensionsv1.JSONSchemaProps
		for _, version := range crd.Spec.Versions {
			if version.Name == "v1beta2" {
				root = version.Schema.OpenAPIV3Schema
			}
		}
		require.NotNil(t, root)
		for _, path := range resource.paths {
			schema := *root
			for _, segment := range strings.Split(path, "/") {
				if segment == "*" {
					require.NotNil(t, schema.Items)
					require.NotNil(t, schema.Items.Schema)
					schema = *schema.Items.Schema
				} else {
					require.Contains(t, schema.Properties, segment)
					schema = schema.Properties[segment]
				}
			}
			var internal apiextensions.JSONSchemaProps
			for _, name := range []string{"schedulers", "nodeSelector", "tolerations", "topologySpreadConstraints", "affinity"} {
				require.Contains(t, schema.Properties["placement"].Properties, name)
				if name != "schedulers" {
					require.NotContains(t, schema.Properties, name)
				}
			}
			for _, name := range []string{"seccompProfiles", "appArmorProfiles"} {
				require.Contains(t, schema.Properties["security"].Properties, name)
				require.NotContains(t, schema.Properties, name)
			}
			require.NoError(t, apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(&schema, &internal, nil))
			validator, _, err := crdvalidation.NewSchemaValidator(&internal)
			require.NoError(t, err)
			for _, appArmor := range []bool{false, true} {
				for _, tc := range []struct {
					name  string
					limit int
				}{
					{"matchers", 64}, {"types", 3}, {"localhostProfiles", 64},
				} {
					for _, size := range []int{tc.limit, tc.limit + 1} {
						t.Run(fmt.Sprintf("%s/%s/apparmor=%t/%s=%d", resource.name, path, appArmor, tc.name, size), func(t *testing.T) {
							body := securityProfileLimitBody(appArmor, tc.name, size)
							data, err := json.Marshal(body.Enforce.Workloads)
							require.NoError(t, err)
							var object any
							require.NoError(t, json.Unmarshal(data, &object))
							errs := crdvalidation.ValidateCustomResource(field.NewPath("workloads"), object, validator)
							if size <= tc.limit {
								require.Empty(t, errs)
							} else {
								require.NotEmpty(t, errs)
								require.Equal(t, field.ErrorTypeTooMany, errs[0].Type)
								group := "seccompProfiles"
								if appArmor {
									group = "appArmorProfiles"
								}
								location := "workloads.security." + group
								if tc.name != "matchers" {
									location += "[0]." + tc.name
								}
								require.Equal(t, location, errs[0].Field)
							}
						})
					}
				}
			}
		}
	}
}

func TestExpressionMatchLimitsInGeneratedSchemas(t *testing.T) {
	for _, resource := range []string{"tenants", "rulestatuses"} {
		data, err := os.ReadFile("../../charts/capsule/crds/capsule.clastix.io_" + resource + ".yaml")
		require.NoError(t, err)
		var crd apiextensionsv1.CustomResourceDefinition
		require.NoError(t, yaml.UnmarshalStrict(data, &crd))
		matches := 0
		var visit func(apiextensionsv1.JSONSchemaProps, string)
		visit = func(schema apiextensionsv1.JSONSchemaProps, path string) {
			if schema.Properties["exact"].Type == "array" && schema.Properties["exp"].Type == "string" {
				matches++
				var internal apiextensions.JSONSchemaProps
				require.NoError(t, apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(&schema, &internal, nil))
				validator, _, err := crdvalidation.NewSchemaValidator(&internal)
				require.NoError(t, err)
				for _, tc := range []struct {
					name, field string
					limit       int
					match       func(int) apiruntime.ExpressionMatch
				}{
					{"exact", "exact", 64, func(n int) apiruntime.ExpressionMatch {
						return apiruntime.ExpressionMatch{Exact: slices.Repeat([]string{"allowed"}, n)}
					}},
					{"exp", "exp", 4096, func(n int) apiruntime.ExpressionMatch {
						return apiruntime.ExpressionMatch{ExpressionRegex: apiruntime.ExpressionRegex{Expression: strings.Repeat("a", n)}}
					}},
					{"unicode", "exp", 4096, func(n int) apiruntime.ExpressionMatch {
						return apiruntime.ExpressionMatch{ExpressionRegex: apiruntime.ExpressionRegex{Expression: strings.Repeat("界", n)}}
					}},
				} {
					for _, size := range []int{tc.limit, tc.limit + 1} {
						t.Run(fmt.Sprintf("%s/%s=%d", path, tc.name, size), func(t *testing.T) {
							data, err := json.Marshal(tc.match(size))
							require.NoError(t, err)
							var object any
							require.NoError(t, json.Unmarshal(data, &object))
							errs := crdvalidation.ValidateCustomResource(field.NewPath("match"), object, validator)
							if size <= tc.limit {
								require.Empty(t, errs)
							} else {
								require.Len(t, errs, 1)
								require.Equal(t, "match."+tc.field, errs[0].Field)
								if tc.field == "exact" {
									require.Equal(t, field.ErrorTypeTooMany, errs[0].Type)
								} else {
									require.Equal(t, field.ErrorTypeTooLong, errs[0].Type)
								}
							}
						})
					}
				}
			}
			for name, property := range schema.Properties {
				visit(property, path+"/"+name)
			}
			if schema.Items != nil && schema.Items.Schema != nil {
				visit(*schema.Items.Schema, path+"/*")
			}
			if schema.AdditionalProperties != nil && schema.AdditionalProperties.Schema != nil {
				visit(*schema.AdditionalProperties.Schema, path+"/{key}")
			}
		}
		for _, version := range crd.Spec.Versions {
			visit(*version.Schema.OpenAPIV3Schema, resource+"/"+version.Name)
		}
		require.Positive(t, matches, "no expression schemas found in %s", resource)
	}
}
