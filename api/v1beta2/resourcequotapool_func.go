// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Retrieves Default-Values, Only Resources which are mentioned in the Quota Hard Spec are given with a default
// If nothing is set, the value will be set to zero
func (g *ResourceQuotaPool) GetResourceDefaults() corev1.ResourceList {
	defaults := corev1.ResourceList{}

	for resourceName, _ := range g.Spec.Quota.Hard {
		amount, exists := g.Spec.Defaults[resourceName]
		if !exists {
			amount = resource.MustParse("0")
		}

		defaults[resourceName] = amount
	}

	return defaults
}

// Gets the Hard specification for the resourcequotas
// This takes into account the default resources being used. However they don't count towards the claim usage
// This can be changed in the future, the default is not calculated as usage because this might interrupt the namespace management
// As we would need to verify if a new namespace with it's defaults still has place in the Pool. Same with attempting to join exisitng namespaces
func (g *ResourceQuotaPool) GetResourceQuotaHardResources(namespace string) corev1.ResourceList {
	// Read Resources which are claimed
	_, claimed := g.GetNamespaceClaims(namespace)

	for resourceName, amount := range g.GetResourceDefaults() {
		usedValue, usedExists := claimed[resourceName]
		if !usedExists {
			usedValue = resource.MustParse("0")
		}

		// Combine with claim
		usedValue.Add(amount)

		claimed[resourceName] = usedValue
	}

	claimedResources := corev1.ResourceList{}

	return claimedResources
}

// Gets the total amount of claimed resources for a namespace
func (g *ResourceQuotaPool) GetNamespaceClaims(namespace string) (claims map[string]*ResourceQuotaPoolClaimsItem, claimedResources corev1.ResourceList) {
	claimedResources = corev1.ResourceList{}

	// First, check if quota exists in the status
	for _, claim := range g.Status.Claims {
		if claim.Namespace.String() == namespace {
			for resourceName, claimed := range claim.Claims {
				usedValue, usedExists := claimedResources[resourceName]
				if !usedExists {
					usedValue = resource.MustParse("0") // Default to zero if no used value is found
				}

				// Combine with claim
				usedValue.Add(claimed)

				claimedResources[resourceName] = usedValue
			}

			claims[string(claim.UID)] = claim
		}

	}

	return
}

func (g *ResourceQuotaPool) AssignNamespaces(namespaces []corev1.Namespace) {
	var l []string

	for _, ns := range namespaces {
		if ns.Status.Phase == corev1.NamespaceActive {
			l = append(l, ns.GetName())
		}
	}

	sort.Strings(l)

	g.Status.Namespaces = l
}
