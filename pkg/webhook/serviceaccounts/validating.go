// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package serviceaccounts

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/meta"
	capsulewebhook "github.com/projectcapsule/capsule/pkg/webhook"
	"github.com/projectcapsule/capsule/pkg/webhook/utils"
)

type handler struct {
	cfg configuration.Configuration
}

func Handler(cfg configuration.Configuration) capsulewebhook.Handler {
	return &handler{cfg: cfg}
}

func (r *handler) OnCreate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.handle(ctx, client, decoder, req, recorder)
	}
}

func (r *handler) OnUpdate(client client.Client, decoder admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.handle(ctx, client, decoder, req, recorder)
	}
}

func (r *handler) OnDelete(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *handler) handle(ctx context.Context, clt client.Client, decoder admission.Decoder, req admission.Request, recorder record.EventRecorder) *admission.Response {
	sa := &corev1.ServiceAccount{}
	if err := decoder.Decode(req, sa); err != nil {
		return utils.ErroredResponse(err)
	}

	tntList := &capsulev1beta2.TenantList{}
	if err := clt.List(ctx, tntList, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", sa.GetNamespace()),
	}); err != nil {
		return utils.ErroredResponse(err)
	}

	if len(tntList.Items) == 0 {
		return nil
	}

	_, hasOwnerPromotion := sa.Labels[meta.OwnerPromotionLabel]
	if !hasOwnerPromotion {
		return nil
	}

	if !r.cfg.AllowServiceAccountPromotion() {
		response := admission.Denied(
			"service account owner promotion is disabled. Contact your system administrators",
		)

		return &response
	}

	// We don't want to allow promoted serviceaccounts to promote other serviceaccounts
	allowed, err := utils.IsTenantOwner(ctx, clt, &tntList.Items[0], req.UserInfo, false)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if allowed {
		return nil
	}

	response := admission.Denied(
		"not permitted to promote serviceaccounts as owners",
	)

	return &response
}
