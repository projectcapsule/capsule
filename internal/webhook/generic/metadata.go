// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"maps"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	clt "github.com/projectcapsule/capsule/pkg/runtime/client"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

type tenantAssignmentHandler struct{}

func TenantAssignmentHandler() handlers.Handler {
	return &tenantAssignmentHandler{}
}

func (r *tenantAssignmentHandler) OnCreate(
	_ client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.handle(ctx, reader, decoder, req)
	}
}

func (r *tenantAssignmentHandler) OnDelete(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *tenantAssignmentHandler) OnUpdate(
	_ client.Client,
	reader client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if req.Namespace == "" {
			return nil
		}

		obj := &metav1.PartialObjectMetadata{}
		if err := decoder.Decode(req, obj); err != nil {
			return ad.ErroredResponse(err)
		}

		if req.SubResource == "" && obj.DeletionTimestamp != nil && len(obj.Finalizers) == 0 &&
			(obj.DeletionGracePeriodSeconds == nil || *obj.DeletionGracePeriodSeconds == 0) &&
			obj.Labels[meta.ManagedByCapsuleLabel] != "" && obj.Labels[meta.ManagedByCapsuleLabel] == obj.Labels[meta.NewTenantLabel] {
			old := &metav1.PartialObjectMetadata{}
			if err := decoder.DecodeRaw(req.OldObject, old); err != nil {
				return ad.ErroredResponse(err)
			}

			// Removing the last finalizer deletes this persisted object. Retagging
			// unchanged Tenant labels cannot affect its surviving state. Return nil
			// so other handlers and validating webhooks still enforce their policies.
			if old.DeletionTimestamp != nil && old.DeletionTimestamp.Equal(obj.DeletionTimestamp) &&
				(old.DeletionGracePeriodSeconds == nil || *old.DeletionGracePeriodSeconds == 0) &&
				old.UID != "" && old.UID == obj.UID && old.Name == obj.Name && old.Namespace == obj.Namespace &&
				obj.Namespace == req.Namespace && len(old.Finalizers) > 0 &&
				maps.Equal(old.Labels, obj.Labels) {
				return nil
			}
		}

		return r.assign(ctx, reader, obj, req)
	}
}

func (r *tenantAssignmentHandler) handle(
	ctx context.Context,
	c client.Reader,
	decoder admission.Decoder,
	req admission.Request,
) *admission.Response {
	if req.Namespace == "" {
		return nil
	}

	obj := &metav1.PartialObjectMetadata{}
	if err := decoder.Decode(req, obj); err != nil {
		return ad.ErroredResponse(err)
	}

	return r.assign(ctx, c, obj, req)
}

func (r *tenantAssignmentHandler) assign(
	ctx context.Context,
	c client.Reader,
	obj *metav1.PartialObjectMetadata,
	req admission.Request,
) *admission.Response {
	tnt, err := tenant.GetTenantNameByNamespace(ctx, c, req.Namespace)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return ad.ErroredResponse(err)
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
		Allowed: true,
		Patches: clt.JSONPatchesToJSONPatchOperation(patches),
	}
}
