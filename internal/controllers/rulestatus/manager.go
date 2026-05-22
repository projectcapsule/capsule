// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rulestatus

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api"
	meta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

type Manager struct {
	client.Client
	reader client.Reader

	Metrics       *metrics.TenantRecorder
	Log           logr.Logger
	Recorder      events.EventRecorder
	Configuration configuration.Configuration
	RESTConfig    *rest.Config
}

func (r *Manager) SetupWithManager(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) error {
	r.reader = mgr.GetAPIReader()

	ctrlBuilder := ctrl.NewControllerManagedBy(mgr).
		Named("capsule/rule-status").
		For(
			&capsulev1beta2.RuleStatus{},
			builder.WithPredicates(
				predicate.Or(
					predicate.GenerationChangedPredicate{},
					predicates.UpdatedMetadataPredicate{},
				),
			),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: ctrlConfig.MaxConcurrentReconciles})

	return ctrlBuilder.Complete(r)
}

func (r Manager) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	r.Log = r.Log.WithValues("Request.Name", request.Name)

	instance := &capsulev1beta2.RuleStatus{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			r.Log.V(5).Info("request object not found, could have been deleted after reconcile request")

			return reconcile.Result{}, nil
		}

		r.Log.Error(err, "error reading the object")

		return result, err
	}

	patchHelper, err := patch.NewHelper(instance, r.Client)
	if err != nil {
		return reconcile.Result{}, err
	}

	defer func() {
		if e := r.updateStatus(ctx, instance, err); e != nil {
			if apierrors.IsNotFound(err) || apierrors.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
				err = nil

				return
			}

			err = fmt.Errorf("cannot update status: %w", e)

			return
		}

		if e := patchHelper.Patch(ctx, instance); err != nil {
			if apierrors.IsNotFound(e) || apierrors.HasStatusCause(e, corev1.NamespaceTerminatingCause) {
				err = nil

				return
			}

			err = fmt.Errorf("cannot patch: %w", e)

			return

		}

		// Controller-Runtime should never receive error
		err = nil
	}()

	// Reconcile
	if err = r.reconcile(ctx, instance); err != nil {
		err = fmt.Errorf("cannot collect available resources: %w", err)

		return result, err
	}

	var reconcileError error
	if err != nil {
		reconcileError = fmt.Errorf("had errors reconciling")
	}

	r.Log.V(4).Info("reconciling completed")

	return ctrl.Result{}, reconcileError
}

func (r Manager) reconcile(ctx context.Context, instance *capsulev1beta2.RuleStatus) (err error) {
	out := api.NamespaceRuleBodyNamespace{}

	for _, rule := range instance.Spec {
		if rule == nil {
			continue
		}

		// Merge enforce body (for now: only registries)
		// Preserve order: append in the order rules are declared.
		if len(rule.Enforce.Registries) > 0 {
			out.Enforce.Registries = append(out.Enforce.Registries, rule.Enforce.Registries...)
		}
	}

	instance.Status.Rule = out

	return nil
}

func (r *Manager) updateStatus(ctx context.Context, instance *capsulev1beta2.RuleStatus, reconcileError error) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.RuleStatus{}
		if err = r.reader.Get(ctx, types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}, latest); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		latest.Status = instance.Status

		// Set Ready Condition
		readyCondition := meta.NewReadyCondition(instance)
		if reconcileError != nil {
			readyCondition.Message = reconcileError.Error()
			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = meta.FailedReason
		}

		latest.Status.Conditions.UpdateConditionByType(readyCondition)

		if err := r.Client.Status().Update(ctx, latest); err != nil {
			return err
		}

		// Keep the in-memory object aligned with what we just wrote.
		instance.Status = latest.Status

		return nil
	})
}
