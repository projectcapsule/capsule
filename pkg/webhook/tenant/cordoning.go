// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/configuration"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type cordoningHandler struct {
	configuration configuration.Configuration
}

func CordoningHandler(configuration configuration.Configuration) capsulewebhook.Handler {
	return &cordoningHandler{
		configuration: configuration,
	}
}

func (h *cordoningHandler) cordonHandler(ctx context.Context, clt client.Client, req admission.Request, recorder record.EventRecorder) *admission.Response {
	tntList := &capsulev1beta2.TenantList{}

	if err := clt.List(ctx, tntList, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", req.Namespace),
	}); err != nil {
		return utils.ErroredResponse(err)
	}
	// resource is not inside a Tenant namespace
	if len(tntList.Items) == 0 {
		return nil
	}

	tnt := tntList.Items[0]
	if tnt.Spec.Cordoned && utils.IsCapsuleUser(ctx, req, clt, h.configuration.UserGroups()) {
		recorder.Eventf(&tnt, corev1.EventTypeWarning, "TenantFreezed", "%s %s/%s cannot be %sd, current Tenant is freezed", req.Kind.String(), req.Namespace, req.Name, strings.ToLower(string(req.Operation)))

		response := admission.Denied(fmt.Sprintf("tenant %s is freezed: please, reach out to the system administrator", tnt.GetName()))

		return &response
	}

	return nil
}

func (h *cordoningHandler) OnCreate(client client.Client, _ *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.cordonHandler(ctx, client, req, recorder)
	}
}

func (h *cordoningHandler) OnDelete(client client.Client, _ *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.cordonHandler(ctx, client, req, recorder)
	}
}

func (h *cordoningHandler) OnUpdate(client client.Client, _ *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.cordonHandler(ctx, client, req, recorder)
	}
}
