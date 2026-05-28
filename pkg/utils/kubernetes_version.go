// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/discovery"
)

var versionsWithNodeFix = []string{"v1.18.18", "v1.19.10", "v1.20.6", "v1.21.0"}

func NodeWebhookSupported(currentVersion *version.Version) (bool, error) {
	versions := make([]*version.Version, 0, len(versionsWithNodeFix))

	for _, v := range versionsWithNodeFix {
		ver, err := version.ParseGeneric(v)
		if err != nil {
			return false, err
		}

		versions = append(versions, ver)
	}

	for _, v := range versions {
		if currentVersion.Major() == v.Major() {
			if currentVersion.Minor() < v.Minor() {
				return false, nil
			}

			if currentVersion.Minor() == v.Minor() && currentVersion.Patch() < v.Patch() {
				return false, nil
			}
		}
	}

	return true, nil
}

func GetK8sVersionFromConfig(dc discovery.DiscoveryInterface) (*version.Version, error) {
	sv, err := dc.ServerVersion()
	if err != nil {
		return nil, err
	}

	return version.ParseGeneric(sv.String())
}
