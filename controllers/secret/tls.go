// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package secret

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/clastix/capsule/pkg/cert"
	"github.com/clastix/capsule/pkg/configuration"
)

type TLSReconciler struct {
	client.Client
	Log           logr.Logger
	Scheme        *runtime.Scheme
	Namespace     string
	Configuration configuration.Configuration
}

func (r *TLSReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).
		Complete(r)
}

func (r TLSReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	var err error

	if request.Name != r.Configuration.TLSSecretName() {
		return ctrl.Result{}, nil
	}

	r.Log = r.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	r.Log.Info("Reconciling TLS Secret")

	// Fetch the Secret instance
	instance := &corev1.Secret{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	var ca cert.CA

	var rq time.Duration

	ca, err = getCertificateAuthority(ctx, r.Client, r.Namespace, r.Configuration.CASecretName())
	if err != nil {
		return reconcile.Result{}, err
	}

	var shouldCreate bool

	for _, key := range []string{corev1.TLSCertKey, corev1.TLSPrivateKeyKey} {
		if _, ok := instance.Data[key]; !ok {
			shouldCreate = true

			break
		}
	}

	if shouldCreate {
		r.Log.Info("Missing Capsule TLS certificate")

		rq = 6 * 30 * 24 * time.Hour

		opts := cert.NewCertOpts(time.Now().Add(rq), fmt.Sprintf("capsule-webhook-service.%s.svc", r.Namespace))

		var crt, key *bytes.Buffer

		if crt, key, err = ca.GenerateCertificate(opts); err != nil {
			r.Log.Error(err, "Cannot generate new TLS certificate")

			return reconcile.Result{}, err
		}

		instance.Data = map[string][]byte{
			corev1.TLSCertKey:       crt.Bytes(),
			corev1.TLSPrivateKeyKey: key.Bytes(),
		}
	} else {
		var c *x509.Certificate
		var b *pem.Block
		b, _ = pem.Decode(instance.Data[corev1.TLSCertKey])
		c, err = x509.ParseCertificate(b.Bytes)
		if err != nil {
			r.Log.Error(err, "cannot parse Capsule TLS")

			return reconcile.Result{}, err
		}

		rq = time.Until(c.NotAfter)

		err = ca.ValidateCert(c)
		if err != nil {
			r.Log.Info("Capsule TLS is expired or invalid, cleaning to obtain a new one")
			instance.Data = map[string][]byte{}
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
	// nolint:nestif
	if instance.Name == r.Configuration.TLSSecretName() && res == controllerutil.OperationResultUpdated {
		r.Log.Info("Capsule TLS certificates has been updated, Controller pods must be restarted to load new certificate")

		hostname, _ := os.Hostname()

		leaderPod := &corev1.Pod{}

		if err = r.Client.Get(ctx, types.NamespacedName{Namespace: os.Getenv("NAMESPACE"), Name: hostname}, leaderPod); err != nil {
			r.Log.Error(err, "cannot retrieve the leader Pod, probably running in out of the cluster mode")

			return reconcile.Result{}, nil
		}

		podList := &corev1.PodList{}
		if err = r.Client.List(ctx, podList, client.MatchingLabels(leaderPod.ObjectMeta.Labels)); err != nil {
			r.Log.Error(err, "cannot retrieve list of Capsule pods requiring restart upon TLS update")

			return reconcile.Result{}, nil
		}

		for _, p := range podList.Items {
			nonLeaderPod := p
			// Skipping this Pod, must be deleted at the end
			if nonLeaderPod.GetName() == leaderPod.GetName() {
				continue
			}

			if err = r.Client.Delete(ctx, &nonLeaderPod); err != nil {
				r.Log.Error(err, "cannot delete the non-leader Pod due to TLS update")
			}
		}

		if err = r.Client.Delete(ctx, leaderPod); err != nil {
			r.Log.Error(err, "cannot delete the leader Pod due to TLS update")
		}
	}

	r.Log.Info("Reconciliation completed, processing back in " + rq.String())

	return reconcile.Result{Requeue: true, RequeueAfter: rq}, nil
}
