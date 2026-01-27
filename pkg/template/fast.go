// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"fmt"
	"io"
	"maps"
	"strings"

	"github.com/valyala/fasttemplate"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RequiresFastTemplate evaluates if given string requires templating
func RequiresFastTemplate(
	template string,
) bool {
	if !strings.Contains(template, "{{") && !strings.Contains(template, "}}") {
		return false
	}

	return true
}

// FastTemplate applies templating to the provided string.
func FastTemplate(
	template string,
	templateContext map[string]string,
) string {
	if !RequiresFastTemplate(template) {
		return template
	}

	t := fasttemplate.New(template, "{{", "}}")

	return t.ExecuteFuncString(func(w io.Writer, tag string) (int, error) {
		key := strings.TrimSpace(tag)
		if v, ok := templateContext[key]; ok {
			return w.Write([]byte(v))
		}

		return 0, nil
	})
}

// FastTemplateMap applies templating to all values in the provided map in place.
func FastTemplateMap(
	m map[string]string,
	templateContext map[string]string,
) map[string]string {
	if len(m) == 0 {
		return map[string]string{}
	}

	out := maps.Clone(m)
	for k, v := range out {
		out[FastTemplate(k, templateContext)] = FastTemplate(v, templateContext)
	}

	return out
}

// FastTemplateMap evaluates if given LabelSelector requires templating
func SelectorRequiresTemplating(sel *metav1.LabelSelector) bool {
	if sel == nil {
		return false
	}
	for k, v := range sel.MatchLabels {
		if RequiresFastTemplate(k) || RequiresFastTemplate(v) {
			return true
		}
	}
	for _, expr := range sel.MatchExpressions {
		if RequiresFastTemplate(expr.Key) {
			return true
		}
		for _, v := range expr.Values {
			if RequiresFastTemplate(v) {
				return true
			}
		}
	}
	return false
}

// FastTemplateMap templates a Labelselector (all keys and values)
func FastTemplateLabelSelector(
	in *metav1.LabelSelector,
	templateContext map[string]string,
) (*metav1.LabelSelector, error) {
	if in == nil {
		return nil, nil
	}

	out := in.DeepCopy()

	out.MatchLabels = FastTemplateMap(out.MatchLabels, templateContext)

	for i := range out.MatchExpressions {
		out.MatchExpressions[i].Key = FastTemplate(out.MatchExpressions[i].Key, templateContext)

		for j := range out.MatchExpressions[i].Values {
			out.MatchExpressions[i].Values[j] =
				FastTemplate(out.MatchExpressions[i].Values[j], templateContext)
		}
	}

	if _, err := metav1.LabelSelectorAsSelector(out); err != nil {
		return nil, fmt.Errorf("templated label selector is invalid: %w", err)
	}

	return out, nil
}
