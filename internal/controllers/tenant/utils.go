// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/utils"
)

func (r *Manager) enqueueAllTenants(ctx context.Context, _ client.Object) []reconcile.Request {
	var tenants capsulev1beta2.TenantList
	if err := r.List(ctx, &tenants); err != nil {
		r.Log.Error(err, "failed to list Tenants for class event")

		return nil
	}

	reqs := make([]reconcile.Request, 0, len(tenants.Items))
	for i := range tenants.Items {
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: tenants.Items[i].Name,
			},
		})
	}

	return reqs
}

// pruningResources is taking care of removing the no more requested sub-resources as LimitRange, ResourceQuota or
// NetworkPolicy using the "exists" and "notin" LabelSelector to perform an outer-join removal.
func (r *Manager) pruningResources(ctx context.Context, ns string, keys []string, obj client.Object) (err error) {
	var capsuleLabel string

	if capsuleLabel, err = utils.GetTypeLabel(obj); err != nil {
		return err
	}

	selector := labels.NewSelector()

	var exists *labels.Requirement

	if exists, err = labels.NewRequirement(capsuleLabel, selection.Exists, []string{}); err != nil {
		return err
	}

	selector = selector.Add(*exists)

	if len(keys) > 0 {
		var notIn *labels.Requirement

		if notIn, err = labels.NewRequirement(capsuleLabel, selection.NotIn, keys); err != nil {
			return err
		}

		selector = selector.Add(*notIn)
	}

	r.Log.V(3).Info("Pruning objects with label selector " + selector.String())

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return r.DeleteAllOf(ctx, obj, &client.DeleteAllOfOptions{
			ListOptions: client.ListOptions{
				LabelSelector: selector,
				Namespace:     ns,
			},
			DeleteOptions: client.DeleteOptions{},
		})
	})
}

func (r *Manager) emitEvent(object runtime.Object, namespace string, res controllerutil.OperationResult, msg string, err error) {
	eventType := corev1.EventTypeNormal

	if err != nil {
		eventType = corev1.EventTypeWarning
		res = "Error"
	}

	r.Recorder.AnnotatedEventf(object, map[string]string{"OperationResult": string(res)}, eventType, namespace, msg)
}
