// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
)

// +kubebuilder:validation:Enum=User;Group;ServiceAccount
type UserKind string

func (k UserKind) String() string {
	return string(k)
}

// +kubebuilder:object:generate=true
type UserSpec struct {
	// Kind of entity. Possible values are "User", "Group", and "ServiceAccount"
	Kind OwnerKind `json:"kind"`
	// Name of the entity.
	Name string `json:"name"`
}

func (u UserSpec) Subject() (subject rbacv1.Subject) {
	if u.Kind == ServiceAccountOwner {
		splitName := strings.Split(u.Name, ":")

		subject = rbacv1.Subject{
			Kind:      u.Kind.String(),
			Name:      splitName[len(splitName)-1],
			Namespace: splitName[len(splitName)-2],
		}
	} else {
		subject = rbacv1.Subject{
			APIGroup: rbacv1.GroupName,
			Kind:     u.Kind.String(),
			Name:     u.Name,
		}
	}

	return subject
}
