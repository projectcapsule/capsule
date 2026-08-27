// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package breaktheglass

import (
	"context"
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
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

const breakRequestTemplateControllerName = "breakrequesttemplate"

// BreakRequestTemplateReconciler resolves namespace selectors for admission and discovery.
type BreakRequestTemplateReconciler struct {
	client.Client

	Log logr.Logger
}

func (r *BreakRequestTemplateReconciler) SetupWithManager(mgr ctrl.Manager, options utils.ControllerOptions) error {
	r.Client = mgr.GetClient()

	return ctrl.NewControllerManagedBy(mgr).
		For(
			&capsulev1beta2.BreakRequestTemplate{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(r.mapNamespaceToTemplates),
			builder.WithPredicates(namespaceLabelsChangedPredicate()),
		).
		Named(breakRequestTemplateControllerName).
		WithOptions(options.Runtime.ToControllerOptions()).
		Complete(r)
}

func (r *BreakRequestTemplateReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	instance := &capsulev1beta2.BreakRequestTemplate{}
	if err := r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	namespaces := []string{"*"}

	if len(instance.Spec.NamespaceSelectors) > 0 {
		var err error

		namespaces, err = selectors.GetNamespacesMatchingSelectorsStrings(ctx, r.Client, instance.Spec.NamespaceSelectors)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.updateStatus(ctx, instance.Name, instance.Generation, namespaces); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *BreakRequestTemplateReconciler) updateStatus(
	ctx context.Context,
	name string,
	generation int64,
	namespaces []string,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &capsulev1beta2.BreakRequestTemplate{}
		if err := r.Get(ctx, types.NamespacedName{Name: name}, latest); err != nil {
			return err
		}

		if latest.Generation != generation {
			return nil
		}

		if latest.Status.ObservedGeneration == generation && reflect.DeepEqual(latest.Status.Namespaces, namespaces) {
			return nil
		}

		latest.Status.ObservedGeneration = generation
		latest.Status.Namespaces = namespaces

		return r.Status().Update(ctx, latest)
	})
}

func (r *BreakRequestTemplateReconciler) mapNamespaceToTemplates(
	ctx context.Context,
	_ client.Object,
) []reconcile.Request {
	list := &capsulev1beta2.BreakRequestTemplateList{}
	if err := r.List(ctx, list); err != nil {
		r.Log.Error(err, "cannot list BreakRequestTemplates for namespace event")

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
