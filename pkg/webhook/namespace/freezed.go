// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package namespace

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/configuration"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type freezedHandler struct {
	configuration configuration.Configuration
}

func FreezeHandler(configuration configuration.Configuration) capsulewebhook.Handler {
	return &freezedHandler{configuration: configuration}
}

func (r *freezedHandler) OnCreate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return utils.ErroredResponse(err)
		}

		for _, objectRef := range ns.ObjectMeta.OwnerReferences {
			// retrieving the selected Tenant
			tnt := &capsulev1beta2.Tenant{}
			if err := client.Get(ctx, types.NamespacedName{Name: objectRef.Name}, tnt); err != nil {
				return utils.ErroredResponse(err)
			}

			if tnt.Spec.Cordoned {
				recorder.Eventf(tnt, corev1.EventTypeWarning, "TenantFreezed", "Namespace %s cannot be attached, the current Tenant is freezed", ns.GetName())

				response := admission.Denied("the selected Tenant is freezed")

				return &response
			}
		}
		// creating NS that is not bounded to any Tenant
		return nil
	}
}

func (r *freezedHandler) OnDelete(c client.Client, _ *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tntList := &capsulev1beta2.TenantList{}
		if err := c.List(ctx, tntList, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".status.namespaces", req.Name),
		}); err != nil {
			return utils.ErroredResponse(err)
		}

		if len(tntList.Items) == 0 {
			return nil
		}

		tnt := tntList.Items[0]

		if tnt.Spec.Cordoned && utils.IsCapsuleUser(ctx, req, c, r.configuration.UserGroups()) {
			recorder.Eventf(&tnt, corev1.EventTypeWarning, "TenantFreezed", "Namespace %s cannot be deleted, the current Tenant is freezed", req.Name)

			response := admission.Denied("the selected Tenant is freezed")

			return &response
		}

		return nil
	}
}

func (r *freezedHandler) OnUpdate(c client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		ns := &corev1.Namespace{}
		if err := decoder.Decode(req, ns); err != nil {
			return utils.ErroredResponse(err)
		}

		tntList := &capsulev1beta2.TenantList{}
		if err := c.List(ctx, tntList, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".status.namespaces", ns.Name),
		}); err != nil {
			return utils.ErroredResponse(err)
		}

		if len(tntList.Items) == 0 {
			return nil
		}

		tnt := tntList.Items[0]

		if tnt.Spec.Cordoned && utils.IsCapsuleUser(ctx, req, c, r.configuration.UserGroups()) {
			recorder.Eventf(&tnt, corev1.EventTypeWarning, "TenantFreezed", "Namespace %s cannot be updated, the current Tenant is freezed", ns.GetName())

			response := admission.Denied("the selected Tenant is freezed")

			return &response
		}

		return nil
	}
}
