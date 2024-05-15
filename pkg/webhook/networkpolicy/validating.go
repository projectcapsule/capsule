// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package networkpolicy

import (
	"context"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type handler struct{}

func Handler() capsulewebhook.Handler {
	return &handler{}
}

func (r *handler) OnCreate(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *handler) OnDelete(client client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		allowed, err := r.handle(ctx, req, client, decoder)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if !allowed {
			response := admission.Denied("Capsule Network Policies cannot be deleted: please, reach out to the system administrators")

			return &response
		}

		return nil
	}
}

func (r *handler) OnUpdate(client client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		allowed, err := r.handle(ctx, req, client, decoder)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if !allowed {
			response := admission.Denied("Capsule Network Policies cannot be updated: please, reach out to the system administrators")

			return &response
		}

		return nil
	}
}

func (r *handler) handle(ctx context.Context, req admission.Request, client client.Client, _ admission.Decoder) (allowed bool, err error) {
	allowed = true

	np := &networkingv1.NetworkPolicy{}
	if err = client.Get(ctx, types.NamespacedName{Namespace: req.AdmissionRequest.Namespace, Name: req.AdmissionRequest.Name}, np); err != nil {
		return false, err
	}

	objectLabel, err := capsuleutils.GetTypeLabel(&networkingv1.NetworkPolicy{})
	if err != nil {
		return
	}

	labels := np.GetLabels()
	if _, ok := labels[objectLabel]; ok {
		allowed = false
	}

	return
}
