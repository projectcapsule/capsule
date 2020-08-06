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

package controllers

import (
	"context"
	"sort"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/clastix/capsule/api/v1alpha1"
)

type NamespaceReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(event event.CreateEvent) (ok bool) {
				ok, _ = getCapsuleReference(event.Meta.GetOwnerReferences())
				return
			},
			DeleteFunc: func(deleteEvent event.DeleteEvent) (ok bool) {
				ok, _ = getCapsuleReference(deleteEvent.Meta.GetOwnerReferences())
				return
			},
			UpdateFunc: func(updateEvent event.UpdateEvent) (ok bool) {
				ok, _ = getCapsuleReference(updateEvent.MetaNew.GetOwnerReferences())
				return
			},
			GenericFunc: func(genericEvent event.GenericEvent) (ok bool) {
				ok, _ = getCapsuleReference(genericEvent.Meta.GetOwnerReferences())
				return
			},
		})).
		Complete(r)
}

func getCapsuleReference(refs []v1.OwnerReference) (ok bool, reference *v1.OwnerReference) {
	for _, r := range refs {
		if r.APIVersion == v1alpha1.GroupVersion.String() {
			return true, r.DeepCopy()
		}
	}
	return false, nil
}

func (r *NamespaceReconciler) removeNamespace(name string, tenant *v1alpha1.Tenant) {
	c := tenant.Status.Namespaces.DeepCopy()
	sort.Sort(c)
	i := sort.SearchStrings(c, name)
	// namespace already removed, do nothing
	if i > c.Len() || i == c.Len() {
		r.Log.Info("Namespace has been already removed")
		return
	}
	// namespace is there, removing it
	r.Log.Info("Removing Namespace from Tenant status")
	tenant.Status.Namespaces = []string{}
	tenant.Status.Namespaces = append(tenant.Status.Namespaces, c[:i]...)
	tenant.Status.Namespaces = append(tenant.Status.Namespaces, c[i+1:]...)
}

func (r *NamespaceReconciler) addNamespace(name string, tenant *v1alpha1.Tenant) {
	c := tenant.Status.Namespaces.DeepCopy()
	sort.Sort(c)
	i := sort.SearchStrings(c, name)
	// namespace already there, nothing to do
	if i < c.Len() && c[i] == name {
		r.Log.Info("Namespace has been already added")
		return
	}
	// missing namespace, let's append it
	r.Log.Info("Adding Namespace to Tenant status")
	if i == 0 {
		tenant.Status.Namespaces = []string{name}
	} else {
		tenant.Status.Namespaces = v1alpha1.NamespaceList{}
		tenant.Status.Namespaces = append(tenant.Status.Namespaces, c[:i]...)
		tenant.Status.Namespaces = append(tenant.Status.Namespaces, name)
	}
	tenant.Status.Namespaces = append(tenant.Status.Namespaces, c[i:]...)
}

func (r NamespaceReconciler) Reconcile(request ctrl.Request) (ctrl.Result, error) {
	r.Log = r.Log.WithValues("Request.Name", request.Name)
	r.Log.Info("Reconciling Namespace")

	// Fetch the Namespace instance
	ns := &corev1.Namespace{}
	if err := r.Get(context.TODO(), request.NamespacedName, ns); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	_, or := getCapsuleReference(ns.OwnerReferences)
	t := &v1alpha1.Tenant{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: or.Name}, t); err != nil {
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if err := r.ensureLabel(ns, t.Name); err != nil {
		r.Log.Error(err, "cannot update Namespace label")
		return reconcile.Result{}, err
	}

	r.updateTenantStatus(ns, t)

	r.Log.Info("Namespace reconciliation processed")
	return reconcile.Result{}, nil
}

func (r *NamespaceReconciler) ensureLabel(ns *corev1.Namespace, tenantName string) error {
	capsuleLabel, err := v1alpha1.GetTypeLabel(&v1alpha1.Tenant{})
	if err != nil {
		return err
	}
	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}
	tl, ok := ns.Labels[capsuleLabel]
	if !ok || tl != tenantName {
		return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			ns.Labels[capsuleLabel] = tenantName
			return r.Client.Update(context.TODO(), ns, &client.UpdateOptions{})
		})
	}
	return nil
}

func (r *NamespaceReconciler) updateTenantStatus(ns *corev1.Namespace, tenant *v1alpha1.Tenant) {
	switch ns.Status.Phase {
	case corev1.NamespaceTerminating:
		r.removeNamespace(ns.Name, tenant)
	case corev1.NamespaceActive:
		r.addNamespace(ns.Name, tenant)
	}
}
