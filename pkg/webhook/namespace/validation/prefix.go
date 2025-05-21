// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/configuration"
	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type prefixHandler struct {
	configuration configuration.Configuration
}

func PrefixHandler(configuration configuration.Configuration) capsulewebhook.Handler {
	return &prefixHandler{
		configuration: configuration,
	}
}

func (r *prefixHandler) OnCreate(clt client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return utils.ErroredResponse(err)
		}

		if exp, _ := r.configuration.ProtectedNamespaceRegexp(); exp != nil {
			if matched := exp.MatchString(ns.GetName()); matched {
				response := admission.Denied(fmt.Sprintf("Creating namespaces with name matching %s regexp is not allowed; please, reach out to the system administrators", exp.String()))

				return &response
			}
		}

		if r.configuration.ForceTenantPrefix() {
			tnt := &capsulev1beta2.Tenant{}

			for _, or := range ns.OwnerReferences {
				if !capsuleutils.IsTenantOwnerReference(or) {
					continue
				}

				// retrieving the selected Tenant
				if err := clt.Get(ctx, types.NamespacedName{Name: or.Name}, tnt); err != nil {
					return utils.ErroredResponse(err)
				}

				// Check for Tenant-level ForceTenantPrefix override
				if tnt.Spec.ForceTenantPrefix != nil && !*tnt.Spec.ForceTenantPrefix {
					return nil
				}

				if e := fmt.Sprintf("%s-%s", tnt.GetName(), ns.GetName()); !strings.HasPrefix(ns.GetName(), fmt.Sprintf("%s-", tnt.GetName())) {
					recorder.Eventf(tnt, corev1.EventTypeWarning, "InvalidTenantPrefix", "Namespace %s does not match the expected prefix for the current Tenant", ns.GetName())

					response := admission.Denied(fmt.Sprintf("The namespace doesn't match the tenant prefix, expected %s", e))

					return &response
				}
			}
		}

		return nil
	}
}

func (r *prefixHandler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *prefixHandler) OnUpdate(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}
