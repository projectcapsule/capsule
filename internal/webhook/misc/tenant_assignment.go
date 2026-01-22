// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package misc

import (
	"context"
	"encoding/json"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
)

type tenantAssignmentHandler struct{}

func TenantAssignmentHandler() capsulewebhook.Handler {
	return &tenantAssignmentHandler{}
}

func (r *tenantAssignmentHandler) OnCreate(c client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.handle(ctx, c, decoder, req)
	}
}

func (r *tenantAssignmentHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *tenantAssignmentHandler) OnUpdate(c client.Client, decoder admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.handle(ctx, c, decoder, req)
	}
}

func (r *tenantAssignmentHandler) handle(ctx context.Context, c client.Client, decoder admission.Decoder, req admission.Request) *admission.Response {
	if req.Namespace == "" {
		return nil
	}

	obj := &unstructured.Unstructured{}
	if err := decoder.Decode(req, obj); err != nil {
		return utils.ErroredResponse(err)
	}

	tnt, err := tenant.TenantByStatusNamespace(ctx, c, req.Namespace)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == nil {
		return nil
	}

	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}

	want := tnt.GetName()

	managedOK := labels[meta.ManagedByCapsuleLabel] == want
	tenantOK := labels[meta.NewTenantLabel] == want

	if managedOK && tenantOK {
		return nil
	}

	if !managedOK {
		labels[meta.ManagedByCapsuleLabel] = want
	}

	if !tenantOK {
		labels[meta.NewTenantLabel] = want
	}

	obj.SetLabels(labels)

	marshaledObj, err := json.Marshal(obj)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	response := admission.PatchResponseFromRaw(req.Object.Raw, marshaledObj)

	return &response
}
