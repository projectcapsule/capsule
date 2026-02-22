// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package configuration

import "os"

const (
	EnvironmentServiceaccountName  string = "SERVICE_ACCOUNT"
	EnvironmentControllerNamespace string = "NAMESPACE"
)

func ControllerServiceAccount() (name string, namespace string) {
	return os.Getenv("SERVICE_ACCOUNT"), os.Getenv("NAMESPACE")
}

func IsControllerServiceAccount(name string, namespace string) bool {
	sa, ns := ControllerServiceAccount()

	if ns == "" || sa == "" {
		return false
	}

	return namespace == ns && name == sa
}
