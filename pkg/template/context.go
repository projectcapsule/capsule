// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"text/template"

	"github.com/projectcapsule/capsule/pkg/api/misc"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Additional Context to enhance templating
// +kubebuilder:object:generate=true
type TemplateContext struct {
	Resources []*TemplateResourceReference `json:"resources,omitempty"`
}

// +kubebuilder:object:generate=true
type TemplateResourceReference struct {
	misc.ResourceReference `json:",inline"`

	// Index to mount the resource in the template context
	Index string `json:"index,omitempty"`
}

func (t *TemplateContext) GatherContext(
	ctx context.Context,
	kubeClient client.Client,
	data map[string]interface{},
	namespace string,
) (context ReferenceContext, errors []error) {
	context = ReferenceContext{}

	if t.Resources == nil {
		return
	}

	// Template Context for Tenant
	if len(data) != 0 {
		if err := t.selfTemplate(data); err != nil {
			return context, []error{fmt.Errorf("cloud not template: %w", err)}
		}
	}

	// Load external Resources
	for index, resource := range t.Resources {
		res, err := resource.LoadResources(ctx, kubeClient, namespace)
		if err != nil {
			errors = append(errors, err)

			continue
		}

		if len(res) > 0 {
			resourceIndex := resource.Index
			if resourceIndex == "" {
				resourceIndex = string(index)
			}

			for _, u := range res {
				SanitizeUnstructured(u, DefaultSanitizeUnstructuredOptions())

			}

			context[resource.Index] = res
		}
	}

	return
}

// Templates itself with the option to populate tenant fields
// this can be useful if you have per tenant items, that you want to interact with
func (t *TemplateContext) selfTemplate(
	data map[string]interface{},
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
type ReferenceContext map[string]interface{}

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
