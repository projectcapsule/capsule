// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"encoding/json"
	"fmt"

	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
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

type volumeSnapshotRules struct {
	regexCache *cache.RegexCache
	selectors  *cache.LabelSelectorCache
	compiler   ruleengine.ConditionCompiler
}

// Handler joins the generic rules chain. Kind/subresource filtering, and the
// skip for updates that keep the class, happen before decoding and
// tenant/ruleset reads; a successful check continues it.
func Handler(
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

	return genericvalidation.ForKind(snapshotv1.SchemeGroupVersion.WithKind("VolumeSnapshot").GroupKind(),
		handlers.MatchingRequest(classMayChange,
			&handlers.TypedTenantWithRulesetHandler[*snapshotv1.VolumeSnapshot]{
				Factory:       func() *snapshotv1.VolumeSnapshot { return &snapshotv1.VolumeSnapshot{} },
				Configuration: cfg,
				Handlers: []handlers.TypedHandlerWithTenantWithRuleset[*snapshotv1.VolumeSnapshot]{
					&volumeSnapshotRules{regexCache: regexCache, selectors: selectors, compiler: compiler},
				},
			}))
}

type classReference struct {
	Spec struct {
		VolumeSnapshotClassName *string `json:"volumeSnapshotClassName"`
	} `json:"spec"`
}

// classMayChange lets updates through only when they change the class.
// Finalizer and label updates, such as the snapshot-controller's, keep it.
// Undecodable payloads continue, so that the typed handler reports them.
func classMayChange(req admission.Request) bool {
	if req.Operation != admissionv1.Update {
		return true
	}

	var current, previous classReference
	if json.Unmarshal(req.Object.Raw, &current) != nil || json.Unmarshal(req.OldObject.Raw, &previous) != nil {
		return true
	}

	return !ptr.Equal(current.Spec.VolumeSnapshotClassName, previous.Spec.VolumeSnapshotClassName)
}

func (h *volumeSnapshotRules) OnCreate(_ client.Client, reader client.Reader, obj *snapshotv1.VolumeSnapshot, _ admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.validate(reader, obj, recorder, tnt, bodies)
}

func (h *volumeSnapshotRules) OnUpdate(_ client.Client, reader client.Reader, _ *snapshotv1.VolumeSnapshot, obj *snapshotv1.VolumeSnapshot, _ admission.Decoder, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return h.validate(reader, obj, recorder, tnt, bodies)
}

func (*volumeSnapshotRules) OnDelete(client.Client, client.Reader, *snapshotv1.VolumeSnapshot, admission.Decoder, events.EventRecorder, *capsulev1beta2.Tenant, []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response { return nil }
}

func (h *volumeSnapshotRules) validate(reader client.Reader, obj *snapshotv1.VolumeSnapshot, recorder events.EventRecorder, tnt *capsulev1beta2.Tenant, bodies []*rules.NamespaceRuleBodyNamespace) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		enforce := ruleengine.EnforceBodiesFromNamespaceRules(bodies)
		if !hasClassRules(enforce) {
			return nil
		}

		enforce, err := ruleengine.FilterEnforcementConditions(ctx,
			ruleengine.NewConditionEvaluator(h.compiler, req.AdmissionRequest), obj, enforce, appliesToVolumeSnapshots)
		if err != nil {
			return ad.Deny(fmt.Sprintf("enforce: %s", err))
		}

		class, err := readClass(ctx, reader, obj, enforce)
		if err != nil {
			return handlers.ErroredResponse(err)
		}

		result, err := h.evaluate(obj, class, enforce)
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
