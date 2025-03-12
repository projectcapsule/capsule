// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0
package globalquota

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type validationHandler struct{}

func ValidationHandler() capsulewebhook.Handler {
	return &validationHandler{}
}

func (r *validationHandler) OnCreate(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *validationHandler) OnDelete(client client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		allowed, err := r.handle(ctx, req, client, decoder)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if !allowed {
			response := admission.Denied("Capsule Resource Quotas cannot be deleted")

			return &response
		}

		return nil
	}
}

func (r *validationHandler) OnUpdate(client client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		allowed, err := r.handle(ctx, req, client, decoder)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if !allowed {
			response := admission.Denied("Capsule ResourceQuotas cannot be updated")

			return &response
		}

		return nil
	}
}

func (r *validationHandler) handle(ctx context.Context, req admission.Request, client client.Client, _ admission.Decoder) (allowed bool, err error) {
	allowed = true

	np := &corev1.ResourceQuota{}
	if err = client.Get(ctx, types.NamespacedName{Namespace: req.AdmissionRequest.Namespace, Name: req.AdmissionRequest.Name}, np); err != nil {
		return false, err
	}

	objectLabel := capsuleutils.GetGlobalResourceQuotaTypeLabel()

	labels := np.GetLabels()
	if _, ok := labels[objectLabel]; ok {
		allowed = false
	}

	return
}
