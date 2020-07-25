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
	"fmt"
	"k8s.io/apimachinery/pkg/types"

	"github.com/clastix/capsule/pkg/cert"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type secretReconciliationFunc func(reconciler *ReconcileSecret, request reconcile.Request) (reconcile.Result, error)

// ReconcileSecret reconciles a Secret object
type ReconcileSecret struct {
	client        client.Client
	scheme        *runtime.Scheme
	logger        logr.Logger
	reconcileFunc secretReconciliationFunc
}

func newReconciler(mgr manager.Manager, name string, f secretReconciliationFunc) reconcile.Reconciler {
	return &ReconcileSecret{
		client:        mgr.GetClient(),
		scheme:        mgr.GetScheme(),
		logger:        log.Log.WithName(name),
		reconcileFunc: f,
	}
}

func (r *ReconcileSecret) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	return r.reconcileFunc(r, request)
}

func (r *ReconcileSecret) GetCertificateAuthority() (ca cert.Ca, err error) {
	instance := &corev1.Secret{}

	err = r.client.Get(context.TODO(), types.NamespacedName{
		Namespace: "capsule-system",
		Name:      CaSecretName,
	}, instance)
	if err != nil {
		return nil, fmt.Errorf("missing secret %s, cannot reconcile", CaSecretName)
	}

	if instance.Data == nil {
		return nil, MissingCaError{}
	}

	ca, err = cert.NewCertificateAuthorityFromBytes(instance.Data[Cert], instance.Data[PrivateKey])
	if err != nil {
		return
	}

	return
}

func filterByName(objName, desired string) bool {
	return objName == desired
}
