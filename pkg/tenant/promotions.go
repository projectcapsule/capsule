// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
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

	promoReq, err := labels.NewRequirement(meta.ServiceAccountPromotionLabel, selection.Equals, []string{meta.ValueTrue})
	if err != nil {
		return nil, err
	}

	staticSel := labels.NewSelector().Add(*promoReq)

	namespaces, err := tnt.GetNamespaceObjects(ctx, c)
	if err != nil {
		return nil, err
	}

	for _, ns := range namespaces {
		for _, ruleset := range tnt.Spec.Rules {
			var ruleSelector labels.Selector

			if ruleset.NamespaceSelector != nil {
				ruleSelector, err = metav1.LabelSelectorAsSelector(ruleset.NamespaceSelector)
				if err != nil {
					return nil, fmt.Errorf("invalid promotion targetSelector for tenant %s: %w", tnt.Name, err)
				}

				if !ruleSelector.Matches(labels.Set(ns.Labels)) {
					continue
				}
			}

			for _, promotion := range ruleset.Permissions.Promotions {
				var targetNamespaces []string

				for _, tenantNamespace := range namespaces {
					if ruleset.NamespaceSelector != nil {
						if !ruleSelector.Matches(labels.Set(tenantNamespace.Labels)) {
							continue
						}
					}

					targetNamespaces = append(targetNamespaces, tenantNamespace.GetName())
				}

				combinedSel := staticSel

				if promotion.Selector != nil {
					ruleSel, err := metav1.LabelSelectorAsSelector(promotion.Selector)
					if err != nil {
						return nil, fmt.Errorf("invalid promotion selector for tenant %s: %w", tnt.Name, err)
					}

					combinedSel = selectors.CombineSelectors(staticSel, ruleSel)
				}

				saList := &corev1.ServiceAccountList{}

				if err := c.List(ctx, saList,
					client.InNamespace(ns.GetName()),
					client.MatchingLabelsSelector{Selector: combinedSel},
				); err != nil {
					return nil, err
				}

				for _, sa := range saList.Items {
					promotions.Upsert(rbac.PromotionSpec{
						UserSpec: rbac.UserSpec{
							Kind: rbac.ServiceAccountOwner,
							Name: users.GetServiceAccountFullName(meta.NamespacedRFC1123ObjectReferenceWithNamespace{
								Name:      meta.RFC1123Name(sa.Name),
								Namespace: meta.RFC1123SubdomainName(sa.Namespace),
							}),
						},
						ClusterRoles: promotion.ClusterRoles,
						Targets:      targetNamespaces,
					})
				}

			}
		}
	}

	return promotions, nil
}
