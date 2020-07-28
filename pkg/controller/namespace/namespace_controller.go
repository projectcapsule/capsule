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

package namespace

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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/clastix/capsule/pkg/apis/capsule/v1alpha1"
)

// Add creates a new Namespace Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileNamespace{
		client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
	}
}

func getCapsuleReference(refs []v1.OwnerReference) (ok bool, reference *v1.OwnerReference) {
	for _, r := range refs {
		if r.APIVersion == v1alpha1.SchemeGroupVersion.String() {
			return true, r.DeepCopy()
		}
	}
	return false, nil
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("namespace-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Namespace
	err = c.Watch(&source.Kind{Type: &corev1.Namespace{}}, &handler.EnqueueRequestForObject{}, predicate.Funcs{
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
	})
	if err != nil {
		return err
	}

	return nil
}

// ReconcileNamespace reconciles a Namespace object
type ReconcileNamespace struct {
	client client.Client
	scheme *runtime.Scheme
	logger logr.Logger
}

func (r *ReconcileNamespace) removeNamespace(name string, tenant *v1alpha1.Tenant) {
	c := tenant.Status.Namespaces.DeepCopy()
	sort.Sort(c)
	i := sort.SearchStrings(c, name)
	// namespace already removed, do nothing
	if i > c.Len() || i == c.Len() {
		return
	}
	// namespace is there, removing it
	tenant.Status.Namespaces = []string{}
	tenant.Status.Namespaces = append(tenant.Status.Namespaces, c[:i]...)
	tenant.Status.Namespaces = append(tenant.Status.Namespaces, c[i+1:]...)
}

func (r *ReconcileNamespace) addNamespace(name string, tenant *v1alpha1.Tenant) {
	c := tenant.Status.Namespaces.DeepCopy()
	sort.Sort(c)
	i := sort.SearchStrings(c, name)
	// namespace already there, nothing to do
	if i < c.Len() && c[i] == name {
		return
	}
	// missing namespace, let's append it
	if i == 0 {
		tenant.Status.Namespaces = []string{name}
	} else {
		tenant.Status.Namespaces = v1alpha1.NamespaceList{}
		tenant.Status.Namespaces = append(tenant.Status.Namespaces, c[:i]...)
		tenant.Status.Namespaces = append(tenant.Status.Namespaces, name)
	}
	tenant.Status.Namespaces = append(tenant.Status.Namespaces, c[i:]...)
}

func (r *ReconcileNamespace) updateNamespaceCount(tenant *v1alpha1.Tenant) error {
	tenant.Status.Size = uint(len(tenant.Status.Namespaces))

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return r.client.Status().Update(context.TODO(), tenant, &client.UpdateOptions{})
	})
}

func (r *ReconcileNamespace) Reconcile(request reconcile.Request) (res reconcile.Result, err error) {
	r.logger = log.Log.WithName("controller_namespace").WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	r.logger.Info("Reconciling Namespace")

	// Fetch the Namespace instance
	ns := &corev1.Namespace{}
	if err := r.client.Get(context.TODO(), request.NamespacedName, ns); err != nil {
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
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: or.Name}, t); err != nil {
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if err := r.ensureLabel(ns, t.Name); err != nil {
		r.logger.Error(err, "cannot update Namespace label")
		return reconcile.Result{}, err
	}

	r.updateTenantStatus(ns, t)

	if err := r.updateNamespaceCount(t); err != nil {
		r.logger.Error(err, "cannot update Namespace list", "tenant", t.Name)
	}

	r.logger.Info("Namespace reconciliation processed")
	return reconcile.Result{}, nil
}

func (r *ReconcileNamespace) ensureLabel(ns *corev1.Namespace, tenantName string) error {
	capsuleLabel, err := v1alpha1.GetTypeLabel(&v1alpha1.Tenant{})
	if err != nil {
		return err
	}
	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}
	tl, ok := ns.Labels[capsuleLabel]
	if !ok || tl != tenantName {
		ns.Labels[capsuleLabel] = tenantName
		return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			return r.client.Update(context.TODO(), ns, &client.UpdateOptions{})
		})
	}
	return nil
}

func (r *ReconcileNamespace) updateTenantStatus(ns *corev1.Namespace, tenant *v1alpha1.Tenant) {
	switch ns.Status.Phase {
	case corev1.NamespaceTerminating:
		r.removeNamespace(ns.Name, tenant)
	case corev1.NamespaceActive:
		r.addNamespace(ns.Name, tenant)
	}
}
