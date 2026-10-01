// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestValidateWorkloadTargets(t *testing.T) {
	for _, tc := range []struct {
		target string
		valid  bool
	}{
		{"pod", true}, {"pod/containers", true}, {"pod/initcontainers", true}, {"pod/ephemeralcontainers", true}, {"pod/volumes", true},
		{"deployment", true}, {"statefulset", true}, {"daemonset", true}, {"replicaset", true}, {"replicationcontroller", true}, {"job", true}, {"cronjob", true},
		{"deployment/containers", true}, {"cronjob/initcontainers", true}, {"job/volumes", true},
		{"", false}, {"DaemonSet", false}, {"daemonsets", false}, {"service", false}, {"*", false}, {"apps/daemonset", false}, {"deployment/", false}, {"deployment/ephemeralcontainers", false}, {"pod/containers/extra", false},
	} {
		t.Run(tc.target, func(t *testing.T) {
			body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: []rules.WorkloadValidationTarget{rules.WorkloadValidationTarget(tc.target)}}}}
			err := ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{body})
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "rules[0].enforce.workloads.targets[0]")
			}
		})
	}
	for _, raw := range []string{`{}`, `{"enforce":{"workloads":{}}}`, `{"enforce":{"workloads":{"targets":[]}}}`, `{"enforce":{"workloads":{"targets":null}}}`} {
		body := &rules.NamespaceRuleBodyNamespace{}
		require.NoError(t, json.Unmarshal([]byte(raw), body))
		require.NoError(t, ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{body}))
	}
}
