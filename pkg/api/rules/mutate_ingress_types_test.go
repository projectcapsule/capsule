// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIngressMutationPresenceAndDeepCopy(t *testing.T) {
	for _, source := range []string{
		`{}`,
		`{"listenerSets":{"parentRef":{"name":"edge"}}}`,
		`{"listenerSets":{"parentRef":{"name":"edge","namespace":"platform","group":"gateway.networking.k8s.io","kind":"Gateway"}}}`,
		`{"listenerSets":{"parentRef":{"name":"edge","group":"","kind":"Service"}}}`,
	} {
		t.Run(source, func(t *testing.T) {
			var original IngressMutation
			require.NoError(t, json.Unmarshal([]byte(source), &original))
			copy := original.DeepCopy()
			encoded, err := json.Marshal(copy)
			require.NoError(t, err)
			require.JSONEq(t, source, string(encoded))
			if ref := copy.ListenerSets.ParentRef; ref != nil {
				ref.Name = "changed"
				for _, field := range []**string{&ref.Namespace, &ref.Group, &ref.Kind} {
					if *field != nil {
						**field = "changed"
					}
				}
				require.Equal(t, "edge", original.ListenerSets.ParentRef.Name)
				encoded, err = json.Marshal(original)
				require.NoError(t, err)
				require.JSONEq(t, source, string(encoded))
			}
		})
	}
	for _, source := range []string{`{}`, `{"ingress":null}`, `{"ingress":{}}`, `{"ingress":{"listenerSets":{"parentRef":null}}}`} {
		var mutation NamespaceRuleMutation
		require.NoError(t, json.Unmarshal([]byte(source), &mutation))
		require.Nil(t, mutation.Ingress.ListenerSets.ParentRef)
		encoded, err := json.Marshal(mutation)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "ingress")
	}
	var ingress IngressMutation
	require.Error(t, json.Unmarshal([]byte(`{"listenerSets":{"parentRef":{"namespace":false}}}`), &ingress))
}
