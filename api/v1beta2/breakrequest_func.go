// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/template"
)

// InitializeFromTemplate Copies all relevant values from the Template.
func (br *BreakRequest) InitializeFromTemplate(brt *BreakRequestTemplate) {
	br.Status.Template = &TemplateProperties{
		Resources:       brt.Spec.Resources,
		ParamSchema:     brt.Spec.ParamSchema,
		Context:         brt.Spec.Context,
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
	properties.Resources = nil

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
func (br *BreakRequest) GenerateApprovedProperties(contexts ...template.ReferenceContext) (*ApprovedProperties, error) {
	tpl := br.Status.Template
	if tpl == nil {
		return nil, errors.New("template not set")
	}

	var resources []apiruntime.ResourceTemplate

	// Loading external context requires a Kubernetes client. Callers such as
	// the CLI can still approve a request without rendering a server-side
	// context preview; the controller always renders it before activation.
	if tpl.Context != nil && len(contexts) == 0 {
		if _, err := br.templateParameters(tpl.ParamSchema); err != nil {
			return nil, err
		}
	} else {
		var err error

		resources, err = br.RenderResources(tpl.ParamSchema, tpl.Resources, contexts...)
		if err != nil {
			return nil, err
		}
	}

	startTime := metav1.Now()
	if br.Spec.StartTime != nil && !br.Spec.StartTime.IsZero() {
		startTime = *br.Spec.StartTime
	}

	return &ApprovedProperties{
		Duration:  br.Spec.Duration,
		StartTime: startTime,
		Resources: resources,
		KeepFor:   tpl.KeepFor,
	}, nil
}

// RenderResources renders direct targets with the flat parameter/context
// values and expands optional multi-document templates with the structured
// .params and .context.resources values.
func (br *BreakRequest) RenderResources(
	schema k8sruntime.RawExtension,
	resources []apiruntime.ResourceTemplate,
	contexts ...template.ReferenceContext,
) ([]apiruntime.ResourceTemplate, error) {
	params, err := br.templateParameters(schema)
	if err != nil {
		return nil, err
	}

	directContext := make(template.ReferenceContext, len(params))
	maps.Copy(directContext, params)

	loadedResources := template.ReferenceContext{}

	for _, additional := range contexts {
		for key, value := range additional {
			if _, found := params[key]; found {
				return nil, fmt.Errorf("template context key %q conflicts with a request parameter", key)
			}

			if _, found := loadedResources[key]; found {
				return nil, fmt.Errorf("template context key %q is defined more than once", key)
			}

			directContext[key] = value
			loadedResources[key] = value
		}
	}

	structuredContext := template.ReferenceContext{
		"params": params,
		"context": template.ReferenceContext{
			"resources": loadedResources,
		},
	}

	rendered := make([]apiruntime.ResourceTemplate, 0, len(resources))

	var renderErr error

	for resourceIndex, resource := range resources {
		renderedResource := apiruntime.ResourceTemplate{Policy: resource.Policy}

		for targetIndex, target := range resource.Targets {
			targetBytes, targetErr := rawExtensionBytes(target)
			if targetErr != nil {
				renderErr = errors.Join(renderErr, fmt.Errorf(
					"reading resource %d target %d: %w",
					resourceIndex,
					targetIndex,
					targetErr,
				))

				continue
			}

			renderedTarget, targetErr := template.RenderTemplateBytes(directContext, "error", targetBytes)
			if targetErr != nil {
				renderErr = errors.Join(renderErr, fmt.Errorf(
					"rendering resource %d target %d: %w",
					resourceIndex,
					targetIndex,
					targetErr,
				))

				continue
			}

			renderedResource.Targets = append(renderedResource.Targets, k8sruntime.RawExtension{Raw: renderedTarget})
		}

		if resource.Template != "" {
			templateItems, templateErr := template.RenderUnstructuredItems(
				structuredContext,
				"error",
				resource.Template,
			)
			if templateErr != nil {
				renderErr = errors.Join(renderErr, fmt.Errorf(
					"rendering resource %d template: %w",
					resourceIndex,
					templateErr,
				))
			} else {
				for _, item := range templateItems {
					renderedResource.Targets = append(renderedResource.Targets, k8sruntime.RawExtension{Object: item})
				}
			}
		}

		if len(renderedResource.Targets) == 0 {
			continue
		}

		rendered = append(rendered, renderedResource)
	}

	return rendered, renderErr
}

func rawExtensionBytes(target k8sruntime.RawExtension) ([]byte, error) {
	if len(target.Raw) > 0 {
		return target.Raw, nil
	}

	if target.Object != nil {
		data, err := json.Marshal(target.Object)
		if err != nil {
			return nil, fmt.Errorf("marshalling target: %w", err)
		}

		return data, nil
	}

	return nil, errors.New("target is empty")
}

// LoadTemplateContext loads the external resources declared by the copied
// BreakRequestTemplate context. Request parameters are available while
// resolving resource names, namespaces, and selectors.
func (br *BreakRequest) LoadTemplateContext(
	ctx context.Context,
	c client.Client,
	mapper k8smeta.RESTMapper,
) (template.ReferenceContext, error) {
	tpl := br.Status.Template
	if tpl == nil {
		return nil, errors.New("template not set")
	}

	if tpl.Context == nil {
		return template.ReferenceContext{}, nil
	}

	if c == nil {
		return nil, errors.New("kubernetes client is required to load template context")
	}

	if mapper == nil {
		return nil, errors.New("REST mapper is required to load template context")
	}

	params, err := br.templateParameters(tpl.ParamSchema)
	if err != nil {
		return nil, err
	}

	fastContext := parameterFastContext(params)

	if err := tpl.Context.ValidateVariables(fastContext); err != nil {
		return nil, fmt.Errorf("validating template context: %w", err)
	}

	loaded, err := tpl.Context.GatherContext(
		ctx,
		c,
		mapper,
		fastContext,
		br.Namespace,
		nil,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("loading template context: %w", err)
	}

	for key := range loaded {
		if _, found := params[key]; found {
			return nil, fmt.Errorf("template context key %q conflicts with a request parameter", key)
		}
	}

	return loaded, nil
}

func (br *BreakRequest) templateParameters(schema k8sruntime.RawExtension) (template.ReferenceContext, error) {
	var paramBytes []byte
	if br.Spec.Params != nil {
		paramBytes = br.Spec.Params.Raw
	}

	if err := template.Validate(schema.Raw, paramBytes); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	params := template.ReferenceContext{}
	if len(paramBytes) > 0 {
		if err := json.Unmarshal(paramBytes, &params); err != nil {
			return nil, fmt.Errorf("error unmarshalling params: %w", err)
		}
	}

	return params, nil
}

func parameterFastContext(params template.ReferenceContext) map[string]string {
	result := make(map[string]string, len(params)*2)

	for key, value := range params {
		flattenParameterFastContext(key, value, result)
	}

	aliases := make(map[string]string, len(result))
	for key, value := range result {
		aliases["."+key] = value
	}

	maps.Copy(result, aliases)

	return result
}

func flattenParameterFastContext(prefix string, value any, result map[string]string) {
	if nested, ok := value.(map[string]any); ok {
		for key, item := range nested {
			flattenParameterFastContext(prefix+"."+key, item, result)
		}

		return
	}

	if stringValue, ok := value.(string); ok {
		result[prefix] = stringValue

		return
	}

	data, err := json.Marshal(value)
	if err == nil {
		result[prefix] = string(data)
	}
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
