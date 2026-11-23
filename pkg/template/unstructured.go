// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"sync"
	"text/template"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/lru"

	"github.com/projectcapsule/capsule/pkg/template/functions"
)

// Keep parsed generators separate from admission rule templates so background
// replication churn cannot evict admission's working set. Only immutable parse
// trees are retained: context, random/time functions and output are always fresh.
var generatorTemplates = struct {
	build sync.Mutex
	cache *lru.Cache
}{cache: lru.New(256)}

type generatorTemplateKey struct {
	source     [sha256.Size]byte
	missingKey MissingKeyOption
}

func prepareGenerator(key MissingKeyOption, source string) (*template.Template, error) {
	compile := func() (*template.Template, error) {
		return template.New("tpl").Option("missingkey=" + key.String()).Funcs(functions.ExtraFuncMap()).Parse(source)
	}
	if len(source) > maxCachedRuleSourceSize {
		return compile()
	}

	cacheKey := generatorTemplateKey{source: sha256.Sum256([]byte(source)), missingKey: key}
	if cached, ok := generatorTemplates.cache.Get(cacheKey); ok {
		if prepared, ok := cached.(*template.Template); ok {
			return prepared, nil
		}
	}

	generatorTemplates.build.Lock()
	defer generatorTemplates.build.Unlock()

	if cached, ok := generatorTemplates.cache.Get(cacheKey); ok {
		if prepared, ok := cached.(*template.Template); ok {
			return prepared, nil
		}
	}

	prepared, err := compile()
	if err == nil {
		generatorTemplates.cache.Add(cacheKey, prepared)
	}

	return prepared, err
}

// RenderUnstructuredItems attempts to render a given string template into a list of unstructured resources.
func RenderUnstructuredItems(
	context ReferenceContext,
	key MissingKeyOption,
	tplString string,
) (items []*unstructured.Unstructured, err error) {
	tmpl, err := prepareGenerator(key, tplString)
	if err != nil {
		return
	}

	var rendered bytes.Buffer
	if err = tmpl.Execute(&rendered, context); err != nil {
		return
	}

	dec := kyaml.NewYAMLOrJSONDecoder(bytes.NewReader(rendered.Bytes()), 4096)

	var out []*unstructured.Unstructured

	for {
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			// Skip pure whitespace/--- separators that decode to nil/empty.
			return nil, fmt.Errorf("decode yaml: %w\nrendered template:\n%s", err, withLineNumbers(rendered.String()))
		}

		if len(obj) == 0 {
			continue
		}

		u := &unstructured.Unstructured{Object: obj}
		if u.GetAPIVersion() == "" && u.GetKind() == "" {
			continue
		}

		out = append(out, u)
	}

	return out, nil
}
