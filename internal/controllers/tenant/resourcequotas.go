// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	"github.com/projectcapsule/capsule/pkg/api/meta"
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

type tenantResourceQuotaSync struct {
	mutex sync.Mutex
	refs  int
}

func (r *Manager) syncResourceQuotas(ctx context.Context, log logr.Logger, tenant *capsulev1beta2.Tenant) error {
	return r.withTenantResourceQuotaSync(tenant.Name, func() error {
		latest, err := r.latestResourceQuotaTenant(ctx, tenant)
		if err != nil || latest == nil {
			return err
		}

		return r.syncResourceQuotasLocked(ctx, log, latest)
	})
}

func (r *Manager) latestResourceQuotaTenant(
	ctx context.Context,
	tenant *capsulev1beta2.Tenant,
) (*capsulev1beta2.Tenant, error) {
	reader := r.reader
	if reader == nil {
		reader = r.Client
	}

	latest := &capsulev1beta2.Tenant{}
	if err := reader.Get(ctx, client.ObjectKey{Name: tenant.Name}, latest); err != nil {
		if apierrors.IsNotFound(err) {
			r.Metrics.DeleteTenantResourceMetrics(tenant.Name)

			return nil, nil
		}

		return nil, err
	}

	if latest.DeletionTimestamp != nil {
		r.Metrics.DeleteTenantResourceMetrics(tenant.Name)

		return nil, nil
	}

	// Keep the status reconciled in the current Tenant pass, but always use the
	// latest persisted quota-related spec. This prevents an older, slower pass
	// from restoring quota objects or metrics after a newer spec removed them.
	snapshot := tenant.DeepCopy()
	snapshot.Spec.ResourceQuota = *latest.Spec.ResourceQuota.DeepCopy()
	snapshot.Spec.NamespaceOptions = latest.Spec.NamespaceOptions.DeepCopy()

	return snapshot, nil
}

func (r *Manager) withTenantResourceQuotaSync(tenant string, syncFn func() error) error {
	r.resourceQuotaSyncMu.Lock()
	if r.resourceQuotaSyncs == nil {
		r.resourceQuotaSyncs = make(map[string]*tenantResourceQuotaSync)
	}

	lock := r.resourceQuotaSyncs[tenant]
	if lock == nil {
		lock = &tenantResourceQuotaSync{}
		r.resourceQuotaSyncs[tenant] = lock
	}

	lock.refs++
	r.resourceQuotaSyncMu.Unlock()

	lock.mutex.Lock()
	defer func() {
		lock.mutex.Unlock()

		r.resourceQuotaSyncMu.Lock()

		lock.refs--
		if lock.refs == 0 {
			delete(r.resourceQuotaSyncs, tenant)
		}
		r.resourceQuotaSyncMu.Unlock()
	}()

	return syncFn()
}

func (r *Manager) syncResourceQuotasLocked(ctx context.Context, log logr.Logger, tenant *capsulev1beta2.Tenant) (err error) { //nolint:gocognit
	if err := r.prepareResourceQuotaSync(ctx, tenant); err != nil {
		return err
	}

	//nolint:nestif
	if tenant.Spec.ResourceQuota.Scope == api.ResourceQuotaScopeTenant {
		scopeErrs := make(chan error, len(tenant.Spec.ResourceQuota.Items))
		group := new(errgroup.Group)

		for i, q := range tenant.Spec.ResourceQuota.Items {
			index, resourceQuota := i, q

			toKeep := sets.New[corev1.ResourceName]()
			for k := range resourceQuota.Hard {
				toKeep.Insert(k)
			}

			group.Go(func() (scopeErr error) {
				defer func() {
					if scopeErr != nil {
						scopeErrs <- fmt.Errorf("resource quota %d: %w", index, scopeErr)
					}
				}()

				// Calculating the Resource Budget at Tenant scope just if this is put in place.
				// Requirement to list ResourceQuota of the current Tenant
				var tntRequirement *labels.Requirement

				if tntRequirement, scopeErr = labels.NewRequirement(meta.NewTenantLabel, selection.Equals, []string{tenant.Name}); scopeErr != nil {
					log.Error(scopeErr, "cannot build ResourceQuota Tenant requirement")
				}
				// Requirement to list ResourceQuota for the current index
				var indexRequirement *labels.Requirement

				if indexRequirement, scopeErr = labels.NewRequirement(meta.ResourceQuotaLabel, selection.Equals, []string{strconv.Itoa(index)}); scopeErr != nil {
					log.Error(scopeErr, "cannot build ResourceQuota index requirement")
				}
				// Listing all the ResourceQuota according to the said requirements.
				// These are required since Capsule is going to sum all the used quota to
				// sum them and get the Tenant one.
				list := &corev1.ResourceQuotaList{}
				if scopeErr = r.reader.List(ctx, list, &client.ListOptions{LabelSelector: labels.NewSelector().Add(*tntRequirement).Add(*indexRequirement)}); scopeErr != nil {
					log.Error(scopeErr, "cannot list ResourceQuota", "tenantFilter", tntRequirement.String(), "indexFilter", indexRequirement.String())

					return scopeErr
				}

				// Prune removed hard resources independently of their current usage.
				// Previously this only happened in the under-quota branch, leaving
				// stale resources behind when a remaining quota was at or over limit.
				for item := range list.Items {
					for name := range list.Items[item].Spec.Hard {
						if !toKeep.Has(name) {
							delete(list.Items[item].Spec.Hard, name)
						}
					}
				}

				// Iterating over all the options declared for the ResourceQuota,
				// summing all the used quota across different Namespaces to determinate
				// if we're hitting a Hard quota at Tenant level.
				// For this case, we're going to block the Quota setting the Hard as the
				// used one.
				for name, hardQuota := range resourceQuota.Hard {
					log.V(4).Info("desired hard quota", "resource", name.String(), "quantity", hardQuota.String())

					// Getting the whole usage across all the Tenant Namespaces
					var quantity resource.Quantity
					for _, item := range list.Items {
						quantity.Add(item.Status.Used[name])
					}

					log.V(4).Info("computed quota for the whole Tenant", "resource", name.String(), "quantity", quantity.String())

					// Expose usage and limit metrics for the resource (name) of the ResourceQuota (index)
					r.Metrics.TenantResourceUsageGauge.WithLabelValues(
						tenant.Name,
						name.String(),
						strconv.Itoa(index),
					).Set(float64(quantity.MilliValue()) / 1000)

					r.Metrics.TenantResourceLimitGauge.WithLabelValues(
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
						}
					}

					if scopeErr = r.resourceQuotasUpdate(ctx, name, quantity, toKeep, resourceQuota.Hard[name], list.Items...); scopeErr != nil {
						return scopeErr
					}
				}

				// An empty hard map has no resource iteration above, but existing
				// ResourceQuotas still need their old hard values and annotations
				// removed.
				if len(resourceQuota.Hard) == 0 {
					if scopeErr = r.resourceQuotasPrune(ctx, toKeep, list.Items...); scopeErr != nil {
						return scopeErr
					}
				}

				return nil
			})
		}

		_ = group.Wait()

		close(scopeErrs)

		var joined []error
		for scopeErr := range scopeErrs {
			joined = append(joined, scopeErr)
		}

		if err = errors.Join(joined...); err != nil {
			return err
		}
	}

	return runForTenantNamespaces(ctx, tenant, func(ctx context.Context, namespace string) error {
		return r.syncResourceQuota(ctx, log, tenant, namespace)
	})
}

func (r *Manager) prepareResourceQuotaSync(
	ctx context.Context,
	tenant *capsulev1beta2.Tenant,
) error {
	// Metrics are derived from the desired Tenant spec. Clear the previous label
	// set before any API work so a cleanup error cannot leave removed quota
	// entries exported indefinitely.
	r.Metrics.DeleteTenantResourceMetrics(tenant.Name)

	if err := r.runGarbageCollection(ctx, tenant, &corev1.ResourceQuota{}); err != nil {
		return err
	}

	// Expose the namespace quota and usage as metrics for the tenant
	r.Metrics.TenantResourceUsageGauge.WithLabelValues(tenant.Name, "namespaces", "").Set(float64(tenant.Status.Size))

	if tenant.Spec.NamespaceOptions != nil && tenant.Spec.NamespaceOptions.Quota != nil {
		r.Metrics.TenantResourceLimitGauge.WithLabelValues(tenant.Name, "namespaces", "").Set(float64(*tenant.Spec.NamespaceOptions.Quota))
	}

	// Prune removed quota items from every namespace still assigned to the
	// Tenant, including namespaces whose Ready condition is currently false.
	// Readiness must not prevent deletion of obsolete enforcement resources.
	keys := make([]string, 0, len(tenant.Spec.ResourceQuota.Items))
	for i := range tenant.Spec.ResourceQuota.Items {
		keys = append(keys, strconv.Itoa(i))
	}

	namespaces := make([]string, 0, len(tenant.Status.Spaces))
	for _, namespace := range tenant.Status.Spaces {
		namespaces = append(namespaces, namespace.Name)
	}

	if err := runForNamespaces(ctx, namespaces, func(ctx context.Context, namespace string) error {
		return r.pruningResources(ctx, namespace, keys, &corev1.ResourceQuota{})
	}); err != nil {
		return err
	}

	return nil
}

func (r *Manager) syncResourceQuota(ctx context.Context, log logr.Logger, tenant *capsulev1beta2.Tenant, namespace string) (err error) {
	// getting ResourceQuota labels for the mutateFn
	var typeLabel string

	if typeLabel, err = utils.GetTypeLabel(&corev1.ResourceQuota{}); err != nil {
		return err
	}

	for index, resQuota := range tenant.Spec.ResourceQuota.Items {
		target := &corev1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("capsule-%s-%d", tenant.Name, index),
				Namespace: namespace,
			},
		}

		var result controllerutil.OperationResult

		err = retry.RetryOnConflict(retry.DefaultBackoff, func() (retryErr error) {
			result, retryErr = controllerutil.CreateOrUpdate(ctx, r.Client, target, func() (err error) {
				targetLabels := target.GetLabels()
				if targetLabels == nil {
					targetLabels = map[string]string{}
				}

				targetLabels[meta.NewTenantLabel] = tenant.Name
				targetLabels[typeLabel] = strconv.Itoa(index)
				targetLabels[meta.NewManagedByCapsuleLabel] = meta.ValueController

				// Remove Legacy labels
				delete(targetLabels, meta.TenantLabel)

				target.SetLabels(targetLabels)

				target.Spec.Scopes = resQuota.Scopes
				target.Spec.ScopeSelector = resQuota.ScopeSelector

				// In case of Namespace scope for the ResourceQuota we can easily apply the bare specification.
				if tenant.Spec.ResourceQuota.Scope == api.ResourceQuotaScopeNamespace {
					target.Spec.Hard = resQuota.Hard
				}

				return controllerutil.SetControllerReference(tenant, target, r.Scheme())
			})

			return retryErr
		})
		if err != nil {
			if apierrors.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
				log.V(4).Info(
					"skipping ResourceQuota sync because namespace is terminating",
					"name", target.Name,
					"namespace", target.Namespace,
					"tenant", tenant.Name,
				)

				return nil
			}

			return err
		}

		log.V(4).Info("ResourceQuota sync result", "result", result, "name", target.Name, "namespace", target.Namespace)
	}

	return nil
}

// Serial ResourceQuota processing is expensive: using Go routines we can speed it up.
// In case of multiple errors these are logged properly, returning a generic error since we have to repush back the
// reconciliation loop.
func (r *Manager) resourceQuotasUpdate(
	ctx context.Context,
	resourceName corev1.ResourceName,
	actual resource.Quantity,
	toKeep sets.Set[corev1.ResourceName],
	limit resource.Quantity,
	list ...corev1.ResourceQuota,
) (err error) {
	return r.persistResourceQuotaState(ctx, &resourceName, actual, toKeep, limit, list...)
}

func (r *Manager) resourceQuotasPrune(
	ctx context.Context,
	toKeep sets.Set[corev1.ResourceName],
	list ...corev1.ResourceQuota,
) error {
	return r.persistResourceQuotaState(ctx, nil, resource.Quantity{}, toKeep, resource.Quantity{}, list...)
}

func (r *Manager) persistResourceQuotaState(
	ctx context.Context,
	resourceName *corev1.ResourceName,
	actual resource.Quantity,
	toKeep sets.Set[corev1.ResourceName],
	limit resource.Quantity,
	list ...corev1.ResourceQuota,
) (err error) {
	group := new(errgroup.Group)

	reader := r.reader
	if reader == nil {
		reader = r.Client
	}

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
			return retry.RetryOnConflict(retry.DefaultBackoff, func() (retryErr error) {
				found := &corev1.ResourceQuota{}

				key := types.NamespacedName{Namespace: rq.Namespace, Name: rq.Name}

				if retryErr = reader.Get(ctx, key, found); retryErr != nil {
					return retryErr
				}

				before := found.DeepCopy()

				applyResourceQuotaState(found, &rq, resourceName, actual, limit, annotationsToKeep)

				if apiequality.Semantic.DeepEqual(before, found) {
					return nil
				}

				return r.Update(ctx, found)
			})
		})
	}

	if err = group.Wait(); err != nil {
		err = fmt.Errorf("update of outer ResourceQuota items has failed: %w", err)
	}

	return err
}

func applyResourceQuotaState(
	found *corev1.ResourceQuota,
	desired *corev1.ResourceQuota,
	resourceName *corev1.ResourceName,
	actual resource.Quantity,
	limit resource.Quantity,
	annotationsToKeep sets.Set[string],
) {
	if found.Annotations == nil {
		found.Annotations = make(map[string]string)
	}

	// Remove quota annotations for resources no longer present in the Tenant
	// spec before writing the current resource values.
	for key := range found.Annotations {
		quotaAnnotation := strings.HasPrefix(key, capsulev1beta2.HardCapsuleQuotaAnnotation) ||
			strings.HasPrefix(key, capsulev1beta2.UsedCapsuleQuotaAnnotation)
		if quotaAnnotation && !annotationsToKeep.Has(key) {
			delete(found.Annotations, key)
		}
	}

	found.Labels = maps.Clone(desired.Labels)

	if resourceName != nil {
		if actualKey, err := capsulev1beta2.UsedQuotaFor(*resourceName); err == nil {
			found.Annotations[actualKey] = actual.String()
		}

		if limitKey, err := capsulev1beta2.HardQuotaFor(*resourceName); err == nil {
			found.Annotations[limitKey] = limit.String()
		}
	}

	if desired.Spec.Hard == nil {
		found.Spec.Hard = nil

		return
	}

	found.Spec.Hard = desired.Spec.Hard.DeepCopy()
}
