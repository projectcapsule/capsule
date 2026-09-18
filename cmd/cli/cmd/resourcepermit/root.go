// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepermit

import (
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

var (
	name          string
	namespace     string
	impersonation impersonationOptions
)

var RootCmd = &cobra.Command{
	Use:     "resource-permit",
	Aliases: []string{"resourcepermit", "rp", "permit"},
	Short:   "Manage ResourcePermits",
}

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(capsulev1beta2.AddToScheme(scheme))
}

func init() {
	RootCmd.PersistentFlags().
		StringVarP(&namespace, "namespace", "n", "default", "Namespace of the ResourcePermits")
	RootCmd.PersistentFlags().
		StringVar(&impersonation.User, "as", "", "Username to impersonate for the operation")
	RootCmd.PersistentFlags().
		StringArrayVar(&impersonation.Groups, "as-group", nil, "Group to impersonate; may be repeated")

	// Add subcommands
	RootCmd.AddCommand(reviewCmd)
	RootCmd.AddCommand(activateCmd)
	RootCmd.AddCommand(expireCmd)
	RootCmd.AddCommand(retryCmd)
}
