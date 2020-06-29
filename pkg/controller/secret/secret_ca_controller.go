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
	"bytes"
	"context"
	"time"

	v1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
func AddCa(mgr manager.Manager) error {
	r := newReconciler(mgr, "controller_secret", caReconcile)
	return ca(mgr, r)
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func ca(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("secret-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to CA Secret
	err = c.Watch(&source.Kind{Type: &corev1.Secret{}}, &handler.EnqueueRequestForObject{}, predicate.Funcs{
		CreateFunc: func(event event.CreateEvent) (ok bool) {
			return filterByName(event.Meta.GetName(), CaSecretName)
		},
		DeleteFunc: func(deleteEvent event.DeleteEvent) bool {
			return filterByName(deleteEvent.Meta.GetName(), CaSecretName)
		},
		UpdateFunc: func(updateEvent event.UpdateEvent) bool {
			return filterByName(updateEvent.MetaNew.GetName(), CaSecretName)
		},
		GenericFunc: func(genericEvent event.GenericEvent) bool {
			return filterByName(genericEvent.Meta.GetName(), CaSecretName)
		},
	})
	if err != nil {
		return err
	}

	return nil
}

func caReconcile(r *ReconcileSecret, request reconcile.Request) (reconcile.Result, error) {
	var err error

	r.logger = r.logger.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	r.logger.Info("Reconciling CA Secret")

	// Fetch the CA instance
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

	r.logger.Info("Handling CA Secret")

	rq, err = ca.ExpiresIn(time.Now())
	if err != nil {
		r.logger.Info("CA is expired, cleaning to obtain a new one")
		instance.Data = map[string][]byte{}
	} else {
		r.logger.Info("Updating CA secret with new PEM and RSA")

		var crt *bytes.Buffer
		var key *bytes.Buffer
		crt, _ = ca.CaCertificatePem()
		key, _ = ca.CaPrivateKeyPem()

		instance.Data = map[string][]byte{
			Cert:       crt.Bytes(),
			PrivateKey: key.Bytes(),
		}

		wh := &v1.MutatingWebhookConfiguration{}
		err = r.client.Get(context.TODO(), types.NamespacedName{
			Name: "capsule",
		}, wh)
		if err != nil {
			r.logger.Error(err, "cannot retrieve MutatingWebhookConfiguration")
			return reconcile.Result{}, err
		}
		for i, w := range wh.Webhooks {
			// Updating CABundle only in case of an internal service reference
			if w.ClientConfig.Service != nil {
				wh.Webhooks[i].ClientConfig.CABundle = instance.Data[Cert]
			}
		}
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			return r.client.Update(context.TODO(), wh, &client.UpdateOptions{})
		})
		if err != nil {
			r.logger.Error(err, "cannot update MutatingWebhookConfiguration webhooks CA bundle")
			return reconcile.Result{}, err
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

	if res == controllerutil.OperationResultUpdated {
		r.logger.Info("Capsule CA has been updated, we need to trigger TLS update too")
		tls := &corev1.Secret{}
		err = r.client.Get(context.TODO(), types.NamespacedName{
			Namespace: "capsuel-system",
			Name:      TlsSecretName,
		}, tls)
		if err != nil {
			r.logger.Error(err, "Capsule TLS Secret missing")
		}
		err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			_, err = controllerutil.CreateOrUpdate(context.TODO(), r.client, tls, func() error {
				tls.Data = map[string][]byte{}
				return nil
			})
			return err
		})
		if err != nil {
			r.logger.Error(err, "Cannot clean Capsule TLS Secret due to CA update")
			return reconcile.Result{}, err
		}
	}

	r.logger.Info("Reconciliation completed, processing back in " + rq.String())
	return reconcile.Result{Requeue: true, RequeueAfter: rq}, nil
}
