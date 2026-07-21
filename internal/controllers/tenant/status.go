// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"sort"

	"github.com/go-logr/logr"
	nodev1 "k8s.io/api/node/v1"
	resources "k8s.io/api/resource/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	capmeta "github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

func setTenantStatusState(tnt *capsulev1beta2.Tenant) {
	if tnt.DeletionTimestamp != nil {
		tnt.Status.State = capsulev1beta2.TenantStateTerminating

		return
	}

	if tnt.Spec.Cordoned {
		tnt.Status.State = capsulev1beta2.TenantStateCordoned

		return
	}

	tnt.Status.State = capsulev1beta2.TenantStateActive
}

// ensureTenantStatusInitialized establishes fields required by the Tenant CRD.
// Partial status writers (owners and available classes) can be the first writer
// for a newly created Tenant, so they cannot assume the main reconciliation has
// already initialized state and conditions.
func ensureTenantStatusInitialized(tnt *capsulev1beta2.Tenant) {
	setTenantStatusState(tnt)

	if tnt.Status.Conditions.GetConditionByType(capmeta.ReadyCondition) == nil {
		tnt.Status.Conditions.UpdateConditionByType(capmeta.NewReadyConditionReconcilingReason(tnt))
	}

	cordonedCondition := capmeta.NewCordonedCondition(tnt)
	if tnt.Spec.Cordoned {
		cordonedCondition.Reason = capmeta.CordonedReason
		cordonedCondition.Message = "Tenant is cordoned"
		cordonedCondition.Status = metav1.ConditionTrue
	}

	tnt.Status.Conditions.UpdateConditionByType(cordonedCondition)
}

func (r *Manager) updateTenantStatus(ctx context.Context, instance *capsulev1beta2.Tenant, reconcileError error) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &capsulev1beta2.Tenant{}
		if err := r.reader.Get(ctx, types.NamespacedName{Name: instance.GetName()}, latest); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		originalStatus := latest.Status.DeepCopy()

		latest.Status = instance.Status
		latest.Status.ObservedGeneration = instance.GetGeneration()
		ensureTenantStatusInitialized(latest)

		readyCondition := capmeta.NewReadyCondition(instance)
		if reconcileError != nil {
			readyCondition.Message = reconcileError.Error()
			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = capmeta.FailedReason
		}

		latest.Status.Conditions.UpdateConditionByType(readyCondition)

		if reflect.DeepEqual(*originalStatus, latest.Status) {
			return nil
		}

		if err := r.Client.Status().Update(ctx, latest); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		instance.Status = latest.Status

		return nil
	})
}

// updateTenantOwnersStatus persists owners as soon as their evaluation finishes.
// Owner authorization must not depend on the remaining tenant reconciliation
// succeeding, which can involve unrelated resources and take considerably longer.
func (r *Manager) updateTenantOwnersStatus(ctx context.Context, instance *capsulev1beta2.Tenant) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &capsulev1beta2.Tenant{}
		if err := r.reader.Get(ctx, types.NamespacedName{Name: instance.GetName()}, latest); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		originalStatus := latest.Status.DeepCopy()
		latest.Status.Owners = instance.Status.Owners.DeepCopy()
		ensureTenantStatusInitialized(latest)

		if reflect.DeepEqual(*originalStatus, latest.Status) {
			return nil
		}

		if err := r.Client.Status().Update(ctx, latest); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		return nil
	})
}

// updateTenantClassStatus updates only the independently collected class
// status. Class events use a distinct workqueue key and may run concurrently
// with the main Tenant reconciliation, so copying the entire cached status here
// could erase freshly evaluated owners, promotions, namespaces, or conditions.
func (r *Manager) updateTenantClassStatus(ctx context.Context, instance *capsulev1beta2.Tenant) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &capsulev1beta2.Tenant{}
		if err := r.reader.Get(ctx, types.NamespacedName{Name: instance.GetName()}, latest); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		originalStatus := latest.Status.DeepCopy()
		latest.Status.Classes = *instance.Status.Classes.DeepCopy()
		ensureTenantStatusInitialized(latest)

		if reflect.DeepEqual(*originalStatus, latest.Status) {
			instance.Status = latest.Status

			return nil
		}

		if err := r.Client.Status().Update(ctx, latest); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		instance.Status = latest.Status

		return nil
	})
}

func (r *Manager) updateReconcilingStatus(ctx context.Context, instance *capsulev1beta2.Tenant) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		latest := &capsulev1beta2.Tenant{}
		if err = r.reader.Get(ctx, types.NamespacedName{Name: instance.GetName(), Namespace: instance.GetNamespace()}, latest); err != nil {
			return err
		}

		originalStatus := latest.Status.DeepCopy()

		// Spec owners are already known and do not depend on ServiceAccount or
		// TenantOwner discovery. Include them in the initial reconciling status
		// write so authorization is not delayed by those potentially expensive
		// list operations. Upsert preserves previously discovered promotions until
		// collectRBAC replaces the owner list with its authoritative evaluation.
		for _, owner := range instance.Spec.Owners.ToStatusOwners() {
			latest.Status.Owners.Upsert(owner)
		}

		ensureTenantStatusInitialized(latest)

		if latest.Status.ObservedGeneration == instance.GetGeneration() {
			// The reconciled object may have come from a cache that has not yet
			// observed the latest status write. Keep the instance (and therefore
			// the patch helper baseline created by the caller) synchronized with
			// the authoritative API-reader result. Missing spec owners still require
			// a repair write even when observedGeneration is already current.
			if reflect.DeepEqual(*originalStatus, latest.Status) {
				instance.Status = latest.Status

				return nil
			}
		} else {
			latest.Status.Conditions.UpdateConditionByType(capmeta.NewReadyConditionReconcilingReason(instance))
		}

		if err := r.Client.Status().Update(ctx, latest); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		instance.Status = latest.Status

		return nil
	})
}

// Sets a label on the Tenant object with it's name.
func (r *Manager) collectRBAC(ctx context.Context, tnt *capsulev1beta2.Tenant) (err error) {
	owners, evaluationErr := tenant.CollectOwners(
		ctx,
		r.Client,
		tnt,
		r.Configuration,
	)
	tnt.Status.Owners = owners

	// CollectOwners returns every owner it could evaluate before an error. Persist
	// that result immediately so later reconciliation failures cannot leave owner
	// authorization stale or empty.
	statusErr := r.updateTenantOwnersStatus(ctx, tnt)
	if err = errors.Join(evaluationErr, statusErr); err != nil {
		return err
	}

	promotions, err := tenant.CollectPromotions(
		ctx,
		r.Client,
		tnt,
		r.Configuration,
	)
	tnt.Status.Promotions = promotions

	if err != nil {
		return err
	}

	return nil
}

func (r *Manager) collectAvailableResources(ctx context.Context, log logr.Logger, tnt *capsulev1beta2.Tenant) (err error) {
	if r.classes.device {
		log.V(5).Info("collecting available deviceclasses")

		if err = r.collectAvailableDeviceClasses(ctx, tnt); err != nil {
			return err
		}

		log.V(5).Info("collected available deviceclasses", "size", len(tnt.Status.Classes.DeviceClasses))
	}

	log.V(5).Info("collecting available storageclasses")

	if err = r.collectAvailableStorageClasses(ctx, tnt); err != nil {
		return err
	}

	log.V(5).Info("collected available storageclasses", "size", len(tnt.Status.Classes.StorageClasses))

	if err = r.collectAvailablePriorityClasses(ctx, tnt); err != nil {
		return err
	}

	if r.classes.gateway {
		log.V(5).Info("collected available priorityclasses", "size", len(tnt.Status.Classes.PriorityClasses))

		if err = r.collectAvailableGatewayClasses(ctx, tnt); err != nil {
			return err
		}

		log.V(5).Info("collected available gatewayclasses", "size", len(tnt.Status.Classes.GatewayClasses))
	}

	if err = r.collectAvailableRuntimeClasses(ctx, tnt); err != nil {
		return err
	}

	log.V(5).Info("collected available runtimeclasses", "size", len(tnt.Status.Classes.RuntimeClasses))

	return nil
}

func (r *Manager) collectAvailableDeviceClasses(ctx context.Context, tnt *capsulev1beta2.Tenant) (err error) {
	if tnt.Status.Classes.DeviceClasses, err = listObjectNamesBySelector2(
		ctx,
		r.reader,
		tnt.Spec.DeviceClasses,
		&resources.DeviceClassList{},
	); err != nil {
		return err
	}

	return nil
}

func (r *Manager) collectAvailableStorageClasses(ctx context.Context, tnt *capsulev1beta2.Tenant) (err error) {
	if tnt.Status.Classes.StorageClasses, err = listObjectNamesBySelector(
		ctx,
		r.reader,
		tnt.Spec.StorageClasses,
		&storagev1.StorageClassList{},
	); err != nil {
		return err
	}

	return nil
}

func (r *Manager) collectAvailablePriorityClasses(ctx context.Context, tnt *capsulev1beta2.Tenant) (err error) {
	if tnt.Status.Classes.PriorityClasses, err = listObjectNamesBySelector(
		ctx,
		r.reader,
		tnt.Spec.PriorityClasses,
		&schedulingv1.PriorityClassList{},
	); err != nil {
		return err
	}

	return nil
}

func (r *Manager) collectAvailableGatewayClasses(ctx context.Context, tnt *capsulev1beta2.Tenant) (err error) {
	if tnt.Status.Classes.GatewayClasses, err = listObjectNamesBySelector(
		ctx,
		r.reader,
		tnt.Spec.GatewayOptions.AllowedClasses,
		&gatewayv1.GatewayClassList{},
	); err != nil {
		return err
	}

	return nil
}

func (r *Manager) collectAvailableRuntimeClasses(ctx context.Context, tnt *capsulev1beta2.Tenant) (err error) {
	if tnt.Status.Classes.RuntimeClasses, err = listObjectNamesBySelector(
		ctx,
		r.reader,
		tnt.Spec.RuntimeClasses,
		&nodev1.RuntimeClassList{},
	); err != nil {
		return err
	}

	return nil
}

// ListObjectNamesBySelector lists Kubernetes objects of the given List type (cluster- or namespaced)
// matching the provided LabelSelector, and returns their .metadata.name values.
func listObjectNamesBySelector(
	ctx context.Context,
	c client.Reader,
	allowed *api.DefaultAllowedListSpec,
	list client.ObjectList,
	opts ...client.ListOption,
) ([]string, error) {
	if err := c.List(ctx, list, opts...); err != nil {
		return nil, err
	}

	objs, err := meta.ExtractList(list)
	if err != nil {
		return nil, err
	}

	objects := make([]string, 0)

	allNames := make(map[string]struct{})
	selected := make(map[string]struct{})

	hasSelector := false
	if allowed != nil {
		hasSelector = len(allowed.MatchLabels) > 0 ||
			len(allowed.MatchExpressions) > 0
	}

	if allowed == nil || (!hasSelector && len(allowed.Exact) == 0) {
		for _, o := range objs {
			accessor, err := meta.Accessor(o)
			if err != nil {
				return nil, err
			}

			objects = append(objects, accessor.GetName())
		}

		sort.Strings(objects)

		return objects, nil
	}

	// Prepare selector
	var sel labels.Selector
	if hasSelector {
		sel, err = metav1.LabelSelectorAsSelector(&allowed.LabelSelector)
		if err != nil {
			return nil, err
		}
	}

	// Evaluate objects
	for _, obj := range objs {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return nil, err
		}

		name := accessor.GetName()

		allNames[name] = struct{}{}

		if hasSelector {
			lbls := labels.Set(accessor.GetLabels())
			if sel.Matches(lbls) {
				selected[name] = struct{}{}
			}
		}
	}

	exact := allowed.Exact
	if allowed.Default != "" {
		exact = append(exact, allowed.Default)
	}

	for _, name := range exact {
		if _, exists := allNames[name]; !exists {
			continue
		}

		selected[name] = struct{}{}
	}

	var regex *regexp.Regexp

	//nolint:staticcheck
	if allowed.Regex != "" {
		regex, err = regexp.Compile(allowed.Regex)
		if err != nil {
			return nil, err
		}
	}

	if regex != nil {
		for name := range allNames {
			if regex.MatchString(name) {
				selected[name] = struct{}{}
			}
		}
	}

	for name := range selected {
		objects = append(objects, name)
	}

	sort.Strings(objects)

	return objects, nil
}

func listObjectNamesBySelector2(
	ctx context.Context,
	c client.Reader,
	allowed *api.SelectorAllowedListSpec,
	list client.ObjectList,
	opts ...client.ListOption,
) ([]string, error) {
	if err := c.List(ctx, list, opts...); err != nil {
		return nil, err
	}

	objs, err := meta.ExtractList(list)
	if err != nil {
		return nil, err
	}

	objects := make([]string, 0)

	allNames := make(map[string]struct{})
	selected := make(map[string]struct{})

	hasSelector := false
	if allowed != nil {
		hasSelector = len(allowed.MatchLabels) > 0 ||
			len(allowed.MatchExpressions) > 0
	}

	if allowed == nil || (!hasSelector && len(allowed.Exact) == 0) {
		for _, o := range objs {
			accessor, err := meta.Accessor(o)
			if err != nil {
				return nil, err
			}

			objects = append(objects, accessor.GetName())
		}

		sort.Strings(objects)

		return objects, nil
	}

	// Prepare selector
	var sel labels.Selector
	if hasSelector {
		sel, err = metav1.LabelSelectorAsSelector(&allowed.LabelSelector)
		if err != nil {
			return nil, err
		}
	}

	// Evaluate objects
	for _, obj := range objs {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return nil, err
		}

		name := accessor.GetName()

		allNames[name] = struct{}{}

		if hasSelector {
			lbls := labels.Set(accessor.GetLabels())
			if sel.Matches(lbls) {
				selected[name] = struct{}{}
			}
		}
	}

	exact := allowed.Exact

	for _, name := range exact {
		if _, exists := allNames[name]; !exists {
			continue
		}

		selected[name] = struct{}{}
	}

	for name := range selected {
		objects = append(objects, name)
	}

	sort.Strings(objects)

	return objects, nil
}
