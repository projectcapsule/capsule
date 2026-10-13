// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package ssa

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func testAddonFieldOwner(obj metav1.Object) string {
	return meta.ResourceFieldOwner("example-addon/" + string(obj.GetUID()))
}
