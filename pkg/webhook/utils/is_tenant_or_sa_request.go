package utils

import (
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	"github.com/clastix/capsule/pkg/utils"
)

func RequestFromOwnerOrSA(tenant capsulev1beta1.Tenant, req admission.Request, userGroups []string) bool {
	for _, owner := range tenant.Spec.Owners {
		switch {
		case (owner.Kind == "User" || owner.Kind == "ServiceAccount") && req.UserInfo.Username == owner.Name:
			return true
		case owner.Kind == "Group":
			groupList := utils.NewUserGroupList(req.UserInfo.Groups)
			for _, group := range userGroups {
				if groupList.Find(group) {
					return true
				}
			}
		}
	}
	for _, group := range req.UserInfo.Groups {
		if len(req.Namespace) > 0 && strings.HasPrefix(group, "system:serviceaccounts:"+req.Namespace) {
			return true
		}
	}
	return false
}
