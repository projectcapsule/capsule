// Copyright 2020-2025 Project Capsule Authors
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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	ctrlutils "github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/utils"
)

type resourcePoolController struct {
	client.Client

	metrics  *metrics.ResourcePoolRecorder
	log      logr.Logger
	recorder record.EventRecorder
}

func (r *resourcePoolController) SetupWithManager(mgr ctrl.Manager, cfg ctrlutils.ControllerOptions) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("resourcepool/pools").
		For(&capsulev1beta2.ResourcePool{}).
		Owns(&corev1.ResourceQuota{}).
		Watches(&capsulev1beta2.ResourcePoolClaim{},
			handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(), &capsulev1beta2.ResourcePool{}),
		).
		Watches(&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, _ client.Object) []reconcile.Request {
				// Fetch all GlobalResourceQuota objects
				grqList := &capsulev1beta2.ResourcePoolList{}
				if err := mgr.GetClient().List(ctx, grqList); err != nil {
					r.log.Error(err, "Failed to list ResourcePools objects")

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
		WithOptions(controller.Options{MaxConcurrentReconciles: cfg.MaxConcurrentReconciles}).
		Complete(r)
}

func (r resourcePoolController) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	log := r.log.WithValues("Request.Name", request.Name)
	// Fetch the Tenant instance
	instance := &capsulev1beta2.ResourcePool{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(3).Info("Request object not found, could have been deleted after reconcile request")

			r.metrics.DeleteResourcePoolMetric(request.Name)

			return reconcile.Result{}, nil
		}

		log.Error(err, "Error reading the object")

		return result, err
	}

	// ResourceQuota Reconciliation
	reconcileErr := r.reconcile(ctx, log, instance)

	r.metrics.ResourceUsageMetrics(instance)

	// Always Post Status
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &capsulev1beta2.ResourcePool{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(instance), current); err != nil {
			return fmt.Errorf("failed to refetch instance before update: %w", err)
		}

		current.Status = instance.Status

		return r.Client.Status().Update(ctx, current)
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	if reconcileErr != nil {
		return ctrl.Result{}, reconcileErr
	}

	err = r.finalize(ctx, instance)

	return ctrl.Result{}, err
}

func (r *resourcePoolController) finalize(
	ctx context.Context,
	pool *capsulev1beta2.ResourcePool,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Re-fetch latest version of the object
		latest := &capsulev1beta2.ResourcePool{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(pool), latest); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		changed := false

		// Case: all claims are gone, remove finalizer
		if latest.Status.ClaimSize == 0 && controllerutil.ContainsFinalizer(latest, meta.ControllerFinalizer) {
			controllerutil.RemoveFinalizer(latest, meta.ControllerFinalizer)

			changed = true
		}

		// Case: claims still exist, add finalizer if not already present
		if latest.Status.ClaimSize > 0 && !controllerutil.ContainsFinalizer(latest, meta.ControllerFinalizer) {
			controllerutil.AddFinalizer(latest, meta.ControllerFinalizer)

			changed = true
		}

		if changed {
			return r.Update(ctx, latest)
		}

		return nil
	})
}

func (r *resourcePoolController) reconcile(
	ctx context.Context,
	log logr.Logger,
	pool *capsulev1beta2.ResourcePool,
) (err error) {
	r.handlePoolHardResources(pool)

	namespaces, err := r.gatherMatchingNamespaces(ctx, log, pool)
	if err != nil {
		log.Error(err, "Can not get matching namespaces")

		return err
	}

	currentNamespaces := make(map[string]struct{}, len(namespaces))
	for _, ns := range namespaces {
		currentNamespaces[ns.Name] = struct{}{}
	}

	claims, err := r.gatherMatchingClaims(ctx, log, pool, currentNamespaces)
	if err != nil {
		log.Error(err, "Can not get matching namespaces")

		return err
	}

	log.V(5).Info("Collected assigned claims", "count", len(claims))

	if err := r.garbageCollection(ctx, log, pool, claims, currentNamespaces); err != nil {
		log.Error(err, "Failed to garbage collect ResourceQuotas")

		return err
	}

	pool.AssignNamespaces(namespaces)

	// Sort by creation timestamp (oldest first)
	sort.Slice(claims, func(i, j int) bool {
		return claims[i].CreationTimestamp.Before(&claims[j].CreationTimestamp)
	})

	// Keeps track of resources which are exhausted by previous resource
	// This is only required when Ordered is active
	exhaustions := make(map[string]api.PoolExhaustionResource)

	// You can now iterate over `allClaims` in order
	for _, claim := range claims {
		log.V(5).Info("Found claim", "name", claim.Name, "namespace", claim.Namespace, "created", claim.CreationTimestamp)

		err = r.reconcileResourceClaim(ctx, log.WithValues("Claim", claim.Name), pool, &claim, exhaustions)
		if err != nil {
			log.Error(err, "Failed to reconcile ResourceQuotaClaim", "claim", claim.Name)
		}
	}

	log.V(7).Info("finalized reconciling claims", "exhaustions", exhaustions)

	r.metrics.CalculateExhaustions(pool, exhaustions)
	pool.Status.Exhaustions = exhaustions

	pool.CalculateClaimedResources()
	pool.AssignClaims()

	return r.syncResourceQuotas(ctx, r.Client, pool, namespaces)
}

// Reconciles a single ResourceClaim.
func (r *resourcePoolController) reconcileResourceClaim(
	ctx context.Context,
	log logr.Logger,
	pool *capsulev1beta2.ResourcePool,
	claim *capsulev1beta2.ResourcePoolClaim,
	exhaustion map[string]api.PoolExhaustionResource,
) (err error) {
	t := pool.GetClaimFromStatus(claim)
	if t != nil {
		// TBD: Future Implementation for Claim Resizing here
		return r.handleClaimToPoolBinding(ctx, pool, claim)
	}

	// Verify if a resource was already exhausted by a previous claim
	if *pool.Spec.Config.OrderedQueue {
		var queued bool

		queued, err = r.handleClaimOrderedExhaustion(
			ctx,
			claim,
			exhaustion,
		)
		if err != nil {
			return err
		}

		if queued {
			log.V(5).Info("Claim is queued", "claim", claim.Name)

			return nil
		}
	}

	// Check if Resources can be Assigned (Enough Resources to claim)
	exhaustions := r.canClaimWithinNamespace(log, pool, claim)
	if len(exhaustions) != 0 {
		log.V(5).Info("exhausting resources", "amount", len(exhaustions))

		return r.handleClaimResourceExhaustion(
			ctx,
			claim,
			exhaustions,
			exhaustion,
		)
	}

	return r.handleClaimToPoolBinding(ctx, pool, claim)
}

func (r *resourcePoolController) canClaimWithinNamespace(
	log logr.Logger,
	pool *capsulev1beta2.ResourcePool,
	claim *capsulev1beta2.ResourcePoolClaim,
) (res map[string]api.PoolExhaustionResource) {
	claimable := pool.GetAvailableClaimableResources()
	log.V(5).Info("claimable resources", "claimable", claimable)

	_, namespaceClaimed := pool.GetNamespaceClaims(claim.Namespace)
	log.V(5).Info("namespace claimed resources", "claimed", namespaceClaimed)

	res = make(map[string]api.PoolExhaustionResource)

	for resourceName, req := range claim.Spec.ResourceClaims {
		// Verify if total Quota is available
		available, exists := claimable[resourceName]
		if !exists || available.IsZero() || available.Cmp(req) < 0 {
			log.V(5).Info("not enough resources available", "available", available, "requesting", req)

			res[resourceName.String()] = api.PoolExhaustionResource{
				Available:  available,
				Requesting: req,
			}

			continue
		}
	}

	return res
}

// Handles exhaustions when a exhaustion was already declared in the given map.
func (r *resourcePoolController) handleClaimOrderedExhaustion(
	ctx context.Context,
	claim *capsulev1beta2.ResourcePoolClaim,
	exhaustions map[string]api.PoolExhaustionResource,
) (queued bool, err error) {
	status := make([]string, 0)

	for resourceName, qt := range claim.Spec.ResourceClaims {
		req, ok := exhaustions[resourceName.String()]
		if !ok {
			continue
		}

		line := fmt.Sprintf(
			"requested: %s=%s, queued: %s=%s",
			resourceName,
			qt.String(),
			resourceName,
			req.Requesting.String(),
		)
		status = append(status, line)
	}

	if len(status) != 0 {
		queued = true

		cond := meta.NewBoundCondition(claim)
		cond.Status = metav1.ConditionFalse
		cond.Reason = meta.QueueExhaustedReason
		cond.Message = strings.Join(status, "; ")

		return queued, updateStatusAndEmitEvent(ctx, r.Client, r.recorder, claim, cond)
	}

	return queued, err
}

func (r *resourcePoolController) handleClaimResourceExhaustion(
	ctx context.Context,
	claim *capsulev1beta2.ResourcePoolClaim,
	currentExhaustions map[string]api.PoolExhaustionResource,
	exhaustions map[string]api.PoolExhaustionResource,
) (err error) {
	status := make([]string, 0)

	resourceNames := make([]string, 0)
	for resourceName := range currentExhaustions {
		resourceNames = append(resourceNames, resourceName)
	}

	sort.Strings(resourceNames)

	for _, resourceName := range resourceNames {
		ex := currentExhaustions[resourceName]

		ext, ok := exhaustions[resourceName]
		if ok {
			ext.Requesting.Add(ex.Requesting)
			exhaustions[resourceName] = ext
		} else {
			exhaustions[resourceName] = ex
		}

		line := fmt.Sprintf(
			"requested: %s=%s, available: %s=%s",
			resourceName,
			ex.Requesting.String(),
			resourceName,
			ex.Available.String(),
		)

		status = append(status, line)
	}

	if len(status) != 0 {
		cond := meta.NewBoundCondition(claim)
		cond.Status = metav1.ConditionFalse
		cond.Reason = meta.PoolExhaustedReason
		cond.Message = strings.Join(status, "; ")

		return updateStatusAndEmitEvent(ctx, r.Client, r.recorder, claim, cond)
	}

	return err
}

func (r *resourcePoolController) handleClaimToPoolBinding(
	ctx context.Context,
	pool *capsulev1beta2.ResourcePool,
	claim *capsulev1beta2.ResourcePoolClaim,
) (err error) {
	cond := meta.NewBoundCondition(claim)
	cond.Status = metav1.ConditionTrue
	cond.Reason = meta.SucceededReason
	cond.Message = "Claimed resources"

	if err = updateStatusAndEmitEvent(ctx, r.Client, r.recorder, claim, cond); err != nil {
		return err
	}

	pool.AddClaimToStatus(claim)

	return err
}

// Attempts to garbage collect a ResourceQuota resource.
func (r *resourcePoolController) handleClaimDisassociation(
	ctx context.Context,
	log logr.Logger,
	pool *capsulev1beta2.ResourcePool,
	claim *capsulev1beta2.ResourcePoolClaimsItem,
) error {
	current := &capsulev1beta2.ResourcePoolClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      claim.Name.String(),
			Namespace: claim.Namespace.String(),
			UID:       claim.UID,
		},
	}

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := r.Get(ctx, types.NamespacedName{
			Name:      claim.Name.String(),
			Namespace: claim.Namespace.String(),
		}, current); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return fmt.Errorf("failed to refetch claim before patch: %w", err)
		}

		if !*pool.Spec.Config.DeleteBoundResources || meta.ReleaseAnnotationTriggers(current) {
			patch := client.MergeFrom(current.DeepCopy())
			meta.RemoveLooseOwnerReference(current, meta.GetLooseOwnerReference(pool))
			meta.ReleaseAnnotationRemove(current)

			if err := r.Patch(ctx, current, patch); err != nil {
				return fmt.Errorf("failed to patch claim: %w", err)
			}
		}

		current.Status.Pool = api.StatusNameUID{}
		if err := r.Client.Status().Update(ctx, current); err != nil {
			return fmt.Errorf("failed to update claim status: %w", err)
		}

		r.recorder.AnnotatedEventf(
			current,
			map[string]string{
				"Status": string(metav1.ConditionFalse),
				"Type":   meta.NotReadyCondition,
			},
			corev1.EventTypeNormal,
			"Disassociated",
			"Claim is disassociated from the pool",
		)

		return nil
	})
	if err != nil {
		log.V(3).Info("Removing owner reference failed", "claim", current.Name, "pool", pool.Name, "error", err)

		return err
	}

	pool.RemoveClaimFromStatus(current)

	return nil
}

// Synchronize resources quotas in all the given namespaces (routines).
func (r *resourcePoolController) syncResourceQuotas(
	ctx context.Context,
	c client.Client,
	quota *capsulev1beta2.ResourcePool,
	namespaces []corev1.Namespace,
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
	namespace corev1.Namespace,
) (err error) {
	// getting ResourceQuota labels for the mutateFn
	var quotaLabel string

	if quotaLabel, err = utils.GetTypeLabel(&capsulev1beta2.ResourcePool{}); err != nil {
		return err
	}

	target := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pool.GetQuotaName(),
			Namespace: namespace.GetName(),
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
			target.Spec.Hard = pool.GetResourceQuotaHardResources(namespace.GetName())

			return controllerutil.SetControllerReference(pool, target, c.Scheme())
		})

		return retryErr
	})
	if err != nil {
		return err
	}

	return nil
}

// Handles new allocated resources before they are passed on to the pool itself.
// It does not verify the same stuff, as the admission for resourcepools.
func (r *resourcePoolController) handlePoolHardResources(pool *capsulev1beta2.ResourcePool) {
	if &pool.Status.Allocation.Hard != &pool.Spec.Quota.Hard {
		for resourceName := range pool.Status.Allocation.Hard {
			if _, ok := pool.Spec.Quota.Hard[resourceName]; !ok {
				r.metrics.DeleteResourcePoolSingleResourceMetric(pool.Name, resourceName.String())
			}
		}
	}

	pool.Status.Allocation.Hard = pool.Spec.Quota.Hard
}

// Get Currently selected namespaces for the resourcepool.
func (r *resourcePoolController) gatherMatchingNamespaces(
	ctx context.Context,
	log logr.Logger,
	pool *capsulev1beta2.ResourcePool,
) (namespaces []corev1.Namespace, err error) {
	// Collect Namespaces (Matching)
	namespaces = make([]corev1.Namespace, 0)
	seenNamespaces := make(map[string]struct{})

	if !pool.DeletionTimestamp.IsZero() {
		return namespaces, err
	}

	for _, selector := range pool.Spec.Selectors {
		selected, serr := selector.GetMatchingNamespaces(ctx, r.Client)
		if serr != nil {
			log.Error(err, "Cannot get matching namespaces")

			continue
		}

		for _, ns := range selected {
			if !ns.DeletionTimestamp.IsZero() {
				continue
			}

			if _, exists := seenNamespaces[ns.Name]; exists {
				continue
			}

			seenNamespaces[ns.Name] = struct{}{}

			namespaces = append(namespaces, ns)
		}
	}

	return namespaces, err
}

// Get Currently selected claims for the resourcepool.
func (r *resourcePoolController) gatherMatchingClaims(
	ctx context.Context,
	log logr.Logger,
	pool *capsulev1beta2.ResourcePool,
	namespaces map[string]struct{},
) (claims []capsulev1beta2.ResourcePoolClaim, err error) {
	if !pool.DeletionTimestamp.IsZero() {
		return claims, err
	}

	claimList := &capsulev1beta2.ResourcePoolClaimList{}
	if err := r.List(ctx, claimList, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".status.pool.uid", string(pool.GetUID())),
	}); err != nil {
		log.Error(err, "failed to list ResourceQuotaClaims")

		return claims, err
	}

	filteredClaims := make([]capsulev1beta2.ResourcePoolClaim, 0)

	for _, claim := range claimList.Items {
		if meta.ReleaseAnnotationTriggers(&claim) {
			continue
		}

		if _, ok := namespaces[claim.Namespace]; !ok {
			continue
		}

		filteredClaims = append(filteredClaims, claim)
	}

	// Sort by creation timestamp (oldest first)
	sort.Slice(filteredClaims, func(i, j int) bool {
		a := filteredClaims[i]
		b := filteredClaims[j]

		// First, sort by CreationTimestamp
		if !a.CreationTimestamp.Equal(&b.CreationTimestamp) {
			return a.CreationTimestamp.Before(&b.CreationTimestamp)
		}

		// Tiebreaker: use name as a stable secondary sort - If CreationTimestamp is equal
		// (e.g., when two claims are created at the same time in Gitops environments or CI/CD pipelines)
		if a.Name != b.Name {
			return a.Name < b.Name
		}

		return a.Namespace < b.Namespace
	})

	return filteredClaims, nil
}

// Attempts to garbage collect a ResourceQuota resource.
func (r *resourcePoolController) garbageCollection(
	ctx context.Context,
	log logr.Logger,
	pool *capsulev1beta2.ResourcePool,
	claims []capsulev1beta2.ResourcePoolClaim,
	namespaces map[string]struct{},
) error {
	activeClaims := make(map[string]struct{}, len(claims))
	for _, claim := range claims {
		activeClaims[string(claim.UID)] = struct{}{}
	}

	log.V(5).Info("available items", "namespaces", namespaces, "claims", activeClaims)

	namespaceMarkedForGC := make(map[string]bool, len(pool.Status.Namespaces))

	for _, ns := range pool.Status.Namespaces {
		_, exists := namespaces[ns]
		if !exists {
			log.V(5).Info("garbage collecting namespace", "namespace", ns)

			namespaceMarkedForGC[ns] = true

			if err := r.garbageCollectNamespace(ctx, pool, ns); err != nil {
				r.log.Error(err, "Failed to garbage collect resource quota", "namespace", ns)

				return err
			}
		}
	}

	// Garbage collect namespaces which no longer match selector
	for ns, clms := range pool.Status.Claims {
		nsMarked := namespaceMarkedForGC[ns]

		for _, cl := range clms {
			_, claimActive := activeClaims[string(cl.UID)]

			if nsMarked || !claimActive {
				log.V(5).Info("Disassociating claim", "claim", cl.Name, "namespace", ns, "uid", cl.UID, "nsGC", nsMarked, "claimGC", claimActive)

				cl.Namespace = api.Name(ns)
				if err := r.handleClaimDisassociation(ctx, log, pool, cl); err != nil {
					r.log.Error(err, "Failed to disassociate claim", "namespace", ns, "uid", cl.UID)

					return err
				}
			}
		}

		if nsMarked || len(pool.Status.Claims[ns]) == 0 {
			delete(pool.Status.Claims, ns)
		}
	}

	// We can recalculate the usage in the end
	// Since it's only going to decrease
	pool.CalculateClaimedResources()

	return nil
}

// Attempts to garbage collect a ResourceQuota resource.
func (r *resourcePoolController) garbageCollectNamespace(
	ctx context.Context,
	pool *capsulev1beta2.ResourcePool,
	namespace string,
) error {
	r.metrics.DeleteResourcePoolNamespaceMetric(pool.Name, namespace)

	// Check if the namespace still exists
	ns := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
		if apierrors.IsNotFound(err) {
			r.log.V(5).Info("Namespace does not exist, skipping garbage collection", "namespace", namespace)

			return nil
		}

		return fmt.Errorf("failed to check namespace existence: %w", err)
	}

	name := pool.GetQuotaName()

	// Attempt to delete the ResourceQuota
	target := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: target.GetName()}, target)
	if err != nil {
		if apierrors.IsNotFound(err) {
			r.log.V(5).Info("ResourceQuota already deleted", "namespace", namespace, "name", name)

			return nil
		}

		return err
	}

	// Delete the ResourceQuota
	if err := r.Delete(ctx, target); err != nil {
		return fmt.Errorf("failed to delete ResourceQuota %s in namespace %s: %w", name, namespace, err)
	}

	return nil
}
