// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	goRuntime "runtime"

	"github.com/spf13/pflag"
	_ "go.uber.org/automaxprocs"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/projectcapsule/capsule/cmd/controller/common"
	"github.com/projectcapsule/capsule/cmd/controller/core"
	"github.com/projectcapsule/capsule/cmd/controller/replications"
	capsuleversion "github.com/projectcapsule/capsule/internal/version"
)

const usage = "Usage: capsule <core|replications> [flags]\n\n  core          Run namespace controllers and admission webhooks\n  replications  Run TenantResource and GlobalTenantResource controllers\n"

func parseCommand(args []string, output io.Writer) (string, common.Options, error) {
	options := common.Options{}

	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		_, err := io.WriteString(output, usage)

		return "", options, err
	}

	if args[0] == "--version" {
		options.Version = true

		return "", options, nil
	}

	command := args[0]
	if command != "core" && command != "replications" {
		return "", options, fmt.Errorf("unknown controller command %q; use core or replications", command)
	}

	flags := pflag.NewFlagSet(command, pflag.ContinueOnError)
	flags.SetOutput(output)
	options.BindFlags(flags)

	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return "", options, nil
		}

		return "", options, err
	}

	if flags.NArg() != 0 {
		return "", options, fmt.Errorf("unexpected arguments: %v", flags.Args())
	}

	if options.CacheSyncTimeout > 0 {
		options.Controller.Runtime.CacheSyncTimeout = options.CacheSyncTimeout
	}

	return command, options, nil
}

func main() {
	command, options, err := parseCommand(os.Args[1:], os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&options.Logging)))

	if command == "" && !options.Version {
		return
	}

	printVersion()

	if options.Version {
		return
	}

	ctx := ctrl.SetupSignalHandler()

	err = run(ctx, command, options)
	if err != nil {
		ctrl.Log.Error(err, "controller stopped", "command", command)
		os.Exit(1)
	}
}

func run(ctx context.Context, command string, options common.Options) error {
	switch command {
	case "core":
		return core.Run(ctx, options)
	case "replications":
		return replications.Run(ctx, options)
	default:
		return fmt.Errorf("unknown controller command %q", command)
	}
}

func printVersion() {
	ctrl.Log.WithName("version").Info(fmt.Sprintf("Capsule Version %s %s%s", capsuleversion.GitTag, capsuleversion.GitCommit, capsuleversion.GitDirty))
	ctrl.Log.WithName("version").Info(fmt.Sprintf("Build from: %s", capsuleversion.GitRepo))
	ctrl.Log.WithName("version").Info(fmt.Sprintf("Build date: %s", capsuleversion.BuildTime))
	ctrl.Log.WithName("version").Info(fmt.Sprintf("Go Version: %s", goRuntime.Version()))
	ctrl.Log.WithName("version").Info(fmt.Sprintf("Go OS/Arch: %s/%s", goRuntime.GOOS, goRuntime.GOARCH))
}
