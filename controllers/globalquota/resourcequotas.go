// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package globalquota

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/metrics"
	"github.com/projectcapsule/capsule/pkg/utils"
)

// When the Resource Budget assigned to a Tenant is Tenant-scoped we have to rely on the ResourceQuota resources to
// represent the resource quota for the single Tenant rather than the single Namespace,
// so abusing of this API although its Namespaced scope.
//
// Since a Namespace could take-up all the available resource quota, the Namespace ResourceQuota will be a 1:1 mapping
// to the Tenant one: in first time Capsule is going to sum all the analogous ResourceQuota resources on other Tenant
// namespaces to check if the Tenant quota has been exceeded or not, reusing the native Kubernetes policy putting the
// .Status.Used value as the .Hard value.
// This will trigger following reconciliations but that's ok: the mutateFn will re-use the same business logic, letting
// the mutateFn along with the CreateOrUpdate to don't perform the update since resources are identical.
//
// In case of Namespace-scoped Resource Budget, we're just replicating the resources across all registered Namespaces.

//nolint:nakedret, gocognit
func (r *Manager) syncResourceQuotas(
	ctx context.Context,
	quota *capsulev1beta2.GlobalResourceQuota,
	matchingNamespaces []string,
) (err error) {
	// getting ResourceQuota labels for the mutateFn
	var quotaLabel, typeLabel string

	if quotaLabel, err = utils.GetTypeLabel(&capsulev1beta2.GlobalResourceQuota{}); err != nil {
		return err
	}

	typeLabel = utils.GetGlobalResourceQuotaTypeLabel()

	// Keep original status to verify if we need to change anything
	originalStatus := quota.Status.DeepCopy()

	// Initialize on empty status
	if quota.Status.Quota == nil {
		quota.Status.Quota = make(capsulev1beta2.GlobalResourceQuotaStatusQuota)
	}

	// Process each item (quota index)
	for index, resourceQuota := range quota.Spec.Items {
		// Fetch the latest tenant quota status
		itemUsage, exists := quota.Status.Quota[index]
		if !exists {
			// Initialize Object
			quota.Status.Quota[index] = &corev1.ResourceQuotaStatus{
				Used: corev1.ResourceList{},
				Hard: corev1.ResourceList{},
			}

			itemUsage = &corev1.ResourceQuotaStatus{
				Used: corev1.ResourceList{},
				Hard: resourceQuota.Hard,
			}
		}

		// ✅ Update the Used state in the global quota
		quota.Status.Quota[index] = itemUsage
	}

	// Update the tenant's status with the computed quota information
	// We only want to update the status if we really have to, resulting in less
	// conflicts because the usage status is updated by the webhook
	if !equality.Semantic.DeepEqual(quota.Status, *originalStatus) {
		if err := r.Status().Update(ctx, quota); err != nil {
			r.Log.Info("updating status", "quota", quota.Status)

			r.Log.Error(err, "Failed to update tenant status")

			return err
		}
	}

	// Remove prior metrics, to avoid cleaning up for metrics of deleted ResourceQuotas
	metrics.TenantResourceUsage.DeletePartialMatch(map[string]string{"quota": quota.Name})
	metrics.TenantResourceLimit.DeletePartialMatch(map[string]string{"quota": quota.Name})

	// Remove Quotas which are no longer mentioned in spec
	for existingIndex := range quota.Status.Quota {
		if _, exists := quota.Spec.Items[api.Name(existingIndex)]; !exists {

			r.Log.V(7).Info("Orphaned quota index detected", "quotaIndex", existingIndex)

			for _, ns := range append(matchingNamespaces, quota.Status.Namespaces...) {
				selector := labels.SelectorFromSet(map[string]string{
					quotaLabel: quota.Name,
					typeLabel:  existingIndex.String(),
				})

				r.Log.V(7).Info("Searching for ResourceQuotas to delete", "namespace", ns, "selector", selector.String())

				// Query and delete all ResourceQuotas with matching labels in the namespace
				rqList := &corev1.ResourceQuotaList{}
				if err := r.Client.List(ctx, rqList, &client.ListOptions{
					Namespace:     ns,
					LabelSelector: selector,
				}); err != nil {
					r.Log.Error(err, "Failed to list ResourceQuotas", "namespace", ns, "quotaName", quota.Name, "index", existingIndex)
					return err
				}

				r.Log.V(7).Info("Found ResourceQuotas for deletion", "count", len(rqList.Items), "namespace", ns, "quotaIndex", existingIndex)

				for _, rq := range rqList.Items {
					if err := r.Client.Delete(ctx, &rq); err != nil {
						r.Log.Error(err, "Failed to delete ResourceQuota", "name", rq.Name, "namespace", ns)
						return err
					}

					r.Log.V(7).Info("Deleted orphaned ResourceQuota", "name", rq.Name, "namespace", ns)
				}
			}

			// Only Remove from status if the ResourceQuota has been deleted
			// Remove the orphaned quota from status
			delete(quota.Status.Quota, existingIndex)
			r.Log.Info("Removed orphaned quota from status", "quotaIndex", existingIndex)
		} else {
			r.Log.V(7).Info("no lifecycle", "quotaIndex", existingIndex)
		}
	}

	// Convert matchingNamespaces to a map for quick lookup
	matchingNamespaceSet := make(map[string]struct{}, len(matchingNamespaces))
	for _, ns := range matchingNamespaces {
		matchingNamespaceSet[ns] = struct{}{}
	}

	// Garbage collect namespaces which no longer match selector
	for _, existingNamespace := range quota.Status.Namespaces {
		if _, exists := matchingNamespaceSet[existingNamespace]; !exists {
			if err := r.gcResourceQuotas(ctx, quota, existingNamespace); err != nil {
				r.Log.Error(err, "Failed to garbage collect resource quota", "namespace", existingNamespace)
				return err
			}
		}
	}

	return SyncResourceQuotas(ctx, r.Client, quota, matchingNamespaces)
}

// Synchronize resources quotas in all the given namespaces (routines)
func SyncResourceQuotas(
	ctx context.Context,
	c client.Client,
	quota *capsulev1beta2.GlobalResourceQuota,
	namespaces []string,
) (err error) {
	group := new(errgroup.Group)

	// Sync resource quotas for matching namespaces
	for _, ns := range namespaces {
		namespace := ns

		group.Go(func() error {
			return SyncResourceQuota(ctx, c, quota, namespace)
		})
	}

	return group.Wait()
}

// Synchronize a single resourcequota
//
//nolint:nakedret
func SyncResourceQuota(
	ctx context.Context,
	c client.Client,
	quota *capsulev1beta2.GlobalResourceQuota,
	namespace string,
) (err error) {
	// getting ResourceQuota labels for the mutateFn
	var quotaLabel, typeLabel string

	if quotaLabel, err = utils.GetTypeLabel(&capsulev1beta2.GlobalResourceQuota{}); err != nil {
		return err
	}

	typeLabel = utils.GetGlobalResourceQuotaTypeLabel()

	for index, resQuota := range quota.Spec.Items {
		target := &corev1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ItemObjectName(index, quota),
				Namespace: namespace,
			},
		}

		// Verify if quota is present
		if err := c.Get(ctx, types.NamespacedName{Name: target.Name, Namespace: target.Namespace}, target); err != nil && !apierrors.IsNotFound(err) {
			return err
		}

		err = retry.RetryOnConflict(retry.DefaultBackoff, func() (retryErr error) {
			_, retryErr = controllerutil.CreateOrUpdate(ctx, c, target, func() (err error) {
				targetLabels := target.GetLabels()
				if targetLabels == nil {
					targetLabels = map[string]string{}
				}

				targetLabels[quotaLabel] = quota.Name
				targetLabels[typeLabel] = index.String()

				target.SetLabels(targetLabels)
				target.Spec.Scopes = resQuota.Scopes
				target.Spec.ScopeSelector = resQuota.ScopeSelector

				// Gather what's left in quota
				space, err := quota.GetAggregatedQuotaSpace(index, target.Status.Used)
				if err != nil {
					return err
				}

				// This is important when a resourcequota is newly added (new namespace)
				// We don't want to have a racing condition and wait until the elements are synced to
				// the quota. But we take what's left (or when first namespace then hard 1:1) and assign it.
				// It may be further reduced by the limits reconciler
				target.Spec.Hard = space

				return controllerutil.SetControllerReference(quota, target, c.Scheme())
			})

			return retryErr
		})

		if err != nil {
			return
		}
	}

	return nil
}

// Attempts to garbage collect a ResourceQuota resource.
func (r *Manager) gcResourceQuotas(ctx context.Context, quota *capsulev1beta2.GlobalResourceQuota, namespace string) error {
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
	for index, _ := range quota.Spec.Items {
		target := &corev1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ItemObjectName(index, quota),
				Namespace: namespace,
			},
		}
		err := r.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: target.GetName()}, target)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.V(5).Info("ResourceQuota already deleted", "namespace", namespace, "name", ItemObjectName(index, quota))
				continue
			}
			return err
		}

		// Delete the ResourceQuota
		if err := r.Client.Delete(ctx, target); err != nil {
			return fmt.Errorf("failed to delete ResourceQuota %s in namespace %s: %w", ItemObjectName(index, quota), namespace, err)
		}
	}

	r.Log.Info("Deleted ResourceQuota", "namespace", namespace)
	return nil
}

// Serial ResourceQuota processing is expensive: using Go routines we can speed it up.
// In case of multiple errors these are logged properly, returning a generic error since we have to repush back the
// reconciliation loop.
func (r *Manager) resourceQuotasUpdate(ctx context.Context, resourceName corev1.ResourceName, actual resource.Quantity, toKeep sets.Set[corev1.ResourceName], limit resource.Quantity, list ...corev1.ResourceQuota) (err error) {
	group := new(errgroup.Group)

	for _, item := range list {
		rq := item

		group.Go(func() (err error) {
			found := &corev1.ResourceQuota{}
			if err = r.Get(ctx, types.NamespacedName{Namespace: rq.Namespace, Name: rq.Name}, found); err != nil {
				return
			}

			return retry.RetryOnConflict(retry.DefaultBackoff, func() (retryErr error) {
				_, retryErr = controllerutil.CreateOrUpdate(ctx, r.Client, found, func() error {
					// Updating the Resource according to the actual.Cmp result
					found.Spec.Hard = rq.Spec.Hard

					return nil
				})

				return retryErr
			})
		})
	}

	if err = group.Wait(); err != nil {
		// We had an error and we mark the whole transaction as failed
		// to process it another time according to the Tenant controller back-off factor.
		r.Log.Error(err, "Cannot update outer ResourceQuotas", "resourceName", resourceName.String())
		err = fmt.Errorf("update of outer ResourceQuota items has failed: %w", err)
	}

	return err
}
