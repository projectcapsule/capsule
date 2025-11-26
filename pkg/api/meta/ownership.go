// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

const (
	CapsuleFieldOwnerPrefix = "capsule"
)

func ControllerFieldOwner() string {
	return ControllerFieldOwnerPrefix("controller")
}

func ControllerFieldOwnerPrefix(fieldowner string) string {
	return CapsuleFieldOwnerPrefix + "/" + fieldowner
}
