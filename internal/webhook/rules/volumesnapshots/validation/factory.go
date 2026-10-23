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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
// skip for updates that can't change the class, happen before decoding and
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

// snapshotFields holds the fields that decide whether an update needs a check.
type snapshotFields struct {
	Metadata struct {
		OwnerReferences []metav1.OwnerReference `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		Source struct {
			VolumeSnapshotContentName *string `json:"volumeSnapshotContentName"`
		} `json:"source"`
		VolumeSnapshotClassName *string `json:"volumeSnapshotClassName"`
	} `json:"spec"`
	Status *struct {
		BoundVolumeSnapshotContentName *string `json:"boundVolumeSnapshotContentName"`
	} `json:"status"`
}

func (f *snapshotFields) defaultsClass() bool {
	return defaultsClass(f.Spec.Source.VolumeSnapshotContentName, f.Metadata.OwnerReferences)
}

func (f *snapshotFields) bound() bool {
	return f.Status != nil && ptr.Deref(f.Status.BoundVolumeSnapshotContentName, "") != ""
}

// classMayChange lets an update through when it changes the class, or when it
// opens an unbound snapshot without a class to defaulting, for example by
// removing its VolumeGroupSnapshot owner. Finalizer and label updates, such as
// the snapshot-controller's, and the garbage collector orphaning a bound group
// member are skipped. Undecodable payloads continue, so that the typed handler
// reports them.
func classMayChange(req admission.Request) bool {
	if req.Operation != admissionv1.Update {
		return true
	}

	var current, previous snapshotFields
	if json.Unmarshal(req.Object.Raw, &current) != nil || json.Unmarshal(req.OldObject.Raw, &previous) != nil {
		return true
	}

	if !ptr.Equal(current.Spec.VolumeSnapshotClassName, previous.Spec.VolumeSnapshotClassName) {
		return true
	}

	return current.Spec.VolumeSnapshotClassName == nil && !current.bound() &&
		current.defaultsClass() && !previous.defaultsClass()
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
