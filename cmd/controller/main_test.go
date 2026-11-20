// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestControllerSubcommands(t *testing.T) {
	for _, command := range []string{"core", "replications"} {
		t.Run(command, func(t *testing.T) {
			var output bytes.Buffer
			got, options, err := parseCommand([]string{command, "--workers=8", "--configuration-name=custom", "--client-connection-qps=75", "--client-connection-burst=150", "--cache-sync-timeout=3m", "--enable-leader-election", "--leader-election-lease-duration=40s", "--tracing-otlp-header=team=capsule"}, &output)
			require.NoError(t, err)
			require.Equal(t, command, got)
			require.Equal(t, 8, options.Controller.Runtime.MaxConcurrentReconciles)
			require.Equal(t, "custom", options.Controller.ConfigurationName)
			require.Equal(t, float32(75), options.ClientConnectionQPS)
			require.Equal(t, int32(150), options.ClientConnectionBurst)
			require.Equal(t, 3*time.Minute, options.Controller.Runtime.CacheSyncTimeout)
			require.True(t, options.EnableLeaderElection)
			require.Equal(t, 40*time.Second, options.LeaderElectionLeaseDuration)
			require.Equal(t, "capsule", options.TracingHeaders["team"])
		})
	}
}

func TestSubcommandHelpAndErrors(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"core", "--help"}, {"replications", "--help"}} {
		var output bytes.Buffer
		command, _, err := parseCommand(args, &output)
		require.NoError(t, err)
		require.Empty(t, command)
		require.NotEmpty(t, output.String())
	}
	for _, args := range [][]string{{"all"}, {"--controller-mode=all"}, {"core", "--controller-mode=all"}, {"replications", "core"}, {"core", "--workers=invalid"}} {
		var output bytes.Buffer
		_, _, err := parseCommand(args, &output)
		require.Error(t, err, "args: %v", args)
	}
	for _, args := range [][]string{{"--version"}, {"core", "--version"}, {"replications", "--version"}} {
		var output bytes.Buffer
		_, options, err := parseCommand(args, &output)
		require.NoError(t, err)
		require.True(t, options.Version)
	}
}
