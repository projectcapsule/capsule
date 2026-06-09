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

// SetRequested Sets Requests to pending.
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

	controllerutil.AddFinalizer(br, meta.ControllerFinalizer)

	if br.Status.Active == nil {
		br.Status.Active = &ActivePeriod{}
	}

	br.Status.Active.ActiveFrom = now

	tpl := br.Status.Template
	if tpl == nil {
		return fmt.Errorf("template not set")
	}

	if br.Status.Template.MaxDuration.Duration > 0 &&
		br.Spec.Duration.Duration > tpl.MaxDuration.Duration {
		return fmt.Errorf("requested duration %s exceeds template maxDuration %s",
			br.Spec.Duration.Duration, tpl.MaxDuration.Duration)
	}

	duration := br.Spec.Duration
	if duration == nil || duration.Duration == 0 {
		duration = tpl.DefaultDuration
	}

	if duration != nil && duration.Duration > 0 {
		// If a duration was set, otherwise the lifecycle must be canceled manually
		activeUntil := now.Add(duration.Duration)
		br.Status.Active.ActiveUntil = metav1.NewTime(activeUntil)

		if tpl.KeepFor > 0 {
			br.Status.KeepUntil = metav1.NewTime(activeUntil.Add(time.Duration(tpl.KeepFor)))
		}
	}

	return nil
}

// ExpireRequest When a request is active, it can be expired. This indicates that the granted access is revoked, however,
// this Request itself may be present longer, for auditing purposes.
func (br *BreakRequest) ExpireRequest(entity *breaktheglass.AccessEntity) (err error) {
	if err := br.transitionRequestPhase(
		RequestPhaseExpired,
		"Access request expired",
		"ExpiredBySystem",
		metav1.Now(),
		entity,
	); err != nil {
		return err
	}

	return err
}

// DeleteRequest Final stage, delete the request.
func (br *BreakRequest) DeleteRequest() {
	controllerutil.RemoveFinalizer(br, meta.ControllerFinalizer)
}

// GenerateApprovedProperties Get the Properties which are relevant for Review and approval.
func (br *BreakRequest) GenerateApprovedProperties() (*ApprovedProperties, error) {
	it, err := br.RenderItemsItems(br.Status.Template.Items)
	if err != nil {
		return nil, err
	}

	var keep breaktheglass.ExtendedDuration
	if tpl := br.Status.Template; tpl != nil {
		keep = tpl.KeepFor
	}

	return &ApprovedProperties{
		Duration:  br.Spec.Duration,
		StartTime: metav1.Now(),
		Items:     it,
		KeepFor:   keep,
	}, nil
}

func (br *BreakRequest) RenderItemsItems(ti breaktheglass.TemplateItems) (breaktheglass.Items, error) {
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

	// Prevent duplicate condition entries of the same type
	for _, cond := range br.Status.Conditions {
		if RequestPhase(cond.Type) == newPhase {
			return nil // Already in this state, no-op
		}
	}

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
