// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package secret

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/clastix/capsule/pkg/cert"
	"github.com/clastix/capsule/pkg/configuration"
)

type CAReconciler struct {
	client.Client
	Log           logr.Logger
	Scheme        *runtime.Scheme
	Namespace     string
	Configuration configuration.Configuration
}

func (r *CAReconciler) SetupWithManager(mgr ctrl.Manager) error {
	enqueueFn := handler.EnqueueRequestsFromMapFunc(func(client.Object) []reconcile.Request {
		return []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{
					Namespace: r.Namespace,
					Name:      r.Configuration.CASecretName(),
				},
			},
		}
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).
		Watches(source.NewKindWithCache(&admissionregistrationv1.ValidatingWebhookConfiguration{}, mgr.GetCache()), enqueueFn, builder.WithPredicates(predicate.NewPredicateFuncs(func(object client.Object) bool {
			return object.GetName() == r.Configuration.ValidatingWebhookConfigurationName()
		}))).
		Watches(source.NewKindWithCache(&admissionregistrationv1.MutatingWebhookConfiguration{}, mgr.GetCache()), enqueueFn, builder.WithPredicates(predicate.NewPredicateFuncs(func(object client.Object) bool {
			return object.GetName() == r.Configuration.MutatingWebhookConfigurationName()
		}))).
		Complete(r)
}

// By default helm doesn't allow to use templates in CRD (https://helm.sh/docs/chart_best_practices/custom_resource_definitions/#method-1-let-helm-do-it-for-you).
// In order to overcome this, we are setting conversion strategy in helm chart to None, and then update it with CA and namespace information.
func (r *CAReconciler) UpdateCustomResourceDefinition(ctx context.Context, caBundle []byte) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		err = r.Get(ctx, types.NamespacedName{Name: "tenants.capsule.clastix.io"}, crd)
		if err != nil {
			r.Log.Error(err, "cannot retrieve CustomResourceDefinition")

			return err
		}

		_, err = controllerutil.CreateOrUpdate(ctx, r.Client, crd, func() error {
			crd.Spec.Conversion = &apiextensionsv1.CustomResourceConversion{
				Strategy: "Webhook",
				Webhook: &apiextensionsv1.WebhookConversion{
					ClientConfig: &apiextensionsv1.WebhookClientConfig{
						Service: &apiextensionsv1.ServiceReference{
							Namespace: r.Namespace,
							Name:      "capsule-webhook-service",
							Path:      pointer.StringPtr("/convert"),
							Port:      pointer.Int32Ptr(443),
						},
						CABundle: caBundle,
					},
					ConversionReviewVersions: []string{"v1alpha1", "v1beta1"},
				},
			}

			return nil
		})

		return err
	})
}

//nolint:dupl
func (r CAReconciler) UpdateValidatingWebhookConfiguration(ctx context.Context, caBundle []byte) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		vw := &admissionregistrationv1.ValidatingWebhookConfiguration{}
		err = r.Get(ctx, types.NamespacedName{Name: r.Configuration.ValidatingWebhookConfigurationName()}, vw)
		if err != nil {
			r.Log.Error(err, "cannot retrieve ValidatingWebhookConfiguration")

			return err
		}
		for i, w := range vw.Webhooks {
			// Updating CABundle only in case of an internal service reference
			if w.ClientConfig.Service != nil {
				vw.Webhooks[i].ClientConfig.CABundle = caBundle
			}
		}

		return r.Update(ctx, vw, &client.UpdateOptions{})
	})
}

//nolint:dupl
func (r CAReconciler) UpdateMutatingWebhookConfiguration(ctx context.Context, caBundle []byte) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		mw := &admissionregistrationv1.MutatingWebhookConfiguration{}
		err = r.Get(ctx, types.NamespacedName{Name: r.Configuration.MutatingWebhookConfigurationName()}, mw)
		if err != nil {
			r.Log.Error(err, "cannot retrieve MutatingWebhookConfiguration")

			return err
		}
		for i, w := range mw.Webhooks {
			// Updating CABundle only in case of an internal service reference
			if w.ClientConfig.Service != nil {
				mw.Webhooks[i].ClientConfig.CABundle = caBundle
			}
		}

		return r.Update(ctx, mw, &client.UpdateOptions{})
	})
}

func (r CAReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	var err error

	if request.Name != r.Configuration.CASecretName() {
		return ctrl.Result{}, nil
	}

	r.Log = r.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	r.Log.Info("Reconciling CA Secret")

	// Fetch the CA instance
	instance := &corev1.Secret{}

	if err = r.Client.Get(ctx, request.NamespacedName, instance); err != nil {
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	var ca cert.CA

	var rq time.Duration

	ca, err = getCertificateAuthority(ctx, r.Client, r.Namespace, r.Configuration.CASecretName())
	if err != nil && errors.Is(err, MissingCaError{}) {
		ca, err = cert.GenerateCertificateAuthority()
		if err != nil {
			return reconcile.Result{}, err
		}
	} else if err != nil {
		return reconcile.Result{}, err
	}

	r.Log.Info("Handling CA Secret")

	rq, err = ca.ExpiresIn(time.Now())
	if err != nil {
		r.Log.Info("CA is expired, cleaning to obtain a new one")

		instance.Data = map[string][]byte{}
	} else {
		r.Log.Info("Updating CA secret with new PEM and RSA")

		var crt *bytes.Buffer
		var key *bytes.Buffer
		crt, _ = ca.CACertificatePem()
		key, _ = ca.CAPrivateKeyPem()

		instance.Data = map[string][]byte{
			corev1.TLSCertKey:       crt.Bytes(),
			corev1.TLSPrivateKeyKey: key.Bytes(),
		}

		group := new(errgroup.Group)
		group.Go(func() error {
			return r.UpdateMutatingWebhookConfiguration(ctx, crt.Bytes())
		})
		group.Go(func() error {
			return r.UpdateValidatingWebhookConfiguration(ctx, crt.Bytes())
		})
		group.Go(func() error {
			return r.UpdateCustomResourceDefinition(ctx, crt.Bytes())
		})

		if err = group.Wait(); err != nil {
			return reconcile.Result{}, err
		}
	}

	var res controllerutil.OperationResult

	t := &corev1.Secret{ObjectMeta: instance.ObjectMeta}

	res, err = controllerutil.CreateOrUpdate(ctx, r.Client, t, func() error {
		t.Data = instance.Data

		return nil
	})
	if err != nil {
		r.Log.Error(err, "cannot update Capsule TLS")

		return reconcile.Result{}, err
	}

	if res == controllerutil.OperationResultUpdated {
		r.Log.Info("Capsule CA has been updated, we need to trigger TLS update too")

		tls := &corev1.Secret{}
		err = r.Get(ctx, types.NamespacedName{
			Namespace: r.Namespace,
			Name:      r.Configuration.TLSSecretName(),
		}, tls)

		if err != nil {
			r.Log.Error(err, "Capsule TLS Secret missing")
		}

		err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			_, err = controllerutil.CreateOrUpdate(ctx, r.Client, tls, func() error {
				tls.Data = map[string][]byte{}

				return nil
			})

			return err
		})
		if err != nil {
			r.Log.Error(err, "Cannot clean Capsule TLS Secret due to CA update")

			return reconcile.Result{}, err
		}
	}

	r.Log.Info("Reconciliation completed, processing back in " + rq.String())

	return reconcile.Result{Requeue: true, RequeueAfter: rq}, nil
}
