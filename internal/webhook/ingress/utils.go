// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"context"
	"fmt"

	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

func TenantFromIngress(ctx context.Context, c client.Client, ingress Ingress) (*capsulev1beta2.Tenant, error) {
	return tenant.TenantByStatusNamespace(ctx, c, ingress.Namespace())
}

func FromRequest(req admission.Request, decoder admission.Decoder) (ingress Ingress, err error) {
	switch req.Kind.Group {
	case "networking.k8s.io":
		if req.Kind.Version == "v1" {
			ingressObj := &networkingv1.Ingress{}
			if err = decoder.Decode(req, ingressObj); err != nil {
				return ingress, err
			}

			ingress = NetworkingV1{Ingress: ingressObj}

			break
		}

		ingressObj := &networkingv1beta1.Ingress{}
		if err = decoder.Decode(req, ingressObj); err != nil {
			return ingress, err
		}

		ingress = NetworkingV1Beta1{Ingress: ingressObj}
	case "extensions":
		ingressObj := &extensionsv1beta1.Ingress{}
		if err = decoder.Decode(req, ingressObj); err != nil {
			return ingress, err
		}

		ingress = Extension{Ingress: ingressObj}
	default:
		err = fmt.Errorf("cannot recognize type %s", req.Kind.Group)
	}

	return ingress, err
}
