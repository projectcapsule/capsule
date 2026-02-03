// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"
	"strconv"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// Ensuring all the LimitRange are applied to each Namespace handled by the Tenant.
//
//nolint:dupl
func (r *Manager) syncLimitRanges(ctx context.Context, tenant *capsulev1beta2.Tenant) error {
	// getting requested LimitRange keys
	keys := make([]string, 0, len(tenant.Spec.LimitRanges.Items)) //nolint:staticcheck

	//nolint:staticcheck
	for i := range tenant.Spec.LimitRanges.Items {
		keys = append(keys, strconv.Itoa(i))
	}

	group := new(errgroup.Group)

	for _, ns := range tenant.Status.Namespaces {
		namespace := ns

		group.Go(func() error {
			return r.syncLimitRange(ctx, tenant, namespace, keys)
		})
	}

	return group.Wait()
}

func (r *Manager) syncLimitRange(ctx context.Context, tenant *capsulev1beta2.Tenant, namespace string, keys []string) (err error) {
	if err = r.pruningResources(ctx, namespace, keys, &corev1.LimitRange{}); err != nil {
		return err
	}

	//nolint:staticcheck
	for i, spec := range tenant.Spec.LimitRanges.Items {
		target := &corev1.LimitRange{
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

			labels[meta.NewManagedByCapsuleLabel] = meta.ValueController
			labels[meta.NewTenantLabel] = tenant.Name
			labels[meta.LimitRangeLabel] = strconv.Itoa(i)

			// Remove Legacy labels
			delete(target.Labels, meta.TenantLabel)

			target.SetLabels(labels)
			target.Spec = spec

			return controllerutil.SetControllerReference(tenant, target, r.Scheme())
		})

		r.Log.V(4).Info("LimitRange sync result: "+string(res), "name", target.Name, "namespace", target.Namespace)

		if err != nil {
			return err
		}
	}

	return nil
}
