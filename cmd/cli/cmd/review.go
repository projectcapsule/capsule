// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/xhit/go-str2duration/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	v1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/breaktheglass"
)

var (
	approveFlag, denyFlag, noColorFlag    bool
	message                               string
	startTimeStr, durationStr, keepForStr string
)

const (
	denyValue    = "deny"
	approveValue = "approve"
)

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Review a BreakRequest",
	Args:  cobra.ExactArgs(1),
	Example: `
  # interactive review
  kubectl break-glass review grant-admin --namespace default

  # non-interactive approve/deny
  kubectl break-glass review grant-admin --namespace default --approve
  kubectl break-glass review grant-admin --namespace default --deny
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		name = args[0]

		ctx := context.Background()

		cfg, k8sClient, err := newK8sClient()
		if err != nil {
			return err
		}

		br := &v1beta2.BreakRequest{}
		if err := k8sClient.Get(
			ctx,
			ctrlclient.ObjectKey{Name: name, Namespace: namespace},
			br,
		); err != nil {
			return err
		}

		if br.Status.Phase == "" {
			return fmt.Errorf(
				"BreakRequest %s is not yet processed, current phase: %q",
				name,
				br.Status.Phase,
			)
		}

		if br.Status.Phase != v1beta2.RequestPhaseRequested {
			return fmt.Errorf(
				"BreakRequest %s is not in Requested phase (already reviewed), current phase: %q",
				name,
				br.Status.Phase,
			)
		}

		props, err := br.GenerateApprovedProperties()
		if err != nil {
			return err
		}

		// Parse Flags and Overwrite
		if keepForStr != "" {
			d, err := str2duration.ParseDuration(keepForStr)
			if err != nil {
				return fmt.Errorf("invalid duration %q: %w", keepForStr, err)
			}

			props.KeepFor = breaktheglass.ExtendedDuration(d)
		}

		if durationStr != "" {
			d, err := time.ParseDuration(durationStr)
			if err != nil {
				return fmt.Errorf("invalid duration %q: %w", durationStr, err)
			}

			props.Duration = &metav1.Duration{Duration: d}
		}

		// Validate Action

		action := ""
		if approveFlag {
			action = approveValue
		} else if denyFlag {
			action = denyValue
		} else {
			printAccessRequestApprovalTable(br, props, !noColorFlag)

			var input string

			for {
				cmd.Print("Approve this request? [y/n]: ")

				_, _ = fmt.Scanln(&input)

				input = strings.ToLower(strings.TrimSpace(input))
				if input == "y" {
					action = approveValue

					break
				} else if input == "n" {
					action = denyValue

					break
				} else {
					cmd.Println("Invalid input. Please type 'y' or 'n'.")
				}
			}
		}

		user := &breaktheglass.AccessEntity{
			Type: breaktheglass.AccessEntityTypeUser,
			Name: cfg.Username,
		}

		return retry.OnError(
			retry.DefaultRetry,
			func(err error) bool {
				return ctrlclient.IgnoreNotFound(err) == nil
			},
			func() error {
				if err := k8sClient.Get(
					ctx,
					ctrlclient.ObjectKey{Name: name, Namespace: namespace},
					br,
				); err != nil {
					return err
				}

				switch action {
				case approveValue:
					if err := br.ApproveRequest(user, props, message); err != nil {
						return err
					}
				case denyValue:
					if err := br.DenyRequest(user, message); err != nil {
						return err
					}
				}

				return k8sClient.Status().Update(ctx, br)
			},
		)
	},
}

func init() {
	// Register the flag to the `approve` subcommand
	reviewCmd.Flags().BoolVar(&approveFlag, "approve", false, "Approve the request")
	reviewCmd.Flags().BoolVar(&denyFlag, "deny", false, "Deny the request")
	reviewCmd.Flags().BoolVar(&noColorFlag, "no-color", false, "Don't colorize the output")
	reviewCmd.Flags().StringVarP(&message, "message", "m", "", "Optional review message")
	reviewCmd.Flags().
		StringVar(&startTimeStr, "start-time", "", "Start time (RFC3339 format, e.g. 2025-07-15T14:45:00Z)")
	reviewCmd.Flags().StringVar(&durationStr, "duration", "",
		"The ExtendedDuration this request is available for (e.g. 5m, 1h30m) "+
			"[Overwrites the value from the request, if defined]")
	reviewCmd.Flags().StringVar(&keepForStr, "keep-for", "",
		"The ExtendedDuration this request is archived for (e.g. 5m, 1h30m) "+
			"[Overwrites the value from the request, if defined]")
}
