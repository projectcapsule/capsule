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
		{"empty groups", `mutate: [{workloads: {placement: {}, security: {}}}]`, "at least one"},
		{"null groups", `mutate: [{workloads: {placement: null, security: null}}]`, "at least one"},
		{"ensure only", `mutate: [{workloads: {placement: {nodeSelector: {pool: shared}}}}]`, ""},
		{"scheduler only", `mutate: [{workloads: {placement: {scheduler: tenant-scheduler}}}]`, ""},
		{"scheduler replace", `mutate: [{action: replace, workloads: {placement: {scheduler: default-scheduler}}}]`, ""},
		{"scheduler null", `mutate: [{workloads: {placement: {scheduler: null}}}]`, "at least one"},
		{"scheduler empty", `mutate: [{workloads: {placement: {scheduler: ''}}}]`, "at least one"},
		{"scheduler blank", `mutate: [{workloads: {placement: {scheduler: '   '}}}]`, "rules[0].mutate[0].workloads.placement.scheduler: scheduler name must not be blank"},
		{"host users false only", `mutate: [{workloads: {security: {hostUsers: false}}}]`, ""},
		{"host users true replace", `mutate: [{action: replace, workloads: {security: {hostUsers: true}}}]`, ""},
		{"read only false", `mutate: [{workloads: {security: {readOnlyRootFilesystem: false}}}]`, ""},
		{"read only pod selects all containers", `mutate: [{workloads: {targets: [pod], security: {readOnlyRootFilesystem: true}}}]`, ""},
		{"read only init", `mutate: [{workloads: {targets: [pod/initcontainers], security: {readOnlyRootFilesystem: true}}}]`, ""},
		{"read only ephemeral", `mutate: [{action: replace, workloads: {targets: [pod/ephemeralcontainers], security: {readOnlyRootFilesystem: false}}}]`, ""},
		{"read only null", `mutate: [{workloads: {security: {readOnlyRootFilesystem: null}}}]`, "at least one"},
		{"targets alone", `mutate: [{workloads: {targets: [pod]}}]`, "at least one"},
		{"mutation volumes", `mutate: [{workloads: {targets: [pod/volumes], security: {readOnlyRootFilesystem: true}}}]`, "unsupported workload mutation target"},
		{"mutation controller", `mutate: [{workloads: {targets: [deployment], security: {readOnlyRootFilesystem: true}}}]`, "unsupported workload mutation target"},
		{"mutation duplicate targets", `mutate: [{workloads: {targets: [pod, pod], security: {readOnlyRootFilesystem: true}}}]`, "duplicate target"},
		{"mutation excessive targets", `mutate: [{workloads: {targets: [pod, pod, pod, pod, pod], security: {readOnlyRootFilesystem: true}}}]`, "at most 4"},
		{"pod property needs pod", `mutate: [{workloads: {targets: [pod/containers], security: {hostUsers: false, readOnlyRootFilesystem: true}}}]`, "Pod-level mutation properties require the pod target"},
		{"pod profiles need pod", `mutate: [{workloads: {targets: [pod/initcontainers], security: {seccompProfile: {type: RuntimeDefault}}}}]`, "Pod-level mutation properties require the pod target"},
		{"null is not a mutation", `mutate: [{workloads: {security: {hostUsers: null}}}]`, "rules[0].mutate[0].workloads: at least one"},
		{"deny all", `enforce: {action: deny, workloads: {placement: {tolerations: [{}], affinity: [{}], nodeSelector: [{}], topologySpreadConstraints: [{}]}}}`, ""},
		{"empty lists", `enforce: {workloads: {placement: {tolerations: [], affinity: []}}}`, ""},
		{"regex", `enforce: {workloads: {placement: {nodeSelector: [{key: {exp: '^example.com/'}}]}}}`, ""},
		{"bad regex", `enforce: {workloads: {placement: {nodeSelector: [{key: {exp: '['}}]}}}`, "nodeSelector[0].key.exp"},
		{"empty expression", `enforce: {workloads: {placement: {tolerations: [{key: {}}]}}}`, "at least one"},
		{"bad toleration operator", `enforce: {workloads: {placement: {tolerations: [{operators: [In]}]}}}`, "operators"},
		{"bad effect", `enforce: {workloads: {placement: {tolerations: [{effects: [Bad]}]}}}`, "effects"},
		{"inverted duration", `enforce: {workloads: {placement: {tolerations: [{tolerationSeconds: {min: 5, max: 1}}]}}}`, "min must not exceed"},
		{"negative duration", `enforce: {workloads: {placement: {tolerations: [{tolerationSeconds: {max: -1}}]}}}`, "outside"},
		{"zero skew", `enforce: {workloads: {placement: {topologySpreadConstraints: [{maxSkew: {max: 0}}]}}}`, "maxSkew"},
		{"Pod selectors cannot use Gt", `enforce: {workloads: {placement: {topologySpreadConstraints: [{labelSelector: {requirements: [{operators: [Gt]}]}}]}}}`, "operators"},
		{"bad affinity type", `enforce: {workloads: {placement: {affinity: [{types: [bad]}]}}}`, "types"},
		{"incompatible topology", `enforce: {workloads: {placement: {affinity: [{types: [nodeAffinity], topologyKey: {exact: [zone]}}]}}}`, "incompatible"},
		{"required weight", `enforce: {workloads: {placement: {affinity: [{modes: [required], weight: {max: 50}}]}}}`, "weight requires"},
		{"unscoped weight", `enforce: {workloads: {placement: {affinity: [{weight: {max: 50}}]}}}`, "weight requires"},
		{"preferred weight", `enforce: {workloads: {placement: {affinity: [{modes: [preferred], weight: {max: 100}}]}}}`, ""},
		{"weight out of range", `enforce: {workloads: {placement: {affinity: [{modes: [preferred], weight: {max: 101}}]}}}`, "outside"},
		{"incompatible namespace selector", `enforce: {workloads: {placement: {affinity: [{namespaceScope: SameNamespace, namespaceSelector: {}}]}}}`, "SameNamespace"},
		{"bad ensured key", `mutate: [{workloads: {placement: {nodeSelector: {'bad key': shared}}}}]`, "invalid label key"},
		{"bad ensured wildcard", `mutate: [{workloads: {placement: {tolerations: [{operator: Equal}]}}}]`, "empty key"},
		{"Exists with value", `mutate: [{workloads: {placement: {tolerations: [{key: pool, operator: Exists, value: shared}]}}}]`, "empty value"},
		{"seconds with wrong effect", `mutate: [{workloads: {placement: {tolerations: [{key: pool, operator: Exists, effect: NoSchedule, tolerationSeconds: 30}]}}}]`, "NoExecute"},
		{"bad spread", `mutate: [{workloads: {placement: {topologySpreadConstraints: [{topologyKey: zone, maxSkew: 0, whenUnsatisfiable: DoNotSchedule}]}}}]`, "maxSkew"},
		{"duplicate spread", `mutate: [{workloads: {placement: {topologySpreadConstraints: [{topologyKey: zone, maxSkew: 1, whenUnsatisfiable: DoNotSchedule}, {topologyKey: zone, maxSkew: 2, whenUnsatisfiable: DoNotSchedule}]}}}]`, "duplicate"},
		{"dynamic selector absent", `mutate: [{workloads: {placement: {topologySpreadConstraints: [{topologyKey: zone, maxSkew: 1, whenUnsatisfiable: DoNotSchedule, matchLabelKeys: [app]}]}}}]`, "require labelSelector"},
		{"dynamic overlap", `mutate: [{workloads: {placement: {topologySpreadConstraints: [{topologyKey: zone, maxSkew: 1, whenUnsatisfiable: DoNotSchedule, labelSelector: {matchLabels: {app: checkout}}, matchLabelKeys: [app]}]}}}]`, "overlapping"},
		{"bad ensured affinity", `mutate: [{workloads: {placement: {affinity: {nodeAffinity: {requiredDuringSchedulingIgnoredDuringExecution: {nodeSelectorTerms: [{matchExpressions: [{key: pool, operator: Wrong}]}]}}}}}}]`, "operator"},
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
