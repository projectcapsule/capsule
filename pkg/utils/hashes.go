// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"

	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

func RoleBindingHashFunc(binding rbac.AdditionalRoleBindingsSpec) string {
	h := fnv.New64a()
	writeField := func(value string) {
		var length [8]byte

		binary.LittleEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = h.Write(length[:])
		_, _ = h.Write([]byte(value))
	}

	writeField(binding.ClusterRoleName)

	for _, sub := range binding.Subjects {
		writeField(sub.APIGroup)
		writeField(sub.Kind)
		writeField(sub.Namespace)
		writeField(sub.Name)
	}

	return fmt.Sprintf("%x", h.Sum64())
}
