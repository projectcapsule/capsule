// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package resourcepools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
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
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/meta"
	"github.com/projectcapsule/capsule/pkg/metrics"
	"github.com/projectcapsule/capsule/pkg/utils"
)

type resourcePoolController struct {
	client.Client
	Metrics    *metrics.ResourcePoolRecorder
	Log        logr.Logger
	Recorder   record.EventRecorder
	RESTConfig *rest.Config
}

func (r *resourcePoolController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.ResourcePool{}).
		Owns(&corev1.ResourceQuota{}).
		Owns(&capsulev1beta2.ResourcePoolClaim{}).
		Watches(&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				// Fetch all GlobalResourceQuota objects
				grqList := &capsulev1beta2.ResourcePoolList{}
				if err := mgr.GetClient().List(ctx, grqList); err != nil {
					// Log the error and return no requests to reconcile
					r.Log.Error(err, "Failed to list ResourcePools objects")
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

func (r resourcePoolController) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	log := r.Log.WithValues("Request.Name", request.Name)
	// Fetch the Tenant instance
	instance := &capsulev1beta2.ResourcePool{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Request object not found, could have been deleted after reconcile request")

			r.Metrics.DeleteResourcePoolMetric(request.Name)

			return reconcile.Result{}, nil
		}

		log.Error(err, "Error reading the object")

		return
	}

	namespaces, err := r.gatherMatchingNamespaces(ctx, log, instance)
	if err != nil {
		log.Error(err, "Cannot get matching namespaces")

		return
	}

	nsNames := make([]string, 0, len(namespaces))
	for _, ns := range namespaces {
		nsNames = append(nsNames, ns.Name)
	}

	// ResourceQuota Reconcilation
	err = r.reconcile(ctx, log, instance, nsNames)

	r.Metrics.ResourceUsageMetrics(instance)

	r.Client.Status().Update(ctx, instance)

	if err != nil {
		log.Error(err, "Cannot sync ResourceQuotas")
	}

	return ctrl.Result{}, err
}

func (r *resourcePoolController) reconcile(
	ctx context.Context,
	log logr.Logger,
	pool *capsulev1beta2.ResourcePool,
	matchingNamespaces []string,
) (err error) {
	pool.Status.Allocation.Hard = pool.Spec.Quota.Hard

	namespaces, err := r.gatherMatchingNamespaces(ctx, log, pool)
	if err != nil {
		log.Error(err, "Cannot get matching namespaces")

		return err
	}

	var allClaims []capsulev1beta2.ResourcePoolClaim

	for _, ns := range namespaces {
		claimList := &capsulev1beta2.ResourcePoolClaimList{}
		if err := r.List(ctx, claimList, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".status.pool.uid", string(pool.GetUID())),
		}); err != nil {
			log.Error(err, "failed to list ResourceQuotaClaims", "namespace", ns)

			return err
		}

		//for _, claim := range claimList.Items {
		//	if claim.DeletionTimestamp != nil {
		//
		//	}
		//}

		allClaims = append(allClaims, claimList.Items...)
	}

	if err := r.garbageCollection(ctx, log, pool, allClaims, namespaces); err != nil {
		log.Error(err, "Failed to garbage collect ResourceQuotas")

		return err
	}

	pool.AssignNamespaces(namespaces)

	// Sort by creation timestamp (oldest first)
	sort.Slice(allClaims, func(i, j int) bool {
		return allClaims[i].CreationTimestamp.Before(&allClaims[j].CreationTimestamp)
	})

	log.Info("Sorted ResourceQuotaClaims", "count", len(allClaims))

	exhaustionPipeline := &PoolExhaustion{}

	// You can now iterate over `allClaims` in order
	for _, claim := range allClaims {
		log.Info("Found claim", "name", claim.Name, "namespace", claim.Namespace, "created", claim.CreationTimestamp)

		err = r.reconcileResourceClaim(ctx, log.WithValues("Claim", claim.Name), pool, &claim, exhaustionPipeline)
		if err != nil {
			log.Error(err, "Failed to reconcile ResourceQuotaClaim", "claim", claim.Name)
		}

		log.V(5).Info("Status resources", "pool", pool.Status)
	}

	pool.CalculateUsage()

	return r.syncResourceQuotas(ctx, r.Client, pool, matchingNamespaces)
}

// Reconciles a single ResourceClaim.
func (r *resourcePoolController) reconcileResourceClaim(
	ctx context.Context,
	log logr.Logger,
	pool *capsulev1beta2.ResourcePool,
	claim *capsulev1beta2.ResourcePoolClaim,
	exhaustion *PoolExhaustion,
) (err error) {
	t := pool.GetClaimFromStatus(claim)
	log.V(5).Info("Reconcile ResourceClaim", "claim", claim.GetUID(), "status", t)

	// Handle claims which are already considered
	if t != nil {
		log.V(5).Info("Claim already exists in pool status", "claim", claim.Name)

		return r.bindClaimToPool(ctx, log, pool, claim)
	}

	// Check if Resources can be Assigned (Enough Resources to claim)
	exhaustions := r.canClaimWithinNamespace(log, pool, claim)
	if len(exhaustions) != 0 {
		var lines []string
		for resourceName, exhaustion := range exhaustions {
			line := fmt.Sprintf(
				"%s: %s/%s",
				resourceName,
				exhaustion.Requesting.String(),
				exhaustion.Available.String(),
			)
			lines = append(lines, line)
		}

		// Join all lines nicely
		combined := fmt.Sprintf("cannot claim resources:\n%s", strings.Join(lines, "\n"))

		cond := meta.NewQueuedReasonCondition(claim)
		cond.Message = combined
		claim.Status.Condition = cond
	}

	// When we are Ordering the claims it's important to
	// verify that the resource would have not been exhausted already
	//if pool.Spec.OrderedQueue {
	//
	//}
	//
	return r.bindClaimToPool(ctx, log, pool, claim)
}

func (r *resourcePoolController) canClaimWithinNamespace(
	log logr.Logger,
	pool *capsulev1beta2.ResourcePool,
	claim *capsulev1beta2.ResourcePoolClaim,
) (res map[string]PoolExhaustionResource) {
	claimable := pool.GetAvailableClaimableResources()
	log.V(5).Info("claimable resources", "claimable", claimable)

	_, namespaceClaimed := pool.GetNamespaceClaims(claim.Namespace)
	log.V(5).Info("namespace claimed resources", "claimed", namespaceClaimed)

	for resourceName, req := range claim.Spec.ResourceClaims {

		// Verify if total Quota is available
		available, exists := claimable[resourceName]
		if !exists || available.IsZero() || available.Cmp(req) < 0 {
			log.V(5).Info("not enough resources available", "available", available, "requesting", req)

			res[resourceName.String()] = PoolExhaustionResource{
				Available:  available,
				Requesting: req,
				Namespace:  false,
			}

			continue
		}

		// Verify that this resource can still be claimed within the namespace
		// Only Necessary when there is a limit
		maxNamespaceAllocation, maxExist := pool.Spec.MaximumAllocation[resourceName]
		if !maxExist {
			continue
		}

		claimed, exists := namespaceClaimed[resourceName]
		if !exists {
			claimed = resource.MustParse("0")
		}

		claimed.Add(req)

		if maxNamespaceAllocation.Cmp(claimed) < 0 {
			log.V(5).Info("maxium for namespace claimed", "max", maxNamespaceAllocation, "claiming", claimed)

			res[resourceName.String()] = PoolExhaustionResource{
				Available:  available,
				Requesting: req,
				Namespace:  true,
			}

			continue
		}
	}

	return
}

func (r *resourcePoolController) bindClaimToPool(
	ctx context.Context,
	log logr.Logger,
	pool *capsulev1beta2.ResourcePool,
	claim *capsulev1beta2.ResourcePoolClaim,
) (err error) {
	cond := meta.NewReadyCondition(claim)
	cond.Reason = meta.BoundReason
	cond.Message = "Claimed resources"

	pool.AddClaimToStatus(claim)

	claim.UpdateCondition(cond)

	return r.Client.Status().Update(ctx, claim)
}

// Handles All the Claims for the ResourcePool.
func (r *resourcePoolController) gatherMatchingNamespaces(
	ctx context.Context,
	log logr.Logger,
	pool *capsulev1beta2.ResourcePool,
) (namespaces []corev1.Namespace, err error) {
	// Collect Namespaces (Matching)
	namespaces = make([]corev1.Namespace, 0)
	seenNamespaces := make(map[string]struct{})

	for _, selector := range pool.Spec.Selectors {
		selected, serr := selector.GetMatchingNamespaces(ctx, r.Client)
		if serr != nil {
			log.Error(err, "Cannot get matching namespaces")

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

			seenNamespaces[ns.Name] = struct{}{}

			namespaces = append(namespaces, ns)
		}
	}

	return
}

// Synchronize resources quotas in all the given namespaces (routines).
func (r *resourcePoolController) syncResourceQuotas(
	ctx context.Context,
	c client.Client,
	quota *capsulev1beta2.ResourcePool,
	namespaces []string,
) (err error) {
	group := new(errgroup.Group)

	for _, ns := range namespaces {
		namespace := ns

		group.Go(func() error {
			return r.syncResourceQuota(ctx, c, quota, namespace)
		})
	}

	return group.Wait()
}

// Synchronize a single resourcequota.
func (r *resourcePoolController) syncResourceQuota(
	ctx context.Context,
	c client.Client,
	pool *capsulev1beta2.ResourcePool,
	namespace string,
) (err error) {
	// getting ResourceQuota labels for the mutateFn
	var quotaLabel string

	if quotaLabel, err = utils.GetTypeLabel(&capsulev1beta2.ResourcePool{}); err != nil {
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
func (r *resourcePoolController) garbageCollection(
	ctx context.Context,
	log logr.Logger,
	pool *capsulev1beta2.ResourcePool,
	claims []capsulev1beta2.ResourcePoolClaim,
	namespaces []corev1.Namespace,
) error {
	matchingNamespaceSet := make(map[string]struct{}, len(namespaces))
	for _, ns := range namespaces {
		matchingNamespaceSet[ns.Name] = struct{}{}
	}

	claimSet := make(map[string]struct{}, len(claims))
	for _, claim := range claims {
		claimSet[string(claim.UID)] = struct{}{}
	}

	// Garbage collect namespaces which no longer match selector
	for ns, clms := range pool.Status.Claims {
		_, namespaceOk := matchingNamespaceSet[ns]
		if !namespaceOk {
			if err := r.garbageCollectNamespace(ctx, pool, ns); err != nil {
				r.Log.Error(err, "Failed to garbage collect resource quota", "namespace", ns)

				return err
			}
		}

		for _, cl := range clms {
			_, ok := claimSet[string(cl.UID)]
			if !namespaceOk || !ok {
				log.V(5).Info("Disassociating claim", "namespace", ns, "uid", cl.UID)

				if err := r.disassociateClaimItem(ctx, pool, cl); err != nil {
					r.Log.Error(err, "Failed to disassociate claim", "namespace", ns, "uid", cl.UID)

					return err
				}
			}
		}

		if !namespaceOk {
			delete(pool.Status.Claims, ns)
		}
	}

	// We can recalculate the usage in the end
	// Since it's only going to decrease
	pool.CalculateUsage()

	return nil
}

// Attempts to garbage collect a ResourceQuota resource.
func (r *resourcePoolController) garbageCollectNamespace(
	ctx context.Context,
	pool *capsulev1beta2.ResourcePool,
	namespace string,
) error {
	r.Metrics.DeleteResourcePoolNamespaceMetric(pool.Name, namespace)

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

// Attempts to garbage collect a ResourceQuota resource.
func (r *resourcePoolController) moveClaimItemToQueue(
	ctx context.Context,
	pool *capsulev1beta2.ResourcePool,
	claim *capsulev1beta2.ResourcePoolClaimsItem,
) error {
	claimObj := &capsulev1beta2.ResourcePoolClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      claim.Name.String(),
			Namespace: claim.Namespace.String(),
			UID:       claim.UID,
		},
	}

	err := r.Client.Get(ctx, types.NamespacedName{
		Name:      claim.Name.String(),
		Namespace: claim.Namespace.String(),
	}, claimObj)
	if err != nil {
		if errors.IsNotFound(err) {
			pool.RemoveClaimFromStatus(claimObj)

			return nil
		}

		return err
	}

	// Remove Pool Reference
	claimObj.Status = capsulev1beta2.ResourcePoolClaimStatus{
		Pool:      api.StatusNameUID{},
		Condition: meta.NewNotReadyCondition(claimObj, "Claim is being disassociated"),
	}

	if err := r.Client.Status().Update(ctx, claimObj); err != nil {
		return err
	}

	pool.RemoveClaimFromStatus(claimObj)

	return nil
}

// Attempts to garbage collect a ResourceQuota resource.
func (r *resourcePoolController) disassociateClaimItem(
	ctx context.Context,
	pool *capsulev1beta2.ResourcePool,
	claim *capsulev1beta2.ResourcePoolClaimsItem,
) error {
	claimObj := &capsulev1beta2.ResourcePoolClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      claim.Name.String(),
			Namespace: claim.Namespace.String(),
			UID:       claim.UID,
		},
	}

	err := r.Client.Get(ctx, types.NamespacedName{
		Name:      claim.Name.String(),
		Namespace: claim.Namespace.String(),
	}, claimObj)
	if err != nil {
		if errors.IsNotFound(err) {
			pool.RemoveClaimFromStatus(claimObj)

			return nil
		}

		return err
	}

	// Remove Pool Reference
	claimObj.Status = capsulev1beta2.ResourcePoolClaimStatus{
		Pool:      api.StatusNameUID{},
		Condition: meta.NewNotReadyCondition(claimObj, "Claim is being disassociated"),
	}

	if err := r.Client.Status().Update(ctx, claimObj); err != nil {
		return err
	}

	pool.RemoveClaimFromStatus(claimObj)

	return nil
}
