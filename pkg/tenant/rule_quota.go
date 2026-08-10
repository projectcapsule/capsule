// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8svalidation "k8s.io/apimachinery/pkg/util/validation"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

func RuleGlobalResourceQuotaName(tnt *capsulev1beta2.Tenant, quotaName string) string {
	return fmt.Sprintf("%s-%s", tnt.Name, quotaName)
}

func ValidateRuleGlobalResourceQuotaName(tnt *capsulev1beta2.Tenant, quotaName string) error {
	name := RuleGlobalResourceQuotaName(tnt, quotaName)
	if errs := k8svalidation.IsDNS1123Subdomain(name); len(errs) > 0 {
		return fmt.Errorf("generated GlobalResourceQuota name %q is invalid: %s", name, strings.Join(errs, "; "))
	}

	return nil
}

func RuleGlobalResourceQuota(
	tnt *capsulev1beta2.Tenant,
	ruleIndex int,
	itemIndex int,
) *capsulev1beta2.GlobalResourceQuota {
	rule := tnt.Spec.Rules[ruleIndex]
	quota := rule.Quota[itemIndex]

	selector := &metav1.LabelSelector{}
	if rule.NamespaceSelector != nil {
		selector = rule.NamespaceSelector.DeepCopy()
	}

	if selector.MatchLabels == nil {
		selector.MatchLabels = map[string]string{}
	}

	// Tenant membership is part of the generated selector so a rule can never
	// consume quota from a namespace owned by another Tenant.
	selector.MatchLabels[meta.TenantLabel] = tnt.Name

	return &capsulev1beta2.GlobalResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name: RuleGlobalResourceQuotaName(tnt, quota.Name),
		},
		Spec: capsulev1beta2.GlobalResourceQuotaSpec{
			NamespaceSelectors: []selectors.NamespaceSelector{{LabelSelector: selector}},
			Quota:              *quota.ResourceQuotaSpec.DeepCopy(),
		},
	}
}
