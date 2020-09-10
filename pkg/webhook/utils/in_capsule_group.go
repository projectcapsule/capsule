/*
Copyright 2020 Clastix Labs.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/pkg/utils"
	"github.com/clastix/capsule/pkg/webhook"
)

func InCapsuleGroup(capsuleGroup string, webhookHandler webhook.Handler) webhook.Handler {
	return &handler{
		handler:      webhookHandler,
		capsuleGroup: capsuleGroup,
	}
}

type handler struct {
	capsuleGroup string
	handler      webhook.Handler
}

// If the user performing action is not a Capsule user, can be skipped
func (h handler) isCapsuleUser(req admission.Request) bool {
	return utils.UserGroupList(req.UserInfo.Groups).IsInCapsuleGroup(h.capsuleGroup)
}

func (h *handler) OnCreate(client client.Client, decoder *admission.Decoder) webhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		if !h.isCapsuleUser(req) {
			return admission.Allowed("")
		}

		return h.handler.OnCreate(client, decoder)(ctx, req)
	}
}

func (h *handler) OnDelete(client client.Client, decoder *admission.Decoder) webhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		if !h.isCapsuleUser(req) {
			return admission.Allowed("")
		}
		return h.handler.OnDelete(client, decoder)(ctx, req)
	}
}

func (h *handler) OnUpdate(client client.Client, decoder *admission.Decoder) webhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		if !h.isCapsuleUser(req) {
			return admission.Allowed("")
		}
		return h.handler.OnUpdate(client, decoder)(ctx, req)
	}
}
