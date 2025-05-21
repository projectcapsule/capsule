// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
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

//nolint:nakedret
func (r *Manager) syncResourceQuotas(ctx context.Context, tenant *capsulev1beta2.Tenant) (err error) { //nolint:gocognit
	// getting ResourceQuota labels for the mutateFn
	var tenantLabel, typeLabel string

	if tenantLabel, err = utils.GetTypeLabel(&capsulev1beta2.Tenant{}); err != nil {
		return err
	}

	if typeLabel, err = utils.GetTypeLabel(&corev1.ResourceQuota{}); err != nil {
		return err
	}

	// Remove prior metrics, to avoid cleaning up for metrics of deleted ResourceQuotas
	metrics.TenantResourceUsage.DeletePartialMatch(map[string]string{"tenant": tenant.Name})
	metrics.TenantResourceLimit.DeletePartialMatch(map[string]string{"tenant": tenant.Name})

	// Expose the namespace quota and usage as metrics for the tenant
	metrics.TenantResourceUsage.WithLabelValues(tenant.Name, "namespaces", "").Set(float64(tenant.Status.Size))

	if tenant.Spec.NamespaceOptions != nil && tenant.Spec.NamespaceOptions.Quota != nil {
		metrics.TenantResourceLimit.WithLabelValues(tenant.Name, "namespaces", "").Set(float64(*tenant.Spec.NamespaceOptions.Quota))
	}

	//nolint:nestif
	if tenant.Spec.ResourceQuota.Scope == api.ResourceQuotaScopeTenant {
		group := new(errgroup.Group)

		for i, q := range tenant.Spec.ResourceQuota.Items {
			index, resourceQuota := i, q

			toKeep := sets.New[corev1.ResourceName]()
			for k := range resourceQuota.Hard {
				toKeep.Insert(k)
			}

			group.Go(func() (scopeErr error) {
				// Calculating the Resource Budget at Tenant scope just if this is put in place.
				// Requirement to list ResourceQuota of the current Tenant
				var tntRequirement *labels.Requirement

				if tntRequirement, scopeErr = labels.NewRequirement(tenantLabel, selection.Equals, []string{tenant.Name}); scopeErr != nil {
					r.Log.Error(scopeErr, "Cannot build ResourceQuota Tenant requirement")
				}
				// Requirement to list ResourceQuota for the current index
				var indexRequirement *labels.Requirement

				if indexRequirement, scopeErr = labels.NewRequirement(typeLabel, selection.Equals, []string{strconv.Itoa(index)}); scopeErr != nil {
					r.Log.Error(scopeErr, "Cannot build ResourceQuota index requirement")
				}
				// Listing all the ResourceQuota according to the said requirements.
				// These are required since Capsule is going to sum all the used quota to
				// sum them and get the Tenant one.
				list := &corev1.ResourceQuotaList{}
				if scopeErr = r.List(ctx, list, &client.ListOptions{LabelSelector: labels.NewSelector().Add(*tntRequirement).Add(*indexRequirement)}); scopeErr != nil {
					r.Log.Error(scopeErr, "Cannot list ResourceQuota", "tenantFilter", tntRequirement.String(), "indexFilter", indexRequirement.String())

					return scopeErr
				}
				// Iterating over all the options declared for the ResourceQuota,
				// summing all the used quota across different Namespaces to determinate
				// if we're hitting a Hard quota at Tenant level.
				// For this case, we're going to block the Quota setting the Hard as the
				// used one.
				for name, hardQuota := range resourceQuota.Hard {
					r.Log.Info("Desired hard " + name.String() + " quota is " + hardQuota.String())

					// Getting the whole usage across all the Tenant Namespaces
					var quantity resource.Quantity
					for _, item := range list.Items {
						quantity.Add(item.Status.Used[name])
					}

					r.Log.Info("Computed " + name.String() + " quota for the whole Tenant is " + quantity.String())

					// Expose usage and limit metrics for the resource (name) of the ResourceQuota (index)
					metrics.TenantResourceUsage.WithLabelValues(
						tenant.Name,
						name.String(),
						strconv.Itoa(index),
					).Set(float64(quantity.MilliValue()) / 1000)

					metrics.TenantResourceLimit.WithLabelValues(
						tenant.Name,
						name.String(),
						strconv.Itoa(index),
					).Set(float64(hardQuota.MilliValue()) / 1000)

					switch quantity.Cmp(resourceQuota.Hard[name]) {
					case 0:
						// The Tenant is matching exactly the Quota:
						// falling through next case since we have to block further
						// resource allocations.
						fallthrough
					case 1:
						// The Tenant is OverQuota:
						// updating all the related ResourceQuota with the current
						// used Quota to block further creations.
						for item := range list.Items {
							if _, ok := list.Items[item].Status.Used[name]; ok {
								list.Items[item].Spec.Hard[name] = list.Items[item].Status.Used[name]
							} else {
								um := make(map[corev1.ResourceName]resource.Quantity)
								um[name] = resource.Quantity{}
								list.Items[item].Spec.Hard = um
							}
						}
					default:
						// The Tenant is respecting the Hard quota:
						// restoring the default one for all the elements,
						// also for the reconciled one.
						for item := range list.Items {
							if list.Items[item].Spec.Hard == nil {
								list.Items[item].Spec.Hard = map[corev1.ResourceName]resource.Quantity{}
							}

							// Effectively this subtracts the usage from all other namespaces in the tenant from the desired tenant hard quota.
							// Thus we can determine, how much is left in this resourcequota (item) for the current resource (name).
							// We use this remaining quota at the tenant level, to update the hard quota for the current namespace.

							newHard := hardQuota                            // start off with desired tenant wide hard quota
							newHard.Sub(quantity)                           // subtract tenant wide usage
							newHard.Add(list.Items[item].Status.Used[name]) // add back usage in current ns

							list.Items[item].Spec.Hard[name] = newHard

							for k := range list.Items[item].Spec.Hard {
								if !toKeep.Has(k) {
									delete(list.Items[item].Spec.Hard, k)
								}
							}
						}
					}

					if scopeErr = r.resourceQuotasUpdate(ctx, name, quantity, toKeep, resourceQuota.Hard[name], list.Items...); scopeErr != nil {
						r.Log.Error(scopeErr, "cannot proceed with outer ResourceQuota")

						return
					}
				}

				return
			})
		}
		// Waiting the update of all ResourceQuotas
		if err = group.Wait(); err != nil {
			return
		}
	}
	// getting requested ResourceQuota keys
	keys := make([]string, 0, len(tenant.Spec.ResourceQuota.Items))

	for i := range tenant.Spec.ResourceQuota.Items {
		keys = append(keys, strconv.Itoa(i))
	}

	group := new(errgroup.Group)

	for _, ns := range tenant.Status.Namespaces {
		namespace := ns

		group.Go(func() error {
			return r.syncResourceQuota(ctx, tenant, namespace, keys)
		})
	}

	return group.Wait()
}

//nolint:nakedret
func (r *Manager) syncResourceQuota(ctx context.Context, tenant *capsulev1beta2.Tenant, namespace string, keys []string) (err error) {
	// getting ResourceQuota labels for the mutateFn
	var tenantLabel, typeLabel string

	if tenantLabel, err = utils.GetTypeLabel(&capsulev1beta2.Tenant{}); err != nil {
		return err
	}

	if typeLabel, err = utils.GetTypeLabel(&corev1.ResourceQuota{}); err != nil {
		return err
	}
	// Pruning resource of non-requested resources
	if err = r.pruningResources(ctx, namespace, keys, &corev1.ResourceQuota{}); err != nil {
		return err
	}

	for index, resQuota := range tenant.Spec.ResourceQuota.Items {
		target := &corev1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("capsule-%s-%d", tenant.Name, index),
				Namespace: namespace,
			},
		}

		var res controllerutil.OperationResult

		err = retry.RetryOnConflict(retry.DefaultBackoff, func() (retryErr error) {
			res, retryErr = controllerutil.CreateOrUpdate(ctx, r.Client, target, func() (err error) {
				targetLabels := target.GetLabels()
				if targetLabels == nil {
					targetLabels = map[string]string{}
				}

				targetLabels[tenantLabel] = tenant.Name
				targetLabels[typeLabel] = strconv.Itoa(index)

				target.SetLabels(targetLabels)
				target.Spec.Scopes = resQuota.Scopes
				target.Spec.ScopeSelector = resQuota.ScopeSelector

				// In case of Namespace scope for the ResourceQuota we can easily apply the bare specification
				if tenant.Spec.ResourceQuota.Scope == api.ResourceQuotaScopeNamespace {
					target.Spec.Hard = resQuota.Hard
				}

				return controllerutil.SetControllerReference(tenant, target, r.Scheme())
			})

			return retryErr
		})

		r.emitEvent(tenant, target.GetNamespace(), res, fmt.Sprintf("Ensuring ResourceQuota %s", target.GetName()), err)

		r.Log.Info("Resource Quota sync result: "+string(res), "name", target.Name, "namespace", target.Namespace)

		if err != nil {
			return
		}
	}

	return nil
}

// Serial ResourceQuota processing is expensive: using Go routines we can speed it up.
// In case of multiple errors these are logged properly, returning a generic error since we have to repush back the
// reconciliation loop.
func (r *Manager) resourceQuotasUpdate(ctx context.Context, resourceName corev1.ResourceName, actual resource.Quantity, toKeep sets.Set[corev1.ResourceName], limit resource.Quantity, list ...corev1.ResourceQuota) (err error) {
	group := new(errgroup.Group)

	annotationsToKeep := sets.New[string]()

	for _, item := range toKeep.UnsortedList() {
		if v, vErr := capsulev1beta2.UsedQuotaFor(item); vErr == nil {
			annotationsToKeep.Insert(v)
		}

		if v, vErr := capsulev1beta2.HardQuotaFor(item); vErr == nil {
			annotationsToKeep.Insert(v)
		}
	}

	for _, item := range list {
		rq := item

		group.Go(func() (err error) {
			found := &corev1.ResourceQuota{}
			if err = r.Get(ctx, types.NamespacedName{Namespace: rq.Namespace, Name: rq.Name}, found); err != nil {
				return
			}

			return retry.RetryOnConflict(retry.DefaultBackoff, func() (retryErr error) {
				_, retryErr = controllerutil.CreateOrUpdate(ctx, r.Client, found, func() error {
					// Ensuring annotation map is there to avoid uninitialized map error and
					// assigning the overall usage
					if found.Annotations == nil {
						found.Annotations = make(map[string]string)
					}
					// Pruning the Capsule quota annotations:
					// if the ResourceQuota is updated by removing some objects,
					// we could still have left-overs which could be misleading.
					// This will not lead to a reconciliation loop since the whole code is idempotent.
					for k := range found.Annotations {
						if (strings.HasPrefix(k, capsulev1beta2.HardCapsuleQuotaAnnotation) || strings.HasPrefix(k, capsulev1beta2.UsedCapsuleQuotaAnnotation)) && !annotationsToKeep.Has(k) {
							delete(found.Annotations, k)
						}
					}

					found.Labels = rq.Labels
					if actualKey, keyErr := capsulev1beta2.UsedQuotaFor(resourceName); keyErr == nil {
						found.Annotations[actualKey] = actual.String()
					}

					if limitKey, keyErr := capsulev1beta2.HardQuotaFor(resourceName); keyErr == nil {
						found.Annotations[limitKey] = limit.String()
					}
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
