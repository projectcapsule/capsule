// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
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

func (r *Manager) collectAvailableResources(ctx context.Context, tnt *capsulev1beta2.Tenant) (err error) {
	log := log.FromContext(ctx)

	log.V(5).Info("collecting available storageclasses")

	if tnt.Status.Classes.StorageClasses, err = listObjectNamesBySelector(
		ctx,
		r.Client,
		tnt.Spec.StorageClasses,
		&storagev1.StorageClassList{},
	); err != nil {
		return err
	}

	log.V(5).Info("collected available storageclasses", "size", len(tnt.Status.Classes.StorageClasses))

	if tnt.Status.Classes.PriorityClasses, err = listObjectNamesBySelector(
		ctx,
		r.Client,
		tnt.Spec.PriorityClasses,
		&schedulingv1.PriorityClassList{},
	); err != nil {
		return err
	}

	log.V(5).Info("collected available priorityclasses", "size", len(tnt.Status.Classes.PriorityClasses))

	if tnt.Status.Classes.GatewayClasses, err = listObjectNamesBySelector(
		ctx,
		r.Client,
		tnt.Spec.GatewayOptions.AllowedClasses,
		&gatewayv1.GatewayClassList{},
	); err != nil {
		return err
	}

	log.V(5).Info("collected available gatewayclasses", "size", len(tnt.Status.Classes.GatewayClasses))

	if tnt.Status.Classes.RuntimeClasses, err = listObjectNamesBySelector(
		ctx,
		r.Client,
		tnt.Spec.RuntimeClasses,
		&nodev1.RuntimeClassList{},
	); err != nil {
		return err
	}

	log.V(5).Info("collected available runtimeclasses", "size", len(tnt.Status.Classes.RuntimeClasses))

	if tnt.Status.Classes.DeviceClasses, err = listObjectNamesBySelector(
		ctx,
		r.Client,
		tnt.Spec.DeviceClasses,
		&resources.DeviceClassList{},
	); err != nil {
		return err
	}

	log.V(5).Info("collected available deviceclasses", "size", len(tnt.Status.Classes.DeviceClasses))

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
) (objects []string, err error) {
	defer func() {
		if err == nil {
			sort.Strings(objects)
		}
	}()

	if err := c.List(ctx, list, opts...); err != nil {
		return nil, err
	}

	objs, err := meta.ExtractList(list)
	if err != nil {
		return nil, err
	}

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

		return objects, nil
	}

	var sel labels.Selector
	if hasSelector {
		sel, err = metav1.LabelSelectorAsSelector(&allowed.LabelSelector)
		if err != nil {
			return nil, err
		}
	}

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

		if _, already := selected[name]; already {
			continue
		}

		selected[name] = struct{}{}
	}

	for name := range selected {
		objects = append(objects, name)
	}

	return objects, nil
}
