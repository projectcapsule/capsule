// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package capsule

import (
	"bytes"
	"os/exec"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/yaml"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestManagedWebhookRegistration(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "helm", "template", "managed-test", ".", "--namespace", "capsule-system", "--show-only", "templates/configuration.yaml").CombinedOutput()
	require.NoError(t, err, "%s", output)
	var config capsulev1beta2.CapsuleConfiguration
	require.NoError(t, yaml.Unmarshal(bytes.TrimSpace(output), &config))
	require.NotNil(t, config.Spec.Admission.Validating)
	for _, hook := range config.Spec.Admission.Validating.Webhooks {
		if hook.Name != "managed.validating.projectcapsule.dev" {
			continue
		}
		selector, err := metav1.LabelSelectorAsSelector(hook.ObjectSelector)
		require.NoError(t, err)
		for _, value := range []string{meta.ValueController, meta.ValueControllerResources} {
			require.True(t, selector.Matches(labels.Set{meta.NewManagedByCapsuleLabel: value}))
		}
		require.False(t, selector.Matches(labels.Set{}), "ordinary requests must not reach the guard")
		require.False(t, selector.Matches(labels.Set{meta.NewManagedByCapsuleLabel: meta.ValueControllerReplications}))
		for _, tc := range []struct {
			group, version, resource string
			scope                    admissionregistrationv1.ScopeType
			want                     bool
		}{
			{"", "v1", "pods", admissionregistrationv1.NamespacedScope, true},
			{"", "v1", "namespaces", admissionregistrationv1.ClusterScope, true},
			{"capsule.clastix.io", "v1beta2", "globalresourcequotas", admissionregistrationv1.ClusterScope, true},
			{"", "v1", "namespaces/finalize", admissionregistrationv1.ClusterScope, false},
			{"", "v1", "namespaces/status", admissionregistrationv1.ClusterScope, false},
			{"rbac.authorization.k8s.io", "v1", "clusterroles", admissionregistrationv1.ClusterScope, false},
		} {
			for _, operation := range []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update, admissionregistrationv1.Delete} {
				matched := false
				for _, rule := range hook.Rules {
					matches := func(values []string, value string) bool {
						return slices.Contains(values, value) || slices.Contains(values, "*")
					}
					if slices.Contains(rule.Operations, operation) && matches(rule.APIGroups, tc.group) && matches(rule.APIVersions, tc.version) && matches(rule.Resources, tc.resource) && (rule.Scope == nil || *rule.Scope == admissionregistrationv1.AllScopes || *rule.Scope == tc.scope) {
						matched = true
					}
				}
				require.Equal(t, tc.want, matched, "%s %s", operation, tc.resource)
			}
		}
		return
	}
	t.Fatal("managed webhook missing from rendered configuration")
}
