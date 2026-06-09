// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"errors"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/projectcapsule/capsule/internal/breaktheglass/template"
	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// InitializeFromTemplate Copies all relevant values from the Template.
func (br *BreakRequest) InitializeFromTemplate(brt *BreakRequestTemplate) {
	br.Status.Template = &TemplateProperties{
		Items:           brt.Spec.Items,
		DefaultDuration: brt.Spec.DefaultDuration,
		MaxDuration:     brt.Spec.MaxDuration,
		KeepFor:         brt.Spec.KeepFor,
	}
}

// SetRequested sets the BreakRequest phase to Requested (pending review).
func (br *BreakRequest) SetRequested() (err error) {
	if err := br.transitionRequestPhase(
		RequestPhaseRequested,
		"Pending Review",
		"PendingReview",
		metav1.Now(),
		nil,
	); err != nil {
		return err
	}

	br.Status.Review = &ReviewInfo{
		Verdict: RequestVerdictPending,
	}

	return err
}

// SetPending Sets Requests to pending.
func (br *BreakRequest) SetPending() (err error) {
	if err := br.transitionRequestPhase(
		RequestPhasePending,
		"Access request pending",
		"PendingBySystem",
		metav1.Now(),
		nil,
	); err != nil {
		return err
	}

	return err
}

// ApproveRequest Approves the BreakRequest. Depending on the start time, it may also directly activate the request.
func (br *BreakRequest) ApproveRequest(
	entity *breaktheglass.AccessEntity,
	properties *ApprovedProperties,
	reason string,
) (err error) {
	if reason == "" {
		reason = "Access request approved"
	}

	if err := br.transitionRequestPhase(
		RequestPhaseApproved,
		reason,
		"ApprovedBy"+entity.Type.String(),
		metav1.Now(),
		entity,
	); err != nil {
		return err
	}

	// items are set by the controller, remove them from the status
	properties.Items = nil

	br.Status.Approved = properties

	br.Status.Review = &ReviewInfo{
		Reviewer: entity,
		Verdict:  RequestVerdictApproved,
		Message:  reason,
	}

	return err
}

// DenyRequest Denies the BreakRequest. It may directly transition to the Denied phase or set a reason for denial.
func (br *BreakRequest) DenyRequest(entity *breaktheglass.AccessEntity, reason string) (err error) {
	if reason == "" {
		reason = "Access request denied"
	}

	if err := br.transitionRequestPhase(
		RequestPhaseDenied,
		reason,
		"DeniedByReviewer",
		metav1.Now(),
		entity,
	); err != nil {
		return err
	}

	br.Status.Review = &ReviewInfo{
		Reviewer: entity,
		Verdict:  RequestVerdictDenied,
		Message:  reason,
	}

	return err
}

// ActiveRequest Activates the BreakRequest, allowing the subject to access the requested resources.
func (br *BreakRequest) ActiveRequest(entity *breaktheglass.AccessEntity) (err error) {
	now := metav1.Now()

	if err := br.transitionRequestPhase(
		RequestPhaseActive,
		"Access request activated",
		"ActivatedBySystem",
		now,
		entity,
	); err != nil {
		return err
	}

	if br.Status.Active == nil {
		br.Status.Active = &ActivePeriod{}
	}

	tpl := br.Status.Template
	if tpl == nil {
		return fmt.Errorf("template not set")
	}

	var duration *metav1.Duration

	switch {
	case br.Status.Approved != nil && br.Status.Approved.Duration != nil:
		// Non-nil approved duration is authoritative; 0 means "unlimited".
		duration = br.Status.Approved.Duration
	case br.Spec.Duration != nil && br.Spec.Duration.Duration != 0:
		duration = br.Spec.Duration
	default:
		duration = tpl.DefaultDuration
	}

	if tpl.MaxDuration.Duration > 0 && duration != nil &&
		duration.Duration > tpl.MaxDuration.Duration {
		return fmt.Errorf("requested duration %s exceeds template maxDuration %s",
			duration.Duration, tpl.MaxDuration.Duration)
	}

	br.Status.Active.ActiveFrom = now

	keepFor := tpl.KeepFor

	if br.Status.Approved != nil {
		keepFor = br.Status.Approved.KeepFor
	}

	if keepFor > 0 {
		controllerutil.AddFinalizer(br, meta.ControllerFinalizer)
	}

	if duration != nil && duration.Duration > 0 {
		// If a duration was set, otherwise the lifecycle must be canceled manually
		activeUntil := now.Add(duration.Duration)
		br.Status.Active.ActiveUntil = metav1.NewTime(activeUntil)

		if keepFor > 0 {
			br.Status.KeepUntil = metav1.NewTime(activeUntil.Add(time.Duration(keepFor)))
		}
	}

	return nil
}

// ExpireRequest When a request is active, it can be expired. This indicates that the granted access is revoked, however,
// this Request itself may be present longer, for auditing purposes.
func (br *BreakRequest) ExpireRequest(entity *breaktheglass.AccessEntity) (err error) {
	now := metav1.Now()

	if err := br.transitionRequestPhase(
		RequestPhaseExpired,
		"Access request expired",
		"ExpiredBySystem",
		now,
		entity,
	); err != nil {
		return err
	}

	keepFor := breaktheglass.ExtendedDuration(0)
	if br.Status.Approved != nil {
		keepFor = br.Status.Approved.KeepFor
	}

	if keepFor > 0 {
		controllerutil.AddFinalizer(br, meta.ControllerFinalizer)
	}

	// If the request had no bounded ActiveUntil (e.g., "unlimited" duration) but keepFor is set,
	// compute KeepUntil from the expiration time so the controller can retain the object for auditing.
	if br.Status.KeepUntil.IsZero() && keepFor > 0 {
		br.Status.KeepUntil = metav1.NewTime(now.Add(time.Duration(keepFor)))
	}

	return nil
}

// DeleteRequest Final stage, delete the request.
func (br *BreakRequest) DeleteRequest() {
	controllerutil.RemoveFinalizer(br, meta.ControllerFinalizer)
}

// GenerateApprovedProperties Get the Properties which are relevant for Review and approval.
func (br *BreakRequest) GenerateApprovedProperties() (*ApprovedProperties, error) {
	tpl := br.Status.Template
	if tpl == nil {
		return nil, errors.New("template not set")
	}

	it, err := br.RenderItems(tpl.Items)
	if err != nil {
		return nil, err
	}

	startTime := metav1.Now()
	if br.Spec.StartTime != nil && !br.Spec.StartTime.IsZero() {
		startTime = *br.Spec.StartTime
	}

	return &ApprovedProperties{
		Duration:  br.Spec.Duration,
		StartTime: startTime,
		Items:     it,
		KeepFor:   tpl.KeepFor,
	}, nil
}

func (br *BreakRequest) RenderItems(ti breaktheglass.TemplateItems) (breaktheglass.Items, error) {
	params := br.Spec.Params
	if params == nil {
		params = breaktheglass.TemplateParams{}
	}

	rendered := make(breaktheglass.Items, len(ti))

	var rerr error

	for name, i := range ti {
		var p []byte
		if ip, ok := params[name]; ok {
			p = ip.Raw
		}

		if err := template.Validate(i.ParamSchema.Raw, p); err != nil {
			rerr = errors.Join(rerr, fmt.Errorf("invalid params for template item %s: %w", name, err))
			rendered[name] = &runtime.RawExtension{}

			continue
		}

		r, err := template.RenderTemplate(i.ManifestTemplate.Raw, p)
		if err != nil {
			rerr = errors.Join(rerr, fmt.Errorf("error rendering template item %s: %w", name, err))
		}

		rendered[name] = &runtime.RawExtension{Raw: r}
	}

	return rendered, rerr
}

// Ensure Phases are valid transitions and handle conditions accordingly.
func (br *BreakRequest) transitionRequestPhase(
	newPhase RequestPhase,
	conditionMessage string,
	reason string,
	now metav1.Time,
	entity *breaktheglass.AccessEntity,
) error {
	// Prevent duplicate condition entries of the same type
	for _, cond := range br.Status.Conditions {
		if RequestPhase(cond.Type) == newPhase {
			return nil
		}
	}

	// Disallow invalid transitions
	switch newPhase {
	case RequestPhaseDenied:
		if br.Status.Phase == RequestPhaseApproved || br.Status.Phase == RequestPhaseActive {
			return fmt.Errorf("cannot deny an already approved or active request")
		}

		setReviewer(br, entity, conditionMessage, RequestVerdictDenied)

	case RequestPhaseApproved:
		if br.Status.Phase == RequestPhaseDenied {
			return fmt.Errorf("cannot approve a denied request")
		}

		setReviewer(br, entity, conditionMessage, RequestVerdictApproved)

	case RequestPhaseActive:
		if br.Status.Phase != RequestPhaseApproved {
			return fmt.Errorf("can only activate an approved request")
		}

	case RequestPhaseExpired:
		if br.Status.Phase != RequestPhaseActive {
			return fmt.Errorf("can only expire an active request")
		}
	case RequestPhasePending, RequestPhaseRequested: // nothing to do here
	}

	// Duplicate condition check already performed above.

	// Add new condition
	br.Status.Conditions = append(
		[]metav1.Condition{{
			Type:               string(newPhase),
			Status:             metav1.ConditionTrue,
			Reason:             reason,
			Message:            conditionMessage,
			LastTransitionTime: now,
		}},
		br.Status.Conditions...,
	)

	// Set the current phase
	br.Status.Phase = newPhase

	return nil
}

func setReviewer(
	ar *BreakRequest,
	entity *breaktheglass.AccessEntity,
	conditionMessage string,
	verdict RequestVerdict,
) {
	if entity != nil {
		ar.Status.Review = &ReviewInfo{
			Reviewer: entity,
			Message:  conditionMessage,
			Verdict:  verdict,
		}
	}
}
