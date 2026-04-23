// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"fmt"
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"

	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

func BuildGatingUserCondition(opts WebhookOptions, users rbac.UserListSpec, admins rbac.UserListSpec) []admissionregistrationv1.MatchCondition {
	var parts []string

	if opts.CapsuleUsers {
		parts = append(parts, ServiceAccountGroupGuardExpr())
		parts = append(parts, CelUserOrGroupExpr(users))
	}

	if opts.Administrators {
		parts = append(parts, CelUserOrGroupExpr(admins))
	}

	if len(parts) == 0 {
		return nil
	}

	expr := parts[0]
	if len(parts) == 2 {
		expr = fmt.Sprintf("(%s) || (%s)", parts[0], parts[1])
	}

	return []admissionregistrationv1.MatchCondition{
		{
			Name:       "capsule-user-gate",
			Expression: expr,
		},
	}
}

func ServiceAccountGroupGuardExpr() string {
	return fmt.Sprintf("request.userInfo.groups.exists(g, g == %s)", CelQuote(serviceaccount.AllServiceAccountsGroup))
}

func CelUserOrGroupExpr(l rbac.UserListSpec) string {
	users, groups := l.SplitUsersAndGroups()

	userExpr := "false"
	if len(users) > 0 {
		userExpr = fmt.Sprintf("request.userInfo.username in %s", CelStringList(users))
	}

	groupExpr := "false"
	if len(groups) > 0 {
		groupExpr = fmt.Sprintf("request.userInfo.groups.exists(g, g in %s)", CelStringList(groups))
	}

	return fmt.Sprintf("(%s) || (%s)", userExpr, groupExpr)
}

func CelStringList(items []string) string {
	q := make([]string, 0, len(items))
	for _, it := range items {
		q = append(q, CelQuote(it))
	}

	return "[" + strings.Join(q, ",") + "]"
}

func CelQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)

	return "'" + s + "'"
}
