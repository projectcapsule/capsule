// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package globalresourcequotas

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	ctrlutils "github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

type Controller struct {
	client.Client

	reader   client.Reader
	log      logr.Logger
	recorder events.EventRecorder
	metrics  *metrics.GlobalResourceQuotaRecorder
}

func (r *Controller) SetupWithManager(mgr ctrl.Manager, options ctrlutils.ControllerOptions) error {
	r.reader = mgr.GetAPIReader()

	return ctrl.NewControllerManagedBy(mgr).
		Named("capsule/global-resource-quotas").
		For(
			&capsulev1beta2.GlobalResourceQuota{},
			builder.WithPredicates(predicate.Or(
				predicate.GenerationChangedPredicate{},
				predicates.UpdatedMetadataPredicate{},
				predicates.DeletionChangedPredicate{},
			)),
		).
		Owns(
			&corev1.ResourceQuota{},
			builder.WithPredicates(predicate.Or(
				predicate.GenerationChangedPredicate{},
				predicates.UpdatedMetadataPredicate{},
				predicates.DeletionChangedPredicate{},
				predicates.ResourceQuotaUsageChangedPredicate{},
			)),
		).
		Owns(&capsulev1beta2.QuantityLedger{}).
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(r.globalQuotasForNamespace),
			builder.WithPredicates(predicates.UpdatedMetadataPredicate{}),
		).
		WithOptions(options.Runtime.ToControllerOptions()).
		Complete(r)
}

func (r *Controller) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	instance := &capsulev1beta2.GlobalResourceQuota{}
	if err := r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			r.metrics.Delete(request.Name)

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	status, initialized, err := r.reconcile(ctx, instance)
	if status == nil {
		status = instance.Status.DeepCopy()
	}

	ready := meta.NewReadyCondition(instance)
	if err != nil {
		ready.Status = metav1.ConditionFalse
		ready.Reason = meta.FailedReason
		ready.Message = err.Error()
	} else if !initialized {
		ready.Status = metav1.ConditionFalse
		ready.Reason = meta.ReconcilingReason
		ready.Message = "waiting for ResourceQuota usage initialization"
	}

	status.Conditions.UpdateConditionByType(ready)
	status.ObservedGeneration = instance.Generation

	if updateErr := r.updateStatus(ctx, instance, *status); updateErr != nil {
		return ctrl.Result{}, updateErr
	}

	instance.Status = *status
	r.metrics.Record(instance)

	return ctrl.Result{}, err
}

func (r *Controller) reconcile(
	ctx context.Context,
	instance *capsulev1beta2.GlobalResourceQuota,
) (*capsulev1beta2.GlobalResourceQuotaStatus, bool, error) {
	namespaces, err := selectors.GetNamespacesMatchingSelectors(
		ctx,
		r.reader,
		instance.Spec.NamespaceSelectors,
	)
	if err != nil {
		return nil, false, err
	}

	if err := r.syncResourceQuotas(ctx, instance, namespaces); err != nil {
		return nil, false, err
	}

	status, initialized, err := r.observeUsage(ctx, instance, namespaces)
	if err != nil {
		return nil, false, err
	}

	ledger, err := r.ensureLedger(ctx, instance)
	if err != nil {
		return status, false, err
	}

	if err := r.reconcileLedger(
		ctx,
		ledger,
		instance.Generation,
		status.Namespaces,
		status.Total.Used,
		initialized,
	); err != nil {
		return status, false, err
	}

	return status, initialized, nil
}

func (r *Controller) syncResourceQuotas(
	ctx context.Context,
	instance *capsulev1beta2.GlobalResourceQuota,
	namespaces []corev1.Namespace,
) error {
	selected := make(map[string]struct{}, len(namespaces))

	for i := range namespaces {
		namespace := &namespaces[i]
		selected[namespace.Name] = struct{}{}

		target := &corev1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      instance.GetResourceQuotaName(),
				Namespace: namespace.Name,
			},
		}

		if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			_, err := controllerutil.CreateOrUpdate(ctx, r.Client, target, func() error {
				targetLabels := target.GetLabels()
				if targetLabels == nil {
					targetLabels = map[string]string{}
				}

				targetLabels[meta.NewManagedByCapsuleLabel] = meta.ValueController
				targetLabels[meta.GlobalResourceQuotaLabel] = instance.Name
				target.SetLabels(targetLabels)
				target.Spec = *instance.Spec.Quota.DeepCopy()

				return controllerutil.SetControllerReference(instance, target, r.Scheme())
			})

			return err
		}); err != nil {
			if apierrors.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
				continue
			}

			return fmt.Errorf("sync ResourceQuota in namespace %s: %w", namespace.Name, err)
		}
	}

	list := &corev1.ResourceQuotaList{}
	if err := r.List(ctx, list, client.MatchingLabels{
		meta.NewManagedByCapsuleLabel: meta.ValueController,
		meta.GlobalResourceQuotaLabel: instance.Name,
	}); err != nil {
		return err
	}

	for i := range list.Items {
		item := &list.Items[i]
		if _, keep := selected[item.Namespace]; keep {
			continue
		}

		if err := r.Delete(ctx, item); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete stale ResourceQuota %s/%s: %w", item.Namespace, item.Name, err)
		}
	}

	return nil
}

func (r *Controller) observeUsage(
	ctx context.Context,
	instance *capsulev1beta2.GlobalResourceQuota,
	namespaces []corev1.Namespace,
) (*capsulev1beta2.GlobalResourceQuotaStatus, bool, error) {
	status := instance.Status.DeepCopy()
	status.Total.Hard = instance.Spec.Quota.Hard.DeepCopy()
	status.Total.Used = capsulev1beta2.ZeroResourceList(instance.Spec.Quota.Hard)
	status.NamespaceUsage = make(capsulev1beta2.GlobalResourceQuotaNamespaceUsage, len(namespaces))
	instanceCopy := instance.DeepCopy()
	instanceCopy.Status = *status
	instanceCopy.AssignNamespaces(namespaces)
	status.Namespaces = instanceCopy.Status.Namespaces
	status.NamespaceSize = instanceCopy.Status.NamespaceSize

	initialized := true

	for i := range namespaces {
		namespace := namespaces[i].Name
		used := capsulev1beta2.ZeroResourceList(instance.Spec.Quota.Hard)
		quota := &corev1.ResourceQuota{}

		err := r.reader.Get(ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      instance.GetResourceQuotaName(),
		}, quota)
		if err != nil {
			if apierrors.IsNotFound(err) {
				initialized = false
				status.NamespaceUsage[namespace] = capsulev1beta2.GlobalResourceQuotaNamespaceStatus{Used: used}

				continue
			}

			return status, false, err
		}

		if !resourceQuotaStatusReady(quota, instance.Spec.Quota.Hard) {
			initialized = false
		}

		for name := range instance.Spec.Quota.Hard {
			value := quota.Status.Used[name]
			used[name] = value.DeepCopy()
			total := status.Total.Used[name]
			total.Add(value)
			status.Total.Used[name] = total
		}

		status.NamespaceUsage[namespace] = capsulev1beta2.GlobalResourceQuotaNamespaceStatus{Used: used}
	}

	statusCopy := instance.DeepCopy()
	statusCopy.Status = *status
	statusCopy.CalculateAvailable()

	return &statusCopy.Status, initialized, nil
}

func (r *Controller) updateStatus(
	ctx context.Context,
	instance *capsulev1beta2.GlobalResourceQuota,
	status capsulev1beta2.GlobalResourceQuotaStatus,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &capsulev1beta2.GlobalResourceQuota{}
		if err := r.reader.Get(ctx, client.ObjectKeyFromObject(instance), current); err != nil {
			return err
		}

		if reflect.DeepEqual(current.Status, status) {
			return nil
		}

		current.Status = *status.DeepCopy()

		return r.Status().Update(ctx, current)
	})
}

func (r *Controller) globalQuotasForNamespace(ctx context.Context, object client.Object) []reconcile.Request {
	namespace, ok := object.(*corev1.Namespace)
	if !ok {
		return nil
	}

	list := &capsulev1beta2.GlobalResourceQuotaList{}
	if err := r.List(ctx, list); err != nil {
		r.log.Error(err, "failed to list GlobalResourceQuotas", "namespace", namespace.Name)

		return nil
	}

	requests := make([]reconcile.Request, 0)

	for i := range list.Items {
		item := &list.Items[i]
		matched := slices.Contains(item.Status.Namespaces, namespace.Name)

		for _, namespaceSelector := range item.Spec.NamespaceSelectors {
			if namespaceSelector.LabelSelector == nil {
				continue
			}

			selector, err := metav1.LabelSelectorAsSelector(namespaceSelector.LabelSelector)
			if err == nil && selector.Matches(labels.Set(namespace.Labels)) {
				matched = true

				break
			}
		}

		if matched {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(item)})
		}
	}

	return requests
}

func resourceQuotaStatusReady(quota *corev1.ResourceQuota, hard corev1.ResourceList) bool {
	for name := range hard {
		if _, ok := quota.Status.Hard[name]; !ok {
			return false
		}
	}

	return true
}
