// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type quotaHandler struct{}

func QuotaHandler() capsulewebhook.Handler {
	return &quotaHandler{}
}

func (r *quotaHandler) OnCreate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return utils.ErroredResponse(err)
		}

		for _, objectRef := range ns.OwnerReferences {
			if !capsuleutils.IsTenantOwnerReference(objectRef) {
				continue
			}

			// retrieving the selected Tenant
			tnt := &capsulev1beta2.Tenant{}
			if err := client.Get(ctx, types.NamespacedName{Name: objectRef.Name}, tnt); err != nil {
				return utils.ErroredResponse(err)
			}

			if tnt.IsFull() {
				// Checking if the Namespace already exists.
				// If this is the case, no need to return the quota exceeded error:
				// the Kubernetes API Server will return an AlreadyExists error,
				// adhering more to the native Kubernetes experience.
				if err := client.Get(ctx, types.NamespacedName{Name: ns.Name}, &corev1.Namespace{}); err == nil {
					return nil
				}

				recorder.Eventf(tnt, corev1.EventTypeWarning, "NamespaceQuotaExceded", "Namespace %s cannot be attached, quota exceeded for the current Tenant", ns.GetName())

				response := admission.Denied(NewNamespaceQuotaExceededError().Error())

				return &response
			}
		}
		// creating NS that is not bounded to any Tenant
		return nil
	}
}

func (r *quotaHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *quotaHandler) OnUpdate(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}
