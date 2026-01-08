// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"sort"

	nodev1 "k8s.io/api/node/v1"
	resources "k8s.io/api/resource/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

// Sets a label on the Tenant object with it's name.
func (r *Manager) collectOwners(ctx context.Context, tnt *capsulev1beta2.Tenant) (err error) {
	owners, err := tnt.CollectOwners(
		ctx,
		r.Client,
		r.Configuration.AllowServiceAccountPromotion(),
		r.Configuration.Administrators(),
	)
	if err != nil {
		return err
	}

	// No Direct Update needed as status is always posted
	tnt.Status.Owners = owners

	return nil
}

func (r Manager) reconcileClassStatus(
	ctx context.Context,
	fn func(context.Context, *capsulev1beta2.Tenant) error,
) (err error) {
	tntList := &capsulev1beta2.TenantList{}
	if err = r.List(ctx, tntList); err != nil {
		return err
	}

	for i := range tntList.Items {
		t := &tntList.Items[i]

		// Collect Ownership for Status
		if err = fn(ctx, t); err != nil {
			err = fmt.Errorf("cannot collect available classes: %w", err)

			return err
		}

		if err = r.updateTenantStatus(ctx, t, err); err != nil {
			err = fmt.Errorf("cannot update tenant status: %w", err)

			return err
		}
	}

	return err
}

func (r *Manager) collectAvailableResources(ctx context.Context, tnt *capsulev1beta2.Tenant) (err error) {
	log := log.FromContext(ctx)

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
		r.Client,
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
		r.Client,
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
		r.Client,
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
		r.Client,
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
		r.Client,
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
	c client.Client,
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

	for name := range selected {
		objects = append(objects, name)
	}

	sort.Strings(objects)

	return objects, nil
}

func listObjectNamesBySelector2(
	ctx context.Context,
	c client.Client,
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
