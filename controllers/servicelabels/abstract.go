// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package servicelabels

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

type abstractServiceLabelsReconciler struct {
	obj    client.Object
	client client.Client
	log    logr.Logger
}

func (r *abstractServiceLabelsReconciler) InjectClient(c client.Client) error {
	r.client = c

	return nil
}

func (r *abstractServiceLabelsReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	tenant, err := r.getTenant(ctx, request.NamespacedName, r.client)
	if err != nil {
		if errors.As(err, &NonTenantObjectError{}) || errors.As(err, &NoServicesMetadataError{}) {
			return reconcile.Result{}, nil
		}

		r.log.Error(err, fmt.Sprintf("Cannot sync %T %s/%s labels", r.obj, r.obj.GetNamespace(), r.obj.GetName()))

		return reconcile.Result{}, err
	}

	err = r.client.Get(ctx, request.NamespacedName, r.obj)
	if err != nil {
		if apierr.IsNotFound(err) {
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

func (r *abstractServiceLabelsReconciler) forOptionPerInstanceName(ctx context.Context) builder.ForOption {
	return builder.WithPredicates(predicate.NewPredicateFuncs(func(object client.Object) bool {
		return r.IsNamespaceInTenant(ctx, object.GetNamespace())
	}))
}

func (r *abstractServiceLabelsReconciler) IsNamespaceInTenant(ctx context.Context, namespace string) bool {
	tl := &capsulev1beta1.TenantList{}
	if err := r.client.List(ctx, tl, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", namespace),
	}); err != nil {
		return false
	}

	return len(tl.Items) > 0
}
