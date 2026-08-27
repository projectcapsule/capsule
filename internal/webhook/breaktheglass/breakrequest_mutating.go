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

		if oldBr.Status.Phase == capsulev1beta2.RequestPhaseApproved ||
			newBr.Status.Phase != capsulev1beta2.RequestPhaseApproved {
			return nil
		}

		// Capture the authenticated reviewer on manual approval. Preserve the
		// controller's explicit System identity for automatic approvals.
		if newBr.Status.Review != nil &&
			newBr.Status.Review.Reviewer != nil &&
			newBr.Status.Review.Reviewer.Type == breaktheglass.AccessEntityTypeSystem {
			return nil
		}

		reviewer := breaktheglass.AccessEntity{
			Name:   req.UserInfo.Username,
			Type:   h.getAccessEntityType(req.UserInfo.Username),
			Groups: req.UserInfo.Groups,
		}

		var patch jsonpatch.JsonPatchOperation
		if newBr.Status.Review == nil {
			patch = jsonpatch.NewOperation("add", "/status/review", capsulev1beta2.ReviewInfo{
				Reviewer: &reviewer,
			})
		} else {
			patch = jsonpatch.NewOperation("add", "/status/review/reviewer", reviewer)
		}

		response := admission.Patched("set authenticated BreakRequest reviewer", patch)

		return &response
	}
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
