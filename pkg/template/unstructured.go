// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"bytes"
	"fmt"
	"io"
	"text/template"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"
)

func RenderUnstructuredItems(
	context ReferenceContext,
	key MissingKeyOption,
	tplString string,
) (items []*unstructured.Unstructured, err error) {
	tmpl, err := template.New("tpl").Option("missingkey=" + key.String()).Funcs(ExtraFuncMap()).Parse(tplString)
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
			if err == io.EOF {
				break
			}
			// Skip pure whitespace/--- separators that decode to nil/empty
			return nil, fmt.Errorf("decode yaml: %w", err)
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
