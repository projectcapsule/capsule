// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package globalquota

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/metrics"
	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
)

type Manager struct {
	client.Client
	Log        logr.Logger
	Recorder   record.EventRecorder
	RESTConfig *rest.Config
}

func (r *Manager) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.GlobalResourceQuota{}).
		Owns(&corev1.ResourceQuota{}).
		Watches(&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				// Fetch all GlobalResourceQuota objects
				grqList := &capsulev1beta2.GlobalResourceQuotaList{}
				if err := mgr.GetClient().List(ctx, grqList); err != nil {
					// Log the error and return no requests to reconcile
					r.Log.Error(err, "Failed to list GlobalResourceQuota objects")
					return nil
				}

				// Enqueue a reconcile request for each GlobalResourceQuota
				var requests []reconcile.Request
				for _, grq := range grqList.Items {
					requests = append(requests, reconcile.Request{
						NamespacedName: client.ObjectKeyFromObject(&grq),
					})
				}

				return requests
			}),
		).
		Complete(r)
}

//nolint:nakedret
func (r Manager) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	r.Log = r.Log.WithValues("Request.Name", request.Name)
	// Fetch the Tenant instance
	instance := &capsulev1beta2.GlobalResourceQuota{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			r.Log.Info("Request object not found, could have been deleted after reconcile request")

			// If tenant was deleted or cannot be found, clean up metrics
			metrics.GlobalResourceUsage.DeletePartialMatch(map[string]string{"quota": request.Name})
			metrics.GlobalResourceLimit.DeletePartialMatch(map[string]string{"quota": request.Name})

			return reconcile.Result{}, nil
		}

		r.Log.Error(err, "Error reading the object")

		return
	}

	// Ensuring the Quota Status
	if err = r.updateQuotaStatus(ctx, instance); err != nil {
		r.Log.Error(err, "Cannot update Tenant status")

		return
	}

	if !instance.Spec.Active {
		r.Log.Info("GlobalResourceQuota is not active, skipping reconciliation")

		return
	}

	// Get Item within Resource Quota
	objectLabel, err := capsuleutils.GetTypeLabel(&capsulev1beta2.Tenant{})
	if err != nil {
		return
	}

	// Collect Namespaces (Matching)
	namespaces := make([]corev1.Namespace, 0)
	seenNamespaces := make(map[string]struct{})

	for _, selector := range instance.Spec.Selectors {
		selected, serr := selector.GetMatchingNamespaces(ctx, r.Client)
		if serr != nil {
			r.Log.Error(err, "Cannot get matching namespaces")

			continue
		}

		for _, ns := range selected {
			// Skip if namespace is being deleted
			if !ns.ObjectMeta.DeletionTimestamp.IsZero() {
				continue
			}

			if _, exists := seenNamespaces[ns.Name]; exists {
				continue // Skip duplicates
			}

			if selector.MustTenantNamespace {
				if _, ok := ns.Labels[objectLabel]; !ok {
					continue
				}
			}

			seenNamespaces[ns.Name] = struct{}{}
			namespaces = append(namespaces, ns)
		}
	}

	nsNames := make([]string, 0, len(namespaces))
	for _, ns := range namespaces {
		nsNames = append(nsNames, ns.Name)
	}

	// ResourceQuota Reconcilation
	err = r.syncResourceQuotas(ctx, instance, nsNames)
	if err != nil {
		r.Log.Error(err, "Cannot sync ResourceQuotas")
	}

	// Collect Namespaces for Status
	if err = r.statusNamespaces(ctx, instance, namespaces); err != nil {
		r.Log.Error(err, "Cannot update Tenant status")

		return
	}

	return ctrl.Result{}, err
}

// Update tracking namespaces
func (r *Manager) statusNamespaces(ctx context.Context, quota *capsulev1beta2.GlobalResourceQuota, ns []corev1.Namespace) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.GlobalResourceQuota{}
		if err := r.Client.Get(ctx, client.ObjectKey{Name: quota.Name}, latest); err != nil {
			r.Log.Error(err, "Failed to fetch the latest Tenant object during retry")

			return err
		}

		latest.AssignNamespaces(ns)

		return r.Client.Status().Update(ctx, latest, &client.SubResourceUpdateOptions{})
	})
}

func (r *Manager) updateQuotaStatus(ctx context.Context, quota *capsulev1beta2.GlobalResourceQuota) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.GlobalResourceQuota{}
		if err := r.Client.Get(ctx, client.ObjectKey{Name: quota.Name}, latest); err != nil {
			r.Log.Error(err, "Failed to fetch the latest Tenant object during retry")

			return err
		}
		// Update the state based on the latest spec
		latest.Status.Active = latest.Spec.Active

		return r.Client.Status().Update(ctx, latest)
	})
}
