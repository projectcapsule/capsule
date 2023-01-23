// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package pv

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	log2 "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	capsuleutils "github.com/clastix/capsule/pkg/utils"
	webhookutils "github.com/clastix/capsule/pkg/webhook/utils"
)

type Controller struct {
	client client.Client
	label  string
}

func (c *Controller) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := log2.FromContext(ctx)

	persistentVolume := corev1.PersistentVolume{}
	if err := c.client.Get(ctx, request.NamespacedName, &persistentVolume); err != nil {
		if errors.IsNotFound(err) {
			log.Info("skipping reconciliation, resource may have been deleted")

			return reconcile.Result{}, nil
		}

		log.Error(err, "cannot retrieve corev1.PersistentVolume")

		return reconcile.Result{}, err
	}

	if persistentVolume.Spec.ClaimRef == nil {
		log.Info("skipping reconciliation, missing claimRef")

		return reconcile.Result{}, nil
	}

	tnt, err := webhookutils.TenantByStatusNamespace(ctx, c.client, persistentVolume.Spec.ClaimRef.Namespace)
	if err != nil {
		log.Error(err, "unable to retrieve Tenant from the claimRef")

		return reconcile.Result{}, err
	}

	if tnt == nil {
		log.Info("skipping reconciliation, PV is claimed by a PVC not managed in a Tenant")

		return reconcile.Result{}, nil
	}

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pv := persistentVolume

		if err = c.client.Get(ctx, request.NamespacedName, &pv); err != nil {
			return err
		}

		labels := pv.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}

		labels[c.label] = tnt.GetName()

		pv.SetLabels(labels)

		return c.client.Update(ctx, &pv)
	})
	if retryErr != nil {
		log.Error(retryErr, "unable to update PersistentVolume with Capsule label")

		return reconcile.Result{}, retryErr
	}

	return reconcile.Result{}, nil
}

func (c *Controller) SetupWithManager(mgr ctrl.Manager) error {
	label, err := capsuleutils.GetTypeLabel(&capsulev1beta2.Tenant{})
	if err != nil {
		return err
	}

	c.client = mgr.GetClient()
	c.label = label

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.PersistentVolume{}, builder.WithPredicates(predicate.NewPredicateFuncs(func(object client.Object) bool {
			pv, ok := object.(*corev1.PersistentVolume)
			if !ok {
				return false
			}

			if pv.Spec.ClaimRef == nil {
				return false
			}

			labels := object.GetLabels()
			_, ok = labels[c.label]

			return !ok
		}))).
		Complete(c)
}
