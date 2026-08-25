// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
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

		metadataMutated := MutateMetadata(obj, gvk, bodies)

		resourcesMutated := false

		if req.Operation == admissionv1.Create {
			var err error

			resourcesMutated, err = MutateWorkloadResources(obj, gvk, bodies)
			if err != nil {
				response := admission.Errored(http.StatusInternalServerError, err)

				return &response
			}
		}

		if !metadataMutated && !resourcesMutated {
			return nil
		}

		marshaled, err := json.Marshal(obj)
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

		return &response
	}
}

func MutateMetadata(
	obj metav1.Object,
	gvk schema.GroupVersionKind,
	bodies []*apirules.NamespaceRuleBodyNamespace,
) bool {
	if obj == nil {
		return false
	}

	labels, annotations := obj.GetLabels(), obj.GetAnnotations()

	var (
		defaultLabels      map[string]string
		managedLabels      map[string]string
		defaultAnnotations map[string]string
		managedAnnotations map[string]string
	)

	for _, body := range bodies {
		if body == nil || body.Enforce == nil {
			continue
		}

		for _, rule := range body.Enforce.Metadata {
			if !rule.MatchesGroupVersionKind(gvk) {
				continue
			}

			if defaultLabels == nil {
				defaultLabels = map[string]string{}
				managedLabels = map[string]string{}
				defaultAnnotations = map[string]string{}
				managedAnnotations = map[string]string{}
			}

			collectMutation(rule.Labels, defaultLabels, managedLabels)
			collectMutation(rule.Annotations, defaultAnnotations, managedAnnotations)
		}
	}

	labels, labelsChanged := applyMutation(labels, defaultLabels, managedLabels)
	annotations, annotationsChanged := applyMutation(annotations, defaultAnnotations, managedAnnotations)

	if !labelsChanged && !annotationsChanged {
		return false
	}

	obj.SetLabels(labels)
	obj.SetAnnotations(annotations)

	return true
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

func applyMutation(current, defaults, managed map[string]string) (map[string]string, bool) {
	if len(defaults) == 0 && len(managed) == 0 {
		return current, false
	}

	if current == nil {
		current = map[string]string{}
	}

	changed := false

	for key, value := range defaults {
		if _, ok := current[key]; !ok {
			current[key] = value
			changed = true
		}
	}

	for key, value := range managed {
		if current[key] == value {
			continue
		}

		current[key] = value
		changed = true
	}

	return current, changed
}
