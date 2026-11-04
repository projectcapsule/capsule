// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	genericvalidation "github.com/projectcapsule/capsule/internal/webhook/rules/generic/validation"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/ruleengine"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

const (
	attributeClassSetName = "volume attributes class"
	attributeClassPath    = "spec.volumeAttributesClassName"
)

// attributeClassKey prefilters requests. API admission snapshots encode keys
// literally, so a claim whose payload lacks the key has no class.
var attributeClassKey = []byte(`"volumeAttributesClassName"`)

type attributeClassRules struct {
	regexCache *cache.RegexCache
	selectors  *cache.LabelSelectorCache
	compiler   ruleengine.ConditionCompiler
}

// AttributeClassHandler joins the generic rules chain, which receives every
// claim write in tenant namespaces, bound claims included. Kind/subresource
// filtering, and the skip for requests that can't apply a new class, happen
// before decoding and tenant/ruleset reads; a successful check continues it.
func AttributeClassHandler(
	cfg configuration.Configuration,
	regexCache *cache.RegexCache,
	selectors *cache.LabelSelectorCache,
	compiler ruleengine.ConditionCompiler,
) handlers.Handler {
	if regexCache == nil {
		regexCache = cache.NewRegexCache()
	}

	if selectors == nil {
		selectors = cache.NewLabelSelectorCache()
	}

	return genericvalidation.ForKind(corev1.SchemeGroupVersion.WithKind("PersistentVolumeClaim").GroupKind(),
		handlers.MatchingRequest(appliesAttributeClass,
			&handlers.TypedTenantWithRulesetHandler[*corev1.PersistentVolumeClaim]{
				Factory:       func() *corev1.PersistentVolumeClaim { return &corev1.PersistentVolumeClaim{} },
				Configuration: cfg,
				Handlers: []handlers.TypedHandlerWithTenantWithRuleset[*corev1.PersistentVolumeClaim]{
					&attributeClassRules{regexCache: regexCache, selectors: selectors, compiler: compiler},
				},
			}))
}

// claimFields holds the fields that decide whether a request needs a check.
type claimFields struct {
	Spec struct {
		VolumeAttributesClassName *string `json:"volumeAttributesClassName"`
	} `json:"spec"`
	Status struct {
		CurrentVolumeAttributesClassName *string `json:"currentVolumeAttributesClassName"`
	} `json:"status"`
}

// appliesAttributeClass lets a request through when it names a class that the
// stored claim neither references nor has applied. Kubernetes applies no class
// to a claim without one, and restoring the applied class cancels a
// modification, so neither applies a new class; resize, label and finalizer
// updates keep it. Undecodable payloads continue, so that the typed handler
// reports them.
func appliesAttributeClass(req admission.Request) bool {
	if !bytes.Contains(req.Object.Raw, attributeClassKey) {
		return false
	}

	var current claimFields
	if json.Unmarshal(req.Object.Raw, &current) != nil {
		return true
	}

	name := ptr.Deref(current.Spec.VolumeAttributesClassName, "")
	if name == "" {
		return false
	}

	if req.Operation != admissionv1.Update {
		return true
	}

	var previous claimFields
	if json.Unmarshal(req.OldObject.Raw, &previous) != nil {
		return true
	}

	return name != ptr.Deref(previous.Spec.VolumeAttributesClassName, "") &&
		name != ptr.Deref(previous.Status.CurrentVolumeAttributesClassName, "")
}

func hasAttributeClasses(body *rules.NamespaceRuleEnforceBody) bool {
	return len(body.Storage.AttributeClasses) > 0
}

// attributeClassReader reads the claim's VolumeAttributesClass at most once,
// and only when a selector has to be matched. A missing class never matches a
// selector.
type attributeClassReader struct {
	reader client.Reader
	name   string

	read  bool
	class *storagev1.VolumeAttributesClass
	err   error
}

func (c *attributeClassReader) get(ctx context.Context) (*storagev1.VolumeAttributesClass, error) {
	if c.read {
		return c.class, c.err
	}

	c.read = true

	class := &storagev1.VolumeAttributesClass{}
	if err := c.reader.Get(ctx, types.NamespacedName{Name: c.name}, class); err != nil {
		if !apierrors.IsNotFound(err) {
			c.err = fmt.Errorf("get VolumeAttributesClass %q: %w", c.name, err)
		}

		return nil, c.err
	}

	c.class = class

	return class, nil
}

func (h *attributeClassRules) OnCreate(_ client.Client, reader client.Reader, obj *corev1.PersistentVolumeClaim, _ admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.validate(reader, obj, recorder, tnt, bodies)
}

func (h *attributeClassRules) OnUpdate(_ client.Client, reader client.Reader, _ *corev1.PersistentVolumeClaim, obj *corev1.PersistentVolumeClaim, _ admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.validate(reader, obj, recorder, tnt, bodies)
}

func (*attributeClassRules) OnDelete(client.Client, client.Reader, *corev1.PersistentVolumeClaim, admission.Decoder, events.EventRecorder, *capsulev1beta2.Tenant, []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { return nil }
}

func (h *attributeClassRules) validate(reader client.Reader, obj *corev1.PersistentVolumeClaim, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		enforce := ruleengine.EnforceBodiesFromNamespaceRules(bodies)
		if !slices.ContainsFunc(enforce, hasAttributeClasses) {
			return nil
		}

		enforce, err := ruleengine.FilterEnforcementConditions(ctx,
			ruleengine.NewConditionEvaluator(h.compiler, req.AdmissionRequest), obj, enforce, hasAttributeClasses)
		if err != nil {
			return ad.Deny(fmt.Sprintf("enforce: %s", err))
		}

		classes := &attributeClassReader{reader: reader, name: ptr.Deref(obj.Spec.VolumeAttributesClassName, "")}

		result, err := h.evaluate(ctx, obj, classes, enforce)
		if classes.err != nil {
			return handlers.ErroredResponse(classes.err)
		}

		if err != nil {
			return ad.Deny(err.Error())
		}

		blocking := result.BlockingError()

		if req.DryRun == nil || !*req.DryRun {
			for _, audit := range result.Audits {
				recorder.LabeledEvent(obj, corev1.EventTypeNormal, events.ReasonNamespaceRuleAudit, events.ActionRuleAudit, audit.Message).
					WithRelated(tnt).WithTenantLabel(tnt).WithRequestAnnotations(req).Emit(ctx)
			}

			if blocking != nil {
				recorder.LabeledEvent(obj, corev1.EventTypeWarning, result.Blocking.EventReason, events.ActionValidationDenied, blocking.Error()).
					WithRelated(tnt).WithTenantLabel(tnt).WithRequestAnnotations(req).Emit(ctx)
			}
		}

		if blocking != nil {
			return ad.Deny(blocking.Error())
		}

		return nil
	}
}

func (h *attributeClassRules) evaluate(
	ctx context.Context,
	obj *corev1.PersistentVolumeClaim,
	classes *attributeClassReader,
	enforce []*rules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	return ruleengine.EvaluateEnforce(obj, enforce, ruleengine.Set[rules.StorageClassMatch, *corev1.PersistentVolumeClaim]{
		Name:        attributeClassSetName,
		EventReason: events.ReasonForbiddenVolumeAttributesClass,
		Values: func(*corev1.PersistentVolumeClaim) []ruleengine.Value {
			return []ruleengine.Value{{Value: classes.name, Path: attributeClassPath}}
		},
		Rules: func(body *rules.NamespaceRuleEnforceBody) []rules.StorageClassMatch {
			return rules.StorageClassesToMatch(body.Action, body.Storage.AttributeClasses, h.regexCache, classes.name)
		},
		Matches: func(match rules.StorageClassMatch, value ruleengine.Value) (ruleengine.Match, error) {
			if match.HasNameMatch() {
				matched, err := match.NameMatch().MatchesWithExpressionMatcher(h.regexCache, value.Value)
				if err != nil {
					return ruleengine.Match{}, err
				}

				if matched {
					return ruleengine.Match{Matched: true, MatchedValue: match.Describe()}, nil
				}
			}

			if !match.HasSelector() {
				return ruleengine.Match{}, nil
			}

			selector, err := h.selectors.GetOrCompile(&match.LabelSelector)
			if err != nil {
				return ruleengine.Match{}, err
			}

			class, err := classes.get(ctx)
			if err != nil || class == nil {
				return ruleengine.Match{}, err
			}

			return ruleengine.Match{Matched: selector.Matches(labels.Set(class.Labels)), MatchedValue: match.Describe()}, nil
		},
		RuleDescription:    rules.StorageClassMatch.Describe,
		AllowedDescription: "Allowed classes",
	})
}
