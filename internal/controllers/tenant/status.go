// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"sort"

	nodev1 "k8s.io/api/node/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

func (r *Manager) collectAvailableResources(ctx context.Context, tnt *capsulev1beta2.Tenant) (err error) {
	tnt.Status.Classes.StorageClasses, err = listObjectNamesBySelector(
		ctx,
		r.Client,
		tnt.Spec.StorageClasses,
		&storagev1.StorageClassList{},
	)
	if err != nil {
		return
	}

	tnt.Status.Classes.PriorityClasses, err = listObjectNamesBySelector(
		ctx,
		r.Client,
		tnt.Spec.PriorityClasses,
		&schedulingv1.PriorityClassList{},
	)
	if err != nil {
		return
	}

	//tnt.Status.Classes.GatewayClasses, err = listObjectNamesBySelector(
	//	ctx,
	//	r.Client,
	//	tnt.Spec.GatewayOptions.AllowedClasses.SelectionListWithSpec,
	//	&gatewayv1.GatewayClassList{},
	//)
	//if err != nil {
	//	return
	//}

	tnt.Status.Classes.RuntimeClasses, err = listObjectNamesBySelector(
		ctx,
		r.Client,
		tnt.Spec.RuntimeClasses,
		&nodev1.RuntimeClassList{},
	)
	if err != nil {
		return
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
) (objects []string, err error) {
	if allowed == nil {
		return nil, nil
	}

	defer func() {
		if err == nil {
			sort.Strings(objects)
		}
	}()

	if len(allowed.Exact) > 0 {
		objects = append(objects, allowed.Exact...)
	}

	if len(allowed.LabelSelector.MatchLabels) == 0 && len(allowed.LabelSelector.MatchExpressions) == 0 {
		return nil, nil
	}

	var sel labels.Selector
	sel, err = metav1.LabelSelectorAsSelector(&allowed.LabelSelector)
	if err != nil {
		return nil, err
	}

	base := []client.ListOption{&client.MatchingLabelsSelector{Selector: sel}}
	base = append(base, opts...)

	if err := c.List(ctx, list, base...); err != nil {
		return nil, err
	}

	objs, err := meta.ExtractList(list)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(objs))
	for _, obj := range objs {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return nil, err
		}
		names = append(names, accessor.GetName())
	}

	sort.Strings(names)
	return names, nil
}
