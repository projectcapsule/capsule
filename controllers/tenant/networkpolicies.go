// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"strconv"

	"golang.org/x/sync/errgroup"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/utils"
)

// Ensuring all the NetworkPolicies are applied to each Namespace handled by the Tenant.
func (r *Manager) syncNetworkPolicies(ctx context.Context, tenant *capsulev1beta2.Tenant) error { //nolint:dupl
	// getting requested NetworkPolicy keys
	keys := make([]string, 0, len(tenant.Spec.NetworkPolicies.Items))

	for i := range tenant.Spec.NetworkPolicies.Items {
		keys = append(keys, strconv.Itoa(i))
	}

	group := new(errgroup.Group)

	for _, ns := range tenant.Status.Namespaces {
		namespace := ns

		group.Go(func() error {
			return r.syncNetworkPolicy(ctx, tenant, namespace, keys)
		})
	}

	return group.Wait()
}

func (r *Manager) syncNetworkPolicy(ctx context.Context, tenant *capsulev1beta2.Tenant, namespace string, keys []string) (err error) {
	if err = r.pruningResources(ctx, namespace, keys, &networkingv1.NetworkPolicy{}); err != nil {
		return err
	}
	// getting NetworkPolicy labels for the mutateFn
	var tenantLabel, networkPolicyLabel string

	if tenantLabel, err = utils.GetTypeLabel(&capsulev1beta2.Tenant{}); err != nil {
		return err
	}

	if networkPolicyLabel, err = utils.GetTypeLabel(&networkingv1.NetworkPolicy{}); err != nil {
		return err
	}

	for i, spec := range tenant.Spec.NetworkPolicies.Items { //nolint:dupl
		target := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("capsule-%s-%d", tenant.Name, i),
				Namespace: namespace,
			},
		}

		var res controllerutil.OperationResult
		res, err = controllerutil.CreateOrUpdate(ctx, r.Client, target, func() (err error) {
			labels := target.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}

			labels[tenantLabel] = tenant.Name
			labels[networkPolicyLabel] = strconv.Itoa(i)

			target.SetLabels(labels)
			target.Spec = spec

			return controllerutil.SetControllerReference(tenant, target, r.Scheme())
		})

		r.emitEvent(tenant, target.GetNamespace(), res, fmt.Sprintf("Ensuring NetworkPolicy %s", target.GetName()), err)

		r.Log.Info("Network Policy sync result: "+string(res), "name", target.Name, "namespace", target.Namespace)

		if err != nil {
			return err
		}
	}

	return nil
}
