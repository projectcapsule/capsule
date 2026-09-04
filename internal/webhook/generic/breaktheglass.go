// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/users"
)

type breakTheGlassResourceHandler struct{}

func BreakTheGlassResourceHandler() handlers.Handler {
	return &breakTheGlassResourceHandler{}
}

func (h *breakTheGlassResourceHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, req, decoder, false)
	}
}

func (h *breakTheGlassResourceHandler) OnDelete(
	_ client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, req, decoder, true)
	}
}

func (h *breakTheGlassResourceHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, req, decoder, true)
	}
}

func (h *breakTheGlassResourceHandler) handle(
	_ context.Context,
	req admission.Request,
	decoder admission.Decoder,
	preferOld bool,
) *admission.Response {
	user := users.NewAdmissionUser(users.AdmissionUserUnknown, req.UserInfo)
	if user.IsControllerServiceAccount() {
		return nil
	}

	obj, err := breakRequestProtectionObject(req, decoder, preferOld)
	if err != nil {
		return ad.ErroredResponse(err)
	}

	if obj != nil && obj.GetAnnotations()[meta.BreakRequestServiceAccountAnnotation] == req.UserInfo.Username {
		return nil
	}

	return ad.Deny(
		"resources protected by a BreakRequest can only be changed by the Capsule controller or the template ServiceAccount",
	)
}

func breakRequestProtectionObject(
	req admission.Request,
	decoder admission.Decoder,
	preferOld bool,
) (*unstructured.Unstructured, error) {
	if decoder == nil {
		return nil, nil
	}

	obj := &unstructured.Unstructured{}
	if preferOld {
		if err := decoder.DecodeRaw(req.OldObject, obj); err != nil {
			return nil, err
		}
	}

	// During adoption the old object is not protected yet, so use the new
	// object's controller-owned authorization annotation. Once protection is
	// active, always trust the old object so callers cannot authorize themselves
	// by changing the annotation in the same request.
	if preferOld && obj.GetLabels()[meta.ProtectedByCapsuleLabel] == meta.ValueControllerBreakTheGlass {
		return obj, nil
	}

	if err := decoder.Decode(req, obj); err != nil {
		return nil, err
	}

	return obj, nil
}
