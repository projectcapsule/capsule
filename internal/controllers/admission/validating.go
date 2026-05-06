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
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/admission"
	clt "github.com/projectcapsule/capsule/pkg/runtime/client"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

type validatingReconciler struct {
	client client.Client

	configuration configuration.Configuration
	log           logr.Logger
}

func (r *validatingReconciler) SetupWithManager(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("capsule/admission/validating").
		For(
			&admissionv1.ValidatingWebhookConfiguration{},
			builder.WithPredicates(
				predicate.GenerationChangedPredicate{},
				predicates.NamesMatchingPredicate{Names: []string{string(r.configuration.Admission().Validating.Name)}},
			),
		).
		Watches(
			&capsulev1beta2.CapsuleConfiguration{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{Name: string(r.configuration.Admission().Validating.Name)},
				}}
			}),
			builder.WithPredicates(
				predicates.CapsuleConfigSpecAdmissionChangedPredicate{},
				predicates.NamesMatchingPredicate{Names: []string{ctrlConfig.ConfigurationName}},
			),
		).
		Watches(
			&capsulev1beta2.CustomQuota{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{Name: string(r.configuration.Admission().Validating.Name)},
				}}
			}),
		).
		Watches(
			&capsulev1beta2.GlobalCustomQuota{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{Name: string(r.configuration.Admission().Validating.Name)},
				}}
			}),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: ctrlConfig.MaxConcurrentReconciles}).
		Complete(r)
}

func (r *validatingReconciler) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
	err = r.reconcileValidatingConfiguration(ctx, r.configuration.Admission().Validating)

	return res, err
}

func (r *validatingReconciler) reconcileValidatingConfiguration(
	ctx context.Context,
	cfg *capsulev1beta2.DynamicValidatingAdmissionConfig,
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

	sort.Slice(hooks, func(i, j int) bool { return hooks[i].Name < hooks[j].Name })

	obj := &admissionv1.ValidatingWebhookConfiguration{
		TypeMeta: metav1.TypeMeta{
			APIVersion: admissionv1.SchemeGroupVersion.String(),
			Kind:       "ValidatingWebhookConfiguration",
		},
		ObjectMeta: metav1.ObjectMeta{Name: string(cfg.Name)},
		Webhooks:   hooks,
	}

	if err := controllerutil.SetOwnerReference(r.configuration.GetConfigObject(), obj, r.client.Scheme()); err != nil {
		return err
	}

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
	cfg *capsulev1beta2.DynamicValidatingAdmissionConfig,
) (hooks []admissionv1.ValidatingWebhook, err error) {
	for _, hook := range cfg.Webhooks {
		h, err := admission.NewValidatingWebhook(hook, cfg.Client, r.configuration.Users(), r.configuration.Administrators())
		if err != nil {
			return nil, err
		}

		hooks = append(hooks, h)
	}

	return hooks, nil
}
