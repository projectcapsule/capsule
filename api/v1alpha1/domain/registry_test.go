/*
Copyright 2020 Clastix Labs.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
