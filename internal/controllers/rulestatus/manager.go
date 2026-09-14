// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package rulestatus

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
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

	originalStatus := instance.Status.DeepCopy()

	defer func() {
		if e := r.updateStatus(ctx, instance, originalStatus, err); e != nil {
			if caperrors.IgnoreGone(e) {
				err = nil

				return
			}

			err = errors.Join(err, fmt.Errorf("cannot update status: %w", e))

			return
		}

		r.Metrics.RecordConditions(instance)
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
		err = fmt.Errorf("cannot reconcile rules: %w", err)

		return result, err
	}

	log.V(4).Info("reconciling completed")

	return ctrl.Result{}, nil
}

func (r Manager) reconcile(ctx context.Context, instance *capsulev1beta2.RuleStatus) error {
	previousRules := instance.Status.Rules
	//nolint:staticcheck // Clear the legacy representation when publishing new rules.
	hadLegacyRule := !reflect.DeepEqual(instance.Status.Rule, rules.NamespaceRuleBodyNamespace{})
	hadManagedMetadata := hasManagedMetadata(previousRules)

	var ruleStatus []*rules.NamespaceRuleBodyNamespace

	for _, rule := range instance.Spec {
		if rule == nil || rule.Enforce == nil {
			continue
		}

		statusRule := rule.DeepCopy()
		// RuleStatus is an enforcement cache. Quota definitions are reconciled
		// independently as GlobalResourceQuotas and may include legacy entries
		// which predate stable quota names.
		statusRule.Quota = nil

		for i := range statusRule.Enforce.Metadata {
			statusRule.Enforce.Metadata[i].APIGroups = statusRule.Enforce.Metadata[i].StatusAPIGroups()
		}

		ruleStatus = append(ruleStatus, statusRule)
	}

	instance.Status.Rules = ruleStatus
	//nolint:staticcheck
	instance.Status.Rule = rules.NamespaceRuleBodyNamespace{}

	if hadManagedMetadata || hasManagedMetadata(ruleStatus) {
		if hadLegacyRule || !reflect.DeepEqual(previousRules, ruleStatus) {
			if err := r.publishRulesStatus(ctx, instance); err != nil {
				return fmt.Errorf("publish rules before managed metadata reconciliation: %w", err)
			}
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

		originalStatus := latest.Status.DeepCopy()
		latest.Status.Rules = instance.Status.Rules
		//nolint:staticcheck
		latest.Status.Rule = instance.Status.Rule

		if reflect.DeepEqual(*originalStatus, latest.Status) {
			return nil
		}

		return r.Client.Status().Update(ctx, latest)
	})
}

func (r *Manager) updateStatus(ctx context.Context, instance *capsulev1beta2.RuleStatus, originalStatus *capsulev1beta2.RuleStatusStatus, reconcileError error) error {
	instance.Status.ObservedGeneration = instance.GetGeneration()

	readyCondition := meta.NewReadyCondition(instance)
	if reconcileError != nil {
		readyCondition.Message = reconcileError.Error()
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = meta.FailedReason
	}

	instance.Status.Conditions.UpdateConditionByType(readyCondition)

	if reflect.DeepEqual(*originalStatus, instance.Status) {
		return nil
	}

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
	status := instance.Status.DeepCopy()
	if !removeQuotaDefinitions(status) && status.ObservedGeneration == instance.GetGeneration() {
		return nil
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.RuleStatus{}
		if err = r.reader.Get(ctx, types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}, latest); err != nil {
			return err
		}

		cleanedQuota := removeQuotaDefinitions(&latest.Status)
		if latest.Status.ObservedGeneration == instance.GetGeneration() {
			if !cleanedQuota {
				return nil
			}

			if err := r.Status().Update(ctx, latest); err != nil {
				return err
			}

			instance.Status = latest.Status

			return nil
		}

		latest.Status.Conditions.UpdateConditionByType(meta.NewReadyConditionReconcilingReason(instance))

		if err := r.Status().Update(ctx, latest); err != nil {
			return err
		}

		instance.Status = latest.Status

		return nil
	})
}

//nolint:staticcheck // The deprecated flattened Rule must be cleaned for objects written by older Capsule versions.
func removeQuotaDefinitions(status *capsulev1beta2.RuleStatusStatus) bool {
	if status == nil {
		return false
	}

	changed := len(status.Rule.Quota) > 0

	for _, rule := range status.Rules {
		if rule != nil && len(rule.Quota) > 0 {
			changed = true
			rule.Quota = nil
		}
	}

	status.Rule.Quota = nil

	return changed
}
