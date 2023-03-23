// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package namespace

import (
	"context"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	capsuleutils "github.com/clastix/capsule/pkg/utils"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type patchHandler struct{}

func PatchHandler() capsulewebhook.Handler {
	return &patchHandler{}
}

func (r *patchHandler) OnCreate(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (r *patchHandler) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (r *patchHandler) OnUpdate(c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		// Decode Namespace
		ns := &corev1.Namespace{}
		if err := decoder.DecodeRaw(req.OldObject, ns); err != nil {
			return utils.ErroredResponse(err)
		}

		// Get Tenant Label
		ln, err := capsuleutils.GetTypeLabel(&capsulev1beta2.Tenant{})
		if err != nil {
			response := admission.Errored(http.StatusBadRequest, err)

			return &response
		}

		// Extract Tenant from namespace
		e := fmt.Sprintf("namespace/%s can not be patched", ns.Name)

		if label, ok := ns.ObjectMeta.Labels[ln]; ok {
			// retrieving the selected Tenant
			tnt := &capsulev1beta2.Tenant{}
			if err = c.Get(ctx, types.NamespacedName{Name: label}, tnt); err != nil {
				response := admission.Errored(http.StatusBadRequest, err)

				return &response
			}

			if !utils.IsTenantOwner(tnt.Spec.Owners, req.UserInfo) {
				recorder.Eventf(tnt, corev1.EventTypeWarning, "NamespacePatch", e)
				response := admission.Denied(e)

				return &response
			}
		}

		return nil
	}
}
