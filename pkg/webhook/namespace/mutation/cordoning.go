// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"context"
	"encoding/json"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/configuration"
	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type cordoningLabelHandler struct {
	cfg configuration.Configuration
}

func CordoningLabelHandler(cfg configuration.Configuration) capsulewebhook.Handler {
	return &cordoningLabelHandler{
		cfg: cfg,
	}
}

func (h *cordoningLabelHandler) OnCreate(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *cordoningLabelHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *cordoningLabelHandler) OnUpdate(c client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return utils.ErroredResponse(err)
		}
		response := h.syncNamespaceCordonLabel(ctx, c, req, ns)
		return response
	}
}

func (h *cordoningLabelHandler) syncNamespaceCordonLabel(ctx context.Context, c client.Client, req admission.Request, ns *corev1.Namespace) *admission.Response {
	tnt := &capsulev1beta2.Tenant{}
	ln, err := capsuleutils.GetTypeLabel(tnt)
	if err != nil {
		response := admission.Errored(http.StatusInternalServerError, err)

		return &response
	}

	if label, ok := ns.Labels[ln]; ok {
		if err = c.Get(ctx, types.NamespacedName{Name: label}, tnt); err != nil {
			admission.Errored(http.StatusInternalServerError, err)
		}
	}

	if !tnt.Spec.Cordoned {
		return nil
	}

	labels := ns.GetLabels()
	if _, ok := labels[capsuleutils.CordonedLabel]; !ok {
		return nil

	}

	ns.Labels[capsuleutils.CordonedLabel] = "true"

	marshaled, err := json.Marshal(ns)
	if err != nil {
		response := admission.Errored(http.StatusInternalServerError, err)

		return &response
	}

	response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

	return &response
}
