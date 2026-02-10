// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl
package admission

import (
	"context"
	"maps"
	"sort"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	clt "github.com/projectcapsule/capsule/pkg/runtime/client"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

type validatingReconciler struct {
	client client.Client

	configuration configuration.Configuration
	log           logr.Logger
}

func (r *validatingReconciler) SetupWithManager(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("capsule/admission/validating").
		For(&capsulev1beta2.CapsuleConfiguration{}, utils.NamesMatchingPredicate(ctrlConfig.ConfigurationName)).
		WithOptions(controller.Options{MaxConcurrentReconciles: ctrlConfig.MaxConcurrentReconciles}).
		Complete(r)
}

func (r *validatingReconciler) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
	err = r.reconcileValidatingConfiguration(ctx, r.configuration.Admission().Validating)

	return res, err
}

func (r *validatingReconciler) reconcileValidatingConfiguration(
	ctx context.Context,
	cfg capsulev1beta2.DynamicAdmissionConfig,
) error {
	desiredName := string(cfg.Name)

	hooks, err := r.validatingWebhooks(ctx, cfg)
	if err != nil {
		return err
	}

	if len(hooks) == 0 {
		managed, err := r.listManagedValidatingWebhookConfigs(ctx)
		if err != nil {
			return err
		}

		for i := range managed {
			if err := r.deleteValidatingWebhookConfig(ctx, managed[i].Name); err != nil {
				return err
			}
		}

		return nil
	}

	obj := &admissionv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: string(cfg.Name)},
	}

	sort.Slice(hooks, func(i, j int) bool { return hooks[i].Name < hooks[j].Name })

	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	maps.Copy(labels, cfg.Labels)

	labels[meta.CreatedByCapsuleLabel] = meta.ValueController

	obj.SetLabels(labels)

	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	maps.Copy(annotations, cfg.Annotations)

	obj.SetAnnotations(annotations)

	if err := clt.PatchApply(ctx, r.client, obj, meta.FieldManagerCapsuleController, true); err != nil {
		return err
	}

	// Garbage-collect any old managed validating webhook configs with different name
	managed, err := r.listManagedValidatingWebhookConfigs(ctx)
	if err != nil {
		return err
	}

	for i := range managed {
		if managed[i].Name == desiredName {
			continue
		}

		if err := r.deleteValidatingWebhookConfig(ctx, managed[i].Name); err != nil {
			return err
		}
	}

	return nil
}

func (r *validatingReconciler) listManagedValidatingWebhookConfigs(ctx context.Context) ([]admissionv1.ValidatingWebhookConfiguration, error) {
	list := &admissionv1.ValidatingWebhookConfigurationList{}
	if err := r.client.List(ctx, list, client.MatchingLabels{
		meta.CreatedByCapsuleLabel: meta.ValueController,
	}); err != nil {
		return nil, err
	}

	return list.Items, nil
}

func (r *validatingReconciler) deleteValidatingWebhookConfig(ctx context.Context, name string) error {
	if name == "" {
		return nil
	}

	obj := &admissionv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}

	err := r.client.Delete(ctx, obj)
	if apierrors.IsNotFound(err) {
		return nil
	}

	return err
}

func (r *validatingReconciler) validatingWebhooks(
	ctx context.Context,
	cfg capsulev1beta2.DynamicAdmissionConfig,
) (hooks []admissionv1.ValidatingWebhook, err error) {
	return
}
