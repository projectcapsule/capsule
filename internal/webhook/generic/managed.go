// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"fmt"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/users"
)

type managedValidatingHandler struct {
	configuration configuration.Configuration
}

func ManagedValidatingHandler(configuration configuration.Configuration) handlers.Handler {
	return &managedValidatingHandler{
		configuration: configuration,
	}
}

func (h *managedValidatingHandler) OnCreate(c client.Client, decoder admission.Decoder, _ events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if !users.IsCapsuleUser(ctx, c, h.configuration, req.UserInfo.Username, req.UserInfo.Groups) {
			return nil
		}

		msg := "Labeling resources as controller managed can only be done by the controller or administrators"

		response := admission.Denied(msg)

		return &response
	}
}

func (h *managedValidatingHandler) OnDelete(c client.Client, _ admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if users.IsCapsuleUser(ctx, c, h.configuration, req.UserInfo.Username, req.UserInfo.Groups) {
			msg := fmt.Sprintf("The attempted operation %s for %s/%s/%s/%s/%s is not permitted for controller managed resources.", req.Operation, req.Namespace, req.RequestKind.Group, req.RequestKind.Version, req.RequestKind.Kind, req.Name)

			response := admission.Denied(msg)

			return &response
		}

		return namespaceHasFinalizers(ctx, c, req.Namespace)
	}
}

func (h *managedValidatingHandler) OnUpdate(c client.Client, _ admission.Decoder, recorder events.EventRecorder) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		if !users.IsCapsuleUser(ctx, c, h.configuration, req.UserInfo.Username, req.UserInfo.Groups) {
			return nil
		}

		msg := fmt.Sprintf("The attempted operation %s for %s/%s/%s/%s/%s is not permitted for controller managed resources.", req.Operation, req.Namespace, req.RequestKind.Group, req.RequestKind.Version, req.RequestKind.Kind, req.Name)

		response := admission.Denied(msg)

		return &response
	}
}
