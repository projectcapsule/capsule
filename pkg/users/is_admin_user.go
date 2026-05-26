// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package users

import (
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func IsAdminUser(req admission.Request, administrators rbac.UserListSpec) bool {
	if IsControllerServiceAccount(req.UserInfo.Username) {
		return true
	}

	return administrators.IsPresent(req.UserInfo.Username, req.UserInfo.Groups)
}

func IsControllerServiceAccount(username string) bool {
	namespace, name, err := serviceaccount.SplitUsername(username)
	if err != nil {
		return false
	}

	controllerName, controllerNamespace := configuration.ControllerServiceAccount()

	return namespace == controllerNamespace && name == controllerName
}
