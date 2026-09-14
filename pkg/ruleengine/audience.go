// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"context"
	"fmt"
	"slices"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/users"
)

func FilterNamespaceRulesByAudience(
	ctx context.Context,
	c client.Client,
	cfg configuration.Configuration,
	tnt *capsulev1beta2.Tenant,
	req admission.Request,
	bodies []*rules.NamespaceRuleBodyNamespace,
) ([]*rules.NamespaceRuleBodyNamespace, error) {
	var out []*rules.NamespaceRuleBodyNamespace

	for i, body := range bodies {
		if body == nil || len(body.Audience) == 0 {
			if out != nil {
				out = append(out, body)
			}

			continue
		}

		if out == nil {
			out = make([]*rules.NamespaceRuleBodyNamespace, 0, len(bodies))
			out = append(out, bodies[:i]...)
		}

		matched, err := matchesAudience(ctx, c, cfg, tnt, req, body.Audience)
		if err != nil {
			return nil, err
		}

		if matched {
			out = append(out, body)
		}
	}

	if out == nil {
		return bodies, nil
	}

	return out, nil
}

func matchesAudience(ctx context.Context, c client.Client, cfg configuration.Configuration, tnt *capsulev1beta2.Tenant, req admission.Request, audience []rules.Audience) (bool, error) {
	for _, subject := range audience {
		switch subject.Kind {
		case rules.AudienceKindUser:
			if req.UserInfo.Username == subject.Name {
				return true, nil
			}
		case rules.AudienceKindGroup:
			if slices.Contains(req.UserInfo.Groups, subject.Name) {
				return true, nil
			}
		case rules.AudienceKindServiceAccount:
			if (rbac.UserListSpec{{Kind: rbac.ServiceAccountOwner, Name: subject.Name}}).IsPresent(req.UserInfo.Username, req.UserInfo.Groups) {
				return true, nil
			}
		case rules.AudienceKindCustom:
			if cfg == nil {
				return false, fmt.Errorf("configuration is required for custom audience %q", subject.Name)
			}

			matched, err := matchesCustomAudience(ctx, c, cfg, tnt, req, rules.CustomAudience(subject.Name))
			if err != nil {
				return false, err
			}

			if matched {
				return true, nil
			}
		default:
			return false, fmt.Errorf("unsupported audience kind %q", subject.Kind)
		}
	}

	return false, nil
}

func matchesCustomAudience(ctx context.Context, c client.Client, cfg configuration.Configuration, tnt *capsulev1beta2.Tenant, req admission.Request, custom rules.CustomAudience) (bool, error) {
	switch custom {
	case rules.CustomAudienceCapsuleUser:
		// Use the same membership check as admission, including aggregated users
		// and all service accounts in tenant namespaces, promoted or otherwise.
		return users.IsCapsuleUser(ctx, c, cfg, req.UserInfo.Username, req.UserInfo.Groups), nil
	case rules.CustomAudienceAdministrator:
		return cfg.Administrators().IsPresent(req.UserInfo.Username, req.UserInfo.Groups), nil
	case rules.CustomAudienceTenantOwner:
		if tnt == nil {
			return false, nil
		}

		return tnt.Spec.Owners.IsOwner(req.UserInfo.Username, req.UserInfo.Groups) ||
			tnt.Status.Owners.IsOwner(req.UserInfo.Username, req.UserInfo.Groups), nil
	case rules.CustomAudienceController:
		return users.IsControllerServiceAccount(req.UserInfo.Username), nil
	default:
		return false, fmt.Errorf("unsupported custom audience %q", custom)
	}
}
