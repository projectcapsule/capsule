// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewRegistry(t *testing.T) {
	type tc struct {
		registry string
		repo     string
		image    string
		tag      string
	}
	for name, tc := range map[string]tc{
		"docker.io/my-org/my-repo:v0.0.1": {
			registry: "docker.io",
			repo:     "my-org",
			image:    "my-repo",
			tag:      "v0.0.1",
		},
		"unnamed/repository:1.2.3": {
			registry: "docker.io",
			repo:     "unnamed",
			image:    "repository",
			tag:      "1.2.3",
		},
		"quay.io/clastix/capsule:v1.0.0": {
			registry: "quay.io",
			repo:     "clastix",
			image:    "capsule",
			tag:      "v1.0.0",
		},
		"docker.io/redis:alpine": {
			registry: "docker.io",
			repo:     "",
			image:    "redis",
			tag:      "alpine",
		},
		"nginx:alpine": {
			registry: "docker.io",
			repo:     "",
			image:    "nginx",
			tag:      "alpine",
		},
		"nginx": {
			registry: "docker.io",
			repo:     "",
			image:    "nginx",
			tag:      "latest",
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := NewRegistry(name)
			assert.Equal(t, tc.registry, r.Registry())
			assert.Equal(t, tc.repo, r.Repository())
			assert.Equal(t, tc.image, r.Image())
			assert.Equal(t, tc.tag, r.Tag())
		})
	}
}
