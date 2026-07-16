// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"github.com/spf13/cobra"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
)

var expireCmd = &cobra.Command{
	Use:   "expire",
	Short: "expire a BreakRequest",
	Args:  cobra.ExactArgs(1),
	Example: `
  # expire an existing BreakRequest
  capsule expire grant-admin --namespace default
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		name = args[0]

		return runBreakRequestAction(
			func(br *capsulev1beta2.BreakRequest, user *breaktheglass.AccessEntity) error {
				return br.ExpireRequest(user)
			},
		)
	},
}
