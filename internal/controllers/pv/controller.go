// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package pv

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	log2 "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/tenant"
	capsuleutils "github.com/projectcapsule/capsule/pkg/utils"
)

type Controller struct {
	client client.Client
	reader client.Reader
	label  string
}

func (c *Controller) SetupWithManager(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) error {
	label, err := capsuleutils.GetTypeLabel(&capsulev1beta2.Tenant{})
	if err != nil {
		return err
	}

	c.client = mgr.GetClient()
	c.reader = mgr.GetAPIReader()
	c.label = label

	return ctrl.NewControllerManagedBy(mgr).
		Named("capsule/persistentvolumes").
		For(&corev1.PersistentVolume{}, builder.WithPredicates(persistentVolumePredicate(c.label))).
		WithOptions(ctrlConfig.Runtime.ToControllerOptions()).
		Complete(c)
}

func persistentVolumePredicate(label string) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			pv, ok := e.Object.(*corev1.PersistentVolume)

			return ok && pv.Spec.ClaimRef != nil
		},
		DeleteFunc: func(event.DeleteEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldPV, oldOK := e.ObjectOld.(*corev1.PersistentVolume)

			newPV, newOK := e.ObjectNew.(*corev1.PersistentVolume)
			if !oldOK || !newOK || newPV.Spec.ClaimRef == nil {
				return false
			}

			return !apiequality.Semantic.DeepEqual(oldPV.Spec.ClaimRef, newPV.Spec.ClaimRef) ||
				persistentVolumeLabelChanged(oldPV, newPV, label)
		},
		GenericFunc: func(event.GenericEvent) bool {
			return false
		},
	}
}

func persistentVolumeLabelChanged(oldPV, newPV *corev1.PersistentVolume, label string) bool {
	oldValue, oldExists := oldPV.GetLabels()[label]
	newValue, newExists := newPV.GetLabels()[label]

	return oldExists != newExists || oldValue != newValue
}

func (c *Controller) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := log2.FromContext(ctx)

	persistentVolume := corev1.PersistentVolume{}
	if err := c.client.Get(ctx, request.NamespacedName, &persistentVolume); err != nil {
		if errors.IsNotFound(err) {
			log.V(5).Info("skipping reconciliation, resource may have been deleted")

			return reconcile.Result{}, nil
		}

		log.Error(err, "cannot retrieve corev1.PersistentVolume")

		return reconcile.Result{}, err
	}

	if persistentVolume.Spec.ClaimRef == nil {
		log.Info("skipping reconciliation, missing claimRef")

		return reconcile.Result{}, nil
	}

	tnt, err := c.resolveNamespaceTenant(ctx, persistentVolume.Spec.ClaimRef.Namespace)
	if err != nil {
		log.Error(err, "unable to retrieve Tenant from the claimRef")

		return reconcile.Result{}, err
	}

	if tnt == nil {
		log.V(4).Info("skipping reconciliation, PV is claimed by a PVC not managed in a Tenant")

		return reconcile.Result{}, nil
	}

	if persistentVolume.GetLabels()[c.label] == tnt.GetName() {
		return reconcile.Result{}, nil
	}

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pv := persistentVolume

		if err = c.client.Get(ctx, request.NamespacedName, &pv); err != nil {
			return err
		}

		if pv.Spec.ClaimRef == nil || pv.Spec.ClaimRef.Namespace != persistentVolume.Spec.ClaimRef.Namespace {
			return nil
		}

		labels := pv.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}

		if labels[c.label] == tnt.GetName() {
			return nil
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

func (c *Controller) resolveNamespaceTenant(
	ctx context.Context,
	namespace string,
) (*capsulev1beta2.Tenant, error) {
	reader := c.reader
	if reader == nil {
		reader = c.client
	}

	ns := &corev1.Namespace{}
	if err := reader.Get(ctx, client.ObjectKey{Name: namespace}, ns); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("get claim namespace %q: %w", namespace, err)
	}

	tnt, err := tenant.ResolveNamespaceTenant(ctx, reader, ns)
	if err != nil {
		return nil, fmt.Errorf("resolve claim namespace %q: %w", namespace, err)
	}

	return tnt, nil
}
