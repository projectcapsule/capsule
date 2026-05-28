// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquotas

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/go-logr/logr"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/quota"
)

type quotaUsageReconcileInput struct {
	Log logr.Logger

	Client client.Client
	Mapper k8smeta.RESTMapper

	JSONPathCache *cache.JSONPathCache

	Sources        []capsulev1beta2.CustomQuotaSpecSource
	ScopeSelectors []metav1.LabelSelector

	// For namespaced CustomQuota:
	//   []string{instance.Namespace}
	//
	// For GlobalCustomQuota:
	//   resolved namespaces or []string{"*"}
	Namespaces []string

	// Namespaced CustomQuota should only accept namespaced targets.
	RequireNamespacedTargets bool

	// Used for compiled target cache.
	CacheKey     string
	TargetsCache *cache.CompiledTargetsCache[string]
}

type quotaUsageReconcileResult struct {
	Targets []capsulev1beta2.CustomQuotaStatusTarget
	Usage   capsulev1beta2.CustomQuotaStatusUsage
	Claims  []capsulev1beta2.CustomQuotaClaimItem
}

type quotaClaimKey struct {
	UID       types.UID
	Group     string
	Version   string
	Kind      string
	Namespace string
	Name      string
}

func reconcileQuotaUsage(
	ctx context.Context,
	in quotaUsageReconcileInput,
	limit resource.Quantity,
) (quotaUsageReconcileResult, error) {
	out := quotaUsageReconcileResult{
		Targets: []capsulev1beta2.CustomQuotaStatusTarget{},
		Usage:   capsulev1beta2.CustomQuotaStatusUsage{},
		Claims:  nil,
	}

	for _, src := range in.Sources {
		kind := src.GroupVersionKind()

		mapping, err := in.Mapper.RESTMapping(kind.GroupKind(), kind.Version)
		if err != nil {
			return out, fmt.Errorf("failed to resolve REST mapping for %s: %w", kind.String(), err)
		}

		if in.RequireNamespacedTargets && mapping.Scope.Name() != k8smeta.RESTScopeNameNamespace {
			return out, fmt.Errorf("GVK %s is not namespaced", kind.String())
		}

		out.Targets = append(out.Targets, capsulev1beta2.CustomQuotaStatusTarget{
			GroupVersionKind:            metav1.GroupVersionKind(kind),
			CustomQuotaSpecSourceConfig: src.CustomQuotaSpecSourceConfig,
			Scope:                       mapping.Scope.Name(),
		})
	}

	targets, err := CompileTargets(in.JSONPathCache, out.Targets)
	if err != nil {
		return out, err
	}

	if in.TargetsCache != nil && in.CacheKey != "" {
		in.TargetsCache.Set(in.CacheKey, targets)
	}

	var errs []error

	itemsByGVK := make(map[schema.GroupVersionKind][]unstructured.Unstructured, len(out.Targets))
	claimsByKey := make(map[quotaClaimKey]capsulev1beta2.CustomQuotaClaimItem)

	for _, target := range targets {
		gvk := schema.GroupVersionKind{
			Group:   target.Group,
			Version: target.Version,
			Kind:    target.Kind,
		}

		items, ok := itemsByGVK[gvk]
		if !ok {
			items, err = getResourcesByGVK(ctx, gvk, in.Client, in.ScopeSelectors, in.Namespaces...)
			if err != nil {
				errs = append(errs, fmt.Errorf("list resources for %s: %w", gvk.String(), err))

				continue
			}

			itemsByGVK[gvk] = items
		}

		in.Log.V(5).Info("listed resources for target",
			"gvk", gvk.String(),
			"count", len(items),
			"namespaces", in.Namespaces,
			"scopeSelectors", in.ScopeSelectors,
		)

		for _, item := range items {
			matches, err := MatchesCompiledSelectorsWithFields(item, target.CompiledSelectors)
			if err != nil {
				errs = append(errs, fmt.Errorf(
					"evaluate selectors for %s/%s (%s): %w",
					item.GetNamespace(),
					item.GetName(),
					item.GetObjectKind().GroupVersionKind().String(),
					err,
				))

				continue
			}

			if !matches {
				continue
			}

			rawUsage, err := usageForTarget(item, target)
			if err != nil {
				errs = append(errs, err)

				continue
			}

			accountingUsage := rawUsage.DeepCopy()

			switch target.Operation {
			case quota.OpSub:
				accountingUsage.Neg()
				out.Usage.Used.Add(accountingUsage)
				quota.ClampQuantityToZero(&out.Usage.Used)

			case quota.OpAdd, quota.OpCount:
				out.Usage.Used.Add(accountingUsage)

			default:
				errs = append(errs, fmt.Errorf(
					"unsupported operation %q for %s/%s (%s)",
					target.Operation,
					item.GetNamespace(),
					item.GetName(),
					item.GetObjectKind().GroupVersionKind().String(),
				))

				continue
			}

			key := quotaClaimKey{
				UID:       item.GetUID(),
				Group:     target.Group,
				Version:   target.Version,
				Kind:      target.Kind,
				Namespace: item.GetNamespace(),
				Name:      item.GetName(),
			}

			claim, exists := claimsByKey[key]
			if !exists {
				claim = capsulev1beta2.CustomQuotaClaimItem{
					GroupVersionKind: metav1.GroupVersionKind{
						Group:   target.Group,
						Version: target.Version,
						Kind:    target.Kind,
					},
					NamespacedObjectWithUIDReference: meta.NamespacedObjectWithUIDReference{
						Name:      item.GetName(),
						Namespace: meta.RFC1123SubdomainName(item.GetNamespace()),
						UID:       item.GetUID(),
					},
					Usage: resource.MustParse("0"),
				}
			}

			// Claims must mirror the same net per-object contribution used by admission reservations.
			// This is required so reservationMaterializedLedger(res, claims) can clear reservations.
			//
			// For pure subtraction sources, the claim remains present but clamps to zero.
			// Example:
			//   claim usage = max(0 - 2Gi, 0) = 0
			//
			// For mixed sources, e.g. count + sub cpu:
			//   claim usage = max(1 - 500m, 0) = 500m
			claim.Usage.Add(accountingUsage)
			quota.ClampQuantityToZero(&claim.Usage)

			claimsByKey[key] = claim
		}
	}

	quota.ClampQuantityToZero(&out.Usage.Used)

	out.Usage.Available = limit.DeepCopy()
	out.Usage.Available.Sub(out.Usage.Used)
	quota.ClampQuantityToZero(&out.Usage.Available)

	out.Claims = make([]capsulev1beta2.CustomQuotaClaimItem, 0, len(claimsByKey))
	for _, claim := range claimsByKey {
		out.Claims = append(out.Claims, claim)
	}

	sort.SliceStable(out.Claims, func(i, j int) bool {
		if out.Claims[i].Namespace != out.Claims[j].Namespace {
			return out.Claims[i].Namespace < out.Claims[j].Namespace
		}

		if out.Claims[i].Kind != out.Claims[j].Kind {
			return out.Claims[i].Kind < out.Claims[j].Kind
		}

		return out.Claims[i].Name < out.Claims[j].Name
	})

	if len(errs) > 0 {
		return out, errors.Join(errs...)
	}

	return out, nil
}

func usageForTarget(
	item unstructured.Unstructured,
	target cache.CompiledTarget,
) (resource.Quantity, error) {
	switch target.Operation {
	case quota.OpCount:
		return *resource.NewQuantity(1, resource.DecimalSI), nil

	case quota.OpAdd, quota.OpSub:
		usage, err := quota.ParseQuantityFromUnstructured(item, target.CompiledPath)
		if err != nil {
			return resource.Quantity{}, fmt.Errorf(
				"get usage from %s/%s (%s) path %q op %q: %w",
				item.GetNamespace(),
				item.GetName(),
				item.GetObjectKind().GroupVersionKind().String(),
				target.Path,
				target.Operation,
				err,
			)
		}

		return usage, nil

	default:
		return resource.Quantity{}, fmt.Errorf(
			"unsupported operation %q for %s/%s (%s)",
			target.Operation,
			item.GetNamespace(),
			item.GetName(),
			item.GetObjectKind().GroupVersionKind().String(),
		)
	}
}
