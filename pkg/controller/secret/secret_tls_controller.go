/*
Copyright 2020 Clastix Labs.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package secret

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/clastix/capsule/pkg/cert"
)

// Add creates a new Secret Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func AddTls(mgr manager.Manager) error {
	return tls(mgr, newReconciler(mgr, "controller_secret_tls", tlsReconcile))
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func tls(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("secret-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to TLS Secret
	err = c.Watch(&source.Kind{Type: &corev1.Secret{}}, &handler.EnqueueRequestForObject{}, predicate.Funcs{
		CreateFunc: func(event event.CreateEvent) (ok bool) {
			return filterByName(event.Meta.GetName(), TlsSecretName)
		},
		DeleteFunc: func(deleteEvent event.DeleteEvent) bool {
			return filterByName(deleteEvent.Meta.GetName(), TlsSecretName)
		},
		UpdateFunc: func(updateEvent event.UpdateEvent) bool {
			return filterByName(updateEvent.MetaNew.GetName(), TlsSecretName)
		},
		GenericFunc: func(genericEvent event.GenericEvent) bool {
			return filterByName(genericEvent.Meta.GetName(), TlsSecretName)
		},
	})
	if err != nil {
		return err
	}

	return nil
}

func tlsReconcile(r *ReconcileSecret, request reconcile.Request) (reconcile.Result, error) {
	var err error

	r.logger = r.logger.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	r.logger.Info("Reconciling TLS/CA Secret")

	// Fetch the Secret instance
	instance := &corev1.Secret{}
	err = r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	var ca cert.Ca
	var rq time.Duration

	ca, err = r.GetCertificateAuthority()
	if err != nil {
		return reconcile.Result{}, err
	}

	var shouldCreate bool
	for _, key := range []string{Cert, PrivateKey} {
		if _, ok := instance.Data[key]; !ok {
			shouldCreate = true
		}
	}

	if shouldCreate {
		r.logger.Info("Missing Capsule TLS certificate")
		rq = 6 * 30 * 24 * time.Hour

		opts := cert.NewCertOpts(time.Now().Add(rq), "capsule.capsule-system.svc")
		crt, key, err := ca.GenerateCertificate(opts)
		if err != nil {
			r.logger.Error(err, "Cannot generate new TLS certificate")
			return reconcile.Result{}, err
		}
		instance.Data = map[string][]byte{
			Cert:       crt.Bytes(),
			PrivateKey: key.Bytes(),
		}
	} else {
		var c *x509.Certificate
		var b *pem.Block
		b, _ = pem.Decode(instance.Data[Cert])
		c, err = x509.ParseCertificate(b.Bytes)
		if err != nil {
			r.logger.Error(err, "cannot parse Capsule TLS")
			return reconcile.Result{}, err
		}

		rq = time.Duration(c.NotAfter.Unix()-time.Now().Unix()) * time.Second
		if time.Now().After(c.NotAfter) {
			r.logger.Info("Capsule TLS is expired, cleaning to obtain a new one")
			instance.Data = map[string][]byte{}
		}
	}

	var res controllerutil.OperationResult
	t := &corev1.Secret{ObjectMeta: instance.ObjectMeta}
	res, err = controllerutil.CreateOrUpdate(context.TODO(), r.client, t, func() error {
		t.Data = instance.Data
		return nil
	})
	if err != nil {
		r.logger.Error(err, "cannot update Capsule TLS")
		return reconcile.Result{}, err
	}

	if instance.Name == TlsSecretName && res == controllerutil.OperationResultUpdated {
		r.logger.Info("Capsule TLS certificates has been updated, we need to restart the Controller")
		os.Exit(15)
	}

	r.logger.Info("Reconciliation completed, processing back in " + rq.String())
	return reconcile.Result{Requeue: true, RequeueAfter: rq}, nil
}
