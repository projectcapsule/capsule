// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type managedValidatingHandler struct {
	configuration configuration.Configuration
}

func ManagedValidatingHandler(configuration configuration.Configuration) handlers.Handler {
	return &managedValidatingHandler{
		configuration: configuration,
	}
}

func (h *managedValidatingHandler) OnCreate(
	c client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, req, c)
	}
}

func (h *managedValidatingHandler) OnDelete(
	c client.Client,
	_ client.Reader,
	_ admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		user := handlers.ResolveAdmissionUser(ctx, c, req, h.configuration)
		if user.IsAdmin() {
			return nil
		}

		// Namespace deletion must be able to finish. Once Kubernetes has marked
		// the namespace terminating, allow its garbage collector and finalizers
		// to remove controller-managed namespaced objects.
		if req.Namespace != "" {
			ns := &corev1.Namespace{}
			if err := c.Get(ctx, types.NamespacedName{Name: req.Namespace}, ns); err != nil {
				if apierrors.IsNotFound(err) {
					return nil
				}

				return ad.ErroredResponse(err)
			}

			if ns.DeletionTimestamp != nil || ns.Status.Phase == corev1.NamespaceTerminating {
				return nil
			}
		}

		return h.handle(ctx, req, c)
	}
}

func (h *managedValidatingHandler) OnUpdate(
	c client.Client,
	_ client.Reader,
	_ admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handle(ctx, req, c)
	}
}

func (h *managedValidatingHandler) handle(
	ctx context.Context,
	req admission.Request,
	c client.Client,
) *admission.Response {
	user := handlers.ResolveAdmissionUser(ctx, c, req, h.configuration)

	if user.IsAdmin() {
		return nil
	}

	return ad.Deny("Labeling resources as controller managed can only be done by the controller or administrators")
}
