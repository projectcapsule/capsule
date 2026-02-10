// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"text/template"

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

func (t *TemplateContext) GatherContext(
	ctx context.Context,
	kubeClient client.Client,
	restMapper k8smeta.RESTMapper,
	data map[string]any,
	namespace string,
	additionSelectors []labels.Selector,
) (ReferenceContext, error) {
	result := ReferenceContext{}

	if t.Resources == nil {
		return result, nil
	}

	var errs []error

	// Template Context
	if len(data) != 0 {
		if err := t.selfTemplate(data); err != nil {
			return result, fmt.Errorf("could not template: %w", err)
		}
	}

	// Load external resources
	for index, resource := range t.Resources {
		res, err := resource.LoadResources(
			ctx,
			kubeClient,
			restMapper,
			namespace,
			additionSelectors,
			map[string]string{},
			true,
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

// Templates itself with the option to populate tenant fields.
func (t *TemplateContext) selfTemplate(
	data map[string]any,
) (err error) {
	dataBytes, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("error marshaling TemplateContext: %w", err)
	}

	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return fmt.Errorf("error unmarshaling TemplateContext into map: %w", err)
	}

	tmpl, err := template.New("tpl").Option("missingkey=error").Funcs(ExtraFuncMap()).Parse(string(dataBytes))
	if err != nil {
		return fmt.Errorf("error parsing template: %w", err)
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, data); err != nil {
		return fmt.Errorf("error executing template: %w", err)
	}

	tplContext := &TemplateContext{}
	if err := json.Unmarshal(rendered.Bytes(), tplContext); err != nil {
		return fmt.Errorf("error unmarshaling JSON into TemplateContext: %w", err)
	}

	// Reassing templated context
	*t = *tplContext

	return nil
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
