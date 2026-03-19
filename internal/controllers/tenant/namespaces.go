// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"maps"

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
func (r *Manager) reconcileNamespaces(ctx context.Context, tnt *capsulev1beta2.Tenant) (err error) {
	// Issue Cascading delete if tenant is being removed
	if tnt.DeletionTimestamp != nil {
		for _, ns := range tnt.Status.Spaces {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ns.Name,
				},
			}

			if err := r.Delete(ctx, ns, &client.DeleteOptions{
				PropagationPolicy: ptr.To(metav1.DeletePropagationBackground),
			}); err != nil && !apierrors.IsNotFound(err) {
				r.Log.V(1).Error(err, "unable to delete namespace", "namespace", ns.Name)
			}
		}
	}

	list := &corev1.NamespaceList{}
	if err := r.List(ctx, list, client.MatchingFields{".metadata.ownerReferences[*].capsule": tnt.GetName()}); err != nil {
		return err
	}

	oldStatus := make(map[string]struct{}, len(tnt.Status.Spaces))
	for i := range tnt.Status.Spaces {
		oldStatus[tnt.Status.Spaces[i].Name] = struct{}{}
	}

	active := map[string]*corev1.Namespace{}
	for _, item := range list.Items {
		active[item.GetName()] = &item
	}

	group := new(errgroup.Group)

	results := make(chan *capsulev1beta2.TenantStatusNamespaceItem, len(list.Items))

	for i := range list.Items {
		ns := list.Items[i]

		group.Go(func() error {
			stat, err := r.reconcileNamespace(ctx, &ns, tnt)
			if stat != nil {
				results <- stat
			}

			return err
		})
	}

	if err = group.Wait(); err != nil {
		err = fmt.Errorf("cannot sync Namespaces: %w", err)
	}

	close(results)

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

func (r *Manager) reconcileNamespace(ctx context.Context, namespace *corev1.Namespace, tnt *capsulev1beta2.Tenant) (
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

	// Always update tenant status condition after reconciliation
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

	// Verify if namespace is still active or terminating
	cleanup := false
	if namespace.DeletionTimestamp != nil {
		terminating = true

		terminatingState := meta.NewTerminatingConditionReason(namespace)

		pending, err := tenant.NamespaceIsPendingPodTerminating(ctx, r.Client, namespace)
		if err != nil {
			terminatingState.Reason = meta.FailedReason
			terminatingState.Status = metav1.ConditionFalse
			terminatingState.Message = err.Error()

			return stat, err
		}

		if pending {
			terminatingState.Reason = meta.PendingUnmanagedContentReason
			terminatingState.Status = metav1.ConditionFalse
			terminatingState.Message = "waiting for pods to finalize"

			stat.Conditions.UpdateConditionByType(terminatingState)

			return stat, nil
		}

		// Initiate Cascading Cleanup
		cleaned, err := tenant.NamespacedCascadingCleanup(ctx, r.Client, r.DiscoveryClient, r.DynamicClient, namespace)
		if err != nil {
			terminatingState.Reason = meta.FailedReason
			terminatingState.Status = metav1.ConditionFalse
			terminatingState.Message = err.Error()

			return stat, err
		}

		if cleaned {
			terminatingState.Reason = meta.PendingUnmanagedContentReason
			terminatingState.Status = metav1.ConditionFalse
			terminatingState.Message = "performing cascading deletion"

			stat.Conditions.UpdateConditionByType(terminatingState)

			return stat, nil
		}

		terminatingState.Message = "removed managed resources"

		stat.Conditions.UpdateConditionByType(terminatingState)

		cleanup = true
	}

	if !cleanup {
		// Collect Rules for namespace
		ruleBody, err := tenant.BuildNamespaceRuleBodyForNamespace(namespace, tnt)
		if err != nil {
			return stat, err
		}

		err = r.ensureRuleStatus(ctx, namespace, tnt, ruleBody, namespace.GetName())
		if err != nil {
			return stat, err
		}

	} else {
		obj := &capsulev1beta2.RuleStatus{
			ObjectMeta: metav1.ObjectMeta{
				Name:      meta.NameForManagedRuleStatus(),
				Namespace: namespace.GetName(),
			},
		}

		err := r.Client.Delete(ctx, obj)
		if apierrors.IsNotFound(err) {
			err = nil
		}

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

func (r *Manager) ensureRuleStatus(
	ctx context.Context,
	ns *corev1.Namespace,
	tnt *capsulev1beta2.Tenant,
	rule *capsulev1beta2.NamespaceRuleBody,
	namespace string,
) error {
	nsStatus := &capsulev1beta2.RuleStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      meta.NameForManagedRuleStatus(),
			Namespace: namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, nsStatus, func() error {
		labels := nsStatus.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}

		labels[meta.NewManagedByCapsuleLabel] = meta.ValueController
		labels[meta.CapsuleNameLabel] = nsStatus.Name

		nsStatus.SetLabels(labels)

		err := controllerutil.SetOwnerReference(tnt, nsStatus, r.Scheme())
		if err != nil {
			return err
		}

		return controllerutil.SetOwnerReference(ns, nsStatus, r.Scheme())
	})
	if err != nil {
		return err
	}

	nsStatus.Status.Rule = *rule

	if err := r.Status().Update(ctx, nsStatus); err != nil {
		return err
	}

	return nil
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

	// Handle User-Defined Metadata, if allowed
	if !managedMetadataOnly {
		if originLabels != nil {
			maps.Copy(originLabels, managedLabels)
		}

		if originAnnotations != nil {
			maps.Copy(originAnnotations, managedAnnotations)
		}

		// Cleanup old Metadata
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
