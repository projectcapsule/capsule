// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

const (
	FieldManagerCapsulePrefix     = "projectcapsule.dev"
	FieldManagerCapsuleController = "projectcapsule.dev/controller"
)

func ControllerFieldOwnerPrefix(fieldowner string) string {
	return FieldManagerCapsulePrefix + "/" + fieldowner
}
