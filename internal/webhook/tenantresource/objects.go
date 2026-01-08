// Copyright 2020-2026 Project Capsule Authors
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

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulewebhook "github.com/projectcapsule/capsule/internal/webhook"
	"github.com/projectcapsule/capsule/internal/webhook/utils"
	"github.com/projectcapsule/capsule/pkg/indexer/tenantresource"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
)

type cordoningHandler struct{}

func WriteOpsHandler() capsulewebhook.Handler {
	return &cordoningHandler{}
}

func (h *cordoningHandler) OnCreate(client.Client, admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (h *cordoningHandler) OnDelete(client client.Client, _ admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handler(ctx, client, req, recorder)
	}
}

func (h *cordoningHandler) OnUpdate(client client.Client, _ admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handler(ctx, client, req, recorder)
	}
}

func (h *cordoningHandler) handler(ctx context.Context, clt client.Client, req admission.Request, recorder record.EventRecorder) *admission.Response {
	tnt, err := tenant.TenantByStatusNamespace(ctx, clt, req.Namespace)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tnt == nil {
		return nil
	}

	// Checking if the object is managed by a TenantResource, local or global
	ors := capsulev1beta2.ObjectReferenceStatus{
		ObjectReferenceAbstract: capsulev1beta2.ObjectReferenceAbstract{
			Kind:       req.Kind.Kind,
			Namespace:  req.Namespace,
			APIVersion: req.Kind.Version,
		},
		Name: req.Name,
	}

	global, local := &capsulev1beta2.GlobalTenantResourceList{}, &capsulev1beta2.TenantResourceList{}

	if err := clt.List(ctx, global, client.MatchingFieldsSelector{Selector: fields.OneTermEqualSelector(tenantresource.IndexerFieldName, ors.String())}); err != nil {
		return utils.ErroredResponse(err)
	}

	if err := clt.List(ctx, local, client.MatchingFieldsSelector{Selector: fields.OneTermEqualSelector(tenantresource.IndexerFieldName, ors.String())}); err != nil {
		return utils.ErroredResponse(err)
	}

	if len(local.Items) > 0 || len(global.Items) > 0 {
		recorder.Eventf(tnt, corev1.EventTypeWarning, "TenantResourceWriteOp", "%s %s/%s cannot be %sd, resource is managed by the Tenant", req.Kind.String(), req.Namespace, req.Name, strings.ToLower(string(req.Operation)))

		response := admission.Denied(fmt.Sprintf("resource %s is managed at the Tenant level", req.Name))

		return &response
	}

	return nil
}
