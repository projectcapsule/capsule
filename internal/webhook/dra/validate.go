// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package dra

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	resources "k8s.io/api/resource/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

// DeviceClass checks the effective namespace profile for every caller, including
// controllers creating claims on behalf of workloads. Native claim and template
// specs are immutable; metadata, status, and deletion never need class validation.
func DeviceClass(cfg configuration.Configuration, regex *cache.RegexCache, selectors *cache.LabelSelectorCache, compiler ruleengine.ConditionCompiler) handlers.Handler {
	if regex == nil {
		regex = cache.NewRegexCache()
	}

	if selectors == nil {
		selectors = cache.NewLabelSelectorCache()
	}

	return handlers.MatchingRequest(func(req admission.Request) bool {
		return req.Operation == admissionv1.Create && req.SubResource == "" &&
			req.Kind.Group == resources.GroupName && (req.Kind.Version == "v1" || req.Kind.Version == "v1beta2") &&
			(req.Kind.Kind == "ResourceClaim" || req.Kind.Kind == "ResourceClaimTemplate")
	}, &handlers.TypedTenantWithRulesetHandler[*unstructured.Unstructured]{
		Factory:       func() *unstructured.Unstructured { return &unstructured.Unstructured{} },
		Configuration: cfg,
		Handlers:      []handlers.TypedHandlerWithTenantWithRuleset[*unstructured.Unstructured]{&deviceClass{regex: regex, selectors: selectors, compiler: compiler}},
	})
}

type deviceClass struct {
	regex     *cache.RegexCache
	selectors *cache.LabelSelectorCache
	compiler  ruleengine.ConditionCompiler
}

func (h *deviceClass) OnCreate(_ client.Client, reader client.Reader, obj *unstructured.Unstructured, _ admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		enforce, err := ruleengine.FilterEnforcementConditions(ctx,
			ruleengine.NewConditionEvaluator(h.compiler, req.AdmissionRequest), obj,
			ruleengine.EnforceBodiesFromNamespaceRules(bodies),
			func(body *rules.NamespaceRuleEnforceBody) bool { return len(body.Devices.Classes) > 0 })
		if err != nil {
			return ad.Deny(fmt.Sprintf("enforce: %v", err))
		}
		// Irrelevant conditions and class reads are skipped even for tenants with
		// unrelated namespace rules.
		active, needsClass := false, tnt.Spec.DeviceClasses != nil
		requireClass := false

		for _, body := range enforce {
			for _, match := range body.Devices.Classes {
				active = true
				needsClass = needsClass || match.Selector != nil
				requireClass = requireClass || (match.Selector != nil && body.Action.OrDefault() != rules.ActionTypeAudit)
			}
		}

		if !active && tnt.Spec.DeviceClasses == nil {
			return nil
		}

		var buffer [resources.DeviceRequestsMaxSize]ruleengine.Value

		refs, err := requestedDeviceClasses(obj, buffer[:0])
		if err != nil {
			return ad.Deny(err.Error())
		}

		classes, response := h.readClasses(ctx, req, reader, obj, tnt, recorder, refs, needsClass)
		if response != nil {
			return response
		}

		if requireClass {
			for _, ref := range refs {
				if classes[ref.Value] == nil {
					return ad.Deny(fmt.Sprintf("DeviceClass %q does not exist at %s", ref.Value, ref.Path))
				}
			}
		}

		evaluation, err := h.evaluateClasses(refs, enforce, classes)
		if err != nil {
			return ad.Deny(err.Error())
		}

		for _, audit := range evaluation.Audits {
			emitClassEvent(ctx, req, obj, tnt, recorder, corev1.EventTypeNormal, events.ReasonNamespaceRuleAudit, events.ActionRuleAudit, audit.Message)
		}

		if evaluation.Blocking != nil {
			decision := evaluation.Blocking
			emitClassEvent(ctx, req, obj, tnt, recorder, corev1.EventTypeWarning, decision.EventReason, events.ActionValidationDenied, decision.Message)

			return ad.Deny(decision.Message)
		}

		return nil
	}
}

func (*deviceClass) OnUpdate(client.Client, client.Reader, *unstructured.Unstructured, *unstructured.Unstructured, admission.Decoder, events.EventRecorder, *capsulev1beta2.Tenant, []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { return nil }
}

func (*deviceClass) OnDelete(client.Client, client.Reader, *unstructured.Unstructured, admission.Decoder, events.EventRecorder, *capsulev1beta2.Tenant, []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { return nil }
}

func (h *deviceClass) readClasses(ctx context.Context, req admission.Request, reader client.Reader, obj client.Object, tnt *capsulev1beta2.Tenant, recorder events.EventRecorder, refs []ruleengine.Value, needsClass bool) (map[string]client.Object, *admission.Response) {
	// These snapshots belong to this admission only. Compilation caches never
	// retain objects, tenant decisions, or lookup failures.
	classes := make(map[string]client.Object, len(refs))

	for _, ref := range refs {
		if ref.Value == "" {
			if allowed := tnt.Spec.DeviceClasses; allowed != nil {
				return nil, ad.Deny(caperrors.NewDeviceClassUndefined(*allowed).Error())
			}

			return nil, ad.Deny("deviceClassName is required at " + ref.Path)
		}

		if _, found := classes[ref.Value]; found {
			continue
		}

		var (
			class client.Object
			err   error
		)

		if needsClass {
			class, err = utils.GetDeviceClassByName(ctx, reader, ref.Value, req.Kind.Version)
			if err != nil && !k8serrors.IsNotFound(err) {
				response := admission.Errored(http.StatusInternalServerError, err)

				return nil, &response
			}

			if k8serrors.IsNotFound(err) {
				class = nil
			}
		}

		classes[ref.Value] = class

		if allowed := tnt.Spec.DeviceClasses; allowed != nil {
			if class == nil {
				return nil, ad.Deny(caperrors.NewDeviceClassUndefined(*allowed).Error())
			}

			//nolint:staticcheck // Preserve legacy allowedRegex and empty-selector semantics.
			matchSelector := len(allowed.MatchLabels) > 0 || len(allowed.MatchExpressions) > 0 || (len(allowed.Exact) == 0 && allowed.Regex == "")
			if !allowed.Match(class.GetName()) && (!matchSelector || !allowed.SelectorMatch(class)) {
				message := caperrors.NewDeviceClassForbidden(class.GetName(), *allowed).Error()
				emitClassEvent(ctx, req, obj, tnt, recorder, corev1.EventTypeWarning, events.ReasonForbiddenDeviceClass, events.ActionValidationDenied, message)

				return nil, ad.Deny(message)
			}
		}
	}

	return classes, nil
}

func (h *deviceClass) evaluateClasses(refs []ruleengine.Value, enforce []*rules.NamespaceRuleEnforceBody, classes map[string]client.Object) (*ruleengine.Evaluation, error) {
	return ruleengine.EvaluateEnforce(refs, enforce, ruleengine.Set[rules.DeviceClassMatch, []ruleengine.Value]{
		Name: "device class", EventReason: events.ReasonForbiddenDeviceClass,
		Values:              func(values []ruleengine.Value) []ruleengine.Value { return values },
		EvaluateEmptyValues: true,
		Rules:               func(body *rules.NamespaceRuleEnforceBody) []rules.DeviceClassMatch { return body.Devices.Classes },
		Matches: func(match rules.DeviceClassMatch, ref ruleengine.Value) (ruleengine.Match, error) {
			matched := false

			if len(match.Exact) > 0 || match.Expression != "" {
				var err error

				matched, err = match.NameMatch().MatchesWithExpressionMatcher(h.regex, ref.Value)
				if err != nil {
					return ruleengine.Match{}, err
				}
			}

			if !matched && match.Selector != nil {
				selector, compileErr := h.selectors.GetOrCompile(match.Selector)
				if compileErr != nil {
					return ruleengine.Match{}, compileErr
				}

				if class := classes[ref.Value]; class != nil {
					matched = selector.Matches(labels.Set(class.GetLabels()))
				}
			}

			return ruleengine.Match{Matched: matched}, nil
		},
		RuleDescription:    describeClassMatch,
		AllowedDescription: "Allowed device classes",
	})
}

func emitClassEvent(ctx context.Context, req admission.Request, obj client.Object, tnt *capsulev1beta2.Tenant, recorder events.EventRecorder, eventType, reason, action, message string) {
	recorder.LabeledEvent(obj, eventType, reason, action, message).WithRelated(tnt).WithTenantLabel(tnt).WithRequestAnnotations(req).Emit(ctx)
}

func describeClassMatch(match rules.DeviceClassMatch) string {
	parts := []string{}
	if len(match.Exact) > 0 || match.Expression != "" {
		parts = append(parts, match.NameMatch().Describe())
	}

	if match.Selector != nil {
		parts = append(parts, fmt.Sprintf("class selector: %v", match.Selector))
	}

	return strings.Join(parts, "; ")
}

// requestedDeviceClasses reads the already decoded object without copying its
// request tree. Preserve every fallback and its path for independent evaluation.
func requestedDeviceClasses(obj *unstructured.Unstructured, refs []ruleengine.Value) ([]ruleengine.Value, error) {
	path := []string{"spec", "devices", "requests"}
	if obj.GetKind() == "ResourceClaimTemplate" {
		path = append([]string{"spec"}, path...)
	}

	raw, _, err := unstructured.NestedFieldNoCopy(obj.Object, path...)
	if err != nil {
		return nil, err
	}

	if raw == nil {
		return refs, nil
	}

	requests, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array", strings.Join(path, "."))
	}

	for i, raw := range requests {
		request, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid device request at index %d", i)
		}

		base := fmt.Sprintf("%s[%d]", strings.Join(path, "."), i)

		if exact, found := request["exactly"]; found && exact != nil {
			value, ok := exact.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s.exactly must be an object", base)
			}

			name, _, err := unstructured.NestedString(value, "deviceClassName")
			if err != nil {
				return nil, err
			}

			refs = append(refs, ruleengine.Value{Value: name, Path: base + ".exactly.deviceClassName"})
		}

		rawAlternatives, _, err := unstructured.NestedFieldNoCopy(request, "firstAvailable")
		if err != nil {
			return nil, err
		}

		alternatives, ok := rawAlternatives.([]any)
		if rawAlternatives != nil && !ok {
			return nil, fmt.Errorf("%s.firstAvailable must be an array", base)
		}

		if request["exactly"] == nil && len(alternatives) == 0 {
			refs = append(refs, ruleengine.Value{Path: base + ".deviceClassName"})
		}

		for j, raw := range alternatives {
			value, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s.firstAvailable[%d] must be an object", base, j)
			}

			name, _, err := unstructured.NestedString(value, "deviceClassName")
			if err != nil {
				return nil, err
			}

			refs = append(refs, ruleengine.Value{Value: name, Path: fmt.Sprintf("%s.firstAvailable[%d].deviceClassName", base, j)})
		}
	}

	return refs, nil
}
