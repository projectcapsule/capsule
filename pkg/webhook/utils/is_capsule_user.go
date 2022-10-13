// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/pkg/utils"
)

func IsCapsuleUser(req admission.Request, userGroups []string) bool {
	groupList := utils.NewUserGroupList(req.UserInfo.Groups)
	// if the user is a ServiceAccount belonging to the kube-system namespace, definitely, it's not a Capsule user
	// and we can skip the check in case of Capsule user group assigned to system:authenticated
	// (ref: https://github.com/clastix/capsule/issues/234)
	if groupList.Find("system:serviceaccounts:kube-system") {
		return false
	}

	for _, group := range userGroups {
		if groupList.Find(group) {
			return true
		}
	}

	return false
}
