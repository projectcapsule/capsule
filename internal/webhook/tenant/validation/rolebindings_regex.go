// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
)

type rbRegexHandler struct{}

func RoleBindingRegexHandler() handlers.TypedHandler[*capsulev1beta2.Tenant] {
	return &rbRegexHandler{}
}

func (h *rbRegexHandler) OnCreate(
	_ client.Client,
	_ client.Reader,
	tnt *capsulev1beta2.Tenant,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.validate(tnt, decoder)
	}
}

func (h *rbRegexHandler) OnDelete(
	client.Client,
	client.Reader,
	*capsulev1beta2.Tenant,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *rbRegexHandler) OnUpdate(
	_ client.Client,
	_ client.Reader,
	tnt *capsulev1beta2.Tenant,
	old *capsulev1beta2.Tenant,
	decoder admission.Decoder,
	_ events.EventRecorder,
) handlers.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.validate(tnt, decoder)
	}
}

func (h *rbRegexHandler) validate(tnt *capsulev1beta2.Tenant, decoder admission.Decoder) *admission.Response {
	bindings := append([]rbac.AdditionalRoleBindingsSpec(nil), tnt.Spec.AdditionalRoleBindings...)

	for _, rule := range tnt.Spec.Rules {
		if rule != nil {
			bindings = append(bindings, rule.Permissions.Bindings...)
		}
	}

	if len(bindings) > 0 {
		for _, binding := range bindings {
			for _, subject := range binding.Subjects {
				if subject.Kind == rbacv1.ServiceAccountKind {
					err := validation.IsDNS1123Subdomain(subject.Name)
					if len(err) > 0 {
						return ad.Denyf("Subject Name '%v' for binding '%v' is invalid. %v", subject.Name, binding.ClusterRoleName, strings.Join(err, ", "))
					}
				}
			}
		}
	}

	return nil
}
