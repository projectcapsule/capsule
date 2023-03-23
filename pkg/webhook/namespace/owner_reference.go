// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package namespace

import (
	"context"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type ownerReferenceHandler struct{}

func OwnerReferenceHandler() capsulewebhook.Handler {
	return &ownerReferenceHandler{}
}

func (r *ownerReferenceHandler) OnCreate(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (r *ownerReferenceHandler) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (r *ownerReferenceHandler) OnUpdate(_ client.Client, decoder *admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		oldNs := &corev1.Namespace{}
		if err := decoder.DecodeRaw(req.OldObject, oldNs); err != nil {
			return utils.ErroredResponse(err)
		}

		if len(oldNs.OwnerReferences) == 0 {
			return nil
		}

		newNs := &corev1.Namespace{}
		if err := decoder.Decode(req, newNs); err != nil {
			return utils.ErroredResponse(err)
		}

		if len(newNs.OwnerReferences) == 0 {
			response := admission.Errored(http.StatusBadRequest, fmt.Errorf("the OwnerReference cannot be removed"))

			return &response
		}

		if oldNs.GetOwnerReferences()[0].UID != newNs.GetOwnerReferences()[0].UID {
			response := admission.Errored(http.StatusBadRequest, fmt.Errorf("the OwnerReference cannot be changed"))

			return &response
		}

		return nil
	}
}
