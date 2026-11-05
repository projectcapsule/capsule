// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

// ClassTypeForGVK identifies supported v1 class references. Gateway API v1 is
// canonical; served v1beta1 Gateway requests use the identical class reference
// and must receive the same rules when wildcard webhooks preserve their version.
// This does not enable other beta Gateway API kinds or legacy Ingress versions.
func ClassTypeForGVK(gvk schema.GroupVersionKind) (rules.IngressType, bool) {
	switch {
	case gvk.Group == "networking.k8s.io" && gvk.Version == "v1" && gvk.Kind == "Ingress":
		return rules.IngressTypeIngress, true
	case gvk.Group == "gateway.networking.k8s.io" && gvk.Kind == "Gateway" && (gvk.Version == "v1" || gvk.Version == "v1beta1"):
		return rules.IngressTypeGateway, true
	default:
		return "", false
	}
}

// ClassReferences retains both Ingress representations: controllers can differ
// in which one takes precedence. Gateway has only its native class field.
type ClassReferences struct {
	Field         string
	Name          string
	Annotation    string
	HasAnnotation bool
}

func ReadClassReferences(obj *unstructured.Unstructured, kind rules.IngressType) (refs ClassReferences, err error) {
	if obj == nil {
		return refs, nil
	}

	switch kind {
	case rules.IngressTypeIngress:
		refs.Field = "ingressClassName"
	case rules.IngressTypeGateway:
		refs.Field = "gatewayClassName"
	case rules.IngressTypeRoute, rules.IngressTypeListenerSet, rules.IngressTypeHTTPRoute, rules.IngressTypeTLSRoute, rules.IngressTypeGRPCRoute:
		return refs, nil
	default:
		return refs, nil
	}

	refs.Name, _, err = classString(obj, "spec", refs.Field)
	if err != nil {
		return refs, err
	}

	if kind == rules.IngressTypeIngress {
		refs.Annotation, refs.HasAnnotation, err = classString(obj, "metadata", "annotations", meta.IngressClassAnnotation)
	}

	return refs, err
}

func classString(obj *unstructured.Unstructured, fields ...string) (string, bool, error) {
	value, found, err := unstructured.NestedFieldNoCopy(obj.Object, fields...)
	if err != nil || value == nil {
		return "", false, err
	}

	name, ok := value.(string)
	if !ok {
		return "", found, fmt.Errorf("class reference %v must be a string", fields)
	}

	return name, found, nil
}
