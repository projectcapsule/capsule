// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

const Path = "/rules/generic/validating"

type genericValidating struct {
	regexCache    *cache.RegexCache
	configuration configuration.Configuration
	resourceRules []handlers.Handler
}

func Register(
	regexCache *cache.RegexCache,
	cfg configuration.Configuration,
	resourceRules ...handlers.Handler,
) handlers.Webhook {
	return &genericValidating{
		regexCache:    regexCache,
		configuration: cfg,
		resourceRules: resourceRules,
	}
}

func (w *genericValidating) GetHandlers() []handlers.Handler {
	out := make([]handlers.Handler, 0, len(w.resourceRules)+2)
	out = append(out, matchingRequest(
		matchesGenericMetadataRequest,
		genericHandler(w.configuration,
			GenericRules(w.regexCache),
		),
	))
	out = append(out, w.resourceRules...)
	out = append(out, matchingRequest(
		func(req admission.Request) bool {
			_, supported := ingressTypeForGVK(requestGVK(req))

			return supported && req.SubResource == ""
		},
		ingressHandler(w.configuration,
			IngressRules(w.regexCache),
		),
	))

	return out
}

func matchesGenericMetadataRequest(req admission.Request) bool {
	if req.SubResource == "" {
		return true
	}

	gvk := requestGVK(req)

	return gvk.Group == "" && gvk.Kind == "Namespace"
}

func (genericValidating) GetPath() string {
	return Path
}

func ingressHandler(cfg configuration.Configuration,
	handler ...handlers.TypedHandlerWithTenantWithRuleset[*unstructured.Unstructured],
) handlers.Handler {
	return &handlers.TypedTenantWithRulesetHandler[*unstructured.Unstructured]{
		Factory:       func() *unstructured.Unstructured { return &unstructured.Unstructured{} },
		Handlers:      handler,
		Configuration: cfg,
	}
}

func genericHandler(cfg configuration.Configuration,
	handler ...handlers.TypedHandlerWithTenantWithRuleset[*metav1.PartialObjectMetadata],
) handlers.Handler {
	return &handlers.TypedTenantWithRulesetHandler[*metav1.PartialObjectMetadata]{
		Factory: func() *metav1.PartialObjectMetadata {
			return &metav1.PartialObjectMetadata{}
		},
		Handlers:      handler,
		Configuration: cfg,
	}
}

// ForKind scopes a validator hosted by the generic rules endpoint to one
// concrete Kubernetes kind. The main resource is always included; named
// subresources may be included explicitly. The scope check runs before
// tenant/ruleset resolution and object decoding.
func ForKind(
	gk schema.GroupKind,
	handler handlers.Handler,
	subresources ...string,
) handlers.Handler {
	return matchingRequest(func(req admission.Request) bool {
		gvk := requestGVK(req)
		if gvk.Group != gk.Group || gvk.Kind != gk.Kind {
			return false
		}

		return req.SubResource == "" || slices.Contains(subresources, req.SubResource)
	}, handler)
}

type requestPredicate func(admission.Request) bool

type matchingHandler struct {
	predicate requestPredicate
	handler   handlers.Handler
}

func matchingRequest(predicate requestPredicate, handler handlers.Handler) handlers.Handler {
	return &matchingHandler{
		predicate: predicate,
		handler:   handler,
	}
}

func (h *matchingHandler) OnCreate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	next := h.handler.OnCreate(c, reader, decoder, recorder)

	return h.handle(next)
}

func (h *matchingHandler) OnUpdate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	next := h.handler.OnUpdate(c, reader, decoder, recorder)

	return h.handle(next)
}

func (h *matchingHandler) OnDelete(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	next := h.handler.OnDelete(c, reader, decoder, recorder)

	return h.handle(next)
}

func (h *matchingHandler) handle(next handlers.Func) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if !h.predicate(req) {
			return nil
		}

		return next(ctx, req)
	}
}

func requestGVK(req admission.Request) schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   req.Kind.Group,
		Version: req.Kind.Version,
		Kind:    req.Kind.Kind,
	}
}
