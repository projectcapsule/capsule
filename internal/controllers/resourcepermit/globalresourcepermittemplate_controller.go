// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepermit

import (
	"context"
	"errors"
	"reflect"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

const globalResourcePermitTemplateControllerName = "globalresourcepermittemplate"

// GlobalResourcePermitTemplateReconciler resolves namespace selectors for admission and discovery.
type GlobalResourcePermitTemplateReconciler struct {
	client.Client

	reader  client.Reader
	Log     logr.Logger
	Metrics *metrics.GlobalResourcePermitTemplateRecorder
}

func (r *GlobalResourcePermitTemplateReconciler) SetupWithManager(mgr ctrl.Manager, options utils.ControllerOptions) error {
	r.Client = mgr.GetClient()
	r.reader = mgr.GetAPIReader()

	return ctrl.NewControllerManagedBy(mgr).
		For(
			&capsulev1beta2.GlobalResourcePermitTemplate{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(r.mapNamespaceToTemplates),
			builder.WithPredicates(namespaceLabelsChangedPredicate()),
		).
		Named(globalResourcePermitTemplateControllerName).
		WithOptions(options.Runtime.ToControllerOptions()).
		Complete(r)
}

func (r *GlobalResourcePermitTemplateReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	instance := &capsulev1beta2.GlobalResourcePermitTemplate{}
	if err := r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			r.Metrics.DeleteMetrics(request.Name)

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	reconcileErr := capsulev1beta2.ValidateResourcePermitTemplate(instance)

	namespaces := []string{"*"}
	if reconcileErr == nil && len(instance.Spec.NamespaceSelectors) > 0 {
		namespaces, reconcileErr = selectors.GetNamespacesMatchingSelectorsStrings(ctx, r.Client, instance.Spec.NamespaceSelectors)
	}

	latest, statusErr := r.updateStatus(ctx, instance, namespaces, reconcileErr)
	if apierrors.IsNotFound(statusErr) {
		r.Metrics.DeleteMetrics(request.Name)

		return ctrl.Result{}, nil
	}

	if statusErr != nil {
		return ctrl.Result{}, errors.Join(reconcileErr, statusErr)
	}

	r.Metrics.RecordConditions(latest)

	return ctrl.Result{}, reconcileErr
}

func (r *GlobalResourcePermitTemplateReconciler) updateStatus(
	ctx context.Context,
	instance *capsulev1beta2.GlobalResourcePermitTemplate,
	namespaces []string,
	reconcileErr error,
) (*capsulev1beta2.GlobalResourcePermitTemplate, error) {
	reader := r.reader
	if reader == nil {
		reader = r.Client
	}

	var updated *capsulev1beta2.GlobalResourcePermitTemplate

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &capsulev1beta2.GlobalResourcePermitTemplate{}
		if err := reader.Get(ctx, client.ObjectKeyFromObject(instance), latest); err != nil {
			return err
		}

		if latest.Generation != instance.Generation || latest.UID != instance.UID {
			return nil
		}

		originalStatus := latest.Status.DeepCopy()
		latest.Status.ObservedGeneration = instance.Generation
		// Admission uses this selection, so failures must not publish stale access.
		if reconcileErr == nil {
			latest.Status.Namespaces = namespaces
		} else {
			latest.Status.Namespaces = nil
		}

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

func (r *GlobalResourcePermitTemplateReconciler) mapNamespaceToTemplates(
	ctx context.Context,
	_ client.Object,
) []reconcile.Request {
	list := &capsulev1beta2.GlobalResourcePermitTemplateList{}
	if err := r.List(ctx, list); err != nil {
		r.Log.Error(err, "cannot list GlobalResourcePermitTemplates for namespace event")

		return nil
	}

	requests := make([]reconcile.Request, 0, len(list.Items))

	for i := range list.Items {
		if len(list.Items[i].Spec.NamespaceSelectors) == 0 {
			continue
		}

		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: list.Items[i].Name},
		})
	}

	return requests
}

func namespaceLabelsChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool { return true },
		DeleteFunc: func(event.DeleteEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.ObjectOld == nil || e.ObjectNew == nil {
				return false
			}

			return !reflect.DeepEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels())
		},
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}
