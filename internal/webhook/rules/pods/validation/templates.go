// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
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

func TemplateRules(regex *cache.RegexCache, registries *cache.RegistryRuleSetCache, compiler ruleengine.ConditionCompiler) handlers.TypedHandlerWithTenantWithRuleset[*unstructured.Unstructured] {
	return &templateRules{pod: newPodRules(regex, registries, compiler)}
}

func (h *templateRules) OnCreate(_ client.Client, _ client.Reader, obj *unstructured.Unstructured, _ admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.validate(nil, obj, recorder, tnt, bodies)
}

func (h *templateRules) OnUpdate(_ client.Client, _ client.Reader, old, obj *unstructured.Unstructured, _ admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.validate(old, obj, recorder, tnt, bodies)
}

func (*templateRules) OnDelete(client.Client, client.Reader, *unstructured.Unstructured, admission.Decoder, events.EventRecorder, *capsulev1beta2.Tenant, []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { return nil }
}

func (h *templateRules) validate(old, obj *unstructured.Unstructured, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
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

		if err := h.pod.validateWorkloadRules(ctx, req, pod, obj, obj, tnt, recorder, enforce, oldPod); err != nil {
			return ad.Deny(strings.Join(path, ".") + ": " + err.Error())
		}

		return nil
	}
}
