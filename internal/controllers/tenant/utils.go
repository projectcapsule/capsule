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
	errs := make(chan error, len(tnt.Status.Spaces))
	group := new(errgroup.Group)
	group.SetLimit(8)

	for _, namespace := range readyTenantNamespaces(tnt) {
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
	default:
		return 0
	}
}
