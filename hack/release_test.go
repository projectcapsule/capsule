// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package hack

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestReleaseSigningBundles(t *testing.T) {
	data, err := os.ReadFile("../.goreleaser.yml")
	require.NoError(t, err)

	var config struct {
		Signs []struct {
			Cmd       string   `yaml:"cmd"`
			Signature string   `yaml:"signature"`
			Args      []string `yaml:"args"`
		} `yaml:"signs"`
	}
	require.NoError(t, yaml.Unmarshal(data, &config))
	require.NotEmpty(t, config.Signs)

	for _, sign := range config.Signs {
		require.Equal(t, "cosign", sign.Cmd)
		require.NotEmpty(t, sign.Signature, "GoReleaser must publish the bundle produced by cosign")

		for _, artifact := range []string{
			"dist/capsule_0.15.0_linux_amd64.tar.gz",
			"dist/capsule_0.15.0_linux_arm64.tar.gz",
			"dist/checksums.txt",
			"dist/capsule_0.15.0_linux_arm64.tar.gz.sbom.json",
		} {
			t.Run(filepath.Base(artifact), func(t *testing.T) {
				signature := os.Expand(sign.Signature, func(key string) string {
					require.Equal(t, "artifact", key)

					return artifact
				})
				require.Equal(t, ".json", filepath.Ext(signature))
				require.NotEqual(t, artifact, signature, "signing must preserve the artifact")

				args := make([]string, len(sign.Args))
				for i, arg := range sign.Args {
					args[i] = os.Expand(arg, func(key string) string {
						switch key {
						case "artifact":
							return artifact
						case "signature":
							return signature
						default:
							t.Fatalf("unexpected signing variable %q", key)

							return ""
						}
					})
				}
				require.NotEmpty(t, args)
				require.Equal(t, "sign-blob", args[0])
				require.Contains(t, args, artifact)
				require.Contains(t, args, "--yes")
				require.Contains(t, args, "--bundle="+signature,
					"cosign must write its bundle to the signature path GoReleaser publishes")
			})
		}
	}
}
