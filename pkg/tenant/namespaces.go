// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

type NamespacedResourceCache interface {
	Get(disco discovery.DiscoveryInterface) ([]schema.GroupVersionResource, error)
}

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

func ResolveNamespaceTenant(
	ctx context.Context,
	reader client.Reader,
	ns *corev1.Namespace,
) (*capsulev1beta2.Tenant, error) {
	if ns == nil || reader == nil {
		return nil, nil
	}

	label := TenanLabelValue(ns)
	refs := TenantOwnerReferences(ns)

	switch {
	case label == "" && len(refs) == 0:
		return nil, nil

	case len(refs) > 1:
		return nil, fmt.Errorf("namespace can not have multiple Tenant ownerReferences")

	case label == "" && len(refs) == 1:
		return nil, fmt.Errorf("namespace has Tenant ownerReference %q but no tenant label", refs[0].Name)

	case label != "" && len(refs) == 0:
		return nil, fmt.Errorf("namespace has tenant label %q but no Tenant ownerReference", label)
	}

	tnt, err := GetTenantByOwnerreferences(ctx, reader, refs)
	if err != nil {
		return nil, err
	}

	if tnt == nil {
		return nil, fmt.Errorf("namespace references unknown Tenant %q", refs[0].Name)
	}

	if tnt.GetName() != label {
		return nil, fmt.Errorf("namespace label %q does not match owner reference %q", label, tnt.GetName())
	}

	return tnt, nil
}

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
		err = fmt.Errorf("unable to create requirement for Namespace filtering and resource replication: %w", err)

		return nil, err
	}

	selector = selector.Add(*tntRequirement)
	// Selecting the targeted Namespace according to the TenantResource specification.
	ns := corev1.NamespaceList{}
	if err = c.List(ctx, &ns, client.MatchingLabelsSelector{Selector: selector}); err != nil {
		err = fmt.Errorf("cannot retrieve Namespaces for resource: %w", err)

		return nil, err
	}

	return ns.Items, nil
}
