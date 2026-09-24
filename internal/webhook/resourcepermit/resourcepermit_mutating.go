// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepermit

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
	"github.com/projectcapsule/capsule/pkg/api/resourcepermit"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/users"
)

func ResourcePermitMutationHandler(log logr.Logger) handlers.Handler {
	return &resourcePermitMutationHandler{
		log: log,
	}
}

type resourcePermitMutationHandler struct {
	log logr.Logger
}

func (h *resourcePermitMutationHandler) OnCreate(_ client.Client, _ client.Reader, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		br := &capsulev1beta2.ResourcePermit{}
		if err := decoder.Decode(req, br); err != nil {
			return ad.ErroredResponse(fmt.Errorf("failed to decode new object: %w", err))
		}

		requestor := resourcepermit.AccessEntity{
			Name:   req.UserInfo.Username,
			Type:   h.getAccessEntityType(req.UserInfo.Username),
			Groups: req.UserInfo.Groups,
		}

		response := admission.Patched(
			"set authenticated ResourcePermit requestor",
			jsonpatch.NewOperation("add", "/spec/requestor", requestor),
		)

		return &response
	}
}

func (h *resourcePermitMutationHandler) OnUpdate(_ client.Client, _ client.Reader, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		if req.SubResource != "status" {
			return nil
		}

		oldBr := &capsulev1beta2.ResourcePermit{}
		newBr := &capsulev1beta2.ResourcePermit{}

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
		entity := &resourcepermit.AccessEntity{
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
		case capsulev1beta2.ResourcePermitPhaseApproved:
			properties := requestForTransition(oldBr, newBr)
			if properties == nil {
				return ad.Deny("cannot approve ResourcePermit without request properties")
			}

			err = transitioned.ApprovePermit(entity, properties, message)
		case capsulev1beta2.ResourcePermitPhaseDenied:
			err = transitioned.DenyPermit(entity, message)
		case capsulev1beta2.ResourcePermitPhaseActive:
			err = transitioned.ActivatePermit(entity)
		case capsulev1beta2.ResourcePermitPhaseExpired:
			err = transitioned.ExpirePermit(entity)
		case capsulev1beta2.ResourcePermitPhaseRetrying:
			err = transitioned.RetryPermit(entity)
		case capsulev1beta2.ResourcePermitPhaseRequested,
			capsulev1beta2.ResourcePermitPhaseCreated,
			capsulev1beta2.ResourcePermitPhasePending,
			capsulev1beta2.ResourcePermitPhaseFailed:
			return ad.Denyf(
				"transitioning ResourcePermit from %s to %s is not supported",
				oldBr.Status.Phase,
				newBr.Status.Phase,
			)
		default:
			return ad.Denyf(
				"transitioning ResourcePermit from %s to %s is not supported",
				oldBr.Status.Phase,
				newBr.Status.Phase,
			)
		}

		if err != nil {
			return ad.Denyf("invalid ResourcePermit transition: %v", err)
		}

		response := admission.Patched(
			"apply authenticated ResourcePermit status transition",
			jsonpatch.NewOperation("add", "/status", transitioned.Status),
		)

		return &response
	}
}

func requestForTransition(
	oldBr,
	newBr *capsulev1beta2.ResourcePermit,
) *capsulev1beta2.ResourcePermitStatusRequest {
	if oldBr.Status.Request == nil {
		return nil
	}

	properties := oldBr.Status.Request.DeepCopy()
	if newBr.Status.Request == nil {
		return properties
	}

	properties.KeepFor = newBr.Status.Request.KeepFor
	properties.Duration = newBr.Status.Request.Duration
	properties.StartTime = newBr.Status.Request.StartTime

	return properties
}

func (h *resourcePermitMutationHandler) OnDelete(_ client.Client, _ client.Reader, _ admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *resourcePermitMutationHandler) getAccessEntityType(username string) resourcepermit.AccessEntityType {
	if strings.HasPrefix(username, serviceaccount.ServiceAccountUsernamePrefix) {
		return resourcepermit.AccessEntityTypeServiceAccount
	}

	return resourcepermit.AccessEntityTypeUser
}
