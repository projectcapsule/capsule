// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"text/template"

	"k8s.io/utils/lru"
	"sigs.k8s.io/yaml"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/template/functions"
)

// Bound both the number and source size of cached rule sets. Admission requests
// can contain rules that are never persisted.
const maxCachedRuleSourceSize = 64 * 1024

var namespaceRuleTemplates = lru.New(256)

type namespaceRuleTemplateKey struct {
	source     [sha256.Size]byte
	missingKey MissingKeyOption
}

// NamespaceRuleTemplate is immutable and can be rendered concurrently. Static
// rules retain their normalized representation; dynamic rules retain only the
// parsed template, so changing inputs and time/random functions remain live.
type NamespaceRuleTemplate struct {
	static []*rules.NamespaceRuleBodyNamespace
	tmpl   *template.Template
}

// PrepareNamespaceRuleBodies prepares the complete ordered rule set together,
// preserving template definitions and variables shared across rule bodies.
func PrepareNamespaceRuleBodies(key MissingKeyOption, bodies []*rules.NamespaceRuleBodyNamespace) (*NamespaceRuleTemplate, error) {
	if len(bodies) == 0 {
		return &NamespaceRuleTemplate{}, nil
	}

	source, err := json.Marshal(bodies)
	if err != nil {
		return nil, fmt.Errorf("marshal namespace rule bodies: %w", err)
	}

	cacheKey := namespaceRuleTemplateKey{source: sha256.Sum256(source), missingKey: key}
	if cached, ok := namespaceRuleTemplates.Get(cacheKey); ok {
		if prepared, ok := cached.(*NamespaceRuleTemplate); ok {
			return prepared, nil
		}
	}

	prepared := &NamespaceRuleTemplate{}
	if !bytes.Contains(source, []byte("{{")) {
		// Use the same JSON normalization as yaml.Marshal without the YAML and
		// template round trips. Return copies from Render, never cached objects.
		if err := json.Unmarshal(source, &prepared.static); err != nil {
			return nil, fmt.Errorf("unmarshal namespace rule bodies: %w", err)
		}
	} else {
		raw, err := yaml.JSONToYAML(source)
		if err != nil {
			return nil, fmt.Errorf("marshal namespace rule bodies: %w", err)
		}

		prepared.tmpl, err = template.New("tpl").
			Option("missingkey=" + key.String()).
			Funcs(functions.ExtraFuncMap()).
			Parse(string(raw))
		if err != nil {
			return nil, fmt.Errorf("render namespace rule bodies template: parse template: %w", err)
		}
	}

	if len(source) <= maxCachedRuleSourceSize {
		namespaceRuleTemplates.Add(cacheKey, prepared)
	}

	return prepared, nil
}

// NeedsContext reports whether execution of a template is necessary.
func (t *NamespaceRuleTemplate) NeedsContext() bool {
	return t.tmpl != nil
}

func RenderNamespaceRuleBodies(
	context map[string]any,
	key MissingKeyOption,
	bodies []*rules.NamespaceRuleBodyNamespace,
) ([]*rules.NamespaceRuleBodyNamespace, error) {
	prepared, err := PrepareNamespaceRuleBodies(key, bodies)
	if err != nil {
		return nil, err
	}

	return prepared.Render(context)
}

func (t *NamespaceRuleTemplate) Render(context map[string]any) ([]*rules.NamespaceRuleBodyNamespace, error) {
	if t.tmpl == nil {
		var out []*rules.NamespaceRuleBodyNamespace
		if t.static != nil {
			out = make([]*rules.NamespaceRuleBodyNamespace, len(t.static))
			for i, body := range t.static {
				out[i] = body.DeepCopy()
			}
		}

		return out, nil
	}

	var rendered bytes.Buffer
	if err := t.tmpl.Execute(&rendered, context); err != nil {
		return nil, fmt.Errorf("render namespace rule bodies template: execute template: %w", err)
	}

	var out []*rules.NamespaceRuleBodyNamespace
	if err := yaml.Unmarshal(rendered.Bytes(), &out); err != nil {
		return nil, fmt.Errorf(
			"unmarshal rendered namespace rule bodies: %w\nrendered template:\n%s",
			err,
			withLineNumbers(rendered.String()),
		)
	}

	return out, nil
}
