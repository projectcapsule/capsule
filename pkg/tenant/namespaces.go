// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/gvk"
)

func NamespaceIsPendingPodTerminating(
	ctx context.Context,
	c client.Reader,
	ns *corev1.Namespace,
) (pending bool, err error) {
	// Pods behave differently, we manually check if they are still present as they are the largest attack vector
	var podList corev1.PodList
	if err := c.List(ctx, &podList, client.InNamespace(ns.Name)); err != nil {
		return false, fmt.Errorf("list pods in namespace %q: %w", ns.Name, err)
	}

	if len(podList.Items) > 0 {
		return true, nil
	}

	return false, nil
}

func NamespaceIsPendingUnmanagedTerminationByStatus(ctx context.Context, c client.Reader, ns *corev1.Namespace) (bool, error) {
	tnt, err := GetTenantByNamespace(ctx, c, ns.GetName())
	if err != nil {
		return false, err
	}

	if tnt == nil {
		return false, nil
	}

	instance := tnt.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{
		Name: ns.GetName(),
	})
	if instance == nil {
		return false, nil
	}

	cond := instance.Conditions.GetConditionByType(meta.TerminatingCondition)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		return false, nil
	}

	return true, nil
}

// First deletes (remove finalizers and deleted) all resources which are not managed by capsule
// Once fullfilled, remove all managed resources
func NamespacedCascadingCleanup(
	ctx context.Context,
	c client.Reader,
	disco discovery.DiscoveryInterface,
	dyn dynamic.Interface,
	ns *corev1.Namespace,
) (cleaned bool, err error) {
	_, err = removeFinalizersFromRemainingNamespacedResources(ctx, disco, dyn, ns.Name, []string{})
	if err != nil {
		return true, err
	}

	return false, nil
}

func removeFinalizersFromRemainingNamespacedResources(
	ctx context.Context,
	disco discovery.DiscoveryInterface,
	dyn dynamic.Interface,
	namespace string,
	ignoredFinalizers []string,
) (bool, error) {
	var errs []error

	resourceLists, err := disco.ServerPreferredNamespacedResources()
	if err != nil {
		if len(resourceLists) == 0 {
			return false, fmt.Errorf("discover namespaced resources: %w", err)
		}

		errs = append(errs, fmt.Errorf("partial discovery failure: %w", err))
	}

	gvrs, err := gvk.NamespacedListableResources(resourceLists)
	if err != nil {
		return false, err
	}

	ignored := make(map[string]struct{}, len(ignoredFinalizers))
	for _, f := range ignoredFinalizers {
		ignored[f] = struct{}{}
	}

	var (
		mu         sync.Mutex
		cleanedAny bool
	)

	g, ctx := errgroup.WithContext(ctx)
	// g.SetLimit(8)

	for _, gvr := range gvrs {
		gvr := gvr

		g.Go(func() error {
			cleaned, err := processResourceType(ctx, dyn, gvr, namespace, ignored)

			mu.Lock()
			defer mu.Unlock()

			if cleaned {
				cleanedAny = true
			}

			if err != nil {
				errs = append(errs, fmt.Errorf("process %s in namespace %q: %w", gvr.String(), namespace, err))
			}

			return nil
		})
	}

	g.Wait()

	return cleanedAny, errors.Join(errs...)
}

func processResourceType(
	ctx context.Context,
	dyn dynamic.Interface,
	gvr schema.GroupVersionResource,
	namespace string,
	ignoredFinalizers map[string]struct{},
) (bool, error) {
	list, err := dyn.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) || apierrors.IsMethodNotSupported(err) {
			return false, nil
		}
		return false, fmt.Errorf("list %s in namespace %q: %w", gvr.String(), namespace, err)
	}

	var errs []error
	cleanedAny := false

	for i := range list.Items {
		obj := &list.Items[i]

		if gvr.Group == "" && gvr.Resource == "pods" {
			continue
		}

		// If the object is not yet deleting, issue a delete first.
		if obj.GetDeletionTimestamp() == nil {
			if err := dyn.Resource(gvr).Namespace(namespace).Delete(ctx, obj.GetName(), metav1.DeleteOptions{}); err != nil {
				if !apierrors.IsNotFound(err) {
					errs = append(errs, fmt.Errorf(
						"delete %s %q/%q: %w",
						gvr.String(),
						namespace,
						obj.GetName(),
						err,
					))
					continue
				}
			} else {
				cleanedAny = true
			}
		}

		finalizers := obj.GetFinalizers()
		if len(finalizers) == 0 {
			continue
		}

		remainingFinalizers, removed := meta.FilterFinalizers(finalizers, ignoredFinalizers)
		if !removed {
			continue
		}

		patch := meta.BuildFinalizersMergePatch(remainingFinalizers)

		_, err := dyn.Resource(gvr).Namespace(namespace).Patch(
			ctx,
			obj.GetName(),
			types.MergePatchType,
			patch,
			metav1.PatchOptions{},
		)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				errs = append(errs, fmt.Errorf(
					"patch finalizers on %s %q/%q: %w",
					gvr.String(),
					namespace,
					obj.GetName(),
					err,
				))
				continue
			}
		} else {
			cleanedAny = true
		}
	}

	return cleanedAny, errors.Join(errs...)
}

func hasNamespaceConditionTrue(ns *corev1.Namespace, t corev1.NamespaceConditionType) bool {
	for _, c := range ns.Status.Conditions {
		if c.Type == t && c.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}

//nolint:gocognit
func CollectTenantNamespaceByLabel(
	ctx context.Context,
	c client.Client,
	tnt capsulev1beta2.Tenant,
	additionalSelector *metav1.LabelSelector,
) (namespaces []corev1.Namespace, err error) {
	// Creating Namespace selector
	var selector labels.Selector

	if additionalSelector != nil {
		selector, err = metav1.LabelSelectorAsSelector(additionalSelector)
		if err != nil {
			return nil, err
		}
	} else {
		selector = labels.NewSelector()
	}

	// Resources can be replicated only on Namespaces belonging to the same Global:
	// preventing a boundary cross by enforcing the selection.
	tntRequirement, err := labels.NewRequirement(meta.TenantLabel, selection.Equals, []string{tnt.GetName()})
	if err != nil {
		err = fmt.Errorf("unable to create requirement for Namespace filtering and resource replication", err)

		return nil, err
	}

	selector = selector.Add(*tntRequirement)
	// Selecting the targeted Namespace according to the TenantResource specification.
	ns := corev1.NamespaceList{}
	if err = c.List(ctx, &ns, client.MatchingLabelsSelector{Selector: selector}); err != nil {
		err = fmt.Errorf("cannot retrieve Namespaces for resource", err)

		return nil, err
	}

	return ns.Items, nil
}
