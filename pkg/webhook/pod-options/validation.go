// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package podoptions

import (
	"context"

	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/configuration"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
	corev1 "k8s.io/api/core/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type validationhandler struct {
	cfg     configuration.Configuration
	version *version.Version
}

func ValidationHandler(cfg configuration.Configuration, version *version.Version) capsulewebhook.Handler {
	return &validationhandler{
		cfg:     cfg,
		version: version,
	}
}

func (h *validationhandler) OnCreate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.mutate(ctx, req, client, decoder, recorder)
	}
}

func (h *validationhandler) OnDelete(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *validationhandler) OnUpdate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.mutate(ctx, req, client, decoder, recorder)
	}
}

func (h *validationhandler) validate(ctx context.Context, req admission.Request, c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) *admission.Response {
	var response admission.Response

	pod := &corev1.Pod{}
	if err := decoder.Decode(req, pod); err != nil {
		return utils.ErroredResponse(err)
	}

	var tnt *capsulev1beta2.Tenant

	tnt, err := utils.TenantByStatusNamespace(ctx, c, pod.Namespace)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == nil {
		return nil
	}

	for _, scheduling := range tnt.Spec.PodOptions.Scheduling {
		if scheduling.IsSelected(pod) {
			if scheduling.Action == api.SchedulingValidate {
				if !scheduling.validate(pod) {
					return utils.DeniedResponse("Pod scheduling options are not valid")
				}
			}
		}
	}

	return &response
}
