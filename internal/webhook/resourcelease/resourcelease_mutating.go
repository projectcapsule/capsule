// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcelease

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
	"github.com/projectcapsule/capsule/pkg/api/resourcelease"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/users"
)

func ResourceLeaseMutationHandler(log logr.Logger) handlers.Handler {
	return &resourceLeaseMutationHandler{
		log: log,
	}
}

type resourceLeaseMutationHandler struct {
	log logr.Logger
}

func (h *resourceLeaseMutationHandler) OnCreate(_ client.Client, _ client.Reader, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		br := &capsulev1beta2.ResourceLease{}
		if err := decoder.Decode(req, br); err != nil {
			return ad.ErroredResponse(fmt.Errorf("failed to decode new object: %w", err))
		}

		requestor := resourcelease.AccessEntity{
			Name:   req.UserInfo.Username,
			Type:   h.getAccessEntityType(req.UserInfo.Username),
			Groups: req.UserInfo.Groups,
		}

		response := admission.Patched(
			"set authenticated ResourceLease requestor",
			jsonpatch.NewOperation("add", "/spec/requestor", requestor),
		)

		return &response
	}
}

func (h *resourceLeaseMutationHandler) OnUpdate(_ client.Client, _ client.Reader, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		if req.SubResource != "status" {
			return nil
		}

		oldBr := &capsulev1beta2.ResourceLease{}
		newBr := &capsulev1beta2.ResourceLease{}

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
		entity := &resourcelease.AccessEntity{
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
		case capsulev1beta2.ResourceLeasePhaseApproved:
			properties := requestForTransition(oldBr, newBr)
			if properties == nil {
				return ad.Deny("cannot approve ResourceLease without request properties")
			}

			err = transitioned.ApproveLease(entity, properties, message)
		case capsulev1beta2.ResourceLeasePhaseDenied:
			err = transitioned.DenyLease(entity, message)
		case capsulev1beta2.ResourceLeasePhaseActive:
			err = transitioned.ActivateLease(entity)
		case capsulev1beta2.ResourceLeasePhaseExpired:
			err = transitioned.ExpireLease(entity)
		case capsulev1beta2.ResourceLeasePhaseRetrying:
			err = transitioned.RetryLease(entity)
		case capsulev1beta2.ResourceLeasePhaseRequested,
			capsulev1beta2.ResourceLeasePhaseCreated,
			capsulev1beta2.ResourceLeasePhasePending,
			capsulev1beta2.ResourceLeasePhaseFailed:
			return ad.Denyf(
				"transitioning ResourceLease from %s to %s is not supported",
				oldBr.Status.Phase,
				newBr.Status.Phase,
			)
		default:
			return ad.Denyf(
				"transitioning ResourceLease from %s to %s is not supported",
				oldBr.Status.Phase,
				newBr.Status.Phase,
			)
		}

		if err != nil {
			return ad.Denyf("invalid ResourceLease transition: %v", err)
		}

		response := admission.Patched(
			"apply authenticated ResourceLease status transition",
			jsonpatch.NewOperation("add", "/status", transitioned.Status),
		)

		return &response
	}
}

func requestForTransition(
	oldBr,
	newBr *capsulev1beta2.ResourceLease,
) *capsulev1beta2.ResourceLeaseStatusRequest {
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

func (h *resourceLeaseMutationHandler) OnDelete(_ client.Client, _ client.Reader, _ admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *resourceLeaseMutationHandler) getAccessEntityType(username string) resourcelease.AccessEntityType {
	if strings.HasPrefix(username, serviceaccount.ServiceAccountUsernamePrefix) {
		return resourcelease.AccessEntityTypeServiceAccount
	}

	return resourcelease.AccessEntityTypeUser
}
