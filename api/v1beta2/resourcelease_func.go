// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/resourcelease"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/template"
)

const capsuleControllerActorName = "capsule-controller"

// SetCreated records the initial lifecycle state using the API server's object
// creation time and attributes it to the authenticated requestor.
func (br *ResourceLease) SetCreated(entity *resourcelease.AccessEntity) error {
	timestamp := metav1.Now()
	if !br.CreationTimestamp.IsZero() {
		timestamp = br.CreationTimestamp
	}

	actor := transitionActor(entity)

	message := "ResourceLease created"
	if actor.Name != "" {
		message = fmt.Sprintf("ResourceLease created by %s", actor.Name)
	}

	reason := "CreatedBy" + actor.Type.String()

	return br.transitionResourceLeasePhase(
		ResourceLeasePhaseCreated,
		message,
		reason,
		timestamp,
		entity,
	)
}

// SetRequested sets the ResourceLease phase to Requested (pending review).
func (br *ResourceLease) SetRequested() error {
	return br.setRequested(nil)
}

// SetRequestedBy sets the ResourceLease phase to Requested and attributes the
// initial lifecycle transition to the authenticated requestor.
func (br *ResourceLease) SetRequestedBy(entity *resourcelease.AccessEntity) error {
	return br.setRequested(entity)
}

func (br *ResourceLease) setRequested(entity *resourcelease.AccessEntity) (err error) {
	if err := br.transitionResourceLeasePhase(
		ResourceLeasePhaseRequested,
		"Pending Review",
		"PendingReview",
		metav1.Now(),
		entity,
	); err != nil {
		return err
	}

	br.Status.Review = &ReviewInfo{
		Verdict: ResourceLeaseVerdictPending,
	}

	return err
}

// SetPending Sets Requests to pending.
func (br *ResourceLease) SetPending() (err error) {
	if err := br.transitionResourceLeasePhase(
		ResourceLeasePhasePending,
		"Resource lease pending",
		"PendingBySystem",
		metav1.Now(),
		nil,
	); err != nil {
		return err
	}

	return err
}

// ApproveLease Approves the ResourceLease. Depending on the start time, it may also directly activate the request.
func (br *ResourceLease) ApproveLease(
	entity *resourcelease.AccessEntity,
	properties *ResourceLeaseStatusRequest,
	reason string,
) (err error) {
	if reason == "" {
		reason = "Resource lease approved"
	}

	if err := br.transitionResourceLeasePhase(
		ResourceLeasePhaseApproved,
		reason,
		"ApprovedBy"+entity.Type.String(),
		metav1.Now(),
		entity,
	); err != nil {
		return err
	}

	br.Status.Request = properties

	br.Status.Review = &ReviewInfo{
		Reviewer: entity,
		Verdict:  ResourceLeaseVerdictApproved,
		Message:  reason,
	}

	return err
}

// DenyLease Denies the ResourceLease. It may directly transition to the Denied phase or set a reason for denial.
func (br *ResourceLease) DenyLease(entity *resourcelease.AccessEntity, reason string) (err error) {
	if reason == "" {
		reason = "Resource lease denied"
	}

	if err := br.transitionResourceLeasePhase(
		ResourceLeasePhaseDenied,
		reason,
		"DeniedByReviewer",
		metav1.Now(),
		entity,
	); err != nil {
		return err
	}

	br.Status.Review = &ReviewInfo{
		Reviewer: entity,
		Verdict:  ResourceLeaseVerdictDenied,
		Message:  reason,
	}

	return err
}

// ActivateLease activates the ResourceLease and materializes its requested resources.
func (br *ResourceLease) ActivateLease(entity *resourcelease.AccessEntity) (err error) {
	now := metav1.Now()

	if err := br.transitionResourceLeasePhase(
		ResourceLeasePhaseActive,
		"Resource lease activated",
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
	case br.Status.Request != nil && br.Status.Request.Duration != nil:
		// Non-nil approved duration is authoritative; 0 means "unlimited".
		duration = br.Status.Request.Duration
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

// ExpireLease terminates a lease from any lifecycle phase. For active leases,
// resources are removed or orphaned according to their deletion policy; the
// lease itself may remain longer for auditing purposes.
func (br *ResourceLease) ExpireLease(entity *resourcelease.AccessEntity) (err error) {
	now := metav1.Now()
	message := "Resource lease expired automatically"
	reason := "ExpiredBySystem"

	if entity != nil {
		message = fmt.Sprintf("Resource lease expired by %s", entity.Name)
		reason = "ExpiredBy" + entity.Type.String()
	}

	if err := br.transitionResourceLeasePhase(
		ResourceLeasePhaseExpired,
		message,
		reason,
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

// FailLease records a recoverable controller failure. The rendered snapshot,
// review, and processed resources remain untouched so Retry can safely resume.
func (br *ResourceLease) FailLease(
	stage ResourceLeaseFailureStage,
	retryPhase ResourceLeasePhase,
	reason,
	message string,
) error {
	if retryPhase != ResourceLeasePhaseRequested && retryPhase != ResourceLeasePhaseApproved {
		return fmt.Errorf("retry phase %q is not supported", retryPhase)
	}

	if err := br.transitionResourceLeasePhase(
		ResourceLeasePhaseFailed,
		message,
		reason,
		metav1.Now(),
		nil,
	); err != nil {
		return err
	}

	br.Status.Failure = &ResourceLeaseFailure{
		Stage:      stage,
		RetryPhase: retryPhase,
		Reason:     reason,
		Message:    message,
	}

	return nil
}

// RetryLease requests one controller-owned recovery attempt. Admission
// reconstructs this transition from the persisted failure status.
func (br *ResourceLease) RetryLease(entity *resourcelease.AccessEntity) error {
	if br.Status.Phase != ResourceLeasePhaseFailed {
		return fmt.Errorf("can only retry a failed request")
	}

	if br.Status.Failure == nil {
		return fmt.Errorf("cannot retry without failure details")
	}

	reason := "RetryLeaseedBySystem"
	message := "ResourceLease retry requested"

	if entity != nil {
		reason = "RetryLeaseedBy" + entity.Type.String()
		message = fmt.Sprintf("ResourceLease retry requested by %s", entity.Name)
	}

	return br.transitionResourceLeasePhase(
		ResourceLeasePhaseRetrying,
		message,
		reason,
		metav1.Now(),
		entity,
	)
}

// CompleteRetry restores the trusted phase captured when the request failed.
// An automatically approved preflight retry receives a system review; an
// activation retry preserves the existing review.
func (br *ResourceLease) CompleteRetry() error {
	if br.Status.Phase != ResourceLeasePhaseRetrying {
		return fmt.Errorf("can only complete a retrying request")
	}

	if br.Status.Failure == nil {
		return fmt.Errorf("cannot complete retry without failure details")
	}

	target := br.Status.Failure.RetryPhase

	var err error

	switch target {
	case ResourceLeasePhaseRequested:
		err = br.SetRequested()
	case ResourceLeasePhaseApproved:
		if br.Status.Review == nil || br.Status.Review.Verdict != ResourceLeaseVerdictApproved {
			err = br.ApproveLease(
				&resourcelease.AccessEntity{Type: resourcelease.AccessEntityTypeSystem},
				br.Status.Request,
				"Auto Approved",
			)
		} else {
			err = br.transitionResourceLeasePhase(
				ResourceLeasePhaseApproved,
				"ResourceLease retry ready for activation",
				"RetrySucceeded",
				metav1.Now(),
				nil,
			)
		}
	case ResourceLeasePhasePending,
		ResourceLeasePhaseCreated,
		ResourceLeasePhaseDenied,
		ResourceLeasePhaseActive,
		ResourceLeasePhaseFailed,
		ResourceLeasePhaseRetrying,
		ResourceLeasePhaseExpired:
		return fmt.Errorf("retry phase %q is not supported", target)
	default:
		return fmt.Errorf("retry phase %q is not supported", target)
	}

	if err != nil {
		return err
	}

	br.Status.Failure = nil

	return nil
}

// DeleteLease Final stage, delete the request.
func (br *ResourceLease) DeleteLease() {
	controllerutil.RemoveFinalizer(br, meta.ControllerFinalizer)
}

// GenerateRequestStatus returns the effective lifecycle properties for a
// review. Passing the referenced template resolves its lifecycle defaults for
// display and review. Already rendered resources are retained in the returned
// request snapshot.
func (br *ResourceLease) GenerateRequestStatus(
	templates ...ResourceLeaseTemplateSource,
) (*ResourceLeaseStatusRequest, error) {
	startTime := metav1.Now()
	if br.Spec.StartTime != nil && !br.Spec.StartTime.IsZero() {
		startTime = *br.Spec.StartTime
	}

	properties := &ResourceLeaseStatusRequest{
		Duration:  br.Spec.Duration,
		StartTime: &startTime,
	}

	if br.Status.Request != nil {
		if br.Status.Request.Approvals != nil {
			properties.Approvals = br.Status.Request.Approvals.DeepCopy()
		}

		properties.Resources = make([]apiruntime.RenderedResource, len(br.Status.Request.Resources))
		for i := range br.Status.Request.Resources {
			br.Status.Request.Resources[i].DeepCopyInto(&properties.Resources[i])
		}
	}

	if len(templates) > 0 && templates[0] != nil {
		if err := br.resolveRequestStatus(templates[0], properties); err != nil {
			return nil, err
		}
	}

	return properties, nil
}

// ResolveLeaseStatus fills template defaults omitted by a reviewer and
// validates the effective duration before activation.
func (br *ResourceLease) ResolveLeaseStatus(brt ResourceLeaseTemplateSource) error {
	if br.Status.Request == nil {
		return errors.New("request status is nil")
	}

	return br.resolveRequestStatus(brt, br.Status.Request)
}

func (br *ResourceLease) resolveRequestStatus(
	brt ResourceLeaseTemplateSource,
	properties *ResourceLeaseStatusRequest,
) error {
	if brt == nil {
		return errors.New("template is nil")
	}

	return br.resolveRequestStatusData(brt.TemplateData(), properties)
}

func (br *ResourceLease) resolveRequestStatusData(
	templateData ResourceLeaseTemplateData,
	properties *ResourceLeaseStatusRequest,
) error {
	if properties.Approvals == nil {
		properties.Approvals = templateData.Approvals.DeepCopy()
	}

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

const requestTemplateContextKey = "request"

// RenderResources renders direct targets with the flat parameter/context
// values and expands optional multi-document templates with the structured
// .params and .context.resources values. Both forms expose trusted request
// metadata under .request.
func (br *ResourceLease) RenderResources(
	schema *k8sruntime.RawExtension,
	resources []apiruntime.ResourceTemplate,
	contexts ...template.ReferenceContext,
) ([]apiruntime.RenderedResource, error) {
	params, err := br.templateParameters(schema)
	if err != nil {
		return nil, err
	}

	if _, found := params[requestTemplateContextKey]; found {
		return nil, fmt.Errorf("request parameter key %q is reserved", requestTemplateContextKey)
	}

	requestContext := br.templateRequestContext()
	directContext := make(template.ReferenceContext, len(params)+1)
	maps.Copy(directContext, params)
	directContext[requestTemplateContextKey] = requestContext

	loadedResources := template.ReferenceContext{}

	for _, additional := range contexts {
		for key, value := range additional {
			if key == requestTemplateContextKey {
				return nil, fmt.Errorf("template context key %q is reserved", key)
			}

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
		"params":  params,
		"request": requestContext,
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

func (br *ResourceLease) templateRequestContext() template.ReferenceContext {
	groups := make([]string, len(br.Spec.Requestor.Groups))
	copy(groups, br.Spec.Requestor.Groups)

	timestamp := ""
	if !br.CreationTimestamp.IsZero() {
		timestamp = br.CreationTimestamp.UTC().Format(time.RFC3339)
	}

	return template.ReferenceContext{
		"name":      br.Name,
		"username":  br.Spec.Requestor.Name,
		"groups":    groups,
		"timestamp": timestamp,
	}
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
// GlobalResourceLeaseTemplate. Request parameters are available while resolving
// resource names, namespaces, and selectors.
func (br *ResourceLease) LoadTemplateContext(
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

	params, err := br.templateParameters(schema) //nolint:contextcheck // validation has no context-aware public API
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

func (br *ResourceLease) templateParameters(schema *k8sruntime.RawExtension) (template.ReferenceContext, error) {
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

// ValidateParameters validates the request parameters against the template's
// parameter schema without loading context or rendering resources.
func (br *ResourceLease) ValidateParameters(schema *k8sruntime.RawExtension) error {
	_, err := br.templateParameters(schema)

	return err
}

func (br *ResourceLease) effectiveKeepFor() resourcelease.ExtendedDuration {
	var keepFor resourcelease.ExtendedDuration

	if br.Status.Request != nil && br.Status.Request.KeepFor != nil {
		keepFor = *br.Status.Request.KeepFor
	}

	return keepFor
}

// SetReady records whether the rendered resources and their reconciliation are
// ready. Phase conditions are kept alongside this condition for lifecycle
// history.
func (br *ResourceLease) SetReady(status metav1.ConditionStatus, reason, message string) {
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

// transitionResourceLeasePhase validates and records an authenticated lifecycle change.
func (br *ResourceLease) transitionResourceLeasePhase(
	newPhase ResourceLeasePhase,
	conditionMessage string,
	reason string,
	now metav1.Time,
	entity *resourcelease.AccessEntity,
) error {
	if br.Status.Phase == newPhase {
		return nil
	}

	// Disallow invalid transitions
	switch newPhase {
	case ResourceLeasePhaseCreated:
		if br.Status.Phase != "" {
			return fmt.Errorf("can only mark an uninitialized request as created")
		}

	case ResourceLeasePhaseDenied:
		if br.Status.Phase == ResourceLeasePhaseApproved || br.Status.Phase == ResourceLeasePhaseActive {
			return fmt.Errorf("cannot deny an already approved or active request")
		}

		setReviewer(br, entity, conditionMessage, ResourceLeaseVerdictDenied)

	case ResourceLeasePhaseApproved:
		if br.Status.Phase == ResourceLeasePhaseDenied {
			return fmt.Errorf("cannot approve a denied request")
		}

		setReviewer(br, entity, conditionMessage, ResourceLeaseVerdictApproved)

	case ResourceLeasePhaseActive:
		if br.Status.Phase != ResourceLeasePhaseApproved {
			return fmt.Errorf("can only activate an approved request")
		}

	case ResourceLeasePhaseFailed:
		if br.Status.Phase == ResourceLeasePhaseExpired {
			return fmt.Errorf("cannot fail an expired request")
		}

	case ResourceLeasePhaseRetrying:
		if br.Status.Phase != ResourceLeasePhaseFailed {
			return fmt.Errorf("can only retry a failed request")
		}

	case ResourceLeasePhaseExpired: // terminal transition from any phase
	case ResourceLeasePhasePending, ResourceLeasePhaseRequested: // nothing to do here
	}

	br.Status.Transitions = append(br.Status.Transitions, ResourceLeaseTransition{
		Type:      newPhase,
		Timestamp: now,
		Actor:     transitionActor(entity),
		Reason:    reason,
		Message:   conditionMessage,
	})

	// Set the current phase
	br.Status.Phase = newPhase

	return nil
}

func transitionActor(entity *resourcelease.AccessEntity) ResourceLeaseTransitionActor {
	if entity == nil || (entity.Name == "" && entity.Type == "") {
		return ResourceLeaseTransitionActor{
			Name: capsuleControllerActorName,
			Type: resourcelease.AccessEntityTypeSystem,
		}
	}

	actor := ResourceLeaseTransitionActor{
		Name: entity.Name,
		Type: entity.Type,
	}
	if actor.Type == resourcelease.AccessEntityTypeSystem && actor.Name == "" {
		actor.Name = capsuleControllerActorName
	}

	return actor
}

// LatestTransition returns the newest audit entry for the given lifecycle type.
func (br *ResourceLease) LatestTransition(phase ResourceLeasePhase) *ResourceLeaseTransition {
	for index, transition := range slices.Backward(br.Status.Transitions) {
		if transition.Type == phase {
			return &br.Status.Transitions[index]
		}
	}

	return nil
}

func setReviewer(
	ar *ResourceLease,
	entity *resourcelease.AccessEntity,
	conditionMessage string,
	verdict ResourceLeaseVerdict,
) {
	if entity != nil {
		ar.Status.Review = &ReviewInfo{
			Reviewer: entity,
			Message:  conditionMessage,
			Verdict:  verdict,
		}
	}
}
