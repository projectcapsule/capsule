// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package gvk

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

// GetGVKByPlural returns the GroupVersionKind for a given plural name.
func ReplacePluralWithKind(discoveryClient *discovery.DiscoveryClient, gvk *schema.GroupVersionKind) error {
	resourceList, err := discoveryClient.ServerResourcesForGroupVersion(gvk.Group + "/" + gvk.Version)
	if err != nil {
		return err
	}

	for _, resource := range resourceList.APIResources {
		if resource.Name == gvk.Kind {
			gvk.Kind = resource.Kind

			return nil
		}
	}

	return fmt.Errorf("could not find GVK for plural name: %s", gvk.Kind)
}
