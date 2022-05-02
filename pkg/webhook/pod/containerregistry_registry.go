// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"regexp"
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

func NewRegistry(value string) Registry {
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

type Registry interface {
	Registry() string
	Repository() string
	Image() string
	Tag() string
}
