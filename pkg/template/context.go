// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/runtime/sanitize"
)

// Additional Context to enhance templating
// +kubebuilder:object:generate=true
type TemplateContext struct {
	Resources []*TemplateResourceReference `json:"resources,omitempty"`
}

// ValidateVariables ensures every template expression in a resource reference
// can be resolved before the reference is loaded.
func (t *TemplateContext) ValidateVariables(values map[string]string) error {
	if t == nil {
		return nil
	}

	var errs []error

	for index, resource := range t.Resources {
		if resource == nil {
			continue
		}

		prefix := fmt.Sprintf("resources[%d]", index)
		errs = append(errs, validateReferenceVariables(prefix+".name", resource.Name, values)...)
		errs = append(errs, validateReferenceVariables(prefix+".namespace", resource.Namespace, values)...)

		if resource.Selector == nil {
			continue
		}

		for key, value := range resource.Selector.MatchLabels {
			errs = append(errs, validateReferenceVariables(prefix+".selector.matchLabels key", key, values)...)
			errs = append(errs, validateReferenceVariables(prefix+".selector.matchLabels."+key, value, values)...)
		}
		for expressionIndex, expression := range resource.Selector.MatchExpressions {
			field := fmt.Sprintf("%s.selector.matchExpressions[%d]", prefix, expressionIndex)
			errs = append(errs, validateReferenceVariables(field+".key", expression.Key, values)...)
			for valueIndex, value := range expression.Values {
				errs = append(errs, validateReferenceVariables(
					fmt.Sprintf("%s.values[%d]", field, valueIndex),
					value,
					values,
				)...)
			}
		}
	}

	return errors.Join(errs...)
}

func validateReferenceVariables(field, value string, variables map[string]string) []error {
	if !ContainsFastTemplateSyntax(value) {
		return nil
	}

	matches := FastTemplateExpression.FindAllStringSubmatch(value, -1)
	if len(matches) == 0 {
		return []error{fmt.Errorf("%s contains a malformed template %q", field, value)}
	}

	remaining := value
	var errs []error

	for _, match := range matches {
		key := FastTemplateNormalize(match[1])
		if _, found := variables[key]; !found {
			errs = append(errs, fmt.Errorf("%s references undefined variable %q", field, key))
		}

		remaining = strings.ReplaceAll(remaining, match[0], "")
	}

	if ContainsFastTemplateSyntax(remaining) {
		errs = append(errs, fmt.Errorf("%s contains a malformed template %q", field, value))
	}

	return errs
}

func (t *TemplateContext) GatherContext(
	ctx context.Context,
	kubeClient client.Client,
	restMapper k8smeta.RESTMapper,
	templateContext map[string]string,
	namespace string,
	additionSelectors []labels.Selector,
	validateNamespace NamespaceValidator,
) (ReferenceContext, error) {
	result := ReferenceContext{}

	if t.Resources == nil {
		return result, nil
	}

	var errs []error

	// Load external resources
	for index, resource := range t.Resources {
		res, err := resource.LoadResources(
			ctx,
			kubeClient,
			restMapper,
			namespace,
			additionSelectors,
			templateContext,
			true,
			validateNamespace,
		)
		if err != nil {
			errs = append(errs, err)

			continue
		}

		if len(res) == 0 {
			continue
		}

		resourceIndex := resource.Index
		if resourceIndex == "" {
			resourceIndex = strconv.Itoa(index)
		}

		items := make([]map[string]any, 0, len(res))

		for _, u := range res {
			sanitize.SanitizeUnstructured(u, sanitize.DefaultSanitizeOptions())

			items = append(items, u.UnstructuredContent())
		}

		result[resourceIndex] = items
	}

	return result, errors.Join(errs...)
}

// +kubebuilder:object:generate=false
type ReferenceContext map[string]any

func (t *ReferenceContext) String() (string, error) {
	dataBytes, err := json.Marshal(t)
	if err != nil {
		return "", fmt.Errorf("error marshaling TemplateContext: %w", err)
	}

	if err := json.Unmarshal(dataBytes, t); err != nil {
		return "", fmt.Errorf("error unmarshaling TemplateContext into map: %w", err)
	}

	return string(dataBytes), nil
}
