// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package podoptions

import (
	"context"
	"encoding/json"

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

type mutationhandler struct {
	cfg     configuration.Configuration
	version *version.Version
}

func MutationHandler(cfg configuration.Configuration, version *version.Version) capsulewebhook.Handler {
	return &mutationhandler{
		cfg:     cfg,
		version: version,
	}
}

func (h *mutationhandler) OnCreate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.mutate(ctx, req, client, decoder, recorder)
	}
}

func (h *mutationhandler) OnDelete(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *mutationhandler) OnUpdate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.mutate(ctx, req, client, decoder, recorder)
	}
}

func (h *mutationhandler) mutate(ctx context.Context, req admission.Request, c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) *admission.Response {
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
			switch scheduling.Action {
			case api.SchedulingOverwrite:
				overwriteSchedulingOptions(pod, scheduling)
			case api.SchedulingAggregate:
				aggregateSchedulingOptions(pod, scheduling)
			}
		}
	}

	// Marshal Pod
	marshaled, err := json.Marshal(pod)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	response = admission.PatchResponseFromRaw(req.Object.Raw, marshaled)

	return &response
}
