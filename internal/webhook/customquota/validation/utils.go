// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"

func getType(cq capsulev1beta2.CustomQuota) string {
	if cq.Namespace != "" {
		return "CustomQuota"
	}

	return "ClusterCustomQuota"
}
