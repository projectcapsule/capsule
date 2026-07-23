// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

var (
	name      string
	namespace string
)

var RootCmd = &cobra.Command{
	Use:     "break-the-glass",
	Aliases: []string{"break", "btg"},
	Short:   "Manage BreakRequests",
}

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(capsulev1beta2.AddToScheme(scheme))
}

func init() {
	RootCmd.PersistentFlags().
		StringVarP(&namespace, "namespace", "n", "default", "Namespace of the BreakRequests")

	// Add subcommands
	RootCmd.AddCommand(reviewCmd)
	RootCmd.AddCommand(activateCmd)
	RootCmd.AddCommand(expireCmd)
}
