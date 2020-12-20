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
