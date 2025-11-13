// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
)

type prefixHandler struct {
	cfg configuration.Configuration
}

func PrefixHandler(configuration configuration.Configuration) capsulewebhook.Handler {
	return &prefixHandler{
		cfg: configuration,
	}
}

func (r *prefixHandler) OnCreate(clt client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return utils.ErroredResponse(err)
		}

		tnt, err := tenant.GetTenantByOwnerreferences(ctx, clt, ns.OwnerReferences)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tnt == nil {
			return nil
		}

		return HandlePrefix(
			ctx,
			clt,
			r.cfg,
			ns,
			tnt,
			recorder,
		)
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

func HandlePrefix(
	ctx context.Context,
	c client.Client,
	cfg configuration.Configuration,
	ns *corev1.Namespace,
	tnt *capsulev1beta2.Tenant,
	recorder record.EventRecorder,
) *admission.Response {
	if exp, _ := cfg.ProtectedNamespaceRegexp(); exp != nil {
		if matched := exp.MatchString(ns.GetName()); matched {
			response := admission.Denied(fmt.Sprintf("Creating namespaces with name matching %s regexp is not allowed; please, reach out to the system administrators", exp.String()))

			return &response
		}
	}

	if cfg.ForceTenantPrefix() {

		if tnt.Spec.ForceTenantPrefix != nil && !*tnt.Spec.ForceTenantPrefix {
			return nil
		}

		if e := fmt.Sprintf("%s-%s", tnt.GetName(), ns.GetName()); !strings.HasPrefix(ns.GetName(), fmt.Sprintf("%s-", tnt.GetName())) {
			recorder.Eventf(tnt, corev1.EventTypeWarning, "InvalidTenantPrefix", "Namespace %s does not match the expected prefix for the current Tenant", ns.GetName())

			response := admission.Denied(fmt.Sprintf("The namespace doesn't match the tenant prefix, expected %s", e))

			return &response
		}
	}

	return nil
}
