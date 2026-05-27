// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

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
