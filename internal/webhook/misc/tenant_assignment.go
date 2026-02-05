// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package misc

import (
	"context"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	clt "github.com/projectcapsule/capsule/pkg/runtime/client"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

type tenantAssignmentHandler struct{}

func TenantAssignmentHandler() handlers.Handler {
	return &tenantAssignmentHandler{}
}

func (r *tenantAssignmentHandler) OnCreate(c client.Client, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.handle(ctx, c, decoder, req)
	}
}

func (r *tenantAssignmentHandler) OnDelete(client.Client, admission.Decoder, events.EventRecorder) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *tenantAssignmentHandler) OnUpdate(c client.Client, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.handle(ctx, c, decoder, req)
	}
}

func (r *tenantAssignmentHandler) handle(ctx context.Context, c client.Client, decoder admission.Decoder, req admission.Request) *admission.Response {
	if req.Namespace == "" {
		return nil
	}

	obj := &metav1.PartialObjectMetadata{}
	if err := decoder.Decode(req, obj); err != nil {
		return utils.ErroredResponse(err)
	}

	tnt, err := tenant.GetTenantNameByStatusNamespace(ctx, c, req.Namespace)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == "" {
		return nil
	}

	labels := obj.GetLabels()

	desired := map[string]string{}
	if labels == nil || labels[meta.ManagedByCapsuleLabel] != tnt {
		desired[meta.ManagedByCapsuleLabel] = tnt
	}

	if labels == nil || labels[meta.NewTenantLabel] != tnt {
		desired[meta.NewTenantLabel] = tnt
	}

	patches := clt.AddLabelsPatch(labels, desired)
	if len(patches) == 0 {
		return nil
	}

	return &admission.Response{
		AdmissionResponse: admissionv1.AdmissionResponse{
			Allowed: true,
		},
		Patches: clt.JSONPatchesToJSONPatchOperation(patches),
	}
}
