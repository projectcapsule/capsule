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
	"github.com/clastix/capsule/pkg/indexer/tenantresource"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type cordoningHandler struct{}

func WriteOpsHandler() capsulewebhook.Handler {
	return &cordoningHandler{}
}

func (h *cordoningHandler) handler(ctx context.Context, clt client.Client, req admission.Request, recorder record.EventRecorder) *admission.Response {
	tntList := &capsulev1beta2.TenantList{}

	if err := clt.List(ctx, tntList, client.MatchingFieldsSelector{Selector: fields.OneTermEqualSelector(".status.namespaces", req.Namespace)}); err != nil {
		return utils.ErroredResponse(err)
	}
	// resource is not inside a Tenant namespace:
	// we can avoid any kind of extra check.
	if len(tntList.Items) == 0 {
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
		tnt := tntList.Items[0]

		recorder.Eventf(&tnt, corev1.EventTypeWarning, "TenantResourceWriteOp", "%s %s/%s cannot be %sd, resource is managed by the Tenant", req.Kind.String(), req.Namespace, req.Name, strings.ToLower(string(req.Operation)))

		response := admission.Denied(fmt.Sprintf("resource %s is managed at the Tenant level", req.Name))

		return &response
	}

	return nil
}

func (h *cordoningHandler) OnCreate(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (h *cordoningHandler) OnDelete(client client.Client, _ *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handler(ctx, client, req, recorder)
	}
}

func (h *cordoningHandler) OnUpdate(client client.Client, _ *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return h.handler(ctx, client, req, recorder)
	}
}
