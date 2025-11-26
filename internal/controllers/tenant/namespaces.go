// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"maps"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
)

// Ensuring all annotations are applied to each Namespace handled by the Tenant.
func (r *Manager) reconcileNamespaces(ctx context.Context, tenant *capsulev1beta2.Tenant) (err error) {
	if err = r.collectNamespaces(ctx, tenant); err != nil {
		err = fmt.Errorf("cannot collect namespaces: %w", err)

		return err
	}

	gcSet := make(map[string]struct{})
	for _, inst := range tenant.Status.Spaces {
		gcSet[inst.Name] = struct{}{}
	}

	group := new(errgroup.Group)

	for _, item := range tenant.Status.Namespaces {
		namespace := item

		delete(gcSet, namespace)

		group.Go(func() error {
			return r.reconcileNamespace(ctx, namespace, tenant)
		})
	}

	if err = group.Wait(); err != nil {
		err = fmt.Errorf("cannot sync Namespaces: %w", err)
	}

	for name := range gcSet {
		r.Metrics.DeleteAllMetricsForNamespace(name)

		tenant.Status.RemoveInstance(&capsulev1beta2.TenantStatusNamespaceItem{
			Name: name,
		})
	}

	tenant.Status.Size = uint(len(tenant.Status.Namespaces))

	return err
}

func (r *Manager) reconcileNamespace(ctx context.Context, namespace string, tnt *capsulev1beta2.Tenant) (err error) {
	ns := &corev1.Namespace{}
	if err = r.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
		return err
	}

	stat := &capsulev1beta2.TenantStatusNamespaceItem{
		Name: namespace,
		UID:  ns.GetUID(),
	}

	metaStatus := &capsulev1beta2.TenantStatusNamespaceMetadata{}

	// Always update tenant status condition after reconciliation
	defer func() {
		instance := tnt.Status.GetInstance(stat)
		if instance != nil {
			stat = instance
		}

		readCondition := meta.NewReadyCondition(ns)

		if err != nil {
			readCondition.Status = metav1.ConditionFalse
			readCondition.Reason = meta.FailedReason
			readCondition.Message = fmt.Sprintf("Failed to reconcile: %v", err)

			if instance != nil && instance.Metadata != nil {
				stat.Metadata = instance.Metadata
			}
		} else if metaStatus != nil {
			stat.Metadata = metaStatus
		}

		stat.Conditions.UpdateConditionByType(readCondition)

		cordonedCondition := meta.NewCordonedCondition(ns)

		if ns.Labels[meta.CordonedLabel] == meta.CordonedLabelTrigger {
			cordonedCondition.Reason = meta.CordonedReason
			cordonedCondition.Message = "namespace is cordoned"
			cordonedCondition.Status = metav1.ConditionTrue
		}

		stat.Conditions.UpdateConditionByType(cordonedCondition)

		tnt.Status.UpdateInstance(stat)

		r.syncNamespaceStatusMetrics(tnt, ns)
	}()

	err = retry.RetryOnConflict(retry.DefaultBackoff, func() (conflictErr error) {
		_, conflictErr = controllerutil.CreateOrUpdate(ctx, r.Client, ns, func() error {
			metaStatus, err = r.reconcileMetadata(ctx, ns, tnt, stat)

			return err
		})

		return conflictErr
	})

	return err
}

//nolint:nestif
func (r *Manager) reconcileMetadata(
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
	tenant.AddTenantNameLabel(originLabels, ns, tnt)

	ns.SetLabels(originLabels)
	ns.SetAnnotations(originAnnotations)

	return managed, err
}

func (r *Manager) collectNamespaces(ctx context.Context, tenant *capsulev1beta2.Tenant) (err error) {
	list := &corev1.NamespaceList{}

	err = r.List(ctx, list, client.MatchingFieldsSelector{
		Selector: fields.OneTermEqualSelector(".metadata.ownerReferences[*].capsule", tenant.GetName()),
	})
	if err != nil {
		return err
	}

	tenant.AssignNamespaces(list.Items)

	return err
}
