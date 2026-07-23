// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/projectcapsule/capsule/cmd/cli/cmd/breaktheglass"
	capsuleversion "github.com/projectcapsule/capsule/internal/version"
)

var rootCmd = &cobra.Command{
	Use:     "capsule",
	Short:   "Capsule CLI",
	Version: fmt.Sprintf("%s %s%s", capsuleversion.GitTag, capsuleversion.GitCommit, capsuleversion.GitDirty),
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		//nolint:forbidigo // no other option here
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	// Add subcommands
	rootCmd.AddCommand(breaktheglass.RootCmd)
}
