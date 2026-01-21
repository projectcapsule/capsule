// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package users

import (
	"context"
	"os"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func IsCapsuleUser(
	ctx context.Context,
	c client.Client,
	cfg configuration.Configuration,
	user string,
	groups []string,
) bool {
	groupList := NewUserGroupList(groups)
	// if the user is a ServiceAccount belonging to the kube-system namespace, definitely, it's not a Capsule user
	// and we can skip the check in case of Capsule user group assigned to system:authenticated
	// (ref: https://github.com/projectcapsule/capsule/issues/234)
	if groupList.Find("system:serviceaccounts:kube-system") {
		return false
	}

	//nolint:nestif
	if sets.NewString(groups...).Has("system:serviceaccounts") {
		namespace, name, err := serviceaccount.SplitUsername(user)
		if err == nil {
			if namespace == os.Getenv("NAMESPACE") && name == os.Getenv("SERVICE_ACCOUNT") {
				return false
			}

			tl := &capsulev1beta2.TenantList{}
			if err := c.List(ctx, tl, client.MatchingFieldsSelector{Selector: fields.OneTermEqualSelector(".status.namespaces", namespace)}); err != nil {
				return false
			}

			if len(tl.Items) == 1 {
				return true
			}
		}
	}

	capsuleUsers := cfg.GetUsersByStatus()

	//nolint:modernize
	for _, group := range capsuleUsers.GetByKinds([]api.OwnerKind{api.GroupOwner}) {
		if groupList.Find(group) {
			if len(cfg.IgnoreUserWithGroups()) > 0 {
				for _, ignoreGroup := range cfg.IgnoreUserWithGroups() {
					if groupList.Find(ignoreGroup) {
						return false
					}
				}
			}

			return true
		}
	}

	users := capsuleUsers.GetByKinds([]api.OwnerKind{api.UserOwner})
	if len(users) > 0 && sets.New[string](users...).Has(user) {
		return true
	}

	return false
}
