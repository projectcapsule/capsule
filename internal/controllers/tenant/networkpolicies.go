// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// Ensuring all the NetworkPolicies are applied to each Namespace handled by the Tenant.
//

func (r *Manager) syncNetworkPolicies(ctx context.Context, log logr.Logger, tenant *capsulev1beta2.Tenant) error {
	//nolint:staticcheck
	keys := make([]string, 0, len(tenant.Spec.NetworkPolicies.Items))

	//nolint:staticcheck
	for i := range tenant.Spec.NetworkPolicies.Items {
		keys = append(keys, strconv.Itoa(i))
	}

	return runForTenantNamespaces(ctx, tenant, func(ctx context.Context, namespace string) error {
		return r.syncNetworkPolicy(ctx, log, tenant, namespace, keys)
	})
}

func (r *Manager) syncNetworkPolicy(ctx context.Context, log logr.Logger, tenant *capsulev1beta2.Tenant, namespace string, keys []string) (err error) {
	if err = r.pruningResources(ctx, namespace, keys, &networkingv1.NetworkPolicy{}); err != nil {
		return err
	}

	//nolint:staticcheck
	for i, spec := range tenant.Spec.NetworkPolicies.Items {
		target := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("capsule-%s-%d", tenant.Name, i),
				Namespace: namespace,
			},
		}

		var result controllerutil.OperationResult

		result, err = controllerutil.CreateOrUpdate(ctx, r.Client, target, func() (err error) {
			labels := target.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}

			labels[meta.NewManagedByCapsuleLabel] = meta.ValueController
			labels[meta.NewTenantLabel] = tenant.Name
			labels[meta.NetworkPolicyLabel] = strconv.Itoa(i)

			// Remove Legacy labels
			delete(labels, meta.TenantLabel)

			target.SetLabels(labels)
			target.Spec = spec

			return controllerutil.SetControllerReference(tenant, target, r.Scheme())
		})
		if err != nil {
			if apierrors.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
				log.V(4).Info(
					"skipping NetworkPolicy sync because namespace is terminating",
					"name", target.Name,
					"namespace", target.Namespace,
					"tenant", tenant.Name,
				)

				return nil
			}

			return err
		}

		log.V(4).Info("NetworkPolicy sync result", "result", result, "name", target.Name, "namespace", target.Namespace)
	}

	return nil
}
