// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package capsule

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestControllerCleanupRBAC(t *testing.T) {
	for _, tc := range []struct {
		name, values string
		strict       bool
	}{
		{name: "default"},
		{name: "strict", values: "manager.rbac.strict=true", strict: true},
		{name: "minimal alias", values: "manager.rbac.minimal=true", strict: true},
		{name: "both", values: "manager.rbac.strict=true,manager.rbac.minimal=true", strict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"template", "cleanup-test", ".", "--namespace", "capsule-system", "--show-only", "templates/rbac.yaml"}
			if tc.values != "" {
				args = append(args, "--set", tc.values)
			}
			// Rendering the real chart also checks the deprecated minimal switch.
			output, err := exec.CommandContext(t.Context(), "helm", args...).CombinedOutput()
			require.NoError(t, err, "helm must be installed for chart tests: %s", output)
			decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(output), 4096)
			var controller *rbacv1.ClusterRole
			var boundRoles []string
			for {
				var object struct {
					rbacv1.ClusterRole `json:",inline"`
					RoleRef            rbacv1.RoleRef `json:"roleRef"`
				}
				err := decoder.Decode(&object)
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(t, err)
				if object.Kind == "ClusterRole" && object.Name == "capsule:cleanup-test:controller" {
					controller = &object.ClusterRole
				}
				if object.Kind == "ClusterRoleBinding" {
					boundRoles = append(boundRoles, object.RoleRef.Name)
				}
			}
			if !tc.strict {
				require.Nil(t, controller)
				require.Contains(t, boundRoles, "cluster-admin")
				return
			}
			require.NotNil(t, controller)
			require.Nil(t, controller.AggregationRule, "test the standalone role without aggregated privileges")
			require.Contains(t, boundRoles, controller.Name)
			require.NotContains(t, boundRoles, "cluster-admin")
			for _, resource := range []struct{ group, name string }{
				{"", "configmaps"}, {"", "secrets"}, {"cleanup.example.com", "cleanupitems"},
			} {
				for _, verb := range []string{"get", "list", "watch", "delete", "deletecollection", "patch", "create", "update"} {
					allowed := false
					for _, rule := range controller.Rules {
						matches := func(values []string, value string) bool {
							return slices.Contains(values, "*") || slices.Contains(values, value)
						}
						if len(rule.ResourceNames) == 0 && matches(rule.APIGroups, resource.group) &&
							matches(rule.Resources, resource.name) && matches(rule.Verbs, verb) {
							allowed = true
						}
					}
					require.Equal(t, verb != "create" && verb != "update", allowed,
						"%s on %s/%s", verb, resource.group, resource.name)
				}
			}
		})
	}
}
