// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ingress

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
	"github.com/projectcapsule/capsule/pkg/runtime/handlers"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/ingress"
)

type collision struct {
	configuration configuration.Configuration
}

func Collision(configuration configuration.Configuration) handlers.Handler {
	return &collision{configuration: configuration}
}

func (r *collision) OnCreate(
	c client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.validate(ctx, c, req, decoder, recorder)
	}
}

func (r *collision) OnUpdate(
	c client.Client,
	_ client.Reader,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) handlers.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.validate(ctx, c, req, decoder, recorder)
	}
}

func (r *collision) OnDelete(
	client.Client,
	client.Reader,
	admission.Decoder,
	events.EventRecorder,
) handlers.Func {
	return func(context.Context, admission.Request) *admission.Response {
		return nil
	}
}

func (r *collision) validate(
	ctx context.Context,
	reader client.Client,
	req admission.Request,
	decoder admission.Decoder,
	recorder events.EventRecorder,
) *admission.Response {
	ing, err := FromRequest(req, decoder)
	if err != nil {
		return ad.ErroredResponse(err)
	}

	var tnt *capsulev1beta2.Tenant

	tnt, err = TenantFromIngress(ctx, reader, ing)
	if err != nil {
		return ad.ErroredResponse(err)
	}

	if tnt == nil || tnt.Spec.IngressOptions.HostnameCollisionScope == api.HostnameCollisionScopeDisabled {
		return nil
	}

	if err = r.validateCollision(ctx, reader, ing, tnt.Spec.IngressOptions.HostnameCollisionScope); err == nil {
		return nil
	}

	var collisionErr *caperrors.IngressHostnameCollisionError
	if errors.As(err, &collisionErr) {
		recorder.LabeledEvent(
			ing.GetClientObject(),
			corev1.EventTypeWarning,
			events.ReasonIngressHostnameCollision,
			events.ActionValidationDenied,
			"ingress hostname is colliding",
		).
			WithRelated(tnt).
			WithTenantLabel(tnt).
			WithRequestAnnotations(req).
			Emit(ctx)
	}

	return ad.Deny(err.Error())
}

//nolint:gocognit,gocyclo,cyclop
func (r *collision) validateCollision(
	ctx context.Context,
	reader client.Reader,
	ing Ingress,
	scope api.HostnameCollisionScope,
) error {
	for hostname, paths := range ing.HostnamePathsPairs() {
		for path := range paths {
			var ingressObjList client.ObjectList

			switch ing.(type) {
			case Extension:
				ingressObjList = &extensionsv1beta1.IngressList{}
			case NetworkingV1:
				ingressObjList = &networkingv1.IngressList{}
			case NetworkingV1Beta1:
				ingressObjList = &networkingv1beta1.IngressList{}
			}

			namespaces := sets.NewString()
			//nolint:exhaustive
			switch scope {
			case api.HostnameCollisionScopeCluster:
				tenantList := &capsulev1beta2.TenantList{}
				if err := reader.List(ctx, tenantList); err != nil {
					return err
				}

				for _, tenant := range tenantList.Items {
					namespaces.Insert(tenant.Status.Namespaces...)
				}
			case api.HostnameCollisionScopeTenant:
				tenantList := &capsulev1beta2.TenantList{}
				if err := reader.List(ctx, tenantList, client.MatchingFields{".status.namespaces": ing.Namespace()}); err != nil {
					return err
				}

				for _, tenant := range tenantList.Items {
					namespaces.Insert(tenant.Status.Namespaces...)
				}
			case api.HostnameCollisionScopeNamespace:
				namespaces.Insert(ing.Namespace())
			}

			if err := reader.List(ctx, ingressObjList, client.MatchingFields{ingress.HostPathPair: fmt.Sprintf("%s;%s", hostname, path)}); err != nil {
				return err
			}

			ingressList := sets.NewInt()

			switch list := ingressObjList.(type) {
			case *extensionsv1beta1.IngressList:
				for index, item := range list.Items {
					if namespaces.Has(item.GetNamespace()) {
						ingressList.Insert(index)
					}
				}

				switch len(ingressList) {
				case 0:
					break
				case 1:
					if index := ingressList.List()[0]; list.Items[index].GetName() == ing.Name() && list.Items[index].GetNamespace() == ing.Namespace() {
						break
					}

					fallthrough
				default:
					return caperrors.NewIngressHostnameCollision(hostname)
				}
			case *networkingv1.IngressList:
				for index, item := range list.Items {
					if namespaces.Has(item.GetNamespace()) {
						ingressList.Insert(index)
					}
				}

				switch len(ingressList) {
				case 0:
					break
				case 1:
					if index := ingressList.List()[0]; list.Items[index].GetName() == ing.Name() && list.Items[index].GetNamespace() == ing.Namespace() {
						break
					}

					fallthrough
				default:
					return caperrors.NewIngressHostnameCollision(hostname)
				}
			case *networkingv1beta1.IngressList:
				for index, item := range list.Items {
					if namespaces.Has(item.GetNamespace()) {
						ingressList.Insert(index)
					}
				}

				switch len(ingressList) {
				case 0:
					break
				case 1:
					if index := ingressList.List()[0]; list.Items[index].GetName() == ing.Name() && list.Items[index].GetNamespace() == ing.Namespace() {
						break
					}

					fallthrough
				default:
					return caperrors.NewIngressHostnameCollision(hostname)
				}
			}
		}
	}

	return nil
}
