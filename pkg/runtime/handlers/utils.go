// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"

	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/users"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type NewObjectFunc[T client.Object] func() T

func ResolveAdmissionUser(
	ctx context.Context,
	c client.Client,
	req admission.Request,
	config configuration.Configuration,
) users.AdmissionUser {
	user := users.NewAdmissionUser(users.AdmissionUserUnknown, req.UserInfo)

	if user.IsControllerServiceAccount() {
		user.Type = users.AdmissionUserAdmin
		return user
	}

	if config.Administrators().IsPresent(req.UserInfo.Username, req.UserInfo.Groups) {
		user.Type = users.AdmissionUserAdmin
		return user
	}

	if users.IsCapsuleUser(ctx, c, config, req.UserInfo.Username, req.UserInfo.Groups) {
		user.Type = users.AdmissionUserCapsule
		return user
	}

	return user
}
