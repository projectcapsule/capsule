// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepermit

import (
	"github.com/spf13/cobra"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

var activateCmd = &cobra.Command{
	Use:   "activate",
	Short: "activate a ResourcePermit",
	Args:  cobra.ExactArgs(1),
	Example: `
  # activate an existing ResourcePermit
  kubectl capsule resource-permit activate grant-admin --namespace default
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		name = args[0]

		return runResourcePermitAction(capsulev1beta2.ResourcePermitPhaseActive)
	},
}
