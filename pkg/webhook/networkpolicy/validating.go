// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package networkpolicy

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	capsuleutils "github.com/clastix/capsule/pkg/utils"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type handler struct{}

func Handler() capsulewebhook.Handler {
	return &handler{}
}

func (r *handler) OnCreate(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *handler) generic(ctx context.Context, req admission.Request, client client.Client, _ *admission.Decoder) (*capsulev1beta2.Tenant, error) {
	var err error

	np := &networkingv1.NetworkPolicy{}
	if err = client.Get(ctx, types.NamespacedName{Namespace: req.AdmissionRequest.Namespace, Name: req.AdmissionRequest.Name}, np); err != nil {
		return nil, err
	}

	tnt := &capsulev1beta2.Tenant{}

	l, _ := capsuleutils.GetTypeLabel(&capsulev1beta2.Tenant{})
	if v, ok := np.GetLabels()[l]; ok {
		if err = client.Get(ctx, types.NamespacedName{Name: v}, tnt); err != nil {
			return nil, err
		}

		return tnt, nil
	}

	return nil, nil //nolint:nilnil
}

//nolint:dupl
func (r *handler) OnDelete(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, err := r.generic(ctx, req, client, decoder)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tnt != nil {
			recorder.Eventf(tnt, corev1.EventTypeWarning, "NetworkPolicyDeletion", "NetworkPolicy %s/%s cannot be deleted", req.Namespace, req.Name)

			response := admission.Denied("Capsule Network Policies cannot be deleted: please, reach out to the system administrators")

			return &response
		}

		return nil
	}
}

//nolint:dupl
func (r *handler) OnUpdate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		tnt, err := r.generic(ctx, req, client, decoder)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tnt != nil {
			recorder.Eventf(tnt, corev1.EventTypeWarning, "NetworkPolicyUpdate", "NetworkPolicy %s/%s cannot be updated", req.Namespace, req.Name)

			response := admission.Denied("Capsule Network Policies cannot be updated: please, reach out to the system administrators")

			return &response
		}

		return nil
	}
}
