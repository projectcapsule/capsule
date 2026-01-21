// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package users

import (
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/projectcapsule/capsule/pkg/api"
)

func IsAdminUser(req admission.Request, administrators api.UserListSpec) bool {
	return administrators.IsPresent(req.UserInfo.Username, req.UserInfo.Groups)
}
