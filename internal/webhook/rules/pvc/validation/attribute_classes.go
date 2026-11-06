// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync/atomic"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	storagev1beta1 "k8s.io/api/storage/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
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

	// unservedVersionTTL is how long an API version the cluster doesn't serve
	// is skipped before it is tried again.
	unservedVersionTTL = 30 * time.Second
)

// attributeClassKey prefilters requests. API admission snapshots encode keys
// literally, so an object whose payload lacks the key sets no class. The search
// runs over every Pod and workload write, and starts with a letter that JSON
// rarely contains: a leading quote would stop at every key and string, which
// makes searching a common Pod two to three times slower. The key also matches
// the class fields of a claim's status, which the decoding then ignores.
var attributeClassKey = []byte(`AttributesClassName"`)

// attributeClassVersions are the VolumeAttributesClass APIs in order of
// preference. Kubernetes serves storage.k8s.io/v1 from 1.34; 1.33 serves only
// v1beta1, behind the VolumeAttributesClass feature gate.
var attributeClassVersions = [...]func() client.Object{
	func() client.Object { return &storagev1.VolumeAttributesClass{} },
	func() client.Object { return &storagev1beta1.VolumeAttributesClass{} },
}

// servedVersions skips the VolumeAttributesClass APIs the cluster doesn't
// serve: reading one costs the REST mapper a discovery request. unserved holds
// the Unix time in nanoseconds until which each version is skipped.
type servedVersions struct {
	now      func() time.Time
	unserved [len(attributeClassVersions)]atomic.Int64
}

func (s *servedVersions) skips(version int) bool {
	return s.now().UnixNano() < s.unserved[version].Load()
}

func (s *servedVersions) unserve(version int) {
	s.unserved[version].Store(s.now().Add(unservedVersionTTL).UnixNano())
}

type attributeClassRules struct {
	regexCache *cache.RegexCache
	selectors  *cache.LabelSelectorCache
	compiler   ruleengine.ConditionCompiler
	versions   servedVersions
}

// AttributeClassHandler joins the generic rules chain, which receives every
// write in tenant namespaces. It checks the class of a claim and the classes of
// the claim templates the StatefulSet and ephemeral volume controllers create
// claims from. Those controllers create the claims under their own identity, so
// a rule for an audience can only apply to the request of the Pod or workload
// that carries the template. Requests that apply no new class skip before
// decoding and tenant/ruleset reads; a successful check continues the chain.
func AttributeClassHandler(
	cfg configuration.Configuration,
	regexCache *cache.RegexCache,
	selectors *cache.LabelSelectorCache,
	compiler ruleengine.ConditionCompiler,
) handlers.Handler {
	return newAttributeClassHandler(cfg, regexCache, selectors, compiler, time.Now)
}

func newAttributeClassHandler(
	cfg configuration.Configuration,
	regexCache *cache.RegexCache,
	selectors *cache.LabelSelectorCache,
	compiler ruleengine.ConditionCompiler,
	now func() time.Time,
) handlers.Handler {
	if regexCache == nil {
		regexCache = cache.NewRegexCache()
	}

	if selectors == nil {
		selectors = cache.NewLabelSelectorCache()
	}

	return handlers.MatchingRequest(appliesAttributeClass,
		&handlers.TypedTenantWithRulesetHandler[*metav1.PartialObjectMetadata]{
			Factory:       func() *metav1.PartialObjectMetadata { return &metav1.PartialObjectMetadata{} },
			Configuration: cfg,
			Handlers: []handlers.TypedHandlerWithTenantWithRuleset[*metav1.PartialObjectMetadata]{
				&attributeClassRules{regexCache: regexCache, selectors: selectors, compiler: compiler, versions: servedVersions{now: now}},
			},
		})
}

// claimClass holds the class of a claim or claim template.
type claimClass struct {
	VolumeAttributesClassName *string `json:"volumeAttributesClassName"`
}

// ephemeralVolume holds the claim template of a generic ephemeral volume.
type ephemeralVolume struct {
	Name      string `json:"name"`
	Ephemeral *struct {
		VolumeClaimTemplate *struct {
			Spec claimClass `json:"spec"`
		} `json:"volumeClaimTemplate"`
	} `json:"ephemeral"`
}

type podTemplate struct {
	Spec struct {
		Volumes []ephemeralVolume `json:"volumes"`
	} `json:"spec"`
}

// claimFields holds the class a claim sets and the one Kubernetes applied.
type claimFields struct {
	Spec   claimClass `json:"spec"`
	Status struct {
		CurrentVolumeAttributesClassName *string `json:"currentVolumeAttributesClassName"`
	} `json:"status"`
}

// templateFields holds the claim templates of Pods and the native workloads.
// The API server only sends the fields of the admitted kind.
type templateFields struct {
	Spec struct {
		Volumes     []ephemeralVolume `json:"volumes"`
		Template    *podTemplate      `json:"template"`
		JobTemplate *struct {
			Spec struct {
				Template *podTemplate `json:"template"`
			} `json:"spec"`
		} `json:"jobTemplate"`
		VolumeClaimTemplates []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec claimClass `json:"spec"`
		} `json:"volumeClaimTemplates"`
	} `json:"spec"`
}

func isClaim(gvk schema.GroupVersionKind) bool {
	return gvk.Group == "" && gvk.Kind == "PersistentVolumeClaim"
}

// setsAttributeClasses reports whether objects of a kind can set a class: a
// claim sets its own, Pods and the native workloads set those of their claim
// templates.
func setsAttributeClasses(gvk schema.GroupVersionKind) bool {
	if isClaim(gvk) {
		return true
	}

	_, ok := rules.WorkloadTargetForGVK(gvk)

	return ok
}

// classValue is a class an object sets. template names the claim template
// that sets it, the way Kubernetes identifies it, and is empty for a claim.
type classValue struct {
	ruleengine.Value

	template string
}

// classValues returns the classes an object sets, and the class Kubernetes
// applied to a claim. A claim or claim template without a class gets none:
// Kubernetes has no default class.
func classValues(gvk schema.GroupVersionKind, raw []byte) ([]classValue, string, error) {
	if isClaim(gvk) {
		var claim claimFields
		if err := json.Unmarshal(raw, &claim); err != nil {
			return nil, "", fmt.Errorf("decode %s classes: %w", gvk.Kind, err)
		}

		var values []classValue
		if name := ptr.Deref(claim.Spec.VolumeAttributesClassName, ""); name != "" {
			values = []classValue{{Value: ruleengine.Value{Value: name, Path: attributeClassPath}}}
		}

		return values, ptr.Deref(claim.Status.CurrentVolumeAttributesClassName, ""), nil
	}

	var obj templateFields
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, "", fmt.Errorf("decode %s classes: %w", gvk.Kind, err)
	}

	target, _ := rules.WorkloadTargetForGVK(gvk)

	var values []classValue

	switch {
	case target == rules.ValidatePod:
		values = ephemeralClasses(values, obj.Spec.Volumes, "spec.volumes")
	case target == rules.ValidateCronJob:
		if job := obj.Spec.JobTemplate; job != nil && job.Spec.Template != nil {
			values = ephemeralClasses(values, job.Spec.Template.Spec.Volumes, "spec.jobTemplate.spec.template.spec.volumes")
		}
	case obj.Spec.Template != nil:
		values = ephemeralClasses(values, obj.Spec.Template.Spec.Volumes, "spec.template.spec.volumes")
	}

	if target == rules.ValidateStatefulSet {
		for i, template := range obj.Spec.VolumeClaimTemplates {
			if name := ptr.Deref(template.Spec.VolumeAttributesClassName, ""); name != "" {
				values = append(values, classValue{
					Value: ruleengine.Value{
						Value: name,
						Path:  fmt.Sprintf("spec.volumeClaimTemplates[%d](%s).%s", i, template.Metadata.Name, attributeClassPath),
					},
					template: "claim/" + template.Metadata.Name,
				})
			}
		}
	}

	return values, "", nil
}

func ephemeralClasses(values []classValue, volumes []ephemeralVolume, path string) []classValue {
	for i, volume := range volumes {
		if volume.Ephemeral == nil || volume.Ephemeral.VolumeClaimTemplate == nil {
			continue
		}

		if name := ptr.Deref(volume.Ephemeral.VolumeClaimTemplate.Spec.VolumeAttributesClassName, ""); name != "" {
			values = append(values, classValue{
				Value: ruleengine.Value{
					Value: name,
					Path:  fmt.Sprintf("%s[%d](%s).ephemeral.volumeClaimTemplate.%s", path, i, volume.Name, attributeClassPath),
				},
				template: "volume/" + volume.Name,
			})
		}
	}

	return values
}

// requestedClasses returns the classes a request applies: those of a claim
// or claim template that the stored object doesn't set, unless Kubernetes
// already applied the class to the claim. A resize, a label change or a scale
// keeps the classes, and restoring the applied class cancels a modification,
// so none of them applies one. A new or renamed template creates new claims,
// so its class is checked. Pods can't change their volumes.
func requestedClasses(req admission.Request) ([]classValue, error) {
	gvk := schema.GroupVersionKind{Group: req.Kind.Group, Version: req.Kind.Version, Kind: req.Kind.Kind}
	if req.SubResource != "" || !setsAttributeClasses(gvk) || !bytes.Contains(req.Object.Raw, attributeClassKey) {
		return nil, nil
	}

	values, _, err := classValues(gvk, req.Object.Raw)
	if err != nil || len(values) == 0 || req.Operation != admissionv1.Update {
		return values, err
	}

	previous, applied, err := classValues(gvk, req.OldObject.Raw)
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(values, func(value classValue) bool {
		if value.template == "" && value.Value.Value == applied {
			return true
		}

		return slices.ContainsFunc(previous, func(old classValue) bool {
			return old.template == value.template && old.Value.Value == value.Value.Value
		})
	}), nil
}

// appliesAttributeClass lets a request through when it applies a class.
// Undecodable payloads continue, so that the handler reports them.
func appliesAttributeClass(req admission.Request) bool {
	values, err := requestedClasses(req)

	return err != nil || len(values) > 0
}

func hasAttributeClasses(body *rules.NamespaceRuleEnforceBody) bool {
	return len(body.Storage.AttributeClasses) > 0
}

// attributeClassReader reads a VolumeAttributesClass at most once, and only
// when a selector has to be matched. A class that doesn't exist, or that a
// cluster without the API can't have, never matches a selector.
type attributeClassReader struct {
	reader   client.Reader
	versions *servedVersions
	name     string

	read   bool
	found  bool
	labels labels.Set
	err    error
}

func (c *attributeClassReader) get(ctx context.Context) (labels.Set, bool, error) {
	if c.read {
		return c.labels, c.found, c.err
	}

	c.read = true

	for version, newClass := range attributeClassVersions {
		if c.versions.skips(version) {
			continue
		}

		class := newClass()

		err := c.reader.Get(ctx, types.NamespacedName{Name: c.name}, class)

		switch {
		case err == nil:
			c.found, c.labels = true, class.GetLabels()

			return c.labels, true, nil
		case meta.IsNoMatchError(err):
			c.versions.unserve(version)
		case apierrors.IsNotFound(err):
			return nil, false, nil
		default:
			c.err = fmt.Errorf("get VolumeAttributesClass %q: %w", c.name, err)

			return nil, false, c.err
		}
	}

	return nil, false, nil
}

func (h *attributeClassRules) OnCreate(_ client.Client, reader client.Reader, obj *metav1.PartialObjectMetadata, _ admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.validate(reader, obj, recorder, tnt, bodies)
}

func (h *attributeClassRules) OnUpdate(_ client.Client, reader client.Reader, _ *metav1.PartialObjectMetadata, obj *metav1.PartialObjectMetadata, _ admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.validate(reader, obj, recorder, tnt, bodies)
}

func (*attributeClassRules) OnDelete(client.Client, client.Reader, *metav1.PartialObjectMetadata, admission.Decoder, events.EventRecorder, *capsulev1beta2.Tenant, []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { return nil }
}

func (h *attributeClassRules) validate(reader client.Reader, obj *metav1.PartialObjectMetadata, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		enforce := ruleengine.EnforceBodiesFromNamespaceRules(bodies)
		if !slices.ContainsFunc(enforce, hasAttributeClasses) {
			return nil
		}

		values, err := requestedClasses(req)
		if err != nil {
			return handlers.ErroredResponse(err)
		}

		// Conditions see the admitted object: the claim, or the Pod or workload
		// with the claim templates.
		enforce, err = ruleengine.FilterEnforcementConditions(ctx,
			ruleengine.NewConditionEvaluator(h.compiler, req.AdmissionRequest), nil, enforce, hasAttributeClasses)
		if err != nil {
			return ad.Deny(fmt.Sprintf("enforce: %s", err))
		}

		// The first value a rule blocks decides the request, as within one
		// evaluation. The request's reader shares the read of a class that
		// several templates name.
		result := &ruleengine.Evaluation{}

		for _, value := range values {
			classes := &attributeClassReader{reader: reader, versions: &h.versions, name: value.Value.Value}

			evaluation, evalErr := h.evaluate(ctx, value.Value, classes, enforce)

			if classes.err != nil {
				return handlers.ErroredResponse(classes.err)
			}

			if evalErr != nil {
				return ad.Deny(evalErr.Error())
			}

			result.Append(evaluation)

			if result.Blocking != nil {
				break
			}
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
	value ruleengine.Value,
	classes *attributeClassReader,
	enforce []*rules.NamespaceRuleEnforceBody,
) (*ruleengine.Evaluation, error) {
	return ruleengine.EvaluateEnforce(value, enforce, ruleengine.Set[rules.StorageClassMatch, ruleengine.Value]{
		Name:        attributeClassSetName,
		EventReason: events.ReasonForbiddenVolumeAttributesClass,
		Values: func(value ruleengine.Value) []ruleengine.Value {
			return []ruleengine.Value{value}
		},
		Rules: func(body *rules.NamespaceRuleEnforceBody) []rules.StorageClassMatch {
			return rules.StorageClassesToMatch(body.Action, body.Storage.AttributeClasses, h.regexCache, value.Value)
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

			classLabels, found, err := classes.get(ctx)
			if err != nil || !found {
				return ruleengine.Match{}, err
			}

			return ruleengine.Match{Matched: selector.Matches(classLabels), MatchedValue: match.Describe()}, nil
		},
		RuleDescription:    rules.StorageClassMatch.Describe,
		AllowedDescription: "Allowed classes",
	})
}
