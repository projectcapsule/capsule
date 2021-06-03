// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/pkg/configuration"
	"github.com/clastix/capsule/pkg/utils"
	"github.com/clastix/capsule/pkg/webhook"
)

func InCapsuleGroups(configuration configuration.Configuration, webhookHandler webhook.Handler) webhook.Handler {
	return &handler{
		configuration: configuration,
		handler:       webhookHandler,
	}
}

type handler struct {
	configuration configuration.Configuration
	handler       webhook.Handler
}

// If the user performing action is not a Capsule user, can be skipped
func (h handler) isCapsuleUser(req admission.Request) bool {
	groupList := utils.NewUserGroupList(req.UserInfo.Groups)
	// if the user is a ServiceAccount belonging to the kube-system namespace, definitely, it's not a Capsule user
	// and we can skip the check in case of Capsule user group assigned to system:authenticated
	// (ref: https://github.com/clastix/capsule/issues/234)
	if groupList.Find("system:serviceaccounts:kube-system") {
		return false
	}
	for _, group := range h.configuration.UserGroups() {
		if groupList.Find(group) {
			return true
		}
	}
	return false
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
