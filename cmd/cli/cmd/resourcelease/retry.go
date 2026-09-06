// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcelease

import (
	"github.com/spf13/cobra"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

var retryCmd = &cobra.Command{
	Use:   "retry",
	Short: "retry a failed ResourceLease",
	Args:  cobra.ExactArgs(1),
	Example: `
  # retry a failed ResourceLease after fixing its execution identity or permissions
  kubectl capsule resource-lease retry grant-admin --namespace default
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		name = args[0]

		return runResourceLeaseAction(capsulev1beta2.ResourceLeasePhaseRetrying)
	},
}
