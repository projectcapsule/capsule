// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepermit

import (
	"context"
	"errors"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// ResourcePermitTemplateReconciler reports readiness of namespaced templates.
type ResourcePermitTemplateReconciler struct {
	client.Client

	reader  client.Reader
	Metrics *metrics.ResourcePermitTemplateRecorder
}

func (r *ResourcePermitTemplateReconciler) SetupWithManager(mgr ctrl.Manager, options utils.ControllerOptions) error {
	r.Client = mgr.GetClient()
	r.reader = mgr.GetAPIReader()

	return ctrl.NewControllerManagedBy(mgr).
		For(
			&capsulev1beta2.ResourcePermitTemplate{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Named("resourcepermittemplate").
		WithOptions(options.Runtime.ToControllerOptions()).
		Complete(r)
}

func (r *ResourcePermitTemplateReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	instance := &capsulev1beta2.ResourcePermitTemplate{}
	if err := r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			r.Metrics.DeleteMetrics(request.Name, request.Namespace)

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	reconcileErr := capsulev1beta2.ValidateResourcePermitTemplate(instance)

	latest, statusErr := r.updateStatus(ctx, instance, reconcileErr)
	if apierrors.IsNotFound(statusErr) {
		r.Metrics.DeleteMetrics(request.Name, request.Namespace)

		return ctrl.Result{}, nil
	}

	if statusErr != nil {
		return ctrl.Result{}, errors.Join(reconcileErr, statusErr)
	}

	r.Metrics.RecordConditions(latest)

	return ctrl.Result{}, reconcileErr
}

func (r *ResourcePermitTemplateReconciler) updateStatus(
	ctx context.Context,
	instance *capsulev1beta2.ResourcePermitTemplate,
	reconcileErr error,
) (*capsulev1beta2.ResourcePermitTemplate, error) {
	reader := r.reader
	if reader == nil {
		reader = r.Client
	}

	var updated *capsulev1beta2.ResourcePermitTemplate

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &capsulev1beta2.ResourcePermitTemplate{}
		if err := reader.Get(ctx, client.ObjectKeyFromObject(instance), latest); err != nil {
			return err
		}

		if latest.Generation != instance.Generation || latest.UID != instance.UID {
			return nil
		}

		originalStatus := latest.Status.DeepCopy()
		latest.Status.ObservedGeneration = instance.Generation
		latest.Status.Conditions.UpdateConditionByType(resourcePermitTemplateReadyCondition(instance, reconcileErr))

		if !reflect.DeepEqual(*originalStatus, latest.Status) {
			if err := r.Status().Update(ctx, latest); err != nil {
				return err
			}
		}

		updated = latest

		return nil
	})

	return updated, err
}

func resourcePermitTemplateReadyCondition(instance client.Object, reconcileErr error) meta.Condition {
	ready := meta.NewReadyCondition(instance)
	ready.ObservedGeneration = instance.GetGeneration()

	if reconcileErr != nil {
		ready.Status = metav1.ConditionFalse
		ready.Reason = meta.FailedReason
		ready.Message = reconcileErr.Error()
	}

	return ready
}
