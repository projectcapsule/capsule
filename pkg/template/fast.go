// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/valyala/fasttemplate"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

var AllowedNamespaceMetadataTemplates = sets.New[string](
	"tenant.name",
	"namespace",
)

var FastTemplateExpression = regexp.MustCompile(`{{\s*([^{}]+)\s*}}`)

func ValidateKubernetesStringOrAllowedTemplates(
	fieldPath string,
	value string,
	validate func(string) []string,
) []string {
	checkValue, errs := validateAllowedTemplatesAndReplace(fieldPath, value)
	if len(errs) > 0 {
		return errs
	}

	return prefixValidationErrors(fieldPath, validate(checkValue))
}

func ValidateAllowedTemplatesOnly(
	fieldPath string,
	value string,
) []string {
	_, errs := validateAllowedTemplatesAndReplace(fieldPath, value)

	return errs
}

func validateAllowedTemplatesAndReplace(
	fieldPath string,
	value string,
) (string, []string) {
	if !ContainsFastTemplateSyntax(value) {
		return value, nil
	}

	matches := FastTemplateExpression.FindAllStringSubmatch(value, -1)
	if len(matches) == 0 {
		return value, []string{
			fmt.Sprintf("%s: malformed template %q", fieldPath, value),
		}
	}

	checkValue := value

	for _, match := range matches {
		raw := match[0]
		name := strings.TrimSpace(match[1])

		if !AllowedNamespaceMetadataTemplates.Has(name) {
			return value, []string{
				fmt.Sprintf(
					"%s: unsupported template %q in %q, allowed templates are {{tenant.name}} and {{namespace}}",
					fieldPath,
					name,
					value,
				),
			}
		}

		checkValue = strings.ReplaceAll(checkValue, raw, "template")
	}

	if strings.Contains(checkValue, "{{") || strings.Contains(checkValue, "}}") {
		return value, []string{
			fmt.Sprintf("%s: malformed template %q", fieldPath, value),
		}
	}

	return checkValue, nil
}

func prefixValidationErrors(fieldPath string, messages []string) []string {
	if len(messages) == 0 {
		return nil
	}

	errs := make([]string, 0, len(messages))

	for _, msg := range messages {
		errs = append(errs, fmt.Sprintf("%s: %s", fieldPath, msg))
	}

	return errs
}

func ContainsFastTemplateSyntax(value string) bool {
	return strings.Contains(value, "{{") || strings.Contains(value, "}}")
}

// RequiresFastTemplate evaluates if given string requires templating.
func RequiresFastTemplate(value string) bool {
	return strings.Contains(value, "{{") && strings.Contains(value, "}}")
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

	out := make(map[string]string, len(m))

	for k, v := range m {
		out[FastTemplate(k, templateContext)] = FastTemplate(v, templateContext)
	}

	return out
}

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

		if slices.ContainsFunc(expr.Values, RequiresFastTemplate) {
			return true
		}
	}

	return false
}

func FastTemplateLabelSelector(
	in *metav1.LabelSelector,
	templateContext map[string]string,
) (*metav1.LabelSelector, error) {
	if in == nil {
		return nil, nil
	}

	out := in.DeepCopy()
	out.MatchLabels = FastTemplateMap(in.MatchLabels, templateContext)

	for i := range out.MatchExpressions {
		out.MatchExpressions[i].Key = FastTemplate(out.MatchExpressions[i].Key, templateContext)

		for j := range out.MatchExpressions[i].Values {
			out.MatchExpressions[i].Values[j] = FastTemplate(out.MatchExpressions[i].Values[j], templateContext)
		}
	}

	if _, err := metav1.LabelSelectorAsSelector(out); err != nil {
		return nil, fmt.Errorf("templated label selector is invalid: %w", err)
	}

	return out, nil
}
