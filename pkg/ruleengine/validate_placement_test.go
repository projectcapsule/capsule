// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func TestValidatePlacementConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, source, errorText string }{
		{"ensure only", `mutate: [{workloads: {nodeSelector: {pool: shared}}}]`, ""},
		{"scheduler only", `mutate: [{workloads: {scheduler: tenant-scheduler}}]`, ""},
		{"scheduler replace", `mutate: [{action: replace, workloads: {scheduler: default-scheduler}}]`, ""},
		{"scheduler null", `mutate: [{workloads: {scheduler: null}}]`, "at least one"},
		{"scheduler empty", `mutate: [{workloads: {scheduler: ''}}]`, "at least one"},
		{"scheduler blank", `mutate: [{workloads: {scheduler: '   '}}]`, "rules[0].mutate[0].workloads.scheduler: scheduler name must not be blank"},
		{"host users false only", `mutate: [{workloads: {hostUsers: false}}]`, ""},
		{"host users true replace", `mutate: [{action: replace, workloads: {hostUsers: true}}]`, ""},
		{"null is not a mutation", `mutate: [{workloads: {hostUsers: null}}]`, "rules[0].mutate[0].workloads: at least one"},
		{"deny all", `enforce: {action: deny, workloads: {tolerations: [{}], affinity: [{}], nodeSelector: [{}], topologySpreadConstraints: [{}]}}`, ""},
		{"empty lists", `enforce: {workloads: {tolerations: [], affinity: []}}`, ""},
		{"regex", `enforce: {workloads: {nodeSelector: [{key: {exp: '^example.com/'}}]}}`, ""},
		{"bad regex", `enforce: {workloads: {nodeSelector: [{key: {exp: '['}}]}}`, "nodeSelector[0].key.exp"},
		{"empty expression", `enforce: {workloads: {tolerations: [{key: {}}]}}`, "at least one"},
		{"bad toleration operator", `enforce: {workloads: {tolerations: [{operators: [In]}]}}`, "operators"},
		{"bad effect", `enforce: {workloads: {tolerations: [{effects: [Bad]}]}}`, "effects"},
		{"inverted duration", `enforce: {workloads: {tolerations: [{tolerationSeconds: {min: 5, max: 1}}]}}`, "min must not exceed"},
		{"negative duration", `enforce: {workloads: {tolerations: [{tolerationSeconds: {max: -1}}]}}`, "outside"},
		{"zero skew", `enforce: {workloads: {topologySpreadConstraints: [{maxSkew: {max: 0}}]}}`, "maxSkew"},
		{"Pod selectors cannot use Gt", `enforce: {workloads: {topologySpreadConstraints: [{labelSelector: {requirements: [{operators: [Gt]}]}}]}}`, "operators"},
		{"bad affinity type", `enforce: {workloads: {affinity: [{types: [bad]}]}}`, "types"},
		{"incompatible topology", `enforce: {workloads: {affinity: [{types: [nodeAffinity], topologyKey: {exact: [zone]}}]}}`, "incompatible"},
		{"required weight", `enforce: {workloads: {affinity: [{modes: [required], weight: {max: 50}}]}}`, "weight requires"},
		{"unscoped weight", `enforce: {workloads: {affinity: [{weight: {max: 50}}]}}`, "weight requires"},
		{"preferred weight", `enforce: {workloads: {affinity: [{modes: [preferred], weight: {max: 100}}]}}`, ""},
		{"weight out of range", `enforce: {workloads: {affinity: [{modes: [preferred], weight: {max: 101}}]}}`, "outside"},
		{"incompatible namespace selector", `enforce: {workloads: {affinity: [{namespaceScope: SameNamespace, namespaceSelector: {}}]}}`, "SameNamespace"},
		{"bad ensured key", `mutate: [{workloads: {nodeSelector: {'bad key': shared}}}]`, "invalid label key"},
		{"bad ensured wildcard", `mutate: [{workloads: {tolerations: [{operator: Equal}]}}]`, "empty key"},
		{"Exists with value", `mutate: [{workloads: {tolerations: [{key: pool, operator: Exists, value: shared}]}}]`, "empty value"},
		{"seconds with wrong effect", `mutate: [{workloads: {tolerations: [{key: pool, operator: Exists, effect: NoSchedule, tolerationSeconds: 30}]}}]`, "NoExecute"},
		{"bad spread", `mutate: [{workloads: {topologySpreadConstraints: [{topologyKey: zone, maxSkew: 0, whenUnsatisfiable: DoNotSchedule}]}}]`, "maxSkew"},
		{"duplicate spread", `mutate: [{workloads: {topologySpreadConstraints: [{topologyKey: zone, maxSkew: 1, whenUnsatisfiable: DoNotSchedule}, {topologyKey: zone, maxSkew: 2, whenUnsatisfiable: DoNotSchedule}]}}]`, "duplicate"},
		{"dynamic selector absent", `mutate: [{workloads: {topologySpreadConstraints: [{topologyKey: zone, maxSkew: 1, whenUnsatisfiable: DoNotSchedule, matchLabelKeys: [app]}]}}]`, "require labelSelector"},
		{"dynamic overlap", `mutate: [{workloads: {topologySpreadConstraints: [{topologyKey: zone, maxSkew: 1, whenUnsatisfiable: DoNotSchedule, labelSelector: {matchLabels: {app: checkout}}, matchLabelKeys: [app]}]}}]`, "overlapping"},
		{"bad ensured affinity", `mutate: [{workloads: {affinity: {nodeAffinity: {requiredDuringSchedulingIgnoredDuringExecution: {nodeSelectorTerms: [{matchExpressions: [{key: pool, operator: Wrong}]}]}}}}}]`, "operator"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &rules.NamespaceRuleBodyNamespace{}
			if err := yaml.UnmarshalStrict([]byte(tc.source), body); err != nil {
				t.Fatal(err)
			}
			err := ValidateRuleStatusBody(nil, []*rules.NamespaceRuleBodyNamespace{body})
			if tc.errorText == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.errorText) {
				t.Fatalf("error = %v, want %q", err, tc.errorText)
			}
		})
	}
}
