// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/runtime/workloads"
)

type templateRules struct{ pod *podRules }

func TemplateRules(regex *cache.RegexCache, registries *cache.RegistryRuleSetCache, selectors *cache.LabelSelectorCache, compiler ruleengine.ConditionCompiler) handlers.TypedHandlerWithTenantWithRuleset[*unstructured.Unstructured] {
	return &templateRules{pod: newPodRules(regex, registries, selectors, compiler)}
}

func (h *templateRules) OnCreate(_ client.Client, reader client.Reader, obj *unstructured.Unstructured, _ admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.validate(reader, nil, obj, recorder, tnt, bodies)
}

func (h *templateRules) OnUpdate(_ client.Client, reader client.Reader, old, obj *unstructured.Unstructured, _ admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.validate(reader, old, obj, recorder, tnt, bodies)
}

func (*templateRules) OnDelete(client.Client, client.Reader, *unstructured.Unstructured, admission.Decoder, events.EventRecorder, *capsulev1beta2.Tenant, []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { return nil }
}

func (h *templateRules) validate(reader client.Reader, old, obj *unstructured.Unstructured, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if req.SubResource == "scale" {
			return h.validateScale(ctx, reader, old, obj, recorder, tnt, bodies, req)
		}

		gvk := schema.GroupVersionKind{Group: req.Kind.Group, Version: req.Kind.Version, Kind: req.Kind.Kind}

		path := workloads.PodTemplatePath(gvk)
		if req.SubResource != "" || obj == nil || len(path) == 0 {
			return nil
		}

		enforce := ruleengine.WorkloadEnforcement(ruleengine.EnforceBodiesFromNamespaceRules(bodies), gvk)
		if len(enforce) == 0 {
			return nil
		}

		obj.SetGroupVersionKind(gvk)

		pod, err := workloads.PodFromTemplate(obj)
		if err != nil {
			return ad.Deny(err.Error())
		}

		var oldPod *corev1.Pod

		if old != nil {
			old.SetGroupVersionKind(gvk)

			oldPod, err = workloads.PodFromTemplate(old)
			if err != nil {
				return ad.Deny(err.Error())
			}
		}

		if err := h.pod.validateWorkloadRules(ctx, req, reader, pod, obj, obj, tnt, recorder, enforce, oldPod); err != nil {
			return rejection(strings.Join(path, ".")+": ", err)
		}

		return nil
	}
}

// appliesToClasses reports whether a rule carries any Pod-level class policy.
func appliesToClasses(body *rules.NamespaceRuleEnforceBody) bool {
	return appliesToPriorityClasses(body) || appliesToRuntimeClasses(body)
}

// validateScale enforces class rules when a scale-up creates Pods from an
// unchanged controller template. Those Pods are created under the controller's
// identity, which an audience-scoped rule never matches, so the scale request
// itself is the last admission the rule's audience performs. A scale-down
// creates no Pods and is never evaluated.
func (h *templateRules) validateScale(ctx context.Context, reader client.Reader, old, obj *unstructured.Unstructured, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace, req admission.Request) *admission.Response {
	gvk := workloads.ScaleWorkloadGVK(req.Resource)
	if gvk.Empty() || obj == nil || old == nil || !scaledUp(old, obj) {
		return nil
	}

	enforce := ruleengine.WorkloadEnforcement(ruleengine.EnforceBodiesFromNamespaceRules(bodies), gvk)
	if !slices.ContainsFunc(enforce, appliesToClasses) {
		return nil
	}

	parent := &unstructured.Unstructured{}
	parent.SetGroupVersionKind(gvk)

	if err := reader.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, parent); err != nil {
		return handlers.ErroredResponse(fmt.Errorf("read scale parent: %w", err))
	}

	if uid := obj.GetUID(); uid != "" && uid != parent.GetUID() {
		return handlers.ErroredResponse(fmt.Errorf("scale parent changed; retry"))
	}

	pod, err := workloads.PodFromTemplate(parent)
	if err != nil {
		return ad.Deny(err.Error())
	}

	enforce, err = ruleengine.FilterEnforcementConditions(ctx, ruleengine.NewConditionEvaluator(h.pod.compiler, req.AdmissionRequest), parent, enforce, appliesToClasses)
	if err != nil {
		return ad.Deny(fmt.Sprintf("enforce: %v", err))
	}

	prefix := strings.Join(workloads.PodTemplatePath(gvk), ".") + ": "

	for _, validate := range []func(context.Context, client.Reader, client.Object, *corev1.Pod, []*rules.NamespaceRuleEnforceBody) (*ruleengine.Evaluation, error){
		h.pod.validatePriorityClasses,
		h.pod.validateRuntimeClasses,
	} {
		evaluation, err := validate(ctx, reader, parent, pod, enforce)
		if err != nil {
			return rejection(prefix, err)
		}

		if evaluation == nil {
			continue
		}

		blockingErr := evaluation.BlockingError()
		emitWorkloadRuleEvents(ctx, req, recorder, parent, tnt, evaluation, blockingErr)

		if blockingErr != nil {
			return rejection(prefix, blockingErr)
		}
	}

	return nil
}

// scaledUp reports whether the scale update raises the replica count. A
// malformed replica field keeps the request evaluated rather than skipped.
func scaledUp(old, obj *unstructured.Unstructured) bool {
	previous, _, err := unstructured.NestedInt64(old.Object, "spec", "replicas")
	if err != nil {
		return true
	}

	current, _, err := unstructured.NestedInt64(obj.Object, "spec", "replicas")
	if err != nil {
		return true
	}

	return current > previous
}
