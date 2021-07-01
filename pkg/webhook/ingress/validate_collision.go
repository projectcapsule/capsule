// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"

	"github.com/clastix/capsule/pkg/configuration"
	capsulewebhook "github.com/clastix/capsule/pkg/webhook"
	"github.com/clastix/capsule/pkg/webhook/utils"
)

type collision struct {
	configuration configuration.Configuration
}

func Collision(configuration configuration.Configuration) capsulewebhook.Handler {
	return &collision{configuration: configuration}
}

func (r *collision) OnCreate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		ingress, err := ingressFromRequest(req, decoder)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		var tenant *capsulev1beta1.Tenant

		tenant, err = tenantFromIngress(ctx, client, ingress)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tenant == nil {
			return nil
		}

		if err = r.validateCollision(ctx, client, ingress); err == nil {
			return nil
		}

		var collisionErr *ingressHostnameCollision

		if errors.As(err, &collisionErr) {
			recorder.Eventf(tenant, corev1.EventTypeWarning, "IngressHostnameCollision", "Ingress %s/%s hostname is colliding", ingress.Namespace(), ingress.Name())
		}

		response := admission.Denied(err.Error())

		return &response
	}
}

func (r *collision) OnUpdate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		ingress, err := ingressFromRequest(req, decoder)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		var tenant *capsulev1beta1.Tenant

		tenant, err = tenantFromIngress(ctx, client, ingress)
		if err != nil {
			return utils.ErroredResponse(err)
		}

		if tenant == nil {
			return nil
		}

		err = r.validateCollision(ctx, client, ingress)

		var collisionErr *ingressHostnameCollision

		if errors.As(err, &collisionErr) {
			recorder.Eventf(tenant, corev1.EventTypeWarning, "IngressHostnameCollision", "Ingress %s/%s hostname is colliding", ingress.Namespace(), ingress.Name())
		}

		response := admission.Denied(err.Error())

		return &response
	}
}

func (r *collision) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (r *collision) validateCollision(ctx context.Context, clt client.Client, ingress Ingress) error {
	if r.configuration.AllowIngressHostnameCollision() {
		return nil
	}

	for _, hostname := range ingress.Hostnames() {
		switch ingress.(type) {
		case Extension:
			ingressObjList := &extensionsv1beta1.IngressList{}
			err := clt.List(ctx, ingressObjList, client.MatchingFieldsSelector{
				Selector: fields.OneTermEqualSelector(".spec.rules[*].host", hostname),
			})
			if err != nil {
				return err
			}

			switch len(ingressObjList.Items) {
			case 0:
				break
			case 1:
				if ingressObj := ingressObjList.Items[0]; ingressObj.GetName() == ingress.Name() && ingressObj.GetNamespace() == ingress.Namespace() {
					break
				}
				fallthrough
			default:
				return NewIngressHostnameCollision(hostname)
			}
		case NetworkingV1:
			ingressObjList := &networkingv1.IngressList{}
			err := clt.List(ctx, ingressObjList, client.MatchingFieldsSelector{
				Selector: fields.OneTermEqualSelector(".spec.rules[*].host", hostname),
			})
			if err != nil {
				return errors.Wrap(err, "cannot list *networkingv1.IngressList by MatchingFieldsSelector")
			}

			switch len(ingressObjList.Items) {
			case 0:
				break
			case 1:
				if ingressObj := ingressObjList.Items[0]; ingressObj.GetName() == ingress.Name() && ingressObj.GetNamespace() == ingress.Namespace() {
					break
				}
				fallthrough
			default:
				return NewIngressHostnameCollision(hostname)
			}
		case NetworkingV1Beta1:
			ingressObjList := &networkingv1beta1.IngressList{}
			err := clt.List(ctx, ingressObjList, client.MatchingFieldsSelector{
				Selector: fields.OneTermEqualSelector(".spec.rules[*].host", hostname),
			})
			if err != nil {
				return errors.Wrap(err, "cannot list *networkingv1beta1.IngressList by MatchingFieldsSelector")
			}

			switch len(ingressObjList.Items) {
			case 0:
				break
			case 1:
				if ingressObj := ingressObjList.Items[0]; ingressObj.GetName() == ingress.Name() && ingressObj.GetNamespace() == ingress.Namespace() {
					break
				}

				fallthrough
			default:
				return NewIngressHostnameCollision(hostname)
			}
		}
	}

	return nil
}
