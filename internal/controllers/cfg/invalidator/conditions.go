// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package invalidator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"k8s.io/apiserver/pkg/cel/environment"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

// Reuse the periodic cache lifecycle to retire deleted/replaced expressions,
// retaining expressions shared by tenants or effective namespace profiles.
func (r *CacheInvalidator) rebuildConditionCache(ctx context.Context) error {
	if r.CELCache == nil {
		return nil
	}

	tenants := &capsulev1beta2.TenantList{}
	if err := r.List(ctx, tenants); err != nil {
		return err
	}

	statuses := &capsulev1beta2.RuleStatusList{}
	if err := r.List(ctx, statuses); err != nil {
		return err
	}

	expressions := make(map[string]struct{})
	collect := func(body *rules.NamespaceRuleBodyNamespace) {
		_ = body.VisitConditions(func(_ string, conditions []rules.AdmissionCondition) error {
			for _, condition := range conditions {
				if !strings.Contains(condition.Expression, "{{") {
					expressions[condition.Expression] = struct{}{}
				}
			}

			return nil
		})
	}

	for _, tenant := range tenants.Items {
		for _, rule := range tenant.Spec.Rules {
			if rule != nil {
				collect(rule.NamespaceRuleBodyNamespace)
			}
		}
	}

	for _, status := range statuses.Items {
		for _, body := range status.Spec {
			collect(body)
		}

		for _, body := range status.Status.Rules {
			collect(body)
		}
	}

	r.CELCache.PruneConditions(expressions)

	var errs []error

	for expression := range expressions {
		for _, mode := range []environment.Type{environment.NewExpressions, environment.StoredExpressions} {
			if _, err := r.CELCache.GetOrCompileCondition(expression, mode); err != nil {
				errs = append(errs, fmt.Errorf("rebuild condition cache: %w", err))
			}
		}
	}

	return errors.Join(errs...)
}
