// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/utils"
)

func readyTenantNamespaces(tnt *capsulev1beta2.Tenant) []string {
	namespaces := make([]string, 0, len(tnt.Status.Spaces))

	for _, ns := range tnt.Status.Spaces {
		ready := ns.Conditions.GetConditionByType(meta.ReadyCondition)
		if ready != nil && ready.Status != metav1.ConditionTrue {
			continue
		}

		terminating := ns.Conditions.GetConditionByType(meta.TerminatingCondition)
		if terminating != nil && terminating.Status == metav1.ConditionTrue {
			continue
		}

		namespaces = append(namespaces, ns.Name)
	}

	return namespaces
}

func runForTenantNamespaces(
	ctx context.Context,
	tnt *capsulev1beta2.Tenant,
	fn func(context.Context, string) error,
) error {
	return runForNamespaces(ctx, readyTenantNamespaces(tnt), fn)
}

func runForNamespaces(
	ctx context.Context,
	namespaces []string,
	fn func(context.Context, string) error,
) error {
	errs := make(chan error, len(namespaces))
	group := new(errgroup.Group)
	group.SetLimit(8)

	for _, namespace := range namespaces {
		group.Go(func() error {
			if err := fn(ctx, namespace); err != nil {
				errs <- fmt.Errorf("namespace %q: %w", namespace, err)
			}

			return nil
		})
	}

	_ = group.Wait()

	close(errs)

	var joined []error
	for err := range errs {
		joined = append(joined, err)
	}

	return errors.Join(joined...)
}

// runGarbageCollection removes resources managed by a Tenant from live
// namespaces which are no longer assigned to that Tenant. Resources in
// terminating namespaces are left to Kubernetes' namespace garbage collector.
func (r *Manager) runGarbageCollection(
	ctx context.Context,
	tnt *capsulev1beta2.Tenant,
	obj client.Object,
) error {
	list, err := managedObjectList(obj)
	if err != nil {
		return err
	}

	selector := labels.SelectorFromSet(labels.Set{
		meta.NewManagedByCapsuleLabel: meta.ValueController,
		meta.NewTenantLabel:           tnt.Name,
	})

	if err := r.List(ctx, list, &client.ListOptions{LabelSelector: selector}); err != nil {
		return err
	}

	tenantNamespaces := make(map[string]struct{}, len(tnt.Status.Spaces))
	for _, namespace := range tnt.Status.Spaces {
		tenantNamespaces[namespace.Name] = struct{}{}
	}

	garbageNamespaces := make(map[string]struct{})

	for _, namespace := range managedObjectNamespaces(list) {
		if _, assigned := tenantNamespaces[namespace]; !assigned {
			garbageNamespaces[namespace] = struct{}{}
		}
	}

	namespaces := make([]string, 0, len(garbageNamespaces))
	for namespace := range garbageNamespaces {
		namespaces = append(namespaces, namespace)
	}

	reader := r.reader
	if reader == nil {
		reader = r.Client
	}

	return runForNamespaces(ctx, namespaces, func(ctx context.Context, namespace string) error {
		ns := &corev1.Namespace{}
		if err := reader.Get(ctx, client.ObjectKey{Name: namespace}, ns); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		if ns.DeletionTimestamp != nil {
			return nil
		}

		deleteTarget, ok := obj.DeepCopyObject().(client.Object)
		if !ok {
			return fmt.Errorf("unsupported managed resource type %T", obj)
		}

		err := r.DeleteAllOf(ctx, deleteTarget, &client.DeleteAllOfOptions{
			ListOptions: client.ListOptions{
				LabelSelector: selector,
				Namespace:     namespace,
			},
		})
		if apierrors.IsNotFound(err) || apierrors.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
			return nil
		}

		return err
	})
}

// pruningResources is taking care of removing the no more requested sub-resources as LimitRange, ResourceQuota or
// NetworkPolicy using the "exists" and "notin" LabelSelector to perform an outer-join removal.
func (r *Manager) pruningResources(ctx context.Context, ns string, keys []string, obj client.Object) (err error) {
	var capsuleLabel string

	if capsuleLabel, err = utils.GetTypeLabel(obj); err != nil {
		return err
	}

	selector := labels.NewSelector()

	var exists *labels.Requirement

	if exists, err = labels.NewRequirement(capsuleLabel, selection.Exists, []string{}); err != nil {
		return err
	}

	selector = selector.Add(*exists)

	if len(keys) > 0 {
		var notIn *labels.Requirement

		if notIn, err = labels.NewRequirement(capsuleLabel, selection.NotIn, keys); err != nil {
			return err
		}

		selector = selector.Add(*notIn)
	}

	r.Log.V(4).Info("pruning objects", "labelSelector", selector.String(), "namespace", ns)

	list, err := managedObjectList(obj)
	if err != nil {
		return err
	}

	if err := r.List(ctx, list, &client.ListOptions{LabelSelector: selector, Namespace: ns}); err != nil {
		if apierrors.IsNotFound(err) || apierrors.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
			return nil
		}

		return err
	}

	if managedObjectListLength(list) == 0 {
		return nil
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		err := r.DeleteAllOf(ctx, obj, &client.DeleteAllOfOptions{
			ListOptions: client.ListOptions{
				LabelSelector: selector,
				Namespace:     ns,
			},
			DeleteOptions: client.DeleteOptions{},
		})
		if err != nil {
			if apierrors.IsNotFound(err) || apierrors.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
				r.Log.V(4).Info(
					"skipping pruning because target namespace or object is gone/terminating",
					"namespace", ns,
					"labelSelector", selector.String(),
				)

				return nil
			}

			return err
		}

		return nil
	})
}

func managedObjectList(obj client.Object) (client.ObjectList, error) {
	switch obj.(type) {
	case *networkingv1.NetworkPolicy:
		return &networkingv1.NetworkPolicyList{}, nil
	case *corev1.LimitRange:
		return &corev1.LimitRangeList{}, nil
	case *corev1.ResourceQuota:
		return &corev1.ResourceQuotaList{}, nil
	case *rbacv1.RoleBinding:
		return &rbacv1.RoleBindingList{}, nil
	case *capsulev1beta2.RuleStatus:
		return &capsulev1beta2.RuleStatusList{}, nil
	default:
		return nil, fmt.Errorf("unsupported managed resource type %T", obj)
	}
}

func managedObjectListLength(list client.ObjectList) int {
	switch typed := list.(type) {
	case *networkingv1.NetworkPolicyList:
		return len(typed.Items)
	case *corev1.LimitRangeList:
		return len(typed.Items)
	case *corev1.ResourceQuotaList:
		return len(typed.Items)
	case *rbacv1.RoleBindingList:
		return len(typed.Items)
	case *capsulev1beta2.RuleStatusList:
		return len(typed.Items)
	default:
		return 0
	}
}

func managedObjectNamespaces(list client.ObjectList) []string {
	switch typed := list.(type) {
	case *networkingv1.NetworkPolicyList:
		return namespacesForItems(typed.Items, func(item networkingv1.NetworkPolicy) string { return item.Namespace })
	case *corev1.LimitRangeList:
		return namespacesForItems(typed.Items, func(item corev1.LimitRange) string { return item.Namespace })
	case *corev1.ResourceQuotaList:
		return namespacesForItems(typed.Items, func(item corev1.ResourceQuota) string { return item.Namespace })
	case *rbacv1.RoleBindingList:
		return namespacesForItems(typed.Items, func(item rbacv1.RoleBinding) string { return item.Namespace })
	case *capsulev1beta2.RuleStatusList:
		return namespacesForItems(typed.Items, func(item capsulev1beta2.RuleStatus) string { return item.Namespace })
	default:
		return nil
	}
}

func namespacesForItems[T any](items []T, namespaceFor func(T) string) []string {
	namespaces := make([]string, 0, len(items))
	for _, item := range items {
		namespaces = append(namespaces, namespaceFor(item))
	}

	return namespaces
}
