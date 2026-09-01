// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	jsonpatch "gomodules.xyz/jsonpatch/v2"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/users"
)

func BreakRequestMutationHandler(log logr.Logger) handlers.Handler {
	return &breakRequestMutationHandler{
		log: log,
	}
}

type breakRequestMutationHandler struct {
	log logr.Logger
}

func (h *breakRequestMutationHandler) OnCreate(_ client.Client, _ client.Reader, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		br := &capsulev1beta2.BreakRequest{}
		if err := decoder.Decode(req, br); err != nil {
			return ad.ErroredResponse(fmt.Errorf("failed to decode new object: %w", err))
		}

		requestor := breaktheglass.AccessEntity{
			Name:   req.UserInfo.Username,
			Type:   h.getAccessEntityType(req.UserInfo.Username),
			Groups: req.UserInfo.Groups,
		}

		response := admission.Patched(
			"set authenticated BreakRequest requestor",
			jsonpatch.NewOperation("add", "/spec/requestor", requestor),
		)

		return &response
	}
}

func (h *breakRequestMutationHandler) OnUpdate(_ client.Client, _ client.Reader, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		if req.SubResource != "status" {
			return nil
		}

		oldBr := &capsulev1beta2.BreakRequest{}
		newBr := &capsulev1beta2.BreakRequest{}

		if err := decoder.DecodeRaw(req.OldObject, oldBr); err != nil {
			return ad.ErroredResponse(err)
		}

		if err := decoder.Decode(req, newBr); err != nil {
			return ad.ErroredResponse(err)
		}

		// The controller already uses the lifecycle API and must be able to write
		// reconciliation status without admission reconstructing it.
		if users.IsControllerServiceAccount(req.UserInfo.Username) ||
			oldBr.Status.Phase == newBr.Status.Phase {
			return nil
		}

		transitioned := oldBr.DeepCopy()
		entity := &breaktheglass.AccessEntity{
			Name:   req.UserInfo.Username,
			Type:   h.getAccessEntityType(req.UserInfo.Username),
			Groups: req.UserInfo.Groups,
		}

		message := ""
		if newBr.Status.Review != nil {
			message = newBr.Status.Review.Message
		}

		var err error

		switch newBr.Status.Phase {
		case capsulev1beta2.RequestPhaseApproved:
			properties := approvedPropertiesForTransition(oldBr, newBr)
			if properties == nil {
				return ad.Deny("cannot approve BreakRequest without approved properties")
			}

			err = transitioned.ApproveRequest(entity, properties, message)
		case capsulev1beta2.RequestPhaseDenied:
			err = transitioned.DenyRequest(entity, message)
		case capsulev1beta2.RequestPhaseActive:
			err = transitioned.ActiveRequest(entity)
		case capsulev1beta2.RequestPhaseExpired:
			err = transitioned.ExpireRequest(entity)
		case capsulev1beta2.RequestPhaseRequested, capsulev1beta2.RequestPhasePending:
			return ad.Denyf(
				"transitioning BreakRequest from %s to %s is not supported",
				oldBr.Status.Phase,
				newBr.Status.Phase,
			)
		default:
			return ad.Denyf(
				"transitioning BreakRequest from %s to %s is not supported",
				oldBr.Status.Phase,
				newBr.Status.Phase,
			)
		}

		if err != nil {
			return ad.Denyf("invalid BreakRequest transition: %v", err)
		}

		response := admission.Patched(
			"apply authenticated BreakRequest status transition",
			jsonpatch.NewOperation("add", "/status", transitioned.Status),
		)

		return &response
	}
}

func approvedPropertiesForTransition(
	oldBr,
	newBr *capsulev1beta2.BreakRequest,
) *capsulev1beta2.ApprovedProperties {
	if oldBr.Status.Approved == nil {
		return nil
	}

	properties := oldBr.Status.Approved.DeepCopy()
	if newBr.Status.Approved == nil {
		return properties
	}

	properties.KeepFor = newBr.Status.Approved.KeepFor
	properties.Duration = newBr.Status.Approved.Duration
	properties.StartTime = newBr.Status.Approved.StartTime

	return properties
}

func (h *breakRequestMutationHandler) OnDelete(_ client.Client, _ client.Reader, _ admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *breakRequestMutationHandler) getAccessEntityType(username string) breaktheglass.AccessEntityType {
	if strings.HasPrefix(username, serviceaccount.ServiceAccountUsernamePrefix) {
		return breaktheglass.AccessEntityTypeServiceAccount
	}

	return breaktheglass.AccessEntityTypeUser
}
