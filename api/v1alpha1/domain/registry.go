// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"regexp"
)

const defaultRegistryName = "docker.io"

type registry map[string]string

func (r registry) Registry() string {
	res, ok := r["registry"]
	if !ok {
		return ""
	}
	if len(res) == 0 {
		return defaultRegistryName
	}
	return res
}

func (r registry) Repository() string {
	res, ok := r["repository"]
	if !ok {
		return ""
	}
	if res == defaultRegistryName {
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
	r := regexp.MustCompile(`(((?P<registry>[a-zA-Z0-9-.]+)\/)?((?P<repository>[a-zA-Z0-9-.]+)\/))?(?P<image>[a-zA-Z0-9-.]+)(:(?P<tag>[a-zA-Z0-9-.]+))?`)
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
