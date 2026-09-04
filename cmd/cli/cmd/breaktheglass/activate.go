// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"github.com/spf13/cobra"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

var activateCmd = &cobra.Command{
	Use:   "activate",
	Short: "activate a BreakRequest",
	Args:  cobra.ExactArgs(1),
	Example: `
  # activate an existing BreakRequest
  kubectl capsule break-the-glass activate grant-admin --namespace default
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		name = args[0]

		return runBreakRequestAction(capsulev1beta2.RequestPhaseActive)
	},
}
