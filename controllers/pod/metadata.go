// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package pod

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/utils"
)

type MetadataReconciler struct {
	Client client.Client
}

func (m *MetadataReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	var pod corev1.Pod

	logger := log.FromContext(ctx)

	tenant, err := m.getTenant(ctx, request.NamespacedName, m.Client)
	if err != nil {
		noTenantObjError := &NonTenantObjectError{}
		noPodMetaError := &NoPodMetadataError{}

		if errors.As(err, &noTenantObjError) || errors.As(err, &noPodMetaError) {
			return reconcile.Result{}, nil
		}

		logger.Error(err, fmt.Sprintf("Cannot get tenant corev1.Pod %s/%s", request.Namespace, request.Name))

		return reconcile.Result{}, err
	}

	err = m.Client.Get(ctx, request.NamespacedName, &pod)
	if err != nil {
		if apierr.IsNotFound(err) {
			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	_, err = controllerutil.CreateOrUpdate(ctx, m.Client, &pod, func() (err error) {
		pod.SetLabels(m.sync(pod.GetLabels(), tenant.Spec.PodOptions.AdditionalMetadata.Labels))
		pod.SetAnnotations(m.sync(pod.GetAnnotations(), tenant.Spec.PodOptions.AdditionalMetadata.Annotations))

		return nil
	})

	return reconcile.Result{}, err
}

func (m *MetadataReconciler) getTenant(ctx context.Context, namespacedName types.NamespacedName, client client.Client) (*capsulev1beta2.Tenant, error) {
	ns := &corev1.Namespace{}
	tenant := &capsulev1beta2.Tenant{}

	if err := client.Get(ctx, types.NamespacedName{Name: namespacedName.Namespace}, ns); err != nil {
		return nil, err
	}

	capsuleLabel, _ := utils.GetTypeLabel(&capsulev1beta2.Tenant{})
	if _, ok := ns.GetLabels()[capsuleLabel]; !ok {
		return nil, NewNonTenantObject(namespacedName.Name)
	}

	if err := client.Get(ctx, types.NamespacedName{Name: ns.Labels[capsuleLabel]}, tenant); err != nil {
		return nil, err
	}

	if tenant.Spec.PodOptions == nil || tenant.Spec.PodOptions.AdditionalMetadata == nil {
		return nil, NewNoPodMetadata(namespacedName.Name)
	}

	return tenant, nil
}

func (m *MetadataReconciler) sync(available map[string]string, tenantSpec map[string]string) map[string]string {
	if tenantSpec != nil {
		if available == nil {
			return tenantSpec
		}

		for key, value := range tenantSpec {
			if available[key] != value {
				available[key] = value
			}
		}
	}

	return available
}

func (m *MetadataReconciler) forOptionPerInstanceName(ctx context.Context) builder.ForOption {
	return builder.WithPredicates(predicate.NewPredicateFuncs(func(object client.Object) bool {
		return m.isNamespaceInTenant(ctx, object.GetNamespace())
	}))
}

func (m *MetadataReconciler) isNamespaceInTenant(ctx context.Context, namespace string) bool {
	tl := &capsulev1beta2.TenantList{}
	if err := m.Client.List(ctx, tl, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.namespaces", namespace),
	}); err != nil {
		return false
	}

	return len(tl.Items) > 0
}

func (m *MetadataReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}, m.forOptionPerInstanceName(ctx)).
		Complete(m)
}
