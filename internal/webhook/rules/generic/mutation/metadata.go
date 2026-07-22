// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type metadataRules struct{}

func MetadataRules() handlers.TypedHandlerWithTenantWithRuleset[*unstructured.Unstructured] {
	return &metadataRules{}
}

func (h *metadataRules) OnCreate(_ client.Client, _ client.Reader, obj *unstructured.Unstructured, _ admission.Decoder, _ events.EventRecorder, _ *capsulev1beta2.Tenant, bodies []*apirules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.mutate(obj, bodies)
}

func (h *metadataRules) OnUpdate(_ client.Client, _ client.Reader, _ *unstructured.Unstructured, obj *unstructured.Unstructured, _ admission.Decoder, _ events.EventRecorder, _ *capsulev1beta2.Tenant, bodies []*apirules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.mutate(obj, bodies)
}

func (*metadataRules) OnDelete(client.Client, client.Reader, *unstructured.Unstructured, admission.Decoder, events.EventRecorder, *capsulev1beta2.Tenant, []*apirules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { return nil }
}

func (*metadataRules) mutate(obj *unstructured.Unstructured, bodies []*apirules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		gvk := schema.GroupVersionKind{Group: req.Kind.Group, Version: req.Kind.Version, Kind: req.Kind.Kind}
		if gvk.Version == "" || gvk.Kind == "" {
			response := admission.Errored(http.StatusBadRequest, fmt.Errorf("admission request kind is incomplete: %s", gvk.String()))

			return &response
		}

		MutateMetadata(obj, gvk, bodies)

		marshaled, err := json.Marshal(obj)
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

		return &response
	}
}

func MutateMetadata(obj metav1.Object, gvk schema.GroupVersionKind, bodies []*apirules.NamespaceRuleBodyNamespace) {
	if obj == nil {
		return
	}

	labels, annotations := obj.GetLabels(), obj.GetAnnotations()
	defaultLabels, managedLabels := map[string]string{}, map[string]string{}
	defaultAnnotations, managedAnnotations := map[string]string{}, map[string]string{}

	for _, body := range bodies {
		if body == nil || body.Enforce == nil {
			continue
		}

		for _, rule := range body.Enforce.Metadata {
			if !rule.MatchesGroupVersionKind(gvk) {
				continue
			}

			collectMutation(rule.Labels, defaultLabels, managedLabels)
			collectMutation(rule.Annotations, defaultAnnotations, managedAnnotations)
		}
	}

	labels = applyMutation(labels, defaultLabels, managedLabels)
	annotations = applyMutation(annotations, defaultAnnotations, managedAnnotations)

	obj.SetLabels(labels)
	obj.SetAnnotations(annotations)
}

func collectMutation(policies map[string]apirules.MetadataValueRule, defaults, managed map[string]string) {
	for key, policy := range policies {
		if policy.Default != nil {
			defaults[key] = *policy.Default
		}

		if policy.Managed != nil {
			managed[key] = *policy.Managed
		}
	}
}

func applyMutation(current, defaults, managed map[string]string) map[string]string {
	if len(defaults) == 0 && len(managed) == 0 {
		return current
	}

	if current == nil {
		current = map[string]string{}
	}

	for key, value := range defaults {
		if _, ok := current[key]; !ok {
			current[key] = value
		}
	}

	maps.Copy(current, managed)

	return current
}
