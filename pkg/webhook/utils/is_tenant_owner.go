// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"strings"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/meta"
)

func IsTenantOwner(
	ctx context.Context,
	c client.Client,
	tenant *capsulev1beta2.Tenant,
	userInfo authenticationv1.UserInfo,
	promotedServiceAccountOwners bool,
) (bool, error) {
	for _, owner := range tenant.Spec.Owners {
		switch owner.Kind {
		case capsulev1beta2.UserOwner, capsulev1beta2.ServiceAccountOwner:
			if userInfo.Username == owner.Name {
				return true, nil
			}
		case capsulev1beta2.GroupOwner:
			for _, group := range userInfo.Groups {
				if group == owner.Name {
					return true, nil
				}
			}
		}
	}

	if promotedServiceAccountOwners {
		parts := strings.Split(userInfo.Username, ":")

		if len(parts) != 4 {
			return false, nil
		}

		saList := &corev1.ServiceAccountList{}
		if err := c.List(ctx, saList,
			client.InNamespace(parts[2]),
			client.MatchingLabels{
				meta.OwnerPromotionLabel: meta.OwnerPromotionLabelTrigger,
			}); err != nil {
			return false, err
		}

		for _, sa := range saList.Items {
			saName := serviceaccount.ServiceAccountUsernamePrefix + sa.Namespace + ":" + sa.Name

			if userInfo.Username == saName {
				return true, nil
			}
		}
	}

	return false, nil
}
