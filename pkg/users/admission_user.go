// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package users

import (
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
)

type AdmissionUserType string

const (
	AdmissionUserUnknown AdmissionUserType = "Unknown"
	AdmissionUserAdmin   AdmissionUserType = "Admin"
	AdmissionUserCapsule AdmissionUserType = "Capsule"
)

type AdmissionUser struct {
	Type     AdmissionUserType
	Username string
	Groups   []string

	ServiceAccount *AdmissionServiceAccount
}

type AdmissionServiceAccount struct {
	Namespace string
	Name      string
}

func NewAdmissionUser(userType AdmissionUserType, info authenticationv1.UserInfo) AdmissionUser {
	return AdmissionUser{
		Type:           userType,
		Username:       info.Username,
		Groups:         info.Groups,
		ServiceAccount: ToServiceAccount(info.Username),
	}
}

func (u AdmissionUser) IsAdmin() bool {
	return u.Type == AdmissionUserAdmin
}

func (u AdmissionUser) IsCapsule() bool {
	return u.Type == AdmissionUserCapsule
}

func (u AdmissionUser) IsUnknown() bool {
	return u.Type == AdmissionUserUnknown
}

func (u AdmissionUser) UserInfo() authenticationv1.UserInfo {
	return authenticationv1.UserInfo{
		Username: u.Username,
		Groups:   u.Groups,
	}
}

func (u AdmissionUser) IsControllerServiceAccount() bool {
	if u.ServiceAccount == nil {
		return false
	}

	name, namespace := configuration.ControllerServiceAccount()

	if namespace == "" || name == "" {
		return false
	}

	return u.ServiceAccount.Namespace == namespace && u.ServiceAccount.Name == name
}

func ToServiceAccount(username string) *AdmissionServiceAccount {
	namespace, name, err := serviceaccount.SplitUsername(username)
	if err != nil {
		return nil
	}

	return &AdmissionServiceAccount{
		Namespace: namespace,
		Name:      name,
	}
}

func ServiceAccountUsername(namespace, name string) string {
	return serviceaccount.MakeUsername(namespace, name)
}

func ServiceAccountGroups(namespace string) []string {
	return GetServiceAccountGroups(namespace)
}

func ServiceAccountUserInfo(namespace, name string) authenticationv1.UserInfo {
	return authenticationv1.UserInfo{
		Username: ServiceAccountUsername(namespace, name),
		Groups:   ServiceAccountGroups(namespace),
	}
}
