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
		{"pull Always", `mutate: [{workloads: {registries: {imagePullPolicy: Always}}}]`, ""},
		{"pull IfNotPresent", `mutate: [{workloads: {targets: [pod/initcontainers], registries: {imagePullPolicy: IfNotPresent}}}]`, ""},
		{"pull Never", `mutate: [{action: replace, workloads: {targets: [pod/ephemeralcontainers], registries: {imagePullPolicy: Never}}}]`, ""},
		{"pull invalid", `mutate: [{workloads: {registries: {imagePullPolicy: Sometimes}}}]`, "rules[0].mutate[0].workloads.registries.imagePullPolicy"},
		{"pull wrong case", `mutate: [{workloads: {registries: {imagePullPolicy: always}}}]`, "unsupported pull policy"},
		{"pull blank", `mutate: [{workloads: {registries: {imagePullPolicy: ' '}}}]`, "unsupported pull policy"},
		{"registries empty", `mutate: [{workloads: {registries: {}}}]`, "at least one"},
		{"registries null", `mutate: [{workloads: {registries: null}}]`, "at least one"},
		{"pull null", `mutate: [{workloads: {registries: {imagePullPolicy: null}}}]`, "at least one"},
		{"pull unsupported target", `mutate: [{workloads: {targets: [deployment], registries: {imagePullPolicy: Always}}}]`, "unsupported workload mutation target"},
		{"pull secrets", `mutate: [{workloads: {registries: {imagePullSecrets: [{name: registry.example}]}}}]`, ""},
		{"pull secrets empty", `mutate: [{action: replace, workloads: {registries: {imagePullSecrets: []}}}]`, ""},
		{"pull secrets null", `mutate: [{workloads: {registries: {imagePullSecrets: null}}}]`, "at least one"},
		{"pull secrets missing name", `mutate: [{workloads: {registries: {imagePullSecrets: [{}]}}}]`, "imagePullSecrets[0].name"},
		{"pull secrets invalid name", `mutate: [{workloads: {registries: {imagePullSecrets: [{name: INVALID}]}}}]`, "invalid secret name"},
		{"pull secrets cross namespace", `mutate: [{workloads: {registries: {imagePullSecrets: [{name: other/secret}]}}}]`, "invalid secret name"},
		{"pull secrets duplicate", `mutate: [{workloads: {registries: {imagePullSecrets: [{name: pull}, {name: pull}]}}}]`, "duplicate secret name"},
		{"pull secrets replace duplicate", `mutate: [{action: replace, workloads: {registries: {imagePullSecrets: [{name: pull}, {name: pull}]}}}]`, "duplicate secret name"},
		{"pull secrets container target", `mutate: [{workloads: {targets: [pod/containers], registries: {imagePullSecrets: [{name: pull}]}}}]`, "Pod-level mutation properties require the pod target"},
		{"pull secrets empty ephemeral target", `mutate: [{workloads: {targets: [pod/ephemeralcontainers], registries: {imagePullSecrets: []}}}]`, "Pod-level mutation properties require the pod target"},
		{"pull secrets template", `mutate: [{workloads: {registries: {imagePullSecrets: [{name: '{{ .Tenant.Name }}-registry'}]}}}]`, ""},
		{"read only false", `mutate: [{workloads: {readOnlyRootFilesystem: false}}]`, ""},
		{"read only pod selects all containers", `mutate: [{workloads: {targets: [pod], readOnlyRootFilesystem: true}}]`, ""},
		{"read only init", `mutate: [{workloads: {targets: [pod/initcontainers], readOnlyRootFilesystem: true}}]`, ""},
		{"read only ephemeral", `mutate: [{action: replace, workloads: {targets: [pod/ephemeralcontainers], readOnlyRootFilesystem: false}}]`, ""},
		{"read only null", `mutate: [{workloads: {readOnlyRootFilesystem: null}}]`, "at least one"},
		{"targets alone", `mutate: [{workloads: {targets: [pod]}}]`, "at least one"},
		{"mutation volumes", `mutate: [{workloads: {targets: [pod/volumes], readOnlyRootFilesystem: true}}]`, "unsupported workload mutation target"},
		{"mutation controller", `mutate: [{workloads: {targets: [deployment], readOnlyRootFilesystem: true}}]`, "unsupported workload mutation target"},
		{"mutation duplicate targets", `mutate: [{workloads: {targets: [pod, pod], readOnlyRootFilesystem: true}}]`, "duplicate target"},
		{"mutation excessive targets", `mutate: [{workloads: {targets: [pod, pod, pod, pod, pod], readOnlyRootFilesystem: true}}]`, "at most 4"},
		{"pod property needs pod", `mutate: [{workloads: {targets: [pod/containers], hostUsers: false, readOnlyRootFilesystem: true}}]`, "Pod-level mutation properties require the pod target"},
		{"pod profiles need pod", `mutate: [{workloads: {targets: [pod/initcontainers], seccompProfile: {type: RuntimeDefault}}}]`, "Pod-level mutation properties require the pod target"},
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
