// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

type registry map[string]string

func (r registry) Registry() string {
	res, ok := r["registry"]
	if !ok {
		return ""
	}

	return res
}

func (r registry) Repository() string {
	res, ok := r["repository"]
	if !ok {
		return ""
	}

	return res
}

func (r registry) Image() string {
	res, ok := r["image"]
	if !ok {
		return ""
	}

	return res
}

func (r registry) Tag() string {
	res, ok := r["tag"]
	if !ok {
		return ""
	}

	if len(res) == 0 {
		res = "latest"
	}

	return res
}

func (r registry) FQCI() string {
	reg := r.Registry()
	repo := r.Repository()
	img := r.Image()
	tag := r.Tag()

	// If there's no image, nothing to return
	if img == "" {
		return ""
	}

	// ensure repo ends with "/" if set
	if repo != "" && repo[len(repo)-1] != '/' {
		repo += "/"
	}

	// always append tag to image (strip any trailing : from image just in case)
	// but our Image() already includes the name:tag, so split carefully
	name := img
	if tag != "" && !strings.Contains(img, ":") {
		name = fmt.Sprintf("%s:%s", img, tag)
	}

	// build: [registry/]repo+image
	if reg != "" {
		return fmt.Sprintf("%s/%s%s", reg, repo, name)
	}

	return fmt.Sprintf("%s%s", repo, name)
}

type Registry interface {
	Registry() string
	Repository() string
	Image() string
	Tag() string
	FQCI() string
}

func NewRegistry(value string, cfg configuration.Configuration) Registry {
	reg := make(registry)
	r := regexp.MustCompile(`((?P<registry>[a-zA-Z0-9-._]+(:\d+)?)\/)?(?P<repository>.*\/)?(?P<image>[a-zA-Z0-9-._]+:(?P<tag>[a-zA-Z0-9-._]+))?`)
	match := r.FindStringSubmatch(value)

	for i, name := range r.SubexpNames() {
		if i > 0 && i <= len(match) {
			reg[name] = match[i]
		}
	}

	return reg
}
