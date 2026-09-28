// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func cachedRuleBodies(value string) []*rules.NamespaceRuleBodyNamespace {
	return []*rules.NamespaceRuleBodyNamespace{{Enforce: &rules.NamespaceRuleEnforceBody{
		Metadata: []rules.MetadataRule{{Labels: map[string]rules.MetadataValueRule{
			"example.com/owner": {Managed: &value},
		}}},
	}}}
}

func TestPreparedStaticRulesPreserveRendering(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "yes", "null", "0123", "1.5", "line one\nline two", "<>&", "${value}"} {
		t.Run(value, func(t *testing.T) {
			bodies := cachedRuleBodies(value)
			raw, err := yaml.Marshal(bodies)
			if err != nil {
				t.Fatal(err)
			}
			rendered, err := RenderTemplateBytes(nil, MissingKeyError, raw)
			if err != nil {
				t.Fatal(err)
			}
			var want []*rules.NamespaceRuleBodyNamespace
			if err := yaml.Unmarshal(rendered, &want); err != nil {
				t.Fatal(err)
			}

			prepared, err := PrepareNamespaceRuleBodies(MissingKeyError, bodies)
			if err != nil {
				t.Fatal(err)
			}
			if prepared.NeedsContext() {
				t.Fatal("static rules request a context")
			}
			for range 2 {
				got, err := prepared.Render(nil)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("got %#v, want %#v", got, want)
				}
				*got[0].Enforce.Metadata[0].Labels["example.com/owner"].Managed = "changed by caller"
			}
		})
	}
}

func TestPreparedRulesReuseParsingWithFreshContext(t *testing.T) {
	t.Parallel()

	bodies := cachedRuleBodies("{{ .name }}")
	prepared, err := PrepareNamespaceRuleBodies(MissingKeyError, bodies)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := PrepareNamespaceRuleBodies(MissingKeyError, bodies)
	if err != nil {
		t.Fatal(err)
	}
	if cached != prepared {
		t.Fatal("unchanged template was parsed again")
	}

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			name := fmt.Sprintf("namespace-%d", i)
			got, err := prepared.Render(map[string]any{"name": name})
			if err != nil {
				t.Error(err)
				return
			}
			if value := *got[0].Enforce.Metadata[0].Labels["example.com/owner"].Managed; value != name {
				t.Errorf("value = %q, want %q", value, name)
			}
		})
	}
	wg.Wait()
	if _, err := prepared.Render(map[string]any{}); err == nil {
		t.Fatal("cached template suppressed missing-key error")
	}

	other, err := PrepareNamespaceRuleBodies(MissingKeyZero, bodies)
	if err != nil {
		t.Fatal(err)
	}
	if other == prepared {
		t.Fatal("missing-key option not included in cache key")
	}
	if _, err := other.Render(map[string]any{}); err != nil {
		t.Fatal(err)
	}

	changed, err := PrepareNamespaceRuleBodies(MissingKeyError, cachedRuleBodies("changed"))
	if err != nil {
		t.Fatal(err)
	}
	if changed == prepared {
		t.Fatal("changed rules reused the old template")
	}
}

func TestPreparedRulesStillExecuteValueGenerators(t *testing.T) {
	t.Parallel()

	prepared, err := PrepareNamespaceRuleBodies(MissingKeyError, cachedRuleBodies("{{ randAlphaNum 32 }}"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := prepared.Render(nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := prepared.Render(nil)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(first, second) {
		t.Fatal("generated template output was cached")
	}
}
