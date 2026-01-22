// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/configuration"
)

type registryHandler struct {
	configuration configuration.Configuration
	cache         *cache.NamespaceRegistriesCache
}

func ContainerRegistry(configuration configuration.Configuration, cache *cache.NamespaceRegistriesCache) capsulewebhook.TypedHandlerWithTenant[*corev1.Pod] {
	return &registryHandler{
		configuration: configuration,
		cache:         cache,
	}
}

func (h *registryHandler) OnCreate(
	c client.Client,
	pod *corev1.Pod,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(req, pod, tnt, recorder)
	}
}

func (h *registryHandler) OnUpdate(
	c client.Client,
	old *corev1.Pod,
	pod *corev1.Pod,
	decoder admission.Decoder,
	recorder events.EventRecorder,
	tnt *capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.validate(req, pod, tnt, recorder)
	}
}

func (h *registryHandler) OnDelete(
	client.Client,
	*corev1.Pod,
	admission.Decoder,
	events.EventRecorder,
	*capsulev1beta2.Tenant,
) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *registryHandler) validate(
	req admission.Request,
	pod *corev1.Pod,
	tnt *capsulev1beta2.Tenant,
	recorder events.EventRecorder,
) *admission.Response {
	rs, ok := h.cache.Get(req.Namespace)
	if !ok || rs == nil {
		resp := admission.Allowed("no registry rules for namespace")

		return &resp
	}

	if rs.HasImages {
		if resp := h.validateContainers(req, pod, tnt, recorder, rs); resp != nil {
			return resp
		}
	}

	if rs.HasVolumes {
		if resp := h.validateVolumes(req, pod, tnt, recorder, rs); resp != nil {
			return resp
		}
	}

	return nil
}

func (h *registryHandler) validateContainers(
	req admission.Request,
	pod *corev1.Pod,
	tnt *capsulev1beta2.Tenant,
	recorder events.EventRecorder,
	rs *cache.RuleSet,
) *admission.Response {
	for i := range pod.Spec.InitContainers {
		c := pod.Spec.InitContainers[i]
		if resp := h.verifyOCIReference(recorder, req, tnt, rs, api.ValidateImages, c.Image, c.ImagePullPolicy, fmt.Sprintf("initContainers[%d]", i)); resp != nil {
			return resp
		}
	}

	for i := range pod.Spec.EphemeralContainers {
		c := pod.Spec.EphemeralContainers[i]
		if resp := h.verifyOCIReference(recorder, req, tnt, rs, api.ValidateImages, c.Image, c.ImagePullPolicy, fmt.Sprintf("ephemeralContainers[%d]", i)); resp != nil {
			return resp
		}
	}

	for i := range pod.Spec.Containers {
		c := pod.Spec.Containers[i]
		if resp := h.verifyOCIReference(recorder, req, tnt, rs, api.ValidateImages, c.Image, c.ImagePullPolicy, fmt.Sprintf("containers[%d]", i)); resp != nil {
			return resp
		}
	}

	return nil
}

func (h *registryHandler) validateVolumes(
	req admission.Request,
	pod *corev1.Pod,
	tnt *capsulev1beta2.Tenant,
	recorder events.EventRecorder,
	rs *cache.RuleSet,
) *admission.Response {
	for i := range pod.Spec.Volumes {
		v := pod.Spec.Volumes[i]
		if v.Image == nil {
			continue
		}

		ref := strings.TrimSpace(v.Image.Reference)
		if ref == "" {
			resp := admission.Denied(fmt.Sprintf("volume %q has empty image.reference", v.Name))

			return &resp
		}

		if resp := h.verifyOCIReference(
			recorder, req, tnt,
			rs, api.ValidateVolumes,
			ref, v.Image.PullPolicy,
			fmt.Sprintf("volumes[%d](%s)", i, v.Name),
		); resp != nil {
			return resp
		}
	}

	return nil
}

type resolvedRegistryConfig struct {
	allowed       bool
	allowedPolicy map[corev1.PullPolicy]struct{} // nil => no restriction
}

func resolveRegistryConfig(
	rules []cache.CompiledRule,
	ref string,
	target api.RegistryValidationTarget,
) resolvedRegistryConfig {
	var res resolvedRegistryConfig

	for i := range rules {
		r := rules[i]

		switch target {
		case api.ValidateImages:
			if !r.ValidateImages { // adjust field name
				continue
			}
		case api.ValidateVolumes:
			if !r.ValidateVolumes { // adjust field name
				continue
			}
		}

		if !r.RE.MatchString(ref) { // adjust field name
			continue
		}

		res.allowed = true

		// only override pullpolicy restriction when explicitly set by a later matching rule
		if len(r.AllowedPolicy) > 0 { // adjust field name
			res.allowedPolicy = r.AllowedPolicy
		}
	}

	return res
}

func (h *registryHandler) verifyOCIReference(
	recorder events.EventRecorder,
	req admission.Request,
	tnt *capsulev1beta2.Tenant,
	rs *cache.RuleSet,
	target api.RegistryValidationTarget,
	reference string,
	pullPolicy corev1.PullPolicy,
	where string,
) *admission.Response {
	ref := strings.TrimSpace(reference)
	if ref == "" {
		resp := admission.Denied(fmt.Sprintf("%s has empty reference", where))

		return &resp
	}

	// Match rules against the FULL OCI reference string.
	// This avoids relying on parsing logic and supports nested paths, digests, etc.
	cfg := resolveRegistryConfig(rs.Compiled, ref, target)
	if !cfg.allowed {
		resp := admission.Denied(fmt.Sprintf("%s reference %q is not allowed", where, ref))

		return &resp
	}

	// No defaulting: enforce only if restricted; empty pullPolicy is rejected under restriction.
	if cfg.allowedPolicy != nil {
		allowed := formatAllowedPullPolicies(cfg.allowedPolicy)

		if pullPolicy == "" {
			resp := admission.Denied(fmt.Sprintf(
				"%s reference %q must explicitly set pullPolicy (allowed: %s)",
				where, ref, allowed,
			))

			return &resp
		}

		if _, ok := cfg.allowedPolicy[pullPolicy]; !ok {
			resp := admission.Denied(fmt.Sprintf(
				"%s reference %q uses pullPolicy=%s which is not allowed (allowed: %s)",
				where, ref, pullPolicy, allowed,
			))

			return &resp
		}
	}

	return nil
}

func formatAllowedPullPolicies(policies map[corev1.PullPolicy]struct{}) string {
	if len(policies) == 0 {
		return ""
	}

	out := make([]string, 0, len(policies))
	for p := range policies {
		out = append(out, string(p))
	}

	sort.Strings(out)

	return strings.Join(out, ", ")
}
