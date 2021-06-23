// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/pkg/configuration"
	"github.com/clastix/capsule/pkg/utils"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/tenant-cordoning,mutating=false,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups="*",resources="*",verbs=create;update;delete,versions="*",name=cordoning.tenant.capsule.clastix.io

type cordoning struct {
	handler capsulewebhook.Handler
}

func Cordoning(handler capsulewebhook.Handler) capsulewebhook.Webhook {
	return &cordoning{handler: handler}
}

func (w cordoning) GetName() string {
	return "TenantCordoning"
}

func (w cordoning) GetPath() string {
	return "/tenant-cordoning"
}

func (w cordoning) GetHandler() capsulewebhook.Handler {
	return w.handler
}

type cordoningHandler struct {
	configuration configuration.Configuration
}

func CordoningHandler() capsulewebhook.Handler {
	return &cordoningHandler{}
}

func (h *cordoningHandler) requestFromOwnerOrSA(tenant capsulev1alpha1.Tenant, req admission.Request) bool {
	switch {
	case tenant.Spec.Owner.Kind == "User" && req.UserInfo.Username == tenant.Spec.Owner.Name:
		return true
	case tenant.Spec.Owner.Kind == "Group":
		groupList := utils.NewUserGroupList(req.UserInfo.Groups)
		for _, group := range h.configuration.UserGroups() {
			if groupList.Find(group) {
				return true
			}
		}
	default:
		for _, group := range req.UserInfo.Groups {
			if len(req.Namespace) > 0 && strings.HasPrefix(group, "system:serviceaccounts:"+req.Namespace) {
				return true
			}
		}
	}
	return false
}

func (h *cordoningHandler) cordonHandler(ctx context.Context, clt client.Client, req admission.Request, recorder record.EventRecorder) admission.Response {
	tntList := &capsulev1alpha1.TenantList{}

	if err := clt.List(ctx, tntList, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", req.Namespace),
	}); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	// resource is not inside a Tenant namespace
	if len(tntList.Items) == 0 {
		return admission.Allowed("")
	}

	tnt := tntList.Items[0]

	if tnt.IsCordoned() {
		if h.requestFromOwnerOrSA(tnt, req) {
			recorder.Eventf(&tnt, corev1.EventTypeWarning, "TenantFreezed", "%s %s/%s cannot be %sd, current Tenant is freezed", req.Kind.String(), req.Namespace, req.Name, strings.ToLower(string(req.Operation)))

			return admission.Denied(fmt.Sprintf("tenant %s is freezed: please, reach out to the system administrator", tnt.GetName()))
		}
	}

	return admission.Allowed("")
}

func (h *cordoningHandler) OnCreate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return h.cordonHandler(ctx, client, req, recorder)
	}
}

func (h *cordoningHandler) OnDelete(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return h.cordonHandler(ctx, client, req, recorder)
	}
}

func (h *cordoningHandler) OnUpdate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return h.cordonHandler(ctx, client, req, recorder)
	}
}
