// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type rbRegexHandler struct{}

func RoleBindingRegexHandler() capsulewebhook.Handler {
	return &rbRegexHandler{}
}

func (h *rbRegexHandler) validate(req admission.Request, decoder *admission.Decoder) *admission.Response {
	tenant := &capsulev1beta2.Tenant{}
	if err := decoder.Decode(req, tenant); err != nil {
		return utils.ErroredResponse(err)
	}

	if len(tenant.Spec.AdditionalRoleBindings) > 0 {
		for _, binding := range tenant.Spec.AdditionalRoleBindings {
			for _, subject := range binding.Subjects {
				if subject.Kind == rbacv1.ServiceAccountKind {
					err := validation.IsDNS1123Subdomain(subject.Name)
					if len(err) > 0 {
						response := admission.Denied(fmt.Sprintf("Subject Name '%v' for binding '%v' is invalid. %v", subject.Name, binding.ClusterRoleName, strings.Join(err, ", ")))

						return &response
					}
				}
			}
		}
	}

	return nil
}

func (h *rbRegexHandler) OnCreate(_ client.Client, decoder *admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.validate(req, decoder)
	}
}

func (h *rbRegexHandler) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *rbRegexHandler) OnUpdate(_ client.Client, decoder *admission.Decoder, _ record.EventRecorder) capsulewebhook.Func {
	return func(_ context.Context, req admission.Request) *admission.Response {
		return h.validate(req, decoder)
	}
}
