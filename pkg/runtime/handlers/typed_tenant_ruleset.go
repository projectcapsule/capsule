// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

type TypedHandlerWithTenantWithRuleset[T client.Object] interface {
	OnCreate(
		c client.Client,
		reader client.Reader,
		obj T,
		decoder admission.Decoder,
		recorder events.EventRecorder,
		tnt *capsulev1beta2.Tenant,
		ruleBlocks []*rules.NamespaceRuleBodyNamespace,
	) Func

	OnUpdate(
		c client.Client,
		reader client.Reader,
		old T,
		obj T,
		decoder admission.Decoder,
		recorder events.EventRecorder,
		tnt *capsulev1beta2.Tenant,
		ruleBlocks []*rules.NamespaceRuleBodyNamespace,
	) Func

	OnDelete(
		c client.Client,
		reader client.Reader,
		obj T,
		decoder admission.Decoder,
		recorder events.EventRecorder,
		tnt *capsulev1beta2.Tenant,
		ruleBlocks []*rules.NamespaceRuleBodyNamespace,
	) Func
}

type TypedTenantWithRulesetHandler[T client.Object] struct {
	Factory       NewObjectFunc[T]
	Handlers      []TypedHandlerWithTenantWithRuleset[T]
	Configuration configuration.Configuration
}

type rulesetReadResult struct {
	rules []*rules.NamespaceRuleBodyNamespace
	found bool
	err   error
}

func (h *TypedTenantWithRulesetHandler[T]) OnCreate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if req.Namespace == "" {
			return nil
		}

		rulesetResult := h.readRulesetAsync(ctx, reader, req.Namespace)

		tnt, err := h.resolveTenant(ctx, reader, req)
		if err != nil {
			return ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		obj := h.Factory()
		if err := decoder.Decode(req, obj); err != nil {
			return ErroredResponse(err)
		}

		ruleBlocks, err := h.resolveRuleset(ctx, c, req.Namespace, tnt, <-rulesetResult)
		if err != nil {
			return ErroredResponse(err)
		}

		ruleBlocks, err = ruleengine.FilterNamespaceRulesByAudience(h.Configuration, tnt, req, ruleBlocks)
		if err != nil {
			return ErroredResponse(err)
		}

		for _, hndl := range h.Handlers {
			if response := hndl.OnCreate(c, reader, obj, decoder, recorder, tnt, ruleBlocks)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *TypedTenantWithRulesetHandler[T]) OnUpdate(
	c client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if req.Namespace == "" {
			return nil
		}

		rulesetResult := h.readRulesetAsync(ctx, reader, req.Namespace)

		tnt, err := h.resolveTenant(ctx, reader, req)
		if err != nil {
			return ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		newObj := h.Factory()
		if err := decoder.Decode(req, newObj); err != nil {
			return ErroredResponse(err)
		}

		oldObj := h.Factory()
		if err := decoder.DecodeRaw(req.OldObject, oldObj); err != nil {
			return ErroredResponse(err)
		}

		ruleBlocks, err := h.resolveRuleset(ctx, c, req.Namespace, tnt, <-rulesetResult)
		if err != nil {
			return ErroredResponse(err)
		}

		ruleBlocks, err = ruleengine.FilterNamespaceRulesByAudience(h.Configuration, tnt, req, ruleBlocks)
		if err != nil {
			return ErroredResponse(err)
		}

		for _, hndl := range h.Handlers {
			if response := hndl.OnUpdate(c, reader, oldObj, newObj, decoder, recorder, tnt, ruleBlocks)(ctx, req); response != nil {
				return response
			}
		}

		return nil
	}
}

func (h *TypedTenantWithRulesetHandler[T]) OnDelete(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *TypedTenantWithRulesetHandler[T]) resolveTenant(
	ctx context.Context,
	c client.Reader,
	req admission.Request,
) (*capsulev1beta2.Tenant, error) {
	if req.Namespace == "" {
		return nil, nil
	}

	return tenant.GetTenantByNamespace(ctx, c, req.Namespace)
}

func (h *TypedTenantWithRulesetHandler[T]) readRulesetAsync(
	ctx context.Context,
	reader client.Reader,
	namespace string,
) <-chan rulesetReadResult {
	result := make(chan rulesetReadResult, 1)

	go func() {
		rs := &capsulev1beta2.RuleStatus{}
		key := types.NamespacedName{
			Namespace: namespace,
			Name:      meta.NameForManagedRuleStatus(),
		}

		err := reader.Get(ctx, key, rs)

		switch {
		case err == nil:
			result <- rulesetReadResult{
				rules: rs.Status.Rules,
				found: true,
			}
		case apierrors.IsNotFound(err):
			result <- rulesetReadResult{}
		default:
			result <- rulesetReadResult{err: err}
		}
	}()

	return result
}

// Resolve the corresponding managed ruleset for this namespace.
// If not yet present, try to calculate it.
func (h *TypedTenantWithRulesetHandler[T]) resolveRuleset(
	ctx context.Context,
	c client.Client,
	namespace string,
	tnt *capsulev1beta2.Tenant,
	result rulesetReadResult,
) ([]*rules.NamespaceRuleBodyNamespace, error) {
	if result.err != nil {
		return nil, result.err
	}

	if result.found {
		return result.rules, nil
	}

	ns := &corev1.Namespace{}
	if err := c.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
		return nil, err
	}

	return tenant.BuildNamespaceRuleBodyStatus(c.Scheme(), ns, tnt)
}
