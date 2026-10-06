// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	apirules "github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/runtime/workloads"
)

type metadataRules struct{ compiler ruleengine.ConditionCompiler }

func MetadataRules(compiler ruleengine.ConditionCompiler) handlers.TypedHandlerWithTenantWithRuleset[*unstructured.Unstructured] {
	return &metadataRules{compiler: compiler}
}

func (h *metadataRules) OnCreate(_ client.Client, _ client.Reader, obj *unstructured.Unstructured, _ admission.Decoder, _ events.EventRecorder, _ *capsulev1beta2.Tenant, bodies []*apirules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.mutate(obj, nil, bodies)
}

func (h *metadataRules) OnUpdate(_ client.Client, _ client.Reader, old *unstructured.Unstructured, obj *unstructured.Unstructured, _ admission.Decoder, _ events.EventRecorder, _ *capsulev1beta2.Tenant, bodies []*apirules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.mutate(obj, old, bodies)
}

func (*metadataRules) OnDelete(client.Client, client.Reader, *unstructured.Unstructured, admission.Decoder, events.EventRecorder, *capsulev1beta2.Tenant, []*apirules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { return nil }
}

func (h *metadataRules) mutate(obj, old *unstructured.Unstructured, bodies []*apirules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		gvk := schema.GroupVersionKind{Group: req.Kind.Group, Version: req.Kind.Version, Kind: req.Kind.Kind}
		if gvk.Version == "" || gvk.Kind == "" {
			response := admission.Errored(http.StatusBadRequest, fmt.Errorf("admission request kind is incomplete: %s", gvk.String()))

			return &response
		}

		conditions := ruleengine.NewConditionEvaluator(h.compiler, req.AdmissionRequest)

		if req.SubResource != "" {
			if req.SubResource != "ephemeralcontainers" || req.Operation != admissionv1.Update || gvk != corev1.SchemeGroupVersion.WithKind("Pod") {
				return nil
			}

			changed, err := mutateEphemeralContainers(ctx, obj, old, bodies, conditions)

			return mutationResponse(obj, req, changed, err)
		}

		mutateResources := req.SubResource == "" && ((req.Operation == admissionv1.Create && gvk == corev1.SchemeGroupVersion.WithKind("Pod")) || len(workloads.PodTemplatePath(gvk)) > 0)

		filtered, err := ruleengine.FilterNamespaceEnforcementConditions(ctx, conditions, obj, bodies,
			func(body *apirules.NamespaceRuleEnforceBody) bool {
				_, matches := body.Workloads.PodTargets(gvk)

				return hasMetadataMutation(gvk, body) || (mutateResources && matches && body.Workloads.Resources != nil)
			})
		if err != nil {
			response := admission.Errored(http.StatusInternalServerError, err)

			return &response
		}

		metadataMutated := MutateMetadata(obj, gvk, filtered)

		conditions.ResetObject()

		resourcesMutated := false

		if mutateResources {
			resourcesMutated, err = mutateWorkloadResources(ctx, obj, gvk, filtered, conditions)
			if err != nil {
				response := admission.Errored(http.StatusInternalServerError, err)

				return &response
			}
		}

		return mutationResponse(obj, req, metadataMutated || resourcesMutated, nil)
	}
}

func mutationResponse(obj *unstructured.Unstructured, req admission.Request, changed bool, err error) *admission.Response {
	if err != nil {
		response := admission.Errored(http.StatusInternalServerError, err)

		return &response
	}

	if !changed {
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

// HasMetadataMutation reports whether any rule can default or manage metadata
// for gvk. Evaluate rendered rules, since templates can supply these fields.
func HasMetadataMutation(gvk schema.GroupVersionKind, bodies []*apirules.NamespaceRuleBodyNamespace) bool {
	for _, body := range bodies {
		if body != nil && hasMetadataMutation(gvk, body.Enforce) {
			return true
		}
	}

	return false
}

func hasMetadataMutation(gvk schema.GroupVersionKind, body *apirules.NamespaceRuleEnforceBody) bool {
	if body == nil {
		return false
	}

	for _, rule := range body.Metadata {
		if !rule.MatchesGroupVersionKind(gvk) {
			continue
		}

		for _, policies := range []map[string]apirules.MetadataValueRule{rule.Labels, rule.Annotations} {
			for _, policy := range policies {
				if policy.Default != nil || policy.Managed != nil {
					return true
				}
			}
		}
	}

	return false
}

// MutateMetadataConditional applies the enclosing enforcement gate before any
// metadata defaults or managed values. Pure validation rules are not evaluated here.
func MutateMetadataConditional(ctx context.Context, obj metav1.Object, gvk schema.GroupVersionKind, bodies []*apirules.NamespaceRuleBodyNamespace, conditions *ruleengine.ConditionEvaluator) (bool, error) {
	filtered, err := ruleengine.FilterNamespaceEnforcementConditions(ctx, conditions, obj, bodies,
		func(body *apirules.NamespaceRuleEnforceBody) bool { return hasMetadataMutation(gvk, body) })
	if err != nil {
		return false, err
	}

	return MutateMetadata(obj, gvk, filtered), nil
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
		if existing, present := current[key]; present && existing == value {
			continue
		}

		current[key] = value
		changed = true
	}

	return current, changed
}
