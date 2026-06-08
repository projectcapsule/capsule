// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package invalidator

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func (r *CacheInvalidator) rebuildRegexCache(ctx context.Context, log logr.Logger) error {
	ruleStatuses := &capsulev1beta2.RuleStatusList{}
	if err := r.List(ctx, ruleStatuses); err != nil {
		return err
	}

	log.V(5).Info("rebuilding regex cache",
		"regexesBefore", r.RegexCache.Stats(),
		"ruleStatuses", len(ruleStatuses.Items),
	)

	r.RegexCache.Reset()

	expressions := make(map[string]api.RegExpression)

	for i := range ruleStatuses.Items {
		rs := &ruleStatuses.Items[i]

		collectRegexExpressionsFromNamespaceRules(expressions, rs.Spec)
		collectRegexExpressionsFromNamespaceRules(expressions, rs.Status.Rules)
		collectRegexExpressionsFromNamespaceRule(expressions, &rs.Status.Rule)
	}

	for _, expr := range expressions {
		if _, _, err := r.RegexCache.GetOrCompile(expr); err != nil {
			return fmt.Errorf("build regex cache entry %q: %w", expr.Expression, err)
		}
	}

	log.V(5).Info("rebuilt regex cache",
		"uniqueExpressions", len(expressions),
		"regexesAfter", r.RegexCache.Stats(),
	)

	return nil
}

func collectRegexExpressionsFromNamespaceRules(
	set map[string]api.RegExpression,
	r []*rules.NamespaceRuleBodyNamespace,
) {
	for _, rule := range r {
		collectRegexExpressionsFromNamespaceRule(set, rule)
	}
}

func collectRegexExpressionsFromNamespaceRule(
	set map[string]api.RegExpression,
	rule *rules.NamespaceRuleBodyNamespace,
) {
	if rule == nil {
		return
	}

	for _, registry := range rule.Enforce.Registries {
		expr := registry.RegExpression
		if expr.Expression == "" {
			continue
		}

		set[cache.HashRegex(expr)] = expr
	}
}
