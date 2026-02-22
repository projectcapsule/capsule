// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

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
