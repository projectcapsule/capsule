// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tls

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

	"github.com/clastix/capsule/controllers/utils"
	"github.com/clastix/capsule/pkg/cert"
	"github.com/clastix/capsule/pkg/configuration"
)

const (
	certificateExpirationThreshold = 3 * 24 * time.Hour
	certificateValidity            = 6 * 30 * 24 * time.Hour
	PodUpdateAnnotationName        = "capsule.clastix.io/updated"
)

type Reconciler struct {
	client.Client
	Log           logr.Logger
	Scheme        *runtime.Scheme
	Namespace     string
	Configuration configuration.Configuration
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	enqueueFn := handler.EnqueueRequestsFromMapFunc(func(context.Context, client.Object) []reconcile.Request {
		return []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{
					Namespace: r.Namespace,
					Name:      r.Configuration.TLSSecretName(),
				},
			},
		}
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}, utils.NamesMatchingPredicate(r.Configuration.TLSSecretName())).
		Watches(&admissionregistrationv1.ValidatingWebhookConfiguration{}, enqueueFn, builder.WithPredicates(predicate.NewPredicateFuncs(func(object client.Object) bool {
			return object.GetName() == r.Configuration.ValidatingWebhookConfigurationName()
		}))).
		Watches(&admissionregistrationv1.MutatingWebhookConfiguration{}, enqueueFn, builder.WithPredicates(predicate.NewPredicateFuncs(func(object client.Object) bool {
			return object.GetName() == r.Configuration.MutatingWebhookConfigurationName()
		}))).
		Watches(&apiextensionsv1.CustomResourceDefinition{}, enqueueFn, builder.WithPredicates(predicate.NewPredicateFuncs(func(object client.Object) bool {
			return object.GetName() == r.Configuration.TenantCRDName()
		}))).
		Complete(r)
}

func (r Reconciler) ReconcileCertificates(ctx context.Context, certSecret *corev1.Secret) error {
	if r.shouldUpdateCertificate(certSecret) {
		r.Log.Info("Generating new TLS certificate")

		ca, err := cert.GenerateCertificateAuthority()
		if err != nil {
			return err
		}

		opts := cert.NewCertOpts(time.Now().Add(certificateValidity), fmt.Sprintf("capsule-webhook-service.%s.svc", r.Namespace))

		crt, key, err := ca.GenerateCertificate(opts)
		if err != nil {
			r.Log.Error(err, "Cannot generate new TLS certificate")

			return err
		}

		caCrt, _ := ca.CACertificatePem()

		certSecret.Data = map[string][]byte{
			corev1.TLSCertKey:              crt.Bytes(),
			corev1.TLSPrivateKeyKey:        key.Bytes(),
			corev1.ServiceAccountRootCAKey: caCrt.Bytes(),
		}

		t := &corev1.Secret{ObjectMeta: certSecret.ObjectMeta}

		_, err = controllerutil.CreateOrUpdate(ctx, r.Client, t, func() error {
			t.Data = certSecret.Data

			return nil
		})
		if err != nil {
			r.Log.Error(err, "cannot update Capsule TLS")

			return err
		}
	}

	var caBundle []byte

	var ok bool

	if caBundle, ok = certSecret.Data[corev1.ServiceAccountRootCAKey]; !ok {
		return fmt.Errorf("missing %s field in %s secret", corev1.ServiceAccountRootCAKey, r.Configuration.TLSSecretName())
	}

	r.Log.Info("Updating caBundle in webhooks and crd")

	group := new(errgroup.Group)
	group.Go(func() error {
		return r.updateMutatingWebhookConfiguration(ctx, caBundle)
	})
	group.Go(func() error {
		return r.updateValidatingWebhookConfiguration(ctx, caBundle)
	})
	group.Go(func() error {
		return r.updateTenantCustomResourceDefinition(ctx, "tenants.capsule.clastix.io", caBundle)
	})
	group.Go(func() error {
		return r.updateTenantCustomResourceDefinition(ctx, "capsuleconfigurations.capsule.clastix.io", caBundle)
	})

	operatorPods, err := r.getOperatorPods(ctx)
	if err != nil {
		if errors.As(err, &RunningInOutOfClusterModeError{}) {
			r.Log.Info("skipping annotation of Pods for cert-manager", "error", err.Error())

			return nil
		}

		return err
	}

	r.Log.Info("Updating capsule operator pods")

	for _, pod := range operatorPods.Items {
		p := pod

		group.Go(func() error {
			return r.updateOperatorPod(ctx, p)
		})
	}

	if err := group.Wait(); err != nil {
		return err
	}

	return nil
}

func (r Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	r.Log = r.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)

	certSecret := &corev1.Secret{}

	if err := r.Client.Get(ctx, request.NamespacedName, certSecret); err != nil {
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if err := r.ReconcileCertificates(ctx, certSecret); err != nil {
		return reconcile.Result{}, err
	}

	certificate, err := cert.GetCertificateFromBytes(certSecret.Data[corev1.TLSCertKey])
	if err != nil {
		return reconcile.Result{}, err
	}

	now := time.Now()
	requeueTime := certificate.NotAfter.Add(-(certificateExpirationThreshold - 1*time.Second))
	rq := requeueTime.Sub(now)

	r.Log.Info("Reconciliation completed, processing back in " + rq.String())

	return reconcile.Result{Requeue: true, RequeueAfter: rq}, nil
}

func (r Reconciler) shouldUpdateCertificate(secret *corev1.Secret) bool {
	if _, ok := secret.Data[corev1.ServiceAccountRootCAKey]; !ok {
		return true
	}

	certificate, key, err := cert.GetCertificateWithPrivateKeyFromBytes(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey])
	if err != nil {
		return true
	}

	if err := cert.ValidateCertificate(certificate, key, certificateExpirationThreshold); err != nil {
		r.Log.Error(err, "failed to validate certificate, generating new one")

		return true
	}

	r.Log.Info("Skipping TLS certificate generation as it is still valid")

	return false
}

// By default helm doesn't allow to use templates in CRD (https://helm.sh/docs/chart_best_practices/custom_resource_definitions/#method-1-let-helm-do-it-for-you).
// In order to overcome this, we are setting conversion strategy in helm chart to None, and then update it with CA and namespace information.
func (r *Reconciler) updateTenantCustomResourceDefinition(ctx context.Context, name string, caBundle []byte) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		err = r.Get(ctx, types.NamespacedName{Name: name}, crd)
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
							Path:      pointer.String("/convert"),
							Port:      pointer.Int32(443),
						},
						CABundle: caBundle,
					},
					ConversionReviewVersions: []string{"v1alpha1", "v1beta1", "v1beta2"},
				},
			}

			return nil
		})

		return err
	})
}

//nolint:dupl
func (r Reconciler) updateValidatingWebhookConfiguration(ctx context.Context, caBundle []byte) error {
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
func (r Reconciler) updateMutatingWebhookConfiguration(ctx context.Context, caBundle []byte) error {
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

func (r Reconciler) updateOperatorPod(ctx context.Context, pod corev1.Pod) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Need to get latest version of pod
		p := &corev1.Pod{}

		if err := r.Client.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, p); err != nil && !apierrors.IsNotFound(err) {
			r.Log.Error(err, "cannot get pod", "name", pod.Name, "namespace", pod.Namespace)

			return err
		}

		if p.Annotations == nil {
			p.Annotations = map[string]string{}
		}

		p.Annotations[PodUpdateAnnotationName] = time.Now().Format(time.RFC3339Nano)

		if err := r.Client.Update(ctx, p, &client.UpdateOptions{}); err != nil {
			r.Log.Error(err, "cannot update pod", "name", pod.Name, "namespace", pod.Namespace)

			return err
		}

		return nil
	})
}

func (r Reconciler) getOperatorPods(ctx context.Context) (*corev1.PodList, error) {
	hostname, _ := os.Hostname()

	leaderPod := &corev1.Pod{}

	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: os.Getenv("NAMESPACE"), Name: hostname}, leaderPod); err != nil {
		return nil, RunningInOutOfClusterModeError{}
	}

	podList := &corev1.PodList{}
	if err := r.Client.List(ctx, podList, client.MatchingLabels(leaderPod.ObjectMeta.Labels)); err != nil {
		r.Log.Error(err, "cannot retrieve list of Capsule pods")

		return nil, err
	}

	return podList, nil
}
