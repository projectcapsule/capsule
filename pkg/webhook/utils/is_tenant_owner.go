package utils

import (
	authenticationv1 "k8s.io/api/authentication/v1"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

func IsTenantOwner(owners capsulev1beta1.OwnerListSpec, userInfo authenticationv1.UserInfo) bool {
	for _, owner := range owners {
		switch owner.Kind {
		case "User", "ServiceAccount":
			if userInfo.Username == owner.Name {
				return true
			}
		case "Group":
			for _, group := range userInfo.Groups {
				if group == owner.Name {
					return true
				}
			}
		}
	}

	return false
}
