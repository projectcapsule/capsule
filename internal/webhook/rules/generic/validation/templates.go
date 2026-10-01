// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/runtime/workloads"
)

// templateBridge shares metadata admission's resolved tenant, audience and
// ruleset. Decode the full controller only when a property policy targets it.
type templateBridge struct {
	next handlers.TypedHandlerWithTenantWithRuleset[*unstructured.Unstructured]
}

func (h *templateBridge) OnCreate(c client.Client, reader client.Reader, _ genericObject, decoder admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if !matchesTemplatePolicies(req, bodies) {
			return nil
		}

		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(req, obj); err != nil {
			return handlers.ErroredResponse(err)
		}

		return h.next.OnCreate(c, reader, obj, decoder, recorder, tnt, bodies)(ctx, req)
	}
}

func (h *templateBridge) OnUpdate(c client.Client, reader client.Reader, _, _ genericObject, decoder admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if !matchesTemplatePolicies(req, bodies) {
			return nil
		}

		obj, old := &unstructured.Unstructured{}, &unstructured.Unstructured{}
		if err := decoder.Decode(req, obj); err != nil {
			return handlers.ErroredResponse(err)
		}

		if err := decoder.DecodeRaw(req.OldObject, old); err != nil {
			return handlers.ErroredResponse(err)
		}

		return h.next.OnUpdate(c, reader, old, obj, decoder, recorder, tnt, bodies)(ctx, req)
	}
}

func (*templateBridge) OnDelete(client.Client, client.Reader, genericObject, admission.Decoder, events.EventRecorder, *capsulev1beta2.Tenant, []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { return nil }
}

func matchesTemplatePolicies(req admission.Request, bodies []*rules.NamespaceRuleBodyNamespace) bool {
	gvk := schema.GroupVersionKind{Group: req.Kind.Group, Version: req.Kind.Version, Kind: req.Kind.Kind}
	if req.SubResource != "" || len(workloads.PodTemplatePath(gvk)) == 0 {
		return false
	}

	for _, body := range bodies {
		if body == nil || body.Enforce == nil || !body.Enforce.Workloads.HasPolicies() {
			continue
		}

		for _, target := range body.Enforce.Workloads.Targets {
			gk, valid := target.GroupKind()
			if valid && gk == gvk.GroupKind() {
				return true
			}
		}
	}

	return false
}
