// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package managed

import (
	"context"
	"net/http"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/pkg/configuration"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type handler struct {
	cfg     configuration.Configuration
	version *version.Version
}

func Handler(cfg configuration.Configuration, version *version.Version) capsulewebhook.Handler {
	return &handler{
		cfg:     cfg,
		version: version,
	}
}

func (h *handler) OnCreate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.mutate(ctx, req, client, decoder, recorder)
	}
}

func (h *handler) OnDelete(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *handler) OnUpdate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.mutate(ctx, req, client, decoder, recorder)
	}
}

func (h *handler) mutate(ctx context.Context, req admission.Request, c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) *admission.Response {
	obj := unstructured.Unstructured{}

	if err := decoder.Decode(req, &obj); err != nil {
		return utils.ErroredResponse(err)
	}

	tnt, err := utils.TenantByStatusNamespace(ctx, c, obj.GetNamespace())
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == nil {
		return nil
	}

	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["capsule.clastix.io/managed-by"] = tnt.GetName()
	obj.SetLabels(labels)

	// Marshal Manifest
	marshaled, err := obj.MarshalJSON()
	if err != nil {
		response := admission.Errored(http.StatusInternalServerError, err)

		return &response
	}

	response := admission.PatchResponseFromRaw(req.Object.Raw, marshaled)
	return &response
}
