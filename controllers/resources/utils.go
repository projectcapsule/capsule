// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/configuration"
	tpl "github.com/projectcapsule/capsule/pkg/template"
	caputils "github.com/projectcapsule/capsule/pkg/utils"
)

func SetGlobalTenantResourceServiceAccount(
	config configuration.Configuration,
	resource *capsulev1beta2.GlobalTenantResource,
) (changed bool) {

	// If name is empty, remove the whole reference
	if resource.Spec.ServiceAccount == nil || resource.Spec.ServiceAccount.Name == "" {
		// If a default is configured, apply it
		if setGlobalTenantDefaultResourceServiceAccount(config, resource) {
			changed = true
		} else {
			if resource.Spec.ServiceAccount != nil {
				resource.Spec.ServiceAccount = nil
				changed = true
			}

			return
		}
	}

	// Sanitize the Name
	sanitizedName := caputils.SanitizeServiceAccountProp(resource.Spec.ServiceAccount.Name.String())
	if resource.Spec.ServiceAccount.Name.String() != sanitizedName {
		resource.Spec.ServiceAccount.Name = api.Name(sanitizedName)
		changed = true
	}

	// Always set the namespace to match the resource
	sanitizedNS := caputils.SanitizeServiceAccountProp(resource.Namespace)
	if resource.Spec.ServiceAccount.Namespace.String() != sanitizedNS {
		resource.Spec.ServiceAccount.Namespace = api.Name(sanitizedNS)
		changed = true
	}

	return
}

func SetTenantResourceServiceAccount(
	config configuration.Configuration,
	resource *capsulev1beta2.TenantResource,
) (changed bool) {
	changed = false

	// If name is empty, remove the whole reference
	if resource.Spec.ServiceAccount == nil || resource.Spec.ServiceAccount.Name == "" {
		// If a default is configured, apply it
		if setTenantDefaultResourceServiceAccount(config, resource) {
			changed = true
		} else {
			// Remove invalid ServiceAccount reference
			if resource.Spec.ServiceAccount != nil {
				resource.Spec.ServiceAccount = nil
				changed = true
			}

			return
		}
	}

	// Sanitize the Name
	sanitizedName := caputils.SanitizeServiceAccountProp(resource.Spec.ServiceAccount.Name.String())
	if resource.Spec.ServiceAccount.Name.String() != sanitizedName {
		resource.Spec.ServiceAccount.Name = api.Name(sanitizedName)
		changed = true
	}

	// Always set the namespace to match the resource
	sanitizedNS := caputils.SanitizeServiceAccountProp(resource.Namespace)
	if resource.Spec.ServiceAccount.Namespace.String() != sanitizedNS {
		resource.Spec.ServiceAccount.Namespace = api.Name(sanitizedNS)
		changed = true
	}

	return
}

func setTenantDefaultResourceServiceAccount(
	config configuration.Configuration,
	resource *capsulev1beta2.TenantResource,
) (changed bool) {
	cfg := config.ServiceAccountClientProperties()
	if cfg == nil {
		return false
	}

	if cfg.TenantDefaultServiceAccount == "" {
		return false
	}

	if resource.Spec.ServiceAccount == nil {
		resource.Spec.ServiceAccount = &api.ServiceAccountReference{}
	}

	resource.Spec.ServiceAccount.Name = api.Name(
		caputils.SanitizeServiceAccountProp(cfg.TenantDefaultServiceAccount.String()),
	)

	return true
}

func setGlobalTenantDefaultResourceServiceAccount(
	config configuration.Configuration,
	resource *capsulev1beta2.GlobalTenantResource,
) (changed bool) {
	cfg := config.ServiceAccountClientProperties()
	if cfg == nil {
		return false
	}

	if cfg.GlobalDefaultServiceAccount == "" && cfg.GlobalDefaultServiceAccountNamespace == "" {
		return false
	}

	if resource.Spec.ServiceAccount == nil {
		resource.Spec.ServiceAccount = &api.ServiceAccountReference{}
	}

	if cfg.GlobalDefaultServiceAccount == "" {
		resource.Spec.ServiceAccount.Name = api.Name(
			caputils.SanitizeServiceAccountProp(cfg.GlobalDefaultServiceAccount.String()),
		)
	}

	if cfg.GlobalDefaultServiceAccountNamespace == "" {
		resource.Spec.ServiceAccount.Namespace = api.Name(
			caputils.SanitizeServiceAccountProp(cfg.GlobalDefaultServiceAccountNamespace.String()),
		)
	}

	return true
}

func maskSensitiveErrData(err error) error {
	if apierrors.IsInvalid(err) {
		// The last part of the error message is the reason for the error.
		if i := strings.LastIndex(err.Error(), `:`); i != -1 {
			err = errors.New(strings.TrimSpace(err.Error()[i+1:]))
		}
	}
	return err
}

func getFieldOwner(name string, namespace string, id api.ResourceID) string {
	if namespace == "" {
		namespace = "cluster"
	}

	return "capsule/" + namespace + "/" + name + "/" + id.Tenant + "/" + id.Namespace + "/" + id.Kind + "/" + id.Name + "/" + id.Index
}

// Field templating for the ArgoCD project properties. Needs to unmarshal in json, because of the json tags from argocd.
func loadTenantToContext(
	tenant *capsulev1beta2.Tenant,
) (context map[string]interface{}) {
	context = make(map[string]interface{})
	context["Tenant"] = tenant

	return
}

// Field templating for the ArgoCD project properties. Needs to unmarshal in json, because of the json tags from argocd.
func renderGeneratorItem(
	generator capsulev1beta2.GeneratorItemSpec,
	context tpl.ReferenceContext,
) (items []*unstructured.Unstructured, err error) {
	tmpl, err := template.New("tpl").Option("missingkey=" + generator.MissingKey.String()).Funcs(tpl.ExtraFuncMap()).Parse(generator.Template)
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
