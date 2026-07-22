// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rulestatus

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	meta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

type Manager struct {
	client.Client

	reader client.Reader

	Metrics       *metrics.RuleStatusRecorder
	Log           logr.Logger
	Recorder      events.EventRecorder
	Configuration configuration.Configuration
	RESTConfig    *rest.Config
	RESTMapper    k8smeta.RESTMapper
}

func (r *Manager) SetupWithManager(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) error {
	r.reader = mgr.GetAPIReader()
	r.RESTMapper = mgr.GetRESTMapper()

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
		WithOptions(ctrlConfig.Runtime.ToControllerOptions())

	return ctrlBuilder.Complete(r)
}

func (r Manager) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	log := r.Log.WithValues("Request.Name", request.Name)

	instance := &capsulev1beta2.RuleStatus{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(5).Info("request object not found, could have been deleted after reconcile request")

			r.Metrics.DeleteMetrics(request.Name, request.Namespace)

			return reconcile.Result{}, nil
		}

		log.Error(err, "error reading the object")

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

		r.Metrics.RecordConditions(instance)

		if e := patchHelper.Patch(ctx, instance); e != nil {
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

	// Best-Effort for Updating the status
	if updateErr := r.updateReconcilingStatus(ctx, instance); updateErr != nil {
		if caperrors.IgnoreGone(updateErr) {
			return reconcile.Result{}, nil
		}

		log.Error(updateErr, "failed to update status")
	}

	// Reconcile
	if err = r.reconcile(ctx, instance); err != nil {
		err = fmt.Errorf("cannot collect available resources: %w", err)

		return result, err
	}

	var reconcileError error
	if err != nil {
		reconcileError = fmt.Errorf("had errors reconciling")
	}

	log.V(4).Info("reconciling completed")

	return ctrl.Result{}, reconcileError
}

func (r Manager) reconcile(ctx context.Context, instance *capsulev1beta2.RuleStatus) error {
	previousRules := instance.Status.Rules
	hadManagedMetadata := hasManagedMetadata(previousRules)
	ruleStatus := make([]*rules.NamespaceRuleBodyNamespace, 0, len(instance.Spec))

	for _, rule := range instance.Spec {
		if rule == nil || rule.Enforce == nil {
			continue
		}

		enforce := rule.Enforce.DeepCopy()

		for i := range enforce.Metadata {
			enforce.Metadata[i].APIGroups = enforce.Metadata[i].StatusAPIGroups()
		}

		statusRule := rule.DeepCopy()
		statusRule.Enforce = enforce
		ruleStatus = append(ruleStatus, statusRule)
	}

	instance.Status.Rules = ruleStatus
	//nolint:staticcheck
	instance.Status.Rule = rules.NamespaceRuleBodyNamespace{}

	if hadManagedMetadata || hasManagedMetadata(ruleStatus) {
		if err := r.publishRulesStatus(ctx, instance); err != nil {
			return fmt.Errorf("publish rules before managed metadata reconciliation: %w", err)
		}

		if err := r.reconcileManagedMetadata(ctx, instance, previousRules, ruleStatus); err != nil {
			return fmt.Errorf("reconcile managed metadata: %w", err)
		}
	}

	return nil
}

func (r *Manager) publishRulesStatus(ctx context.Context, instance *capsulev1beta2.RuleStatus) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &capsulev1beta2.RuleStatus{}
		if err := r.reader.Get(ctx, client.ObjectKeyFromObject(instance), latest); err != nil {
			return err
		}

		latest.Status.Rules = instance.Status.Rules
		//nolint:staticcheck
		latest.Status.Rule = instance.Status.Rule

		return r.Client.Status().Update(ctx, latest)
	})
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

		originalStatus := latest.Status.DeepCopy()

		latest.Status = instance.Status
		latest.Status.ObservedGeneration = instance.GetGeneration()

		// Set Ready Condition
		readyCondition := meta.NewReadyCondition(instance)
		if reconcileError != nil {
			readyCondition.Message = reconcileError.Error()
			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = meta.FailedReason
		}

		latest.Status.Conditions.UpdateConditionByType(readyCondition)

		if reflect.DeepEqual(*originalStatus, latest.Status) {
			return nil
		}

		if err := r.Client.Status().Update(ctx, latest); err != nil {
			return err
		}

		// Keep the in-memory object aligned with what we just wrote.
		instance.Status = latest.Status

		return nil
	})
}

func (r *Manager) updateReconcilingStatus(ctx context.Context, instance *capsulev1beta2.RuleStatus) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.RuleStatus{}
		if err = r.reader.Get(ctx, types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}, latest); err != nil {
			return err
		}

		if latest.Status.ObservedGeneration == instance.GetGeneration() {
			return nil
		}

		latest.Status.Conditions.UpdateConditionByType(meta.NewReadyConditionReconcilingReason(instance))

		return r.Status().Update(ctx, latest)
	})
}
