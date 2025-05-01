// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"errors"
	"sort"

	"github.com/projectcapsule/capsule/pkg/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func (r *ResourcePool) AssignNamespaces(namespaces []corev1.Namespace) {
	var l []string

	for _, ns := range namespaces {
		if ns.Status.Phase == corev1.NamespaceActive && ns.DeletionTimestamp == nil {
			l = append(l, ns.GetName())
		}
	}

	sort.Strings(l)

	r.Status.Size = uint(len(l))
	r.Status.Namespaces = l
}

func (r *ResourcePool) GetClaimFromStatus(cl *ResourcePoolClaim) *ResourcePoolClaimsItem {
	ns := cl.Namespace

	claims := r.Status.Claims[ns]
	if claims == nil {
		return nil
	}

	for _, claim := range claims {
		if claim.UID == cl.UID {
			return claim
		}
	}

	return nil
}

func (r *ResourcePool) AddClaimToStatus(claim *ResourcePoolClaim) {
	ns := claim.Namespace

	if r.Status.Claims == nil {
		r.Status.Claims = ResourcePoolNamespaceClaimsStatus{}
	}
	if r.Status.Allocation.Claimed == nil {
		r.Status.Allocation.Claimed = corev1.ResourceList{}
	}

	claims := r.Status.Claims[ns]
	if claims == nil {
		claims = ResourcePoolClaimsList{}
	}

	scl := &ResourcePoolClaimsItem{
		StatusNameUID: api.StatusNameUID{
			UID:       claim.UID,
			Name:      api.Name(claim.Name),
			Namespace: api.Name(ns),
		},
		Claims: claim.Spec.ResourceClaims,
	}

	// Try to update existing entry if UID matches
	exists := false
	for i, cl := range claims {
		if cl.UID == claim.UID {
			claims[i] = scl

			exists = true
			break
		}
	}

	if !exists {
		claims = append(claims, scl)
	}

	r.Status.Claims[ns] = claims

	r.CalculateUsage()
}

func (r *ResourcePool) RemoveClaimFromStatus(claim *ResourcePoolClaim) {
	newClaims := ResourcePoolClaimsList{}
	claims, ok := r.Status.Claims[claim.Namespace]
	if !ok {
		return
	}

	for _, cl := range claims {
		if cl.UID != claim.UID {
			newClaims = append(newClaims, cl)
		}
	}

	r.Status.Claims[claim.Namespace] = newClaims

	if len(newClaims) == 0 {
		delete(r.Status.Claims, claim.Namespace)
	}
}

func (r *ResourcePool) CalculateUsage() {
	usage := corev1.ResourceList{}

	if len(r.Status.Claims) == 0 {
		for r, _ := range r.Status.Allocation.Hard {
			usage[r] = resource.MustParse("0")
		}

		return
	} else {
		for _, claims := range r.Status.Claims {
			for _, claim := range claims {
				for resourceName, qt := range claim.Claims {
					amount, exists := usage[resourceName]
					if !exists {
						amount = resource.MustParse("0")
					}

					amount.Add(qt)
					usage[resourceName] = amount
				}
			}
		}
	}

	r.Status.Allocation.Claimed = usage
}

func (g *ResourcePool) CanClaimFromPool(claim corev1.ResourceList) []error {
	claimable := g.GetAvailableClaimableResources()
	errs := []error{}

	for resourceName, req := range claim {
		available, exists := claimable[resourceName]
		if !exists || available.IsZero() || available.Cmp(req) < 0 {
			errs = append(errs, errors.New("not enough resources"+string(resourceName)+"available"))
		}

	}

	return errs
}

func (g *ResourcePool) GetAvailableClaimableResources() corev1.ResourceList {
	hard := g.Status.Allocation.Hard.DeepCopy()

	for resourceName, qt := range hard {
		claimed, exists := g.Status.Allocation.Claimed[resourceName]
		if !exists {
			claimed = resource.MustParse("0")
		}

		qt.Sub(claimed)

		hard[resourceName] = qt
	}

	return hard
}

// Retrieves Default-Values, Only Resources which are mentioned in the Quota Hard Spec are given with a default
// If nothing is set, the value will be set to zero
func (g *ResourcePool) GetResourceDefaults() corev1.ResourceList {
	defaults := corev1.ResourceList{}

	for resourceName := range g.Spec.Quota.Hard {
		amount, exists := g.Spec.Defaults.Resources[resourceName]
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
func (g *ResourcePool) GetResourceQuotaHardResources(namespace string) corev1.ResourceList {
	// Read Resources which are claimed
	_, claimed := g.GetNamespaceClaims(namespace)

	// Only Consider Default, when enabled
	if g.Spec.Defaults.Enabled {
		for resourceName, amount := range g.GetResourceDefaults() {
			usedValue, usedExists := claimed[resourceName]
			if !usedExists {
				usedValue = resource.MustParse("0")
			}

			// Combine with claim
			usedValue.Add(amount)

			claimed[resourceName] = usedValue
		}
	}

	return claimed
}

// Gets the total amount of claimed resources for a namespace
func (g *ResourcePool) GetNamespaceClaims(namespace string) (claims map[string]*ResourcePoolClaimsItem, claimedResources corev1.ResourceList) {
	claimedResources = corev1.ResourceList{}
	claims = map[string]*ResourcePoolClaimsItem{}

	// First, check if quota exists in the status
	for ns, cl := range g.Status.Claims {
		if ns != namespace {
			continue
		}

		for _, claim := range cl {
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

// Calculate usage for each namespace
func (g *ResourcePool) GetClaimedByNamespaceClaims() (claims map[string]corev1.ResourceList) {
	claims = map[string]corev1.ResourceList{}

	// First, check if quota exists in the status
	for ns, cl := range g.Status.Claims {
		claims[ns] = corev1.ResourceList{}
		nsScope := claims[ns]

		for _, claim := range cl {
			for resourceName, claimed := range claim.Claims {
				usedValue, usedExists := nsScope[resourceName]
				if !usedExists {
					usedValue = resource.MustParse("0")
				}

				usedValue.Add(claimed)
				nsScope[resourceName] = usedValue
			}
		}
	}

	return
}
