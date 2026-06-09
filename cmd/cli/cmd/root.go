// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsuleversion "github.com/projectcapsule/capsule/internal/version"
)

var (
	name      string
	namespace string
)

var rootCmd = &cobra.Command{
	Use:     "capsule",
	Short:   "Manage BreakRequests",
	Version: fmt.Sprintf("%s %s%s", capsuleversion.GitTag, capsuleversion.GitCommit, capsuleversion.GitDirty),
}

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(capsulev1beta2.AddToScheme(scheme))
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		//nolint:forbidigo // no other option here
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().
		StringVarP(&namespace, "namespace", "n", "default", "Namespace of the BreakRequests")

	// Add subcommands
	rootCmd.AddCommand(reviewCmd)
	rootCmd.AddCommand(activateCmd)
	rootCmd.AddCommand(expireCmd)
}
