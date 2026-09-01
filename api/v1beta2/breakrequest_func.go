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

	var duration *metav1.Duration

	switch {
	case br.Status.Approved != nil && br.Status.Approved.Duration != nil:
		// Non-nil approved duration is authoritative; 0 means "unlimited".
		duration = br.Status.Approved.Duration
	case br.Spec.Duration != nil && br.Spec.Duration.Duration != 0:
		duration = br.Spec.Duration
	}

	br.Status.Active.ActiveFrom = &now

	keepFor := br.effectiveKeepFor()

	if keepFor > 0 {
		controllerutil.AddFinalizer(br, meta.ControllerFinalizer)
	}

	if duration != nil && duration.Duration > 0 {
		// If a duration was set, otherwise the lifecycle must be canceled manually
		activeUntil := now.Add(duration.Duration)
		activeUntilTime := metav1.NewTime(activeUntil)
		br.Status.Active.ActiveUntil = &activeUntilTime

		if keepFor > 0 {
			keepUntil := metav1.NewTime(activeUntil.Add(time.Duration(keepFor)))
			br.Status.KeepUntil = &keepUntil
		}
	}

	return nil
}

// ExpireRequest terminates a request from any lifecycle phase. For active
// requests this revokes granted access; the request itself may remain longer
// for auditing purposes.
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

	keepFor := br.effectiveKeepFor()

	if keepFor > 0 {
		controllerutil.AddFinalizer(br, meta.ControllerFinalizer)
	}

	// If the request had no bounded ActiveUntil (e.g., "unlimited" duration) but keepFor is set,
	// compute KeepUntil from the expiration time so the controller can retain the object for auditing.
	if br.Status.KeepUntil == nil && keepFor > 0 {
		keepUntil := metav1.NewTime(now.Add(time.Duration(keepFor)))
		br.Status.KeepUntil = &keepUntil
	}

	return nil
}

// DeleteRequest Final stage, delete the request.
func (br *BreakRequest) DeleteRequest() {
	controllerutil.RemoveFinalizer(br, meta.ControllerFinalizer)
}

// GenerateApprovedProperties returns the effective lifecycle properties for a
// review. Passing the referenced template resolves its lifecycle defaults for
// display and approval. Already rendered resources are retained in the returned
// approval snapshot.
func (br *BreakRequest) GenerateApprovedProperties(
	templates ...BreakRequestTemplateSource,
) (*ApprovedProperties, error) {
	startTime := metav1.Now()
	if br.Spec.StartTime != nil && !br.Spec.StartTime.IsZero() {
		startTime = *br.Spec.StartTime
	}

	properties := &ApprovedProperties{
		Duration:  br.Spec.Duration,
		StartTime: &startTime,
	}
	if br.Status.Approved != nil {
		properties.Resources = make([]apiruntime.RenderedResource, len(br.Status.Approved.Resources))
		for i := range br.Status.Approved.Resources {
			br.Status.Approved.Resources[i].DeepCopyInto(&properties.Resources[i])
		}
	}

	if len(templates) > 0 && templates[0] != nil {
		if err := br.resolveApprovedProperties(templates[0], properties); err != nil {
			return nil, err
		}
	}

	return properties, nil
}

// ResolveApprovedProperties fills template defaults omitted by a reviewer and
// validates the effective duration before activation.
func (br *BreakRequest) ResolveApprovedProperties(brt BreakRequestTemplateSource) error {
	if br.Status.Approved == nil {
		return errors.New("approved status is nil")
	}

	return br.resolveApprovedProperties(brt, br.Status.Approved)
}

func (br *BreakRequest) resolveApprovedProperties(
	brt BreakRequestTemplateSource,
	properties *ApprovedProperties,
) error {
	if brt == nil {
		return errors.New("template is nil")
	}

	templateData := brt.TemplateData()

	if properties.Duration == nil {
		switch {
		case br.Spec.Duration != nil:
			duration := *br.Spec.Duration
			properties.Duration = &duration
		case templateData.DefaultDuration != nil:
			duration := *templateData.DefaultDuration
			properties.Duration = &duration
		}
	}

	if templateData.MaxDuration != nil && templateData.MaxDuration.Duration > 0 && properties.Duration != nil &&
		properties.Duration.Duration > templateData.MaxDuration.Duration {
		return fmt.Errorf(
			"requested duration %s exceeds template maxDuration %s",
			properties.Duration.Duration,
			templateData.MaxDuration.Duration,
		)
	}

	if properties.KeepFor == nil && templateData.KeepFor != nil {
		keepFor := *templateData.KeepFor
		properties.KeepFor = &keepFor
	}

	if properties.StartTime == nil {
		startTime := metav1.Now()
		if br.Spec.StartTime != nil && !br.Spec.StartTime.IsZero() {
			startTime = *br.Spec.StartTime
		}

		properties.StartTime = &startTime
	}

	return nil
}

// RenderResources renders direct targets with the flat parameter/context
// values and expands optional multi-document templates with the structured
// .params and .context.resources values.
func (br *BreakRequest) RenderResources(
	schema *k8sruntime.RawExtension,
	resources []apiruntime.ResourceTemplate,
	contexts ...template.ReferenceContext,
) ([]apiruntime.RenderedResource, error) {
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

	rendered := make([]apiruntime.RenderedResource, 0, len(resources))

	var renderErr error

	for resourceIndex, resource := range resources {
		renderedResource := apiruntime.RenderedResource{Policy: resource.Policy}

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

// LoadTemplateContext loads external resources declared by the referenced
// GlobalBreakRequestTemplate. Request parameters are available while resolving
// resource names, namespaces, and selectors.
func (br *BreakRequest) LoadTemplateContext(
	ctx context.Context,
	c client.Client,
	mapper k8smeta.RESTMapper,
	schema *k8sruntime.RawExtension,
	templateContext *template.TemplateContext,
) (template.ReferenceContext, error) {
	if templateContext == nil {
		return template.ReferenceContext{}, nil
	}

	if c == nil {
		return nil, errors.New("kubernetes client is required to load template context")
	}

	if mapper == nil {
		return nil, errors.New("REST mapper is required to load template context")
	}

	params, err := br.templateParameters(schema)
	if err != nil {
		return nil, err
	}

	fastContext := parameterFastContext(params)

	if err := templateContext.ValidateVariables(fastContext); err != nil {
		return nil, fmt.Errorf("validating template context: %w", err)
	}

	loaded, err := templateContext.GatherContext(
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

func (br *BreakRequest) templateParameters(schema *k8sruntime.RawExtension) (template.ReferenceContext, error) {
	var paramBytes []byte
	if br.Spec.Params != nil {
		paramBytes = br.Spec.Params.Raw
	}

	var schemaBytes []byte
	if schema != nil {
		schemaBytes = schema.Raw
	}

	if err := template.Validate(schemaBytes, paramBytes); err != nil {
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

func (br *BreakRequest) effectiveKeepFor() breaktheglass.ExtendedDuration {
	var keepFor breaktheglass.ExtendedDuration

	if br.Status.Approved != nil && br.Status.Approved.KeepFor != nil {
		keepFor = *br.Status.Approved.KeepFor
	}

	return keepFor
}

// SetReady records whether the rendered resources and their reconciliation are
// ready. Phase conditions are kept alongside this condition for lifecycle
// history.
func (br *BreakRequest) SetReady(status metav1.ConditionStatus, reason, message string) {
	k8smeta.SetStatusCondition(&br.Status.Conditions, metav1.Condition{
		Type:               meta.ReadyCondition,
		Status:             status,
		ObservedGeneration: br.Generation,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
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

	case RequestPhaseExpired: // terminal transition from any phase
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
