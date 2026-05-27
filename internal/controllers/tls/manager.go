// Copyright 2020-2026 Project Capsule Authors
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
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	caperrors "github.com/projectcapsule/capsule/pkg/api/errors"
	"github.com/projectcapsule/capsule/pkg/runtime/cert"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
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
		For(
			&corev1.Secret{},
			builder.WithPredicates(
				predicates.NamesMatchingPredicate{Names: []string{r.Configuration.TLSSecretName()}},
			),
		).
		Named("capsule/tls").
		Watches(
			&admissionregistrationv1.ValidatingWebhookConfiguration{},
			enqueueFn,
			builder.WithPredicates(
				predicates.NamesMatchingPredicate{Names: []string{string(r.Configuration.Admission().Validating.Name)}},
			),
		).
		Watches(
			&admissionregistrationv1.MutatingWebhookConfiguration{},
			enqueueFn,
			builder.WithPredicates(
				predicates.NamesMatchingPredicate{Names: []string{string(r.Configuration.Admission().Mutating.Name)}},
			),
		).
		Watches(
			&apiextensionsv1.CustomResourceDefinition{},
			enqueueFn,
			builder.WithPredicates(
				predicates.NamesMatchingPredicate{Names: r.managedCRDNames()},
			),
		).
		Complete(r)
}

func (r Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	r.Log = r.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)

	certSecret := &corev1.Secret{}

	if err := r.Get(ctx, request.NamespacedName, certSecret); err != nil {
		if !apierrors.IsNotFound(err) {
			return reconcile.Result{}, err
		}

		certSecret = &corev1.Secret{}
		certSecret.Name = request.Name
		certSecret.Namespace = request.Namespace
		certSecret.Type = corev1.SecretTypeTLS
		certSecret.Data = map[string][]byte{}
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
	requeueAfter := requeueTime.Sub(now)
	requeueAfter = max(requeueAfter, 0)

	r.Log.V(4).Info("Reconciliation completed, processing back in " + requeueAfter.String())

	return reconcile.Result{Requeue: true, RequeueAfter: requeueAfter}, nil
}

func (r Reconciler) ReconcileCertificates(ctx context.Context, certSecret *corev1.Secret) error {
	if r.shouldUpdateCertificate(certSecret) {
		r.Log.V(3).Info("Generating new TLS certificate")

		ca, err := cert.GenerateCertificateAuthority()
		if err != nil {
			return err
		}

		opts := cert.NewCertOpts(
			time.Now().Add(certificateValidity),
			fmt.Sprintf("capsule-webhook-service.%s.svc", r.Namespace),
		)

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

		t := &corev1.Secret{
			ObjectMeta: certSecret.ObjectMeta,
			Type:       corev1.SecretTypeTLS,
		}

		_, err = controllerutil.CreateOrUpdate(ctx, r.Client, t, func() error {
			t.Data = certSecret.Data

			return nil
		})
		if err != nil {
			r.Log.Error(err, "cannot update Capsule TLS")

			return err
		}
	}

	caBundle, ok := certSecret.Data[corev1.ServiceAccountRootCAKey]
	if !ok {
		return fmt.Errorf("missing %s field in %s secret", corev1.ServiceAccountRootCAKey, r.Configuration.TLSSecretName())
	}

	r.Log.V(4).Info("Updating caBundle in webhooks and CRDs")

	patchGroup := new(errgroup.Group)

	patchGroup.Go(func() error {
		return r.updateMutatingWebhookConfiguration(ctx, caBundle)
	})

	patchGroup.Go(func() error {
		return r.updateValidatingWebhookConfiguration(ctx, caBundle)
	})

	for key, crd := range r.conversionManagedCRDs() {
		patchGroup.Go(func() error {
			if err := r.updateManagedCustomResourceDefinition(ctx, crd, caBundle); err != nil {
				return fmt.Errorf("cannot update managed CRD %q (%s): %w", key, crd.Name, err)
			}

			return nil
		})
	}

	if err := patchGroup.Wait(); err != nil {
		return err
	}

	r.annotateOperatorPodsBestEffort(ctx)

	return nil
}

func (r Reconciler) annotateOperatorPodsBestEffort(ctx context.Context) {
	operatorPods, err := r.getOperatorPods(ctx)
	if err != nil {
		if errors.As(err, &caperrors.RunningInOutOfClusterModeError{}) {
			r.Log.Info("skipping annotation of Pods for cert-manager", "error", err.Error())
			return
		}

		r.Log.Error(err, "cannot retrieve Capsule operator pods for TLS reload")
		return
	}

	r.Log.V(4).Info("Updating capsule operator pods")

	group := new(errgroup.Group)
	group.SetLimit(4)

	for _, pod := range operatorPods.Items {
		p := pod

		group.Go(func() error {
			if err := r.updateOperatorPod(ctx, p); err != nil {
				r.Log.Error(err, "cannot update capsule operator pod", "name", p.Name, "namespace", p.Namespace)
			}

			return nil
		})
	}

	_ = group.Wait()
}

func (r Reconciler) shouldUpdateCertificate(secret *corev1.Secret) bool {
	if secret == nil {
		return true
	}

	if secret.Data == nil {
		return true
	}

	if _, ok := secret.Data[corev1.ServiceAccountRootCAKey]; !ok {
		return true
	}

	certificate, key, err := cert.GetCertificateWithPrivateKeyFromBytes(
		secret.Data[corev1.TLSCertKey],
		secret.Data[corev1.TLSPrivateKeyKey],
	)
	if err != nil {
		return true
	}

	if err := cert.ValidateCertificate(certificate, key, certificateExpirationThreshold); err != nil {
		r.Log.Error(err, "failed to validate certificate, generating new one")

		return true
	}

	r.Log.V(4).Info("Skipping TLS certificate generation as it is still valid")

	return false
}

func (r *Reconciler) updateManagedCustomResourceDefinition(
	ctx context.Context,
	managed ManagedCRD,
	caBundle []byte,
) error {
	if !managed.ManageConversion {
		return nil
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		crd := &apiextensionsv1.CustomResourceDefinition{}

		if err := r.Get(ctx, types.NamespacedName{Name: managed.Name}, crd); err != nil {
			return err
		}

		path := managed.ConversionPath
		if path == "" {
			path = "/convert"
		}

		versions := managed.ConversionReviewVersions
		if len(versions) == 0 {
			versions = []string{"v1", "v1beta1"}
		}

		port := int32(443)

		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, crd, func() error {
			crd.Spec.Conversion = &apiextensionsv1.CustomResourceConversion{
				Strategy: apiextensionsv1.WebhookConverter,
				Webhook: &apiextensionsv1.WebhookConversion{
					ClientConfig: &apiextensionsv1.WebhookClientConfig{
						Service: &apiextensionsv1.ServiceReference{
							Namespace: r.Namespace,
							Name:      r.Configuration.Admission().ServiceName,
							Path:      &path,
							Port:      &port,
						},
						CABundle: caBundle,
					},
					ConversionReviewVersions: versions,
				},
			}

			return nil
		})

		return err
	})
}

//nolint:dupl
func (r Reconciler) updateValidatingWebhookConfiguration(ctx context.Context, caBundle []byte) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		vw := &admissionregistrationv1.ValidatingWebhookConfiguration{}

		if err := r.Get(ctx, types.NamespacedName{Name: string(r.Configuration.Admission().Validating.Name)}, vw); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		for i, w := range vw.Webhooks {
			if w.ClientConfig.Service != nil {
				vw.Webhooks[i].ClientConfig.CABundle = caBundle
			}
		}

		return r.Update(ctx, vw, &client.UpdateOptions{})
	})
}

//nolint:dupl
func (r Reconciler) updateMutatingWebhookConfiguration(ctx context.Context, caBundle []byte) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		mw := &admissionv1.MutatingWebhookConfiguration{
			TypeMeta: metav1.TypeMeta{
				APIVersion: admissionv1.SchemeGroupVersion.String(),
				Kind:       "MutatingWebhookConfiguration",
			},
		}

		if err := r.Get(ctx, types.NamespacedName{Name: string(r.Configuration.Admission().Mutating.Name)}, mw); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
		}

		for i, w := range mw.Webhooks {
			if w.ClientConfig.Service != nil {
				mw.Webhooks[i].ClientConfig.CABundle = caBundle
			}
		}

		return r.Update(ctx, mw, &client.UpdateOptions{})
	})
}

func (r Reconciler) updateOperatorPod(ctx context.Context, pod corev1.Pod) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		p := &corev1.Pod{}

		if err := r.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, p); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			r.Log.Error(err, "cannot get pod", "name", pod.Name, "namespace", pod.Namespace)

			return err
		}

		if p.Annotations == nil {
			p.Annotations = map[string]string{}
		}

		p.Annotations[PodUpdateAnnotationName] = time.Now().Format(time.RFC3339Nano)

		if err := r.Update(ctx, p, &client.UpdateOptions{}); err != nil {
			r.Log.Error(err, "cannot update pod", "name", pod.Name, "namespace", pod.Namespace)

			return err
		}

		return nil
	})
}

func (r Reconciler) getOperatorPods(ctx context.Context) (*corev1.PodList, error) {
	hostname, _ := os.Hostname()

	leaderPod := &corev1.Pod{}

	if err := r.Get(ctx, types.NamespacedName{Namespace: os.Getenv("NAMESPACE"), Name: hostname}, leaderPod); err != nil {
		return nil, caperrors.RunningInOutOfClusterModeError{}
	}

	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.MatchingLabels(leaderPod.Labels)); err != nil {
		r.Log.Error(err, "cannot retrieve list of Capsule pods")

		return nil, err
	}

	return podList, nil
}
