// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/users"
)

func BreakRequestValidationHandler(log logr.Logger, cfg configuration.Configuration) handlers.Handler {
	return &breakRequestValidationHandler{
		log:           log,
		configuration: cfg,
	}
}

type breakRequestValidationHandler struct {
	log           logr.Logger
	configuration configuration.Configuration
}

func (b *breakRequestValidationHandler) OnCreate(_ client.Client, reader client.Reader, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		b.log.Info("Validation for BreakRequest upon creation", "name", req.Name)

		br := &capsulev1beta2.BreakRequest{}
		if err := decoder.Decode(req, br); err != nil {
			return ad.ErroredResponse(fmt.Errorf("failed to decode new object: %w", err))
		}

		if br.Spec.Template.Kind != capsulev1beta2.BreakRequestTemplateKind &&
			br.Spec.Template.Kind != capsulev1beta2.GlobalBreakRequestTemplateKind {
			return ad.Denyf("template kind %q is not supported", br.Spec.Template.Kind)
		}

		templateName := br.Spec.Template.Name

		brt, err := loadBreakRequestTemplate(ctx, reader, br)
		if err != nil {
			if client.IgnoreNotFound(err) == nil {
				return ad.Denyf("template %s not found", templateName)
			}

			return ad.ErroredResponse(fmt.Errorf("error loading template %s: %w", templateName, err))
		}

		if global, ok := brt.(*capsulev1beta2.GlobalBreakRequestTemplate); ok && len(global.Spec.NamespaceSelectors) > 0 {
			if global.Status.ObservedGeneration != global.Generation {
				return ad.Denyf("template %s namespace selection is not ready", templateName)
			}

			namespace := req.Namespace
			if namespace == "" {
				namespace = br.Namespace
			}

			if !global.Status.NamespacePresent(namespace) {
				return ad.Denyf(
					"template %s is not available in namespace %s",
					templateName,
					namespace,
				)
			}
		}

		templateData := brt.TemplateData()
		if err := br.ValidateParameters(templateData.ParamSchema); err != nil { //nolint:contextcheck // schema validation has no context-aware public API
			return ad.Denyf("parameters for template %s are invalid: %v", templateName, err)
		}

		if templateData.MaxDuration != nil && templateData.MaxDuration.Duration > 0 &&
			br.Spec.Duration != nil &&
			br.Spec.Duration.Duration > templateData.MaxDuration.Duration {
			return ad.Denyf("requested duration %s exceeds template maxDuration %s",
				br.Spec.Duration.Duration, templateData.MaxDuration.Duration)
		}

		if br.Spec.StartTime != nil &&
			!br.Spec.StartTime.After(time.Now()) {
			return ad.Denyf("start time %s must be in the future", br.Spec.StartTime.String())
		}

		if templateData.Approvals.Auto {
			if err := brt.CheckApprovalConditions(ctx, br); err != nil {
				return ad.Denyf("approval conditions not satisfied for template %s: %v", templateName, err)
			}
		}

		return nil
	}
}

func (b *breakRequestValidationHandler) OnDelete(
	_ client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		br := &capsulev1beta2.BreakRequest{}
		if err := decoder.DecodeRaw(req.OldObject, br); err != nil {
			return ad.ErroredResponse(fmt.Errorf("failed to decode deleted object: %w", err))
		}

		if users.IsAdminUser(req, b.administrators()) {
			return nil
		}

		if br.Status.Phase == capsulev1beta2.RequestPhaseCreated ||
			br.Status.Phase == capsulev1beta2.RequestPhaseRequested ||
			br.Status.Phase == capsulev1beta2.RequestPhasePending {
			return nil
		}

		if br.Status.Phase != capsulev1beta2.RequestPhaseExpired {
			phase := string(br.Status.Phase)
			if phase == "" {
				phase = "Initializing"
			}

			return ad.Denyf(
				"BreakRequest cannot be deleted before it has expired (current phase: %s)",
				phase,
			)
		}

		if br.Status.KeepUntil != nil && br.Status.KeepUntil.After(time.Now()) {
			return ad.Denyf(
				"BreakRequest cannot be deleted before archive retention expires at %s",
				br.Status.KeepUntil.UTC().Format(time.RFC3339),
			)
		}

		return nil
	}
}

func (b *breakRequestValidationHandler) OnUpdate(_ client.Client, reader client.Reader, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		oldBr := &capsulev1beta2.BreakRequest{}
		newBr := &capsulev1beta2.BreakRequest{}

		if err := decoder.DecodeRaw(req.OldObject, oldBr); err != nil {
			return ad.ErroredResponse(err)
		}

		if err := decoder.Decode(req, newBr); err != nil {
			return ad.ErroredResponse(err)
		}

		if req.SubResource == "" && oldBr.Spec.Template != newBr.Spec.Template {
			return ad.Denyf(
				"template cannot be changed. old: %s/%s, new: %s/%s",
				oldBr.Spec.Template.Kind,
				oldBr.Spec.Template.Name,
				newBr.Spec.Template.Kind,
				newBr.Spec.Template.Name,
			)
		}

		if req.SubResource == "status" &&
			!users.IsControllerServiceAccount(req.UserInfo.Username) &&
			!reflect.DeepEqual(requestResources(oldBr), requestResources(newBr)) {
			return ad.Deny("rendered resources can only be changed by the Capsule controller")
		}

		if req.SubResource == "status" &&
			!users.IsControllerServiceAccount(req.UserInfo.Username) &&
			!reflect.DeepEqual(requestImpersonation(oldBr), requestImpersonation(newBr)) {
			return ad.Deny("resolved impersonation can only be changed by the Capsule controller")
		}

		if req.SubResource == "status" &&
			!users.IsControllerServiceAccount(req.UserInfo.Username) &&
			!reflect.DeepEqual(resolvedRequestTemplate(oldBr), resolvedRequestTemplate(newBr)) {
			return ad.Deny("resolved template can only be changed by the Capsule controller")
		}

		if req.SubResource == "status" &&
			!users.IsControllerServiceAccount(req.UserInfo.Username) &&
			!reflect.DeepEqual(requestApprovals(oldBr), requestApprovals(newBr)) {
			return ad.Deny("resolved approvals can only be changed by the Capsule controller")
		}

		if req.SubResource == "status" &&
			!users.IsControllerServiceAccount(req.UserInfo.Username) &&
			oldBr.Status.Phase == newBr.Status.Phase &&
			!reflect.DeepEqual(oldBr.Status, newBr.Status) {
			return ad.Deny("BreakRequest status can only be changed by requesting a lifecycle transition")
		}

		if oldBr.Status.Phase != capsulev1beta2.RequestPhaseApproved &&
			newBr.Status.Phase == capsulev1beta2.RequestPhaseApproved {
			return b.validateApproval(ctx, req, reader, oldBr, newBr)
		}

		return nil
	}
}

func (b *breakRequestValidationHandler) administrators() rbac.UserListSpec {
	if b.configuration == nil {
		return nil
	}

	return b.configuration.Administrators()
}

func requestResources(br *capsulev1beta2.BreakRequest) []apiruntime.RenderedResource {
	if br.Status.Request == nil {
		return nil
	}

	return br.Status.Request.Resources
}

func requestImpersonation(br *capsulev1beta2.BreakRequest) *meta.NamespacedRFC1123ObjectReferenceWithNamespace {
	if br.Status.Request == nil {
		return nil
	}

	return br.Status.Request.Impersonation
}

func resolvedRequestTemplate(br *capsulev1beta2.BreakRequest) *capsulev1beta2.ResolvedBreakRequestTemplateReference {
	if br.Status.Request == nil {
		return nil
	}

	return br.Status.Request.Template
}

func requestApprovals(br *capsulev1beta2.BreakRequest) *breaktheglass.ApprovalSpec {
	if br.Status.Request == nil {
		return nil
	}

	return br.Status.Request.Approvals
}

func (b *breakRequestValidationHandler) validateApproval(
	ctx context.Context,
	req admission.Request,
	reader client.Reader,
	oldBr *capsulev1beta2.BreakRequest,
	newBr *capsulev1beta2.BreakRequest,
) *admission.Response {
	ready := k8smeta.FindStatusCondition(oldBr.Status.Conditions, meta.ReadyCondition)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		message := "rendered resources are not ready"
		if ready != nil && ready.Message != "" {
			message += ": " + ready.Message
		}

		return ad.Denyf("cannot approve BreakRequest: %s", message)
	}

	automaticApproval := users.IsControllerServiceAccount(req.UserInfo.Username) &&
		newBr.Status.Review != nil &&
		newBr.Status.Review.Reviewer != nil &&
		newBr.Status.Review.Reviewer.Type == breaktheglass.AccessEntityTypeSystem

	brt, err := loadBreakRequestTemplate(ctx, reader, newBr)
	if err != nil {
		return ad.ErroredResponse(fmt.Errorf(
			"failed to get template %s: %w",
			newBr.Spec.Template.Name,
			err,
		))
	}

	approvals := newBr.ApprovalPolicy(brt)
	if !automaticApproval && !approvals.IsApprover(req.UserInfo.Username, req.UserInfo.Groups) {
		return ad.Denyf(
			"subject %q is not permitted to approve requests for template %s",
			req.UserInfo.Username,
			newBr.Spec.Template.Name,
		)
	}

	if err := newBr.CheckApprovalConditions(ctx, brt); err != nil {
		return ad.Denyf(
			"approval conditions not satisfied for template %s: %v",
			newBr.Spec.Template.Name,
			err,
		)
	}

	if err := newBr.ResolveRequestStatus(brt); err != nil {
		return ad.Denyf("request properties are invalid: %v", err)
	}

	return nil
}

func loadBreakRequestTemplate(
	ctx context.Context,
	reader client.Reader,
	br *capsulev1beta2.BreakRequest,
) (capsulev1beta2.BreakRequestTemplateSource, error) {
	switch br.Spec.Template.Kind {
	case capsulev1beta2.BreakRequestTemplateKind:
		brt := &capsulev1beta2.BreakRequestTemplate{}
		if err := reader.Get(ctx, client.ObjectKey{Namespace: br.Namespace, Name: br.Spec.Template.Name}, brt); err != nil {
			return nil, err
		}

		return brt, nil
	case capsulev1beta2.GlobalBreakRequestTemplateKind:
		brt := &capsulev1beta2.GlobalBreakRequestTemplate{}
		if err := reader.Get(ctx, client.ObjectKey{Name: br.Spec.Template.Name}, brt); err != nil {
			return nil, err
		}

		return brt, nil
	default:
		return nil, fmt.Errorf("template kind %q is not supported", br.Spec.Template.Kind)
	}
}
