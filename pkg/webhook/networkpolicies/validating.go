// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package networkpolicies

import (
	"context"
	"net/http"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/clastix/capsule/api/v1alpha1"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
)

// +kubebuilder:webhook:path=/validating-v1-network-policy,mutating=false,sideEffects=None,admissionReviewVersions=v1,failurePolicy=fail,groups=networking.k8s.io,resources=networkpolicies,verbs=create;update;delete,versions=v1,name=validating.network-policy.capsule.clastix.io

type webhook struct {
	handler capsulewebhook.Handler
}

func Webhook(handler capsulewebhook.Handler) capsulewebhook.Webhook {
	return &webhook{handler: handler}
}

func (w *webhook) GetHandler() capsulewebhook.Handler {
	return w.handler
}

func (w *webhook) GetName() string {
	return "NetworkPolicy"
}

func (w *webhook) GetPath() string {
	return "/validating-v1-network-policy"
}

type handler struct {
}

func Handler() capsulewebhook.Handler {
	return &handler{}
}

func (r *handler) OnCreate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		return admission.Allowed("")
	}
}

func (r *handler) generic(ctx context.Context, req admission.Request, client client.Client, _ *admission.Decoder) (bool, error) {
	var err error
	np := &networkingv1.NetworkPolicy{}
	err = client.Get(ctx, types.NamespacedName{Namespace: req.AdmissionRequest.Namespace, Name: req.AdmissionRequest.Name}, np)
	if err != nil {
		return false, err
	}

	return r.isCapsuleNetworkPolicy(np), nil
}

func (r *handler) OnDelete(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		ok, err := r.generic(ctx, req, client, decoder)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
		if ok {
			return admission.Denied("Capsule Network Policies cannot be deleted: please, reach out to the system administrators")
		}

		return admission.Allowed("")
	}
}

func (r *handler) isCapsuleNetworkPolicy(np *networkingv1.NetworkPolicy) (ok bool) {
	l, _ := v1alpha1.GetTypeLabel(&v1alpha1.Tenant{})
	_, ok = np.GetLabels()[l]
	return
}

func (r *handler) OnUpdate(client client.Client, decoder *admission.Decoder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) admission.Response {
		ok, err := r.generic(ctx, req, client, decoder)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
		if ok {
			return admission.Denied("Capsule Network Policies cannot be updated: please, reach out to the system administrators")
		}

		return admission.Allowed("")
	}
}
