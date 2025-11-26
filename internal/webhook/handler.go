// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type Func func(ctx context.Context, req admission.Request) *admission.Response

type Handler interface {
	OnCreate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) Func
	OnDelete(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) Func
	OnUpdate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) Func
}

type HanderWithTenant interface {
	OnCreate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder, tnt *capsulev1beta2.Tenant) Func
	OnUpdate(c client.Client, decoder admission.Decoder, recorder record.EventRecorder, tnt *capsulev1beta2.Tenant) Func
	OnDelete(c client.Client, decoder admission.Decoder, recorder record.EventRecorder, tnt *capsulev1beta2.Tenant) Func
}

type TypedHandler[T client.Object] interface {
	OnCreate(c client.Client, obj T, decoder admission.Decoder, recorder record.EventRecorder) Func
	OnUpdate(c client.Client, obj T, old T, decoder admission.Decoder, recorder record.EventRecorder) Func
	OnDelete(c client.Client, obj T, decoder admission.Decoder, recorder record.EventRecorder) Func
}

type TypedHandlerWithTenant[T client.Object] interface {
	OnCreate(c client.Client, obj T, decoder admission.Decoder, recorder record.EventRecorder, tnt *capsulev1beta2.Tenant) Func
	OnUpdate(c client.Client, obj T, old T, decoder admission.Decoder, recorder record.EventRecorder, tnt *capsulev1beta2.Tenant) Func
	OnDelete(c client.Client, obj T, decoder admission.Decoder, recorder record.EventRecorder, tnt *capsulev1beta2.Tenant) Func
}
