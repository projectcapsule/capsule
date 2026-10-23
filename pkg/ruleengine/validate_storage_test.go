// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestValidateStorageRules(t *testing.T) {
	for _, tc := range []struct {
		name    string
		matches []rules.PersistentVolumeMatch
		want    string
	}{
		{name: "absent"},
		{name: "explicit all", matches: []rules.PersistentVolumeMatch{{Selector: &metav1.LabelSelector{}}}},
		{name: "missing selector", matches: []rules.PersistentVolumeMatch{{}}, want: "selector is required"},
		{name: "invalid name", matches: []rules.PersistentVolumeMatch{{Name: "UPPER", Selector: &metav1.LabelSelector{}}}, want: "name"},
		{name: "duplicate name", matches: []rules.PersistentVolumeMatch{{Name: "same", Selector: &metav1.LabelSelector{}}, {Name: "same", Selector: &metav1.LabelSelector{}}}, want: "duplicate"},
		{name: "invalid label", matches: []rules.PersistentVolumeMatch{{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "invalid!"}}}}, want: "selector"},
		{name: "invalid operator", matches: []rules.PersistentVolumeMatch{{Selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "pool", Operator: "Unknown"}}}}}, want: "selector"},
		{name: "template", matches: []rules.PersistentVolumeMatch{{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "{{ .tenant.metadata.name }}"}}}}},
		{name: "too many", matches: make([]rules.PersistentVolumeMatch, 65), want: "at most 64"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateStorageRules(0, rules.NamespaceRuleEnforceStorageBody{Volumes: tc.matches})
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
		})
	}
}

func TestValidateStorageClassRules(t *testing.T) {
	snapshot := []rules.StorageValidationTarget{rules.StorageTargetVolumeSnapshot}
	labels := metav1.LabelSelector{MatchLabels: map[string]string{"env": "customer"}}

	for _, tc := range []struct {
		name    string
		targets []rules.StorageValidationTarget
		classes []rules.StorageClassMatch
		want    string
	}{
		{name: "absent"},
		{name: "classes without targets", classes: []rules.StorageClassMatch{{Exact: []string{"csi-snapclass"}}}},
		{name: "every matcher", targets: snapshot, classes: []rules.StorageClassMatch{{Exact: []string{"internal.example.com"}, Expression: `^[a-z0-9-]+\.example\.com$`, LabelSelector: labels}}},
		{name: "selector only", targets: snapshot, classes: []rules.StorageClassMatch{{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "env", Operator: metav1.LabelSelectorOpExists}}}}},
		{name: "template selector", targets: snapshot, classes: []rules.StorageClassMatch{{MatchLabels: map[string]string{"tenant": "{{ .tenant.metadata.name }}"}}}},
		{name: "targets without classes", targets: snapshot, want: "storage.classes is invalid: classes must be configured when targets are configured"},
		{name: "unsupported target", targets: []rules.StorageValidationTarget{"persistentvolumeclaim"}, classes: []rules.StorageClassMatch{{Exact: []string{"gold"}}}, want: `storage.targets[0] "persistentvolumeclaim" is invalid: unsupported storage target`},
		{name: "duplicate target", targets: []rules.StorageValidationTarget{rules.StorageTargetVolumeSnapshot, rules.StorageTargetVolumeSnapshot}, classes: []rules.StorageClassMatch{{Exact: []string{"gold"}}}, want: `storage.targets[1] "volumesnapshot" is invalid: duplicate target`},
		{name: "empty match", targets: snapshot, classes: []rules.StorageClassMatch{{}}, want: "storage.classes[0] is invalid: at least one of exact, exp, matchLabels or matchExpressions must be set"},
		{name: "invalid regex", classes: []rules.StorageClassMatch{{Exact: []string{"gold"}}, {Expression: "("}}, want: "storage.classes[1].exp"},
		{name: "too many exact values", classes: []rules.StorageClassMatch{{Exact: make([]string, 65)}}, want: "storage.classes[0].exact: at most 64 values"},
		{name: "invalid label value", classes: []rules.StorageClassMatch{{MatchLabels: map[string]string{"env": "invalid!"}}}, want: "storage.classes[0].matchLabels"},
		{name: "invalid operator", classes: []rules.StorageClassMatch{{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "env", Operator: "Unknown"}}}}, want: "storage.classes[0].matchExpressions"},
		{name: "too many classes", classes: make([]rules.StorageClassMatch, 65), want: "storage.classes: at most 64 matches are supported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateStorageRules(0, rules.NamespaceRuleEnforceStorageBody{Targets: tc.targets, Classes: tc.classes})
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
		})
	}
}

func TestValidateStorageAndNetworkRules(t *testing.T) {
	for _, tc := range []struct {
		name     string
		selector *metav1.LabelSelector
		cidr     string
		want     string
	}{
		{name: "both valid", selector: &metav1.LabelSelector{}, cidr: "10.0.0.0/8"},
		{name: "invalid storage", cidr: "10.0.0.0/8", want: "selector is required"},
		{name: "invalid network", selector: &metav1.LabelSelector{}, cidr: "invalid", want: "enforce.network.policies.egress.cidrs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
				Storage: rules.NamespaceRuleEnforceStorageBody{Volumes: []rules.PersistentVolumeMatch{{Selector: tc.selector}}},
				Network: rules.NamespaceRuleEnforceNetworkBody{Policies: rules.NamespaceRuleEnforceNetworkPoliciesBody{
					Egress: &rules.NetworkPolicyCIDRRule{CIDRs: []string{tc.cidr}},
				}},
			}}
			err := ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{body})
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
		})
	}
}
