// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package resourcequotapools

import (
	"context"
	"fmt"
	"sort"

	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/metrics"
	"github.com/projectcapsule/capsule/pkg/utils"
	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
)

type resourcePoolController struct {
	client.Client
	Log        logr.Logger
	Recorder   record.EventRecorder
	RESTConfig *rest.Config
}

func (r *resourcePoolController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.ResourceQuotaPool{}).
		Owns(&corev1.ResourceQuota{}).
		Watches(&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				// Fetch all GlobalResourceQuota objects
				grqList := &capsulev1beta2.ResourceQuotaPoolList{}
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
func (r resourcePoolController) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	r.Log = r.Log.WithValues("Request.Name", request.Name)
	// Fetch the Tenant instance
	instance := &capsulev1beta2.ResourceQuotaPool{}
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
	err = r.reconcile(ctx, instance, nsNames)
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

//nolint:nakedret, gocognit
func (r *resourcePoolController) reconcile(
	ctx context.Context,
	quota *capsulev1beta2.ResourceQuotaPool,
	matchingNamespaces []string,
) (err error) {
	// Remove prior metrics, to avoid cleaning up for metrics of deleted ResourceQuotas
	metrics.TenantResourceUsage.DeletePartialMatch(map[string]string{"quota": quota.Name})
	metrics.TenantResourceLimit.DeletePartialMatch(map[string]string{"quota": quota.Name})

	// Convert matchingNamespaces to a map for quick lookup
	matchingNamespaceSet := make(map[string]struct{}, len(matchingNamespaces))
	for _, ns := range matchingNamespaces {
		matchingNamespaceSet[ns] = struct{}{}
	}

	// Garbage collect namespaces which no longer match selector
	for _, existingNamespace := range quota.Status.Namespaces {
		if _, exists := matchingNamespaceSet[existingNamespace]; !exists {
			if err := r.garbageCollectNamespace(ctx, quota, existingNamespace); err != nil {
				r.Log.Error(err, "Failed to garbage collect resource quota", "namespace", existingNamespace)
				return err
			}
		}
	}

	return r.syncResourceQuotas(ctx, r.Client, quota, matchingNamespaces)
}

// Handles All the Claims for the ResourcePool
func (r *resourcePoolController) reconcileResourceClaims(
	ctx context.Context,
	log logr.Logger,
	quota *capsulev1beta2.ResourceQuotaPool,
	namespaces []string,
) (err error) {
	var allClaims []capsulev1beta2.ResourceQuotaClaim

	for _, ns := range namespaces {
		var claimList capsulev1beta2.ResourceQuotaClaimList
		if err := r.List(ctx, &claimList, &client.ListOptions{
			Namespace: ns,
		}); err != nil {
			log.Error(err, "failed to list ResourceQuotaClaims", "namespace", ns)
			return err
		}
		allClaims = append(allClaims, claimList.Items...)
	}

	// Sort by creation timestamp (oldest first)
	sort.Slice(allClaims, func(i, j int) bool {
		return allClaims[i].CreationTimestamp.Before(&allClaims[j].CreationTimestamp)
	})

	log.Info("Sorted ResourceQuotaClaims", "count", len(allClaims))

	// You can now iterate over `allClaims` in order
	for _, claim := range allClaims {
		log.Info("Found claim", "name", claim.Name, "namespace", claim.Namespace, "created", claim.CreationTimestamp)

		r.reconcileResourceClaim(ctx, log.WithValues("Claim", claim.Name), quota, claim)
	}

}

// Reconciles a single ResourceClaim
func (r *resourcePoolController) reconcileResourceClaim(
	ctx context.Context,
	log logr.Logger,
	quota *capsulev1beta2.ResourceQuotaPool,
	claim capsulev1beta2.ResourceQuotaClaim,
) (err error) {
	// Handles Claims which are already in status

	var claimList capsulev1beta2.ResourceQuotaClaimList
	if err := r.List(ctx, &claimList, &client.ListOptions{}); err != nil {
		r.Log.Error(err, "failed to gather claims")
		return nil
	}

}

func (r *resourcePoolController) handlePoolClaimExhaustion(
	ctx context.Context,
	log logr.Logger,
	c client.Client,
	quota *capsulev1beta2.ResourceQuotaPool,
	claim capsulev1beta2.ResourceQuotaClaim,
) (err error) {
	var claimList capsulev1beta2.ResourceQuotaClaimList
	if err := r.List(ctx, &claimList, &client.ListOptions{}); err != nil {
		r.Log.Error(err, "failed to gather claims")
		return nil
	}

}

// Synchronize resources quotas in all the given namespaces (routines)
func (r *resourcePoolController) syncResourceQuotas(
	ctx context.Context,
	c client.Client,
	quota *capsulev1beta2.ResourceQuotaPool,
	namespaces []string,
) (err error) {
	group := new(errgroup.Group)

	// Sync resource quotas for matching namespaces
	for _, ns := range namespaces {
		namespace := ns

		group.Go(func() error {
			return r.syncResourceQuota(ctx, c, quota, namespace)
		})
	}

	return group.Wait()
}

// Synchronize a single resourcequota
func (r *resourcePoolController) syncResourceQuota(
	ctx context.Context,
	c client.Client,
	pool *capsulev1beta2.ResourceQuotaPool,
	namespace string,
) (err error) {
	// getting ResourceQuota labels for the mutateFn
	var quotaLabel string

	if quotaLabel, err = utils.GetTypeLabel(&capsulev1beta2.ResourceQuotaPool{}); err != nil {
		return err
	}

	target := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceQuotaItemName(pool),
			Namespace: namespace,
		},
	}

	if err := c.Get(ctx, types.NamespacedName{Name: target.Name, Namespace: target.Namespace}, target); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	err = retry.RetryOnConflict(retry.DefaultBackoff, func() (retryErr error) {
		_, retryErr = controllerutil.CreateOrUpdate(ctx, c, target, func() (err error) {
			targetLabels := target.GetLabels()
			if targetLabels == nil {
				targetLabels = map[string]string{}
			}

			targetLabels[quotaLabel] = pool.Name

			target.SetLabels(targetLabels)
			target.Spec.Scopes = pool.Spec.Quota.Scopes
			target.Spec.ScopeSelector = pool.Spec.Quota.ScopeSelector

			// Assign to resourcequota all the claims + defaults
			target.Spec.Hard = pool.GetResourceQuotaHardResources(namespace)

			return controllerutil.SetControllerReference(pool, target, c.Scheme())
		})

		return retryErr
	})

	if err != nil {
		return err
	}

	return nil
}

// Attempts to garbage collect a ResourceQuota resource.
func (r *resourcePoolController) garbageCollectNamespace(
	ctx context.Context,
	pool *capsulev1beta2.ResourceQuotaPool,
	namespace string,
) error {
	// Check if the namespace still exists
	ns := &corev1.Namespace{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
		if errors.IsNotFound(err) {
			r.Log.V(5).Info("Namespace does not exist, skipping garbage collection", "namespace", namespace)
			return nil
		}
		return fmt.Errorf("failed to check namespace existence: %w", err)
	}

	// Attempt to delete the ResourceQuota
	target := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceQuotaItemName(pool),
			Namespace: namespace,
		},
	}
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: target.GetName()}, target)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.V(5).Info("ResourceQuota already deleted", "namespace", namespace, "name", resourceQuotaItemName(pool))
		}
		return err
	}

	// Delete the ResourceQuota
	if err := r.Client.Delete(ctx, target); err != nil {
		return fmt.Errorf("failed to delete ResourceQuota %s in namespace %s: %w", resourceQuotaItemName(pool), namespace, err)
	}

	r.Log.Info("Deleted ResourceQuota", "namespace", namespace)
	return nil
}

// Update tracking namespaces
func (r *resourcePoolController) statusNamespaces(
	ctx context.Context,
	quota *capsulev1beta2.ResourceQuotaPool,
	ns []corev1.Namespace,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.ResourceQuotaPool{}
		if err := r.Client.Get(ctx, client.ObjectKey{Name: quota.Name}, latest); err != nil {
			r.Log.Error(err, "Failed to fetch the latest Tenant object during retry")

			return err
		}

		latest.AssignNamespaces(ns)

		return r.Client.Status().Update(ctx, latest, &client.SubResourceUpdateOptions{})
	})
}
