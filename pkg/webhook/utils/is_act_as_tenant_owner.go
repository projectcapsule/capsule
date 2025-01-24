// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	authenticationv1 "k8s.io/api/authentication/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

func IsActAsTenantOwner(additionalRoleBindings []api.AdditionalRoleBindingsSpec, userInfo authenticationv1.UserInfo) bool {
	for _, additionalRoleBinding := range additionalRoleBindings {
		if additionalRoleBinding.ActAsOwner {
			for _, subject := range additionalRoleBinding.Subjects {
				switch subject.Kind {
				case string(capsulev1beta2.UserOwner), string(capsulev1beta2.ServiceAccountOwner):
					if userInfo.Username == subject.Name {
						return true
					}
				case string(capsulev1beta2.GroupOwner):
					for _, group := range userInfo.Groups {
						if group == subject.Name {
							return true
						}
					}
				}
			}
		}
	}

	return false
}
