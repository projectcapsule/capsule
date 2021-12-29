// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package servicelabels

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

type abstractServiceLabelsReconciler struct {
	obj    client.Object
	client client.Client
	log    logr.Logger
	scheme *runtime.Scheme
}

func (r *abstractServiceLabelsReconciler) InjectClient(c client.Client) error {
	r.client = c
	return nil
}

func (r *abstractServiceLabelsReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	tenant, err := r.getTenant(ctx, request.NamespacedName, r.client)
	if err != nil {
		switch err.(type) {
		case *NonTenantObject, *NoServicesMetadata:
			return reconcile.Result{}, nil
		default:
			r.log.Error(err, fmt.Sprintf("Cannot sync %t labels", r.obj))
			return reconcile.Result{}, err
		}
	}

	err = r.client.Get(ctx, request.NamespacedName, r.obj)
	if err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.client, r.obj, func() (err error) {
		r.obj.SetLabels(r.sync(r.obj.GetLabels(), tenant.Spec.ServiceOptions.AdditionalMetadata.Labels))
		r.obj.SetAnnotations(r.sync(r.obj.GetAnnotations(), tenant.Spec.ServiceOptions.AdditionalMetadata.Annotations))
		return nil
	})

	return reconcile.Result{}, err
}

func (r *abstractServiceLabelsReconciler) getTenant(ctx context.Context, namespacedName types.NamespacedName, client client.Client) (*capsulev1beta1.Tenant, error) {
	ns := &corev1.Namespace{}
	tenant := &capsulev1beta1.Tenant{}

	if err := client.Get(ctx, types.NamespacedName{Name: namespacedName.Namespace}, ns); err != nil {
		return nil, err
	}

	capsuleLabel, _ := capsulev1beta1.GetTypeLabel(&capsulev1beta1.Tenant{})
	if _, ok := ns.GetLabels()[capsuleLabel]; !ok {
		return nil, NewNonTenantObject(namespacedName.Name)
	}

	if err := client.Get(ctx, types.NamespacedName{Name: ns.Labels[capsuleLabel]}, tenant); err != nil {
		return nil, err
	}

	if tenant.Spec.ServiceOptions == nil || tenant.Spec.ServiceOptions.AdditionalMetadata == nil {
		return nil, NewNoServicesMetadata(namespacedName.Name)
	}

	return tenant, nil
}

func (r *abstractServiceLabelsReconciler) sync(available map[string]string, tenantSpec map[string]string) map[string]string {
	if tenantSpec != nil {
		if available == nil {
			available = tenantSpec
		} else {
			for key, value := range tenantSpec {
				if available[key] != value {
					available[key] = value
				}
			}
		}
	}
	return available
}

func (r *abstractServiceLabelsReconciler) forOptionPerInstanceName() builder.ForOption {
	return builder.WithPredicates(predicate.Funcs{
		CreateFunc: func(event event.CreateEvent) bool {
			return r.IsNamespaceInTenant(event.Object.GetNamespace())
		},
		DeleteFunc: func(deleteEvent event.DeleteEvent) bool {
			return r.IsNamespaceInTenant(deleteEvent.Object.GetNamespace())
		},
		UpdateFunc: func(updateEvent event.UpdateEvent) bool {
			return r.IsNamespaceInTenant(updateEvent.ObjectNew.GetNamespace())
		},
		GenericFunc: func(genericEvent event.GenericEvent) bool {
			return r.IsNamespaceInTenant(genericEvent.Object.GetNamespace())
		},
	})
}

func (r *abstractServiceLabelsReconciler) IsNamespaceInTenant(namespace string) bool {
	tl := &capsulev1beta1.TenantList{}
	if err := r.client.List(context.Background(), tl, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", namespace),
	}); err != nil {
		return false
	}
	return len(tl.Items) > 0
}
