// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"
	"hash/fnv"

	"github.com/projectcapsule/capsule/pkg/api"
)

func RoleBindingHashFunc(binding api.AdditionalRoleBindingsSpec) string {
	h := fnv.New64a()

	_, _ = h.Write([]byte(binding.ClusterRoleName))

	for _, sub := range binding.Subjects {
		_, _ = h.Write([]byte(sub.Kind + sub.Name))
	}

	return fmt.Sprintf("%x", h.Sum64())
}
