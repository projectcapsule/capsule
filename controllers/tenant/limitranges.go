// Copyright 2020-2021 Clastix Labs
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

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/utils"
)

// Ensuring all the LimitRange are applied to each Namespace handled by the Tenant.
func (r *Manager) syncLimitRanges(ctx context.Context, tenant *capsulev1beta2.Tenant) error { //nolint:dupl
	// getting requested LimitRange keys
	keys := make([]string, 0, len(tenant.Spec.LimitRanges.Items))

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
	// getting LimitRange labels for the mutateFn
	var tenantLabel, limitRangeLabel string

	if tenantLabel, err = utils.GetTypeLabel(&capsulev1beta2.Tenant{}); err != nil {
		return err
	}

	if limitRangeLabel, err = utils.GetTypeLabel(&corev1.LimitRange{}); err != nil {
		return err
	}

	if err = r.pruningResources(ctx, namespace, keys, &corev1.LimitRange{}); err != nil {
		return err
	}

	for i, spec := range tenant.Spec.LimitRanges.Items {
		target := &corev1.LimitRange{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("capsule-%s-%d", tenant.Name, i),
				Namespace: namespace,
			},
		}

		var res controllerutil.OperationResult
		res, err = controllerutil.CreateOrUpdate(ctx, r.Client, target, func() (err error) {
			target.ObjectMeta.Labels = map[string]string{
				tenantLabel:     tenant.Name,
				limitRangeLabel: strconv.Itoa(i),
			}
			target.Spec = spec

			return controllerutil.SetControllerReference(tenant, target, r.Client.Scheme())
		})

		r.emitEvent(tenant, target.GetNamespace(), res, fmt.Sprintf("Ensuring LimitRange %s", target.GetName()), err)

		r.Log.Info("LimitRange sync result: "+string(res), "name", target.Name, "namespace", target.Namespace)

		if err != nil {
			return err
		}
	}

	return nil
}
