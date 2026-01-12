// Copyright 2020-2025 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func getFieldOwner(name string, namespace string) string {
	if namespace == "" {
		namespace = "cluster"
	}

	return meta.CapsuleFieldOwnerPrefix + "/" + "resource" + "/" + namespace + "/" + name + "/"
}
