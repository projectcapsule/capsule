// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl
package admission

import (
	"context"
	"maps"
	"sort"
	"time"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/internal/cache"
	"github.com/projectcapsule/capsule/internal/controllers/tls"
	"github.com/projectcapsule/capsule/internal/controllers/utils"
	"github.com/projectcapsule/capsule/internal/metrics"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
	"github.com/projectcapsule/capsule/pkg/runtime/replication"
)

type validatingReconciler struct {
	client client.Client

	configuration  configuration.Configuration
	log            logr.Logger
	conditions     *cache.CELCache
	triggerMetrics *metrics.TriggerRecorder
	delivery       *replication.DeliveryKey
}

func (r *validatingReconciler) SetupWithManager(mgr ctrl.Manager, ctrlConfig utils.ControllerOptions) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("capsule/admission/validating").
		For(
			&admissionv1.ValidatingWebhookConfiguration{},
			builder.WithPredicates(
				predicates.ValidatingAdmissionConfigurationChangedPredicate{},
				predicates.NamesMatchingPredicate{Names: []string{string(r.configuration.Admission().Validating.Name)}},
			),
		).
		Watches(
			&capsulev1beta2.CapsuleConfiguration{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{{
					Name:      string(r.configuration.Admission().Validating.Name),
					Namespace: admissionConfigurationEventMarker,
				}}
			}),
			builder.WithPredicates(
				predicates.CapsuleConfigSpecAdmissionChangedPredicate{},
				predicates.NamesMatchingPredicate{Names: []string{ctrlConfig.ConfigurationName}},
			),
		).
		Watches(&capsulev1beta2.TenantResource{}, handler.EnqueueRequestsFromMapFunc(r.enqueueTriggerConfiguration), builder.WithPredicates(triggerSourceChanged())).
		Watches(&capsulev1beta2.GlobalTenantResource{}, handler.EnqueueRequestsFromMapFunc(r.enqueueTriggerConfiguration), builder.WithPredicates(triggerSourceChanged())).
		WithOptions(ctrlConfig.Runtime.ToControllerOptions()).
		Complete(r)
}

func triggerSourceChanged() predicate.Predicate {
	return predicate.Or(predicate.GenerationChangedPredicate{}, predicates.DeletionChangedPredicate{})
}

func (r *validatingReconciler) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, err error) {
	started := time.Now()
	defer logOperationDuration(ctrllog.FromContext(ctx), "reconcile validating configuration", started)

	err = r.reconcileValidatingConfiguration(
		ctx,
		r.configuration.Admission().Validating,
		request.Namespace == admissionConfigurationEventMarker,
	)

	return res, err
}

func (r *validatingReconciler) enqueueTriggerConfiguration(context.Context, client.Object) []reconcile.Request {
	return []reconcile.Request{{Name: string(r.configuration.Admission().Validating.Name)}}
}

func (r *validatingReconciler) reconcileValidatingConfiguration(
	ctx context.Context,
	cfg *capsulev1beta2.DynamicValidatingAdmissionConfig,
	cleanup bool,
) error {
	desiredName := string(cfg.Name)

	desiredHooks, err := r.validatingWebhooks(ctx, cfg)
	if err != nil {
		return err
	}

	if len(desiredHooks) == 0 {
		if r.triggerMetrics != nil {
			r.triggerMetrics.OmittedRegistrations.Set(0)
		}

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

	sort.Slice(desiredHooks, func(i, j int) bool { return desiredHooks[i].Name < desiredHooks[j].Name })

	obj := &admissionv1.ValidatingWebhookConfiguration{
		APIVersion: admissionv1.SchemeGroupVersion.String(),
		Kind:       "ValidatingWebhookConfiguration",
		Name:       string(cfg.Name),
	}

	updateStarted := time.Now()
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

		// Preserve existing CA Information (cert-manager)
		existingCABundles := validatingWebhookCABundlesByName(obj.Webhooks)

		obj.Webhooks = desiredHooks

		var caCert []byte

		if r.configuration.EnableTLSConfiguration() {
			caStarted := time.Now()
			caCert, err = tls.FetchCurrentCaBundleForAdmission(ctx, r.client, r.configuration)
			logOperationDuration(ctrllog.FromContext(ctx), "fetch validating admission CA bundle", caStarted)

			if err != nil {
				return err
			}
		} else {
			caCert = cfg.Client.CABundle
		}

		if len(caCert) > 0 {
			preserveValidatingWebhookCABundles(obj.Webhooks, caCert)
		} else {
			restoreValidatingWebhookCABundles(obj.Webhooks, existingCABundles)
		}

		bounded, omitted, budgetErr := boundTriggerRegistrations(obj.Webhooks)
		if budgetErr != nil {
			return budgetErr
		}

		obj.Webhooks = bounded

		if r.triggerMetrics != nil {
			r.triggerMetrics.OmittedRegistrations.Set(float64(omitted))
		}

		if omitted > 0 {
			r.log.Info("trigger registration budget exhausted; periodic reconciliation remains active", "omittedGroups", omitted)
		}

		annotations[predicates.AdmissionStateHashAnnotation] = predicates.ValidatingAdmissionStateHash(obj)
		obj.SetAnnotations(annotations)

		return err
	})

	logOperationDuration(ctrllog.FromContext(ctx), "create or update validating webhook configuration", updateStarted)

	if err != nil {
		return err
	}

	if !cleanup {
		return nil
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
	started := time.Now()
	defer logOperationDuration(ctrllog.FromContext(ctx), "list managed validating webhook configurations", started)

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
		Name: name,
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
	if len(cfg.Webhooks) > 0 {
		users, administrators := r.configuration.Users(), r.configuration.Administrators()
		for _, hook := range cfg.Webhooks {
			h, err := admission.NewValidatingWebhook(hook, cfg.Client, users, administrators)
			if err != nil {
				return nil, err
			}

			hooks = append(hooks, h)
		}
	}

	triggerHooks, err := r.triggerWebhooks(ctx, cfg.Client)
	if err != nil {
		return nil, err
	}

	return append(hooks, triggerHooks...), nil
}

func (r *validatingReconciler) triggerWebhooks(ctx context.Context, config *admissionv1.WebhookClientConfig) ([]admissionv1.ValidatingWebhook, error) {
	// Authentication is required only for notifications. Keep the existing
	// enforcement configuration reconciling if key initialization failed.
	if r.delivery == nil {
		return nil, nil
	}

	global := &capsulev1beta2.GlobalTenantResourceList{}
	local := &capsulev1beta2.TenantResourceList{}

	filter := client.MatchingFields{replication.TriggerIndexerField: replication.TriggerEnabledKey}
	if err := r.client.List(ctx, global, filter, client.UnsafeDisableDeepCopy); err != nil {
		return nil, err
	}

	if err := r.client.List(ctx, local, filter, client.UnsafeDisableDeepCopy); err != nil {
		return nil, err
	}

	if len(global.Items)+len(local.Items) == 0 {
		return nil, nil
	}

	if err := admission.ValidateWebhookClientConfig(config); err != nil {
		return nil, err
	}

	hooks := make(map[string]admissionv1.ValidatingWebhook)
	namespaces := make(map[string]map[string]struct{})
	globalGroups := make(map[string]bool)

	add := func(spec *capsulev1beta2.TenantResourceCommonSpec, namespace string) {
		if err := replication.ValidateTriggers(spec, namespace != "", r.conditions); err != nil {
			r.log.Error(err, "skipping invalid replication triggers", "namespace", namespace)

			return
		}

		for i := range spec.Triggers {
			id := replication.MatchID(&spec.Triggers[i])
			if _, exists := hooks[id]; !exists {
				hooks[id] = replication.Webhook(&spec.Triggers[i], "", *config, r.delivery)
			}

			if namespace == "" {
				globalGroups[id] = true
			} else {
				if namespaces[id] == nil {
					namespaces[id] = make(map[string]struct{})
				}

				namespaces[id][namespace] = struct{}{}
			}
		}
	}
	for i := range global.Items {
		add(&global.Items[i].Spec.TenantResourceCommonSpec, "")
	}

	for i := range local.Items {
		add(&local.Items[i].Spec.TenantResourceCommonSpec, local.Items[i].Namespace)
	}

	out := make([]admissionv1.ValidatingWebhook, 0, len(hooks))

	for id, hook := range hooks {
		if !globalGroups[id] {
			names := make([]string, 0, len(namespaces[id]))
			for name := range namespaces[id] {
				names = append(names, name)
			}

			sort.Strings(names)
			hook.NamespaceSelector = &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: meta.TenantNameLabel, Operator: metav1.LabelSelectorOpIn, Values: names}}}
		}

		out = append(out, hook)
	}

	return out, nil
}

func validatingWebhookCABundlesByName(
	hooks []admissionv1.ValidatingWebhook,
) map[string][]byte {
	out := make(map[string][]byte, len(hooks))

	for _, hook := range hooks {
		if hook.Name == "" || len(hook.ClientConfig.CABundle) == 0 {
			continue
		}

		out[hook.Name] = append([]byte(nil), hook.ClientConfig.CABundle...)
	}

	return out
}

func restoreValidatingWebhookCABundles(
	hooks []admissionv1.ValidatingWebhook,
	existingCABundles map[string][]byte,
) {
	for i := range hooks {
		existingCABundle := existingCABundles[hooks[i].Name]
		if len(existingCABundle) == 0 {
			continue
		}

		hooks[i].ClientConfig.CABundle = append([]byte(nil), existingCABundle...)
	}
}

func preserveValidatingWebhookCABundles(
	hooks []admissionv1.ValidatingWebhook,
	caBundle []byte,
) {
	if len(caBundle) == 0 {
		return
	}

	for i := range hooks {
		hooks[i].ClientConfig.CABundle = append([]byte(nil), caBundle...)
	}
}
