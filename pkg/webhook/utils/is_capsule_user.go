// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"os"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/utils"
)

func IsCapsuleUser(ctx context.Context, req admission.Request, clt client.Client, users []string, userGroups []string, ignoreGroups []string) bool {
	groupList := utils.NewUserGroupList(req.UserInfo.Groups)
	// if the user is a ServiceAccount belonging to the kube-system namespace, definitely, it's not a Capsule user
	// and we can skip the check in case of Capsule user group assigned to system:authenticated
	// (ref: https://github.com/projectcapsule/capsule/issues/234)
	if groupList.Find("system:serviceaccounts:kube-system") {
		return false
	}

	//nolint:nestif
	if sets.NewString(req.UserInfo.Groups...).Has("system:serviceaccounts") {
		namespace, name, err := serviceaccount.SplitUsername(req.UserInfo.Username)
		if err == nil {
			if namespace == os.Getenv("NAMESPACE") && name == os.Getenv("SERVICE_ACCOUNT") {
				return false
			}

			tl := &capsulev1beta2.TenantList{}
			if err := clt.List(ctx, tl, client.MatchingFieldsSelector{Selector: fields.OneTermEqualSelector(".status.namespaces", namespace)}); err != nil {
				return false
			}

			if len(tl.Items) == 1 {
				return true
			}
		}
	}

	for _, group := range userGroups {
		if groupList.Find(group) {
			if len(ignoreGroups) > 0 {
				for _, ignoreGroup := range ignoreGroups {
					if groupList.Find(ignoreGroup) {
						return false
					}
				}
			}

			return true
		}
	}

	if len(users) > 0 && sets.New[string](users...).Has(req.UserInfo.Username) {
		return true
	}

	return false
}
