// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"errors"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/projectcapsule/capsule/pkg/api"
)

func (r *ResourcePool) AssignNamespaces(namespaces []corev1.Namespace) {
	var l []string

	for _, ns := range namespaces {
		if ns.Status.Phase == corev1.NamespaceActive && ns.DeletionTimestamp == nil {
			l = append(l, ns.GetName())
		}
	}

	sort.Strings(l)

	r.Status.NamespaceSize = uint(len(l))
	r.Status.Namespaces = l
}

func (r *ResourcePool) AssignClaims() {
	var size uint

	for _, claims := range r.Status.Claims {
		for range claims {
			size++
		}
	}

	r.Status.ClaimSize = size
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
			UID:  claim.UID,
			Name: api.Name(claim.Name),
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

	r.CalculateClaimedResources()
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

func (r *ResourcePool) CalculateClaimedResources() {
	usage := corev1.ResourceList{}

	for res := range r.Status.Allocation.Hard {
		usage[res] = resource.MustParse("0")
	}

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

	r.Status.Allocation.Claimed = usage

	r.CalculateAvailableResources()
}

func (r *ResourcePool) CalculateAvailableResources() {
	available := corev1.ResourceList{}

	for res, qt := range r.Status.Allocation.Hard {
		amount, exists := r.Status.Allocation.Claimed[res]
		if exists {
			qt.Sub(amount)
		}

		available[res] = qt
	}

	r.Status.Allocation.Available = available
}

func (r *ResourcePool) CanClaimFromPool(claim corev1.ResourceList) []error {
	claimable := r.GetAvailableClaimableResources()
	errs := []error{}

	for resourceName, req := range claim {
		available, exists := claimable[resourceName]
		if !exists || available.IsZero() || available.Cmp(req) < 0 {
			errs = append(errs, errors.New("not enough resources"+string(resourceName)+"available"))
		}
	}

	return errs
}

func (r *ResourcePool) GetAvailableClaimableResources() corev1.ResourceList {
	hard := r.Status.Allocation.Hard.DeepCopy()

	for resourceName, qt := range hard {
		claimed, exists := r.Status.Allocation.Claimed[resourceName]
		if !exists {
			claimed = resource.MustParse("0")
		}

		qt.Sub(claimed)

		hard[resourceName] = qt
	}

	return hard
}

// Gets the Hard specification for the resourcequotas
// This takes into account the default resources being used. However they don't count towards the claim usage
// This can be changed in the future, the default is not calculated as usage because this might interrupt the namespace management
// As we would need to verify if a new namespace with it's defaults still has place in the Pool. Same with attempting to join existing namespaces.
func (r *ResourcePool) GetResourceQuotaHardResources(namespace string) corev1.ResourceList {
	_, claimed := r.GetNamespaceClaims(namespace)

	for resourceName, amount := range claimed {
		if amount.IsZero() {
			delete(claimed, resourceName)
		}
	}

	// Only Consider Default, when enabled
	for resourceName, amount := range r.Spec.Defaults {
		usedValue := claimed[resourceName]
		usedValue.Add(amount)

		claimed[resourceName] = usedValue
	}

	return claimed
}

// Gets the total amount of claimed resources for a namespace.
func (r *ResourcePool) GetNamespaceClaims(namespace string) (claims map[string]*ResourcePoolClaimsItem, claimedResources corev1.ResourceList) {
	claimedResources = corev1.ResourceList{}
	claims = map[string]*ResourcePoolClaimsItem{}

	// First, check if quota exists in the status
	for ns, cl := range r.Status.Claims {
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

	return claims, claimedResources
}

// Calculate usage for each namespace.
func (r *ResourcePool) GetClaimedByNamespaceClaims() (claims map[string]corev1.ResourceList) {
	claims = map[string]corev1.ResourceList{}

	// First, check if quota exists in the status
	for ns, cl := range r.Status.Claims {
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

	return claims
}
