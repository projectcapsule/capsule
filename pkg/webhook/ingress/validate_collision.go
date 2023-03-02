// Copyright 2020-2021 Clastix Labs
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
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/api"
	"github.com/clastix/capsule/pkg/configuration"
	"github.com/clastix/capsule/pkg/indexer/ingress"
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
		return r.validate(ctx, client, req, decoder, recorder)
	}
}

func (r *collision) OnUpdate(client client.Client, decoder *admission.Decoder, recorder record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return r.validate(ctx, client, req, decoder, recorder)
	}
}

func (r *collision) OnDelete(client.Client, *admission.Decoder, record.EventRecorder) capsulewebhook.Func {
	return func(ctx context.Context, req admission.Request) *admission.Response {
		return nil
	}
}

func (r *collision) validate(ctx context.Context, client client.Client, req admission.Request, decoder *admission.Decoder, recorder record.EventRecorder) *admission.Response {
	ing, err := FromRequest(req, decoder)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	var tenant *capsulev1beta2.Tenant

	tenant, err = TenantFromIngress(ctx, client, ing)
	if err != nil {
		return utils.ErroredResponse(err)
	}

	if tenant == nil || tenant.Spec.IngressOptions.HostnameCollisionScope == api.HostnameCollisionScopeDisabled {
		return nil
	}

	if err = r.validateCollision(ctx, client, ing, tenant.Spec.IngressOptions.HostnameCollisionScope); err == nil {
		return nil
	}

	var collisionErr *ingressHostnameCollisionError

	if errors.As(err, &collisionErr) {
		recorder.Eventf(tenant, corev1.EventTypeWarning, "IngressHostnameCollision", "Ingress %s/%s hostname is colliding", ing.Namespace(), ing.Name())
	}

	response := admission.Denied(err.Error())

	return &response
}

//nolint:gocognit,gocyclo,cyclop
func (r *collision) validateCollision(ctx context.Context, clt client.Client, ing Ingress, scope api.HostnameCollisionScope) error {
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
				if err := clt.List(ctx, tenantList); err != nil {
					return err
				}

				for _, tenant := range tenantList.Items {
					namespaces.Insert(tenant.Status.Namespaces...)
				}
			case api.HostnameCollisionScopeTenant:
				selector := client.MatchingFieldsSelector{Selector: fields.OneTermEqualSelector(".status.namespaces", ing.Namespace())}

				tenantList := &capsulev1beta2.TenantList{}
				if err := clt.List(ctx, tenantList, selector); err != nil {
					return err
				}

				for _, tenant := range tenantList.Items {
					namespaces.Insert(tenant.Status.Namespaces...)
				}
			case api.HostnameCollisionScopeNamespace:
				namespaces.Insert(ing.Namespace())
			}

			fieldSelector := fields.OneTermEqualSelector(ingress.HostPathPair, fmt.Sprintf("%s;%s", hostname, path))

			if err := clt.List(ctx, ingressObjList, client.MatchingFieldsSelector{Selector: fieldSelector}); err != nil {
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
					return NewIngressHostnameCollision(hostname)
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
					return NewIngressHostnameCollision(hostname)
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
					return NewIngressHostnameCollision(hostname)
				}
			}
		}
	}

	return nil
}
