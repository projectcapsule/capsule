// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/users"
)

func FilterNamespaceRulesByAudience(
	cfg configuration.Configuration,
	tnt *capsulev1beta2.Tenant,
	req admission.Request,
	bodies []*rules.NamespaceRuleBodyNamespace,
) ([]*rules.NamespaceRuleBodyNamespace, error) {
	out := make([]*rules.NamespaceRuleBodyNamespace, 0, len(bodies))
	for _, body := range bodies {
		if body == nil || body.Enforce == nil || len(body.Enforce.Audience) == 0 {
			out = append(out, body)
			continue
		}
		matched, err := matchesAudience(cfg, tnt, req, body.Enforce.Audience)
		if err != nil {
			return nil, err
		}
		if matched {
			out = append(out, body)
		}
	}
	return out, nil
}

func matchesAudience(cfg configuration.Configuration, tnt *capsulev1beta2.Tenant, req admission.Request, audience []rules.Audience) (bool, error) {
	for _, subject := range audience {
		switch subject.Kind {
		case rules.AudienceKindUser:
			if req.UserInfo.Username == subject.Name {
				return true, nil
			}
		case rules.AudienceKindGroup:
			for _, group := range req.UserInfo.Groups {
				if group == subject.Name {
					return true, nil
				}
			}
		case rules.AudienceKindServiceAccount:
			if (rbac.UserListSpec{{Kind: rbac.ServiceAccountOwner, Name: subject.Name}}).IsPresent(req.UserInfo.Username, req.UserInfo.Groups) {
				return true, nil
			}
		case rules.AudienceKindCustom:
			matched, err := matchesCustomAudience(cfg, tnt, req, rules.CustomAudience(subject.Name))
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

func matchesCustomAudience(cfg configuration.Configuration, tnt *capsulev1beta2.Tenant, req admission.Request, custom rules.CustomAudience) (bool, error) {
	switch custom {
	case rules.CustomAudienceCapsuleUser:
		return cfg.Users().IsPresent(req.UserInfo.Username, req.UserInfo.Groups), nil
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
