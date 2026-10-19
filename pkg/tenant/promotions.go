// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
	"github.com/projectcapsule/capsule/pkg/users"
)

func CollectPromotions(
	ctx context.Context,
	c client.Client,
	tnt *capsulev1beta2.Tenant,
	cfg configuration.Configuration,
) (promotions rbac.PromotionStatusListSpec, err error) {
	if len(tnt.Status.Namespaces) == 0 {
		return nil, nil
	}

	promotions = rbac.PromotionStatusListSpec{}
	if !slices.ContainsFunc(tnt.Spec.Rules, func(rule *rules.NamespaceRuleBodyTenant) bool {
		return rule != nil && len(rule.Permissions.Promotions) > 0
	}) {
		return promotions, nil
	}

	promoReq, err := labels.NewRequirement(meta.ServiceAccountPromotionLabel, selection.Equals, []string{meta.ValueTrue})
	if err != nil {
		return nil, err
	}

	staticSel := labels.NewSelector().Add(*promoReq)

	namespaces, err := promotionNamespaceObjects(ctx, c, tnt)
	if err != nil {
		return nil, err
	}

	for _, ruleset := range tnt.Spec.Rules {
		if ruleset == nil || len(ruleset.Permissions.Promotions) == 0 {
			continue
		}

		var namespaceSelector labels.Selector

		if ruleset.NamespaceSelector != nil {
			namespaceSelector, err = metav1.LabelSelectorAsSelector(ruleset.NamespaceSelector)
			if err != nil {
				return nil, fmt.Errorf("invalid promotion namespaceSelector for tenant %s: %w", tnt.Name, err)
			}
		}

		var (
			matchingNamespaces []corev1.Namespace
			targetNamespaces   []string
		)

		for _, ns := range namespaces {
			if namespaceSelector != nil && !namespaceSelector.Matches(labels.Set(ns.Labels)) {
				continue
			}

			matchingNamespaces = append(matchingNamespaces, ns)
			targetNamespaces = append(targetNamespaces, ns.GetName())
		}

		if len(matchingNamespaces) == 0 {
			continue
		}

		for _, promotion := range ruleset.Permissions.Promotions {
			if promotion == nil {
				continue
			}

			combinedSel := staticSel

			if promotion.Selector != nil {
				ruleSel, err := metav1.LabelSelectorAsSelector(promotion.Selector)
				if err != nil {
					return nil, fmt.Errorf("invalid promotion selector for tenant %s: %w", tnt.Name, err)
				}

				combinedSel = selectors.CombineSelectors(staticSel, ruleSel)
			}

			for _, ns := range matchingNamespaces {
				saList := &corev1.ServiceAccountList{}

				if err := c.List(ctx, saList,
					client.InNamespace(ns.GetName()),
					client.MatchingLabelsSelector{Selector: combinedSel},
				); err != nil {
					return nil, err
				}

				for _, sa := range saList.Items {
					targets := appendTargetNamespace(targetNamespaces, sa.Namespace)

					promotions.Upsert(rbac.PromotionSpec{
						Kind: rbac.ServiceAccountOwner,
						Name: users.GetServiceAccountFullName(meta.NamespacedRFC1123ObjectReferenceWithNamespace{
							Name:      meta.RFC1123Name(sa.Name),
							Namespace: meta.RFC1123SubdomainName(sa.Namespace),
						}),
						ClusterRoles: promotion.ClusterRoles,
						Targets:      targets,
					})
				}
			}
		}
	}

	return promotions, nil
}

func appendTargetNamespace(targets []string, namespace string) []string {
	result := append([]string(nil), targets...)

	if slices.Contains(result, namespace) {
		return result
	}

	return append(result, namespace)
}

func promotionNamespaceObjects(ctx context.Context, c client.Reader, tnt *capsulev1beta2.Tenant) ([]corev1.Namespace, error) {
	list := &corev1.NamespaceList{}
	if err := c.List(ctx, list, client.MatchingFields{NamespaceOwnerReferenceIndexerFieldName: tnt.Name}); err != nil {
		return nil, err
	}

	names := make(map[string]struct{}, len(tnt.Status.Namespaces))
	for _, name := range tnt.Status.Namespaces {
		names[name] = struct{}{}
	}

	list.Items = slices.DeleteFunc(list.Items, func(ns corev1.Namespace) bool {
		if _, ok := names[ns.Name]; !ok {
			return true
		}

		return !slices.ContainsFunc(ns.OwnerReferences, func(ref metav1.OwnerReference) bool { return IsTenantOwnerReferenceForTenant(ref, tnt) })
	})

	return list.Items, nil
}
