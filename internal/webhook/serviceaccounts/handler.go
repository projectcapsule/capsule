// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package serviceaccounts

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/users"
)

func Handler(cfg configuration.Configuration, handler ...handlers.TypedHandlerWithTenantUser[*corev1.ServiceAccount]) handlers.Handler {
	return &handlers.TypedTenantWithUserHandler[*corev1.ServiceAccount]{
		Factory: func() *corev1.ServiceAccount {
			return &corev1.ServiceAccount{}
		},
		Handlers:      handler,
		Configuration: cfg,
		Predicate: func(
			_ admission.Request,
			sa *corev1.ServiceAccount,
			_ *corev1.ServiceAccount,
		) bool {
			labels := sa.GetLabels()
			if len(labels) == 0 {
				return false
			}

			_, promotion := labels[meta.ServiceAccountPromotionLabel]
			_, ownerPromotion := labels[meta.OwnerPromotionLabel]

			return promotion || ownerPromotion
		},
		UserResolver: resolvePromotionUser,
	}
}

func resolvePromotionUser(
	_ context.Context,
	_ client.Client,
	req admission.Request,
	cfg configuration.Configuration,
) users.AdmissionUser {
	user := users.NewAdmissionUser(users.AdmissionUserUnknown, req.UserInfo)

	if user.IsControllerServiceAccount() {
		user.Type = users.AdmissionUserAdmin

		return user
	}

	config := cfg.GetConfigObject()
	if users.HasIgnoredGroup(req.UserInfo.Groups, config.Spec.IgnoreUserWithGroups) {
		return user
	}

	if config.Spec.Administrators.IsPresent(req.UserInfo.Username, req.UserInfo.Groups) {
		user.Type = users.AdmissionUserAdmin
	}

	return user
}
