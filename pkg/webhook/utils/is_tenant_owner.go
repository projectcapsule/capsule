// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	authenticationv1 "k8s.io/api/authentication/v1"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

func IsTenantOwner(owners capsulev1beta2.OwnerListSpec, userInfo authenticationv1.UserInfo) bool {
	for _, owner := range owners {
		switch owner.Kind {
		case capsulev1beta2.UserOwner, capsulev1beta2.ServiceAccountOwner:
			if userInfo.Username == owner.Name {
				return true
			}
		case capsulev1beta2.GroupOwner:
			for _, group := range userInfo.Groups {
				if group == owner.Name {
					return true
				}
			}
		}
	}

	return false
}
