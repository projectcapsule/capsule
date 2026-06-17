// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

// Ensuring all annotations are applied to each Namespace handled by the Tenant.
func (r *Manager) reconcileNamespaces(
	ctx context.Context,
	log logr.Logger,
	tnt *capsulev1beta2.Tenant,
) error {
	if tnt.DeletionTimestamp != nil {
		return r.reconcileDeletingTenantNamespaces(ctx, log, tnt)
	}

	return r.reconcileActiveTenantNamespaces(ctx, log, tnt)
}

func (r *Manager) reconcileDeletingTenantNamespaces(
	ctx context.Context,
	log logr.Logger,
	tnt *capsulev1beta2.Tenant,
) error {
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(8)

	results := make(chan *capsulev1beta2.TenantStatusNamespaceItem, len(tnt.Status.Spaces))
	removed := make(chan string, len(tnt.Status.Spaces))
	errs := make(chan error, len(tnt.Status.Spaces))

	for i := range tnt.Status.Spaces {
		statusNamespace := tnt.Status.Spaces[i]

		group.Go(func() error {
			namespace := &corev1.Namespace{}

			err := r.Get(ctx, client.ObjectKey{Name: statusNamespace.Name}, namespace)
			if apierrors.IsNotFound(err) {
				removed <- statusNamespace.Name

				return nil
			}

			if err != nil {
				errs <- fmt.Errorf("get namespace %q: %w", statusNamespace.Name, err)

				return nil
			}

			if namespace.DeletionTimestamp == nil {
				if err := r.Delete(ctx, namespace, &client.DeleteOptions{
					PropagationPolicy: ptr.To(metav1.DeletePropagationBackground),
				}); err != nil && !apierrors.IsNotFound(err) {
					log.Error(err, "unable to delete tenant namespace",
						"tenant", tnt.GetName(),
						"namespace", namespace.GetName(),
					)

					errs <- fmt.Errorf("delete namespace %q: %w", namespace.Name, err)

					return nil
				}

				latest := &corev1.Namespace{}

				err := r.Get(ctx, client.ObjectKey{Name: namespace.Name}, latest)
				if apierrors.IsNotFound(err) {
					removed <- statusNamespace.Name

					return nil
				}

				if err != nil {
					errs <- fmt.Errorf("get namespace %q after delete: %w", namespace.Name, err)

					return nil
				}

				namespace = latest
			}

			stat, err := r.reconcileNamespace(ctx, log, namespace, tnt)
			if stat != nil {
				results <- stat
			}

			if err != nil {
				log.Error(err, "failed to reconcile deleting namespace",
					"tenant", tnt.GetName(),
					"namespace", namespace.GetName(),
				)

				errs <- fmt.Errorf("namespace %q: %w", namespace.Name, err)
			}

			return nil
		})
	}

	_ = group.Wait()

	close(results)
	close(removed)
	close(errs)

	for name := range removed {
		r.Metrics.DeleteAllMetricsForNamespace(name)

		tnt.Status.RemoveInstance(&capsulev1beta2.TenantStatusNamespaceItem{
			Name: name,
		})
	}

	for stat := range results {
		if stat == nil {
			continue
		}

		tnt.Status.UpdateInstance(stat)
	}

	var joined []error
	for itemErr := range errs {
		joined = append(joined, itemErr)
	}

	tnt.Status.Size = uint(len(tnt.Status.Spaces))

	return errors.Join(joined...)
}

func (r *Manager) reconcileActiveTenantNamespaces(
	ctx context.Context,
	log logr.Logger,
	tnt *capsulev1beta2.Tenant,
) error {
	list := &corev1.NamespaceList{}
	if err := r.List(ctx, list, client.MatchingFields{".metadata.ownerReferences[*].capsule": tnt.GetName()}); err != nil {
		return err
	}

	oldStatus := make(map[string]struct{}, len(tnt.Status.Spaces))
	for i := range tnt.Status.Spaces {
		oldStatus[tnt.Status.Spaces[i].Name] = struct{}{}
	}

	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(8)

	results := make(chan *capsulev1beta2.TenantStatusNamespaceItem, len(list.Items))
	errs := make(chan error, len(list.Items))

	for i := range list.Items {
		ns := list.Items[i].DeepCopy()

		group.Go(func() error {
			stat, err := r.reconcileNamespace(ctx, log, ns, tnt)
			if stat != nil {
				results <- stat
			}

			if err != nil {
				log.Error(err, "failed to reconcile namespace",
					"tenant", tnt.GetName(),
					"namespace", ns.GetName(),
				)

				errs <- fmt.Errorf("namespace %q: %w", ns.Name, err)
			}

			return nil
		})
	}

	_ = group.Wait()

	close(results)
	close(errs)

	var joined []error
	for itemErr := range errs {
		joined = append(joined, itemErr)
	}

	err := errors.Join(joined...)

	desiredStatus := make(map[string]struct{}, len(list.Items))

	for stat := range results {
		if stat == nil {
			continue
		}

		tnt.Status.UpdateInstance(stat)

		desiredStatus[stat.Name] = struct{}{}
	}

	for name := range oldStatus {
		if _, keep := desiredStatus[name]; keep {
			continue
		}

		r.Metrics.DeleteAllMetricsForNamespace(name)

		tnt.Status.RemoveInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: name})
	}

	tnt.Status.Size = uint(len(tnt.Status.Spaces))

	tnt.AssignNamespaces(list.Items)

	return err
}

func (r *Manager) reconcileNamespace(
	ctx context.Context,
	log logr.Logger,
	namespace *corev1.Namespace,
	tnt *capsulev1beta2.Tenant,
) (
	stat *capsulev1beta2.TenantStatusNamespaceItem,
	err error,
) {
	terminating := false

	stat = &capsulev1beta2.TenantStatusNamespaceItem{
		Name: namespace.GetName(),
		UID:  namespace.GetUID(),
	}

	metaStatus := &capsulev1beta2.TenantStatusNamespaceMetadata{}

	instance := tnt.Status.GetInstance(stat)
	if instance != nil {
		stat = instance
	}

	// Always update tenant status condition after reconciliation.
	defer func() {
		readCondition := meta.NewReadyCondition(namespace)

		switch {
		case terminating:
			readCondition.Status = metav1.ConditionFalse
			readCondition.Reason = meta.TerminatingReason
			readCondition.Message = "Namespace is terminating"
		case err != nil:
			readCondition.Status = metav1.ConditionFalse
			readCondition.Reason = meta.FailedReason
			readCondition.Message = fmt.Sprintf("Failed to reconcile: %v", err)

			if instance != nil && instance.Metadata != nil {
				stat.Metadata = instance.Metadata
			}
		default:
			if metaStatus != nil {
				stat.Metadata = metaStatus
			}
		}

		stat.Conditions.UpdateConditionByType(readCondition)

		cordonedCondition := meta.NewCordonedCondition(namespace)

		if namespace.Labels[meta.CordonedLabel] == meta.ValueTrue {
			cordonedCondition.Reason = meta.CordonedReason
			cordonedCondition.Message = "namespace is cordoned"
			cordonedCondition.Status = metav1.ConditionTrue
		}

		stat.Conditions.UpdateConditionByType(cordonedCondition)

		r.syncNamespaceStatusMetrics(tnt, namespace)
	}()

	// Verify if namespace is still active or terminating.
	if namespace.DeletionTimestamp != nil {
		terminating = true

		terminatingState := meta.NewTerminatingConditionReason(namespace)

		pending, err := tenant.NamespaceIsPendingPodTerminating(ctx, r.Client, namespace)
		if err != nil {
			terminatingState.Reason = meta.FailedReason
			terminatingState.Status = metav1.ConditionFalse
			terminatingState.Message = err.Error()
			stat.Conditions.UpdateConditionByType(terminatingState)

			return stat, err
		}

		if pending {
			terminatingState.Reason = meta.PendingUnmanagedContentReason
			terminatingState.Status = metav1.ConditionFalse
			terminatingState.Message = "waiting for pods to finalize"
			stat.Conditions.UpdateConditionByType(terminatingState)

			return stat, nil
		}

		cleaned, err := tenant.NamespacedCascadingCleanup(ctx, r.Client, r.DiscoveryClient, &r.discoveryCache, r.DynamicClient, namespace)
		if err != nil {
			terminatingState.Reason = meta.FailedReason
			terminatingState.Status = metav1.ConditionFalse
			terminatingState.Message = err.Error()
			stat.Conditions.UpdateConditionByType(terminatingState)

			return stat, err
		}

		if cleaned {
			terminatingState.Reason = meta.PendingUnmanagedContentReason
			terminatingState.Status = metav1.ConditionFalse
			terminatingState.Message = "performing cascading deletion"
			stat.Conditions.UpdateConditionByType(terminatingState)

			return stat, nil
		}

		terminatingState.Reason = meta.TerminatingReason
		terminatingState.Status = metav1.ConditionFalse
		terminatingState.Message = "waiting for namespace finalization"
		stat.Conditions.UpdateConditionByType(terminatingState)

		return stat, nil
	}

	// Collect Rules for namespace.
	err = r.reconcileRuleStatus(ctx, log, tnt, namespace)
	if err != nil {
		return stat, err
	}

	err = retry.RetryOnConflict(retry.DefaultBackoff, func() (conflictErr error) {
		_, conflictErr = controllerutil.CreateOrUpdate(ctx, r.Client, namespace, func() error {
			metaStatus, err = r.reconcileNamespaceMetadata(ctx, namespace, tnt, stat)

			return err
		})

		return conflictErr
	})

	return stat, err
}

//nolint:nestif
func (r *Manager) reconcileNamespaceMetadata(
	ctx context.Context,
	ns *corev1.Namespace,
	tnt *capsulev1beta2.Tenant,
	stat *capsulev1beta2.TenantStatusNamespaceItem,
) (
	managed *capsulev1beta2.TenantStatusNamespaceMetadata,
	err error,
) {
	originLabels := ns.GetLabels()
	if originLabels == nil {
		originLabels = make(map[string]string)
	}

	originAnnotations := ns.GetAnnotations()
	if originAnnotations == nil {
		originAnnotations = make(map[string]string)
	}

	managedLabels, managedAnnotations, err := tenant.BuildNamespaceMetadataForTenant(ns, tnt)
	if err != nil {
		return nil, err
	}

	managedMetadataOnly := tnt.Spec.NamespaceOptions != nil && tnt.Spec.NamespaceOptions.ManagedMetadataOnly

	// Handle User-Defined Metadata, if allowed.
	if !managedMetadataOnly {
		if originLabels != nil {
			maps.Copy(originLabels, managedLabels)
		}

		if originAnnotations != nil {
			maps.Copy(originAnnotations, managedAnnotations)
		}

		// Cleanup old Metadata.
		instance := tnt.Status.GetInstance(stat)
		if instance != nil && instance.Metadata != nil {
			for label := range instance.Metadata.Labels {
				if _, ok := managedLabels[label]; ok {
					continue
				}

				delete(originLabels, label)
			}

			for annotation := range instance.Metadata.Annotations {
				if _, ok := managedAnnotations[annotation]; ok {
					continue
				}

				delete(originAnnotations, annotation)
			}
		}

		managed = &capsulev1beta2.TenantStatusNamespaceMetadata{
			Labels:      managedLabels,
			Annotations: managedAnnotations,
		}
	} else {
		originLabels = managedLabels
		originAnnotations = managedAnnotations
	}

	tenant.AddNamespaceNameLabels(originLabels, ns)
	tenant.AddTenantNameLabel(originLabels, tnt)

	ns.SetLabels(originLabels)
	ns.SetAnnotations(originAnnotations)

	return managed, err
}
