// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	k8svalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	tenantutils "github.com/projectcapsule/capsule/pkg/tenant"
)

func (r *Manager) syncGlobalResourceQuotas(
	ctx context.Context,
	tnt *capsulev1beta2.Tenant,
) error {
	desired := make(map[string]struct{})
	quotaNames := make(map[string]struct{})

	for ruleIndex, rule := range tnt.Spec.Rules {
		if rule == nil || rule.NamespaceRuleBodyNamespace == nil {
			continue
		}

		for itemIndex := range rule.Quota {
			quotaName := rule.Quota[itemIndex].Name
			if errs := k8svalidation.IsDNS1123Label(quotaName); len(errs) > 0 {
				return fmt.Errorf(
					"rules[%d].quota[%d].name %q is invalid: %s",
					ruleIndex,
					itemIndex,
					quotaName,
					strings.Join(errs, "; "),
				)
			}

			if _, duplicate := quotaNames[quotaName]; duplicate {
				return fmt.Errorf("rules[%d].quota[%d].name %q is duplicated", ruleIndex, itemIndex, quotaName)
			}

			quotaNames[quotaName] = struct{}{}

			if err := tenantutils.ValidateRuleGlobalResourceQuotaName(tnt, quotaName); err != nil {
				return fmt.Errorf("rules[%d].quota[%d]: %w", ruleIndex, itemIndex, err)
			}

			target := tenantutils.RuleGlobalResourceQuota(tnt, ruleIndex, itemIndex)
			desired[target.Name] = struct{}{}

			if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				_, err := controllerutil.CreateOrUpdate(ctx, r.Client, target, func() error {
					currentLabels := target.GetLabels()
					if currentLabels == nil {
						currentLabels = map[string]string{}
					}

					currentLabels[meta.NewManagedByCapsuleLabel] = meta.ValueController
					currentLabels[meta.NewTenantLabel] = tnt.Name
					currentLabels[meta.RuleQuotaLabel] = quotaName
					target.SetLabels(currentLabels)

					desiredQuota := tenantutils.RuleGlobalResourceQuota(tnt, ruleIndex, itemIndex)
					target.Spec = *desiredQuota.Spec.DeepCopy()

					return controllerutil.SetControllerReference(tnt, target, r.Scheme())
				})

				return err
			}); err != nil {
				return fmt.Errorf("sync GlobalResourceQuota %s: %w", target.Name, err)
			}
		}
	}

	return r.pruneGlobalResourceQuotas(ctx, tnt, desired)
}

func (r *Manager) pruneGlobalResourceQuotas(
	ctx context.Context,
	tnt *capsulev1beta2.Tenant,
	desired map[string]struct{},
) error {
	list := &capsulev1beta2.GlobalResourceQuotaList{}

	selector := labels.SelectorFromSet(labels.Set{
		meta.NewManagedByCapsuleLabel: meta.ValueController,
		meta.NewTenantLabel:           tnt.Name,
	})

	reader := client.Reader(r.Client)
	if r.reader != nil {
		reader = r.reader
	}

	if err := reader.List(ctx, list, &client.ListOptions{LabelSelector: selector}); err != nil {
		return err
	}

	for i := range list.Items {
		item := &list.Items[i]
		if _, generated := item.Labels[meta.RuleQuotaLabel]; !generated {
			continue
		}

		if _, keep := desired[item.Name]; keep {
			continue
		}

		if err := r.Delete(ctx, item); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete stale GlobalResourceQuota %s: %w", item.Name, err)
		}
	}

	return nil
}

func hasRuleGlobalResourceQuotas(tnt *capsulev1beta2.Tenant) bool {
	for _, rule := range tnt.Spec.Rules {
		if rule != nil && rule.NamespaceRuleBodyNamespace != nil && len(rule.Quota) > 0 {
			return true
		}
	}

	return false
}
