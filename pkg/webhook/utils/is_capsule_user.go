package utils

import (
	"github.com/clastix/capsule/pkg/utils"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func IsCapsuleUser(req admission.Request, userGroups []string, ignoredUserGroups sets.String) bool {
	groupList := utils.NewUserGroupList(req.UserInfo.Groups)
	// if a user is found to be in a user group that is provided in the "ignore-user-groups" annotation,
	// the user is not authorized to carry out the operation
	if ignoredUserGroups.HasAny(req.UserInfo.Groups...) {
		return false
	}

	// TODO: remove after confirmation
	// if the user is a ServiceAccount belonging to the kube-system namespace, definitely, it's not a Capsule user
	// and we can skip the check in case of Capsule user group assigned to system:authenticated
	// (ref: https://github.com/clastix/capsule/issues/234)
	//if groupList.Find("system:serviceaccounts:kube-system") {
	//	return false
	//}

	for _, group := range userGroups {
		if groupList.Find(group) {
			return true
		}
	}

	return false
}
