// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package meta

import "fmt"

func NameForManagedRoleBindings(hash string) string {
	return fmt.Sprintf("capsule:managed:%s", hash)
}
