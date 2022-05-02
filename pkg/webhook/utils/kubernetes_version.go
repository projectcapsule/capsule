// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"path/filepath"

	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
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

func GetK8sVersion() (*version.Version, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}

	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	v, err := client.Discovery().ServerVersion()
	if err != nil {
		return nil, err
	}

	return version.ParseGeneric(v.String())
}
