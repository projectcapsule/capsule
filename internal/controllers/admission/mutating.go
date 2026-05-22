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
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

type mutatingReconciler struct {
	client client.Client

	configuration configuration.Configuration
	log           logr.Logger
}

func (r *mutatingReconciler) SetupWithManager(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("capsule/admission/mutating").
		For(
			&admissionv1.MutatingWebhookConfiguration{},
			builder.WithPredicates(
				predicate.GenerationChangedPredicate{},
				predicates.NamesMatchingPredicate{Names: []string{string(r.configuration.Admission().Mutating.Name)}},
			),
		).
		Watches(
			&capsulev1beta2.CapsuleConfiguration{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{Name: string(r.configuration.Admission().Mutating.Name)},
				}}
			}),
			builder.WithPredicates(
				predicate.Or(
					predicate.Funcs{
						CreateFunc: func(e event.CreateEvent) bool {
							return e.Object.GetName() == ctrlConfig.ConfigurationName
						},
					},
					predicates.CapsuleConfigSpecAdmissionChangedPredicate{},
				),
				predicates.NamesMatchingPredicate{Names: []string{ctrlConfig.ConfigurationName}},
			),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: ctrlConfig.MaxConcurrentReconciles}).
		Complete(r)
}

func (r *mutatingReconciler) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
	err = r.reconcileConfiguration(ctx, r.configuration.Admission().Mutating)

	return res, err
}

func (r *mutatingReconciler) reconcileConfiguration(
	ctx context.Context,
	cfg *capsulev1beta2.DynamicMutatingAdmissionConfig,
) error {
	desiredName := string(cfg.Name)

	desiredHooks, err := r.webhooks(ctx, cfg)
	if err != nil {
		return err
	}

	if len(desiredHooks) == 0 {
		managed, err := r.listManagedWebhookConfigs(ctx)
		if err != nil {
			return err
		}

		for i := range managed {
			if err := r.deleteWebhookConfig(ctx, managed[i].Name); err != nil {
				return err
			}
		}

		return nil
	}

	sort.Slice(desiredHooks, func(i, j int) bool { return desiredHooks[i].Name < desiredHooks[j].Name })

	obj := &admissionv1.MutatingWebhookConfiguration{
		TypeMeta: metav1.TypeMeta{
			APIVersion: admissionv1.SchemeGroupVersion.String(),
			Kind:       "MutatingWebhookConfiguration",
		},
		ObjectMeta: metav1.ObjectMeta{Name: string(cfg.Name)},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.client, obj, func() error {
		if err := controllerutil.SetOwnerReference(
			r.configuration.GetConfigObject(),
			obj,
			r.client.Scheme(),
		); err != nil {
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

		obj.Webhooks = desiredHooks

		// Do not overwrite caBundle. cert-manager or the legacy TLS reconciler owns it.
		//if cfg.Client.CABundle == nil {
		//	obj.Webhooks = preserveMutatingWebhookCABundles(obj.Webhooks, desiredHooks)
		//} else {
		//	obj.Webhooks = desiredHooks
		//}

		return err
	})
	if err != nil {
		return err
	}

	// Garbage-collect any old managed validating webhook configs with different name
	managed, err := r.listManagedWebhookConfigs(ctx)
	if err != nil {
		return err
	}

	for i := range managed {
		if managed[i].Name == desiredName {
			continue
		}

		if err := r.deleteWebhookConfig(ctx, managed[i].Name); err != nil {
			return err
		}
	}

	return nil
}

func (r *mutatingReconciler) listManagedWebhookConfigs(ctx context.Context) ([]admissionv1.MutatingWebhookConfiguration, error) {
	list := &admissionv1.MutatingWebhookConfigurationList{}
	if err := r.client.List(ctx, list, client.MatchingLabels{
		meta.CreatedByCapsuleLabel: meta.ValueController,
	}); err != nil {
		return nil, err
	}

	return list.Items, nil
}

func (r *mutatingReconciler) deleteWebhookConfig(ctx context.Context, name string) error {
	if name == "" {
		return nil
	}

	obj := &admissionv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}

	err := r.client.Delete(ctx, obj)
	if apierrors.IsNotFound(err) {
		return nil
	}

	return err
}

func (r *mutatingReconciler) webhooks(
	ctx context.Context,
	cfg *capsulev1beta2.DynamicMutatingAdmissionConfig,
) (hooks []admissionv1.MutatingWebhook, err error) {
	for _, hook := range cfg.Webhooks {
		h, err := admission.NewMutatingWebhook(hook, cfg.Client, r.configuration.Users(), r.configuration.Administrators())
		if err != nil {
			return nil, err
		}

		hooks = append(hooks, h)
	}

	return hooks, nil
}

func preserveMutatingWebhookCABundles(
	existing []admissionv1.MutatingWebhook,
	desired []admissionv1.MutatingWebhook,
) []admissionv1.MutatingWebhook {
	existingByName := make(map[string][]byte, len(existing))

	for _, hook := range existing {
		if len(hook.ClientConfig.CABundle) == 0 {
			continue
		}

		existingByName[hook.Name] = append([]byte(nil), hook.ClientConfig.CABundle...)
	}

	for i := range desired {
		if caBundle, ok := existingByName[desired[i].Name]; ok {
			desired[i].ClientConfig.CABundle = append([]byte(nil), caBundle...)
		}
	}

	return desired
}
