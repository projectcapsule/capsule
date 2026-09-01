// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	gotemplate "text/template"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8svalidation "k8s.io/apimachinery/pkg/util/validation"

	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/template/functions"
)

// validateJSONSchemaFormExtensions validates every x-capsule-form extension in
// a JSON Schema. It deliberately validates the contract syntactically without
// resolving the GVK: discovery remains a form-consumer concern and a template
// may legitimately be installed before the referenced API is available.
func validateJSONSchemaFormExtensions(schemaData []byte) error {
	var root any
	if err := json.Unmarshal(schemaData, &root); err != nil {
		return err
	}

	return walkJSONSchemaNodes(root, "$", func(schemaNode map[string]any, path string) error {
		if extension, found := schemaNode[apiruntime.JSONSchemaFormExtensionKey]; found {
			if err := validateJSONSchemaFormExtension(extension); err != nil {
				return fmt.Errorf("%s.%s is invalid: %w", path, apiruntime.JSONSchemaFormExtensionKey, err)
			}
		}

		return nil
	})
}

func validateJSONSchemaFormExtension(raw any) error {
	data, err := json.Marshal(raw)
	if err != nil {
		return err
	}

	extension := &apiruntime.JSONSchemaFormExtension{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(extension); err != nil {
		return fmt.Errorf("decode extension: %w", err)
	}

	if extension.Widget != apiruntime.JSONSchemaFormWidgetKubernetesResource {
		return fmt.Errorf("widget must be %q", apiruntime.JSONSchemaFormWidgetKubernetesResource)
	}

	if extension.Source == nil {
		return fmt.Errorf("source is required")
	}

	if err := validateKubernetesResourceFormSource(extension.Source); err != nil {
		return fmt.Errorf("source: %w", err)
	}

	if err := validateKubernetesResourceFormOption(extension.Option); err != nil {
		return fmt.Errorf("option: %w", err)
	}

	return nil
}

func validateKubernetesResourceFormSource(source *apiruntime.KubernetesResourceFormSource) error {
	if source.APIVersion == "" {
		return fmt.Errorf("apiVersion is required")
	}

	if _, err := schema.ParseGroupVersion(source.APIVersion); err != nil {
		return fmt.Errorf("apiVersion %q is invalid: %w", source.APIVersion, err)
	}

	if strings.TrimSpace(source.Kind) == "" {
		return fmt.Errorf("kind is required")
	}

	if source.Kind == "*" {
		return fmt.Errorf("kind must identify one concrete resource kind")
	}

	if source.Namespace != "" &&
		source.Namespace != apiruntime.JSONSchemaFormRequestNamespace &&
		source.Namespace != apiruntime.JSONSchemaFormAllNamespaces {
		if errors := k8svalidation.IsDNS1123Label(source.Namespace); len(errors) > 0 {
			return fmt.Errorf("namespace %q is invalid: %s", source.Namespace, strings.Join(errors, ", "))
		}
	}

	if source.LabelSelector != "" {
		if _, err := labels.Parse(source.LabelSelector); err != nil {
			return fmt.Errorf("labelSelector is invalid: %w", err)
		}
	}

	if source.FieldSelector != "" {
		if _, err := fields.ParseSelector(source.FieldSelector); err != nil {
			return fmt.Errorf("fieldSelector is invalid: %w", err)
		}
	}

	return nil
}

func validateKubernetesResourceFormOption(option *apiruntime.KubernetesResourceFormOption) error {
	if option == nil {
		return nil
	}

	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "labelTemplate", value: option.LabelTemplate},
		{name: "valueTemplate", value: option.ValueTemplate},
	} {
		if field.value == "" {
			field.value = apiruntime.JSONSchemaFormDefaultOptionTemplate
		}

		if _, err := gotemplate.New(field.name).
			Option("missingkey=error").
			Funcs(functions.ExtraFuncMap()).
			Parse(field.value); err != nil {
			return fmt.Errorf("%s is invalid: %w", field.name, err)
		}
	}

	return nil
}
