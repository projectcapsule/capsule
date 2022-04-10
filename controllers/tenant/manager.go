package tenant

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

type Manager struct {
	client.Client
	Log        logr.Logger
	Scheme     *runtime.Scheme
	Recorder   record.EventRecorder
	RESTConfig *rest.Config
}

func (r *Manager) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta1.Tenant{}).
		Owns(&corev1.Namespace{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&corev1.LimitRange{}).
		Owns(&corev1.ResourceQuota{}).
		Owns(&rbacv1.RoleBinding{}).
		Complete(r)
}

func (r Manager) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	r.Log = r.Log.WithValues("Request.Name", request.Name)

	// Fetch the Tenant instance
	instance := &capsulev1beta1.Tenant{}
	if err = r.Get(ctx, request.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Request object not found, could have been deleted after reconcile request")
			return reconcile.Result{}, nil
		}
		r.Log.Error(err, "Error reading the object")
		return
	}
	// Ensuring the Tenant Status
	if err = r.updateTenantStatus(instance); err != nil {
		r.Log.Error(err, "Cannot update Tenant status")
		return
	}

	r.Log.Info("Ensuring limit resources count is updated")
	if err = r.syncCustomResourceQuotaUsages(ctx, instance); err != nil {
		r.Log.Error(err, "Cannot count limited resources")
		return
	}

	// Ensuring all namespaces are collected
	r.Log.Info("Ensuring all Namespaces are collected")
	if err = r.collectNamespaces(instance); err != nil {
		r.Log.Error(err, "Cannot collect Namespace resources")
		return
	}

	r.Log.Info("Starting processing of Namespaces", "items", len(instance.Status.Namespaces))
	if err = r.syncNamespaces(instance); err != nil {
		r.Log.Error(err, "Cannot sync Namespace items")
		return
	}

	r.Log.Info("Starting processing of Network Policies")
	if err = r.syncNetworkPolicies(instance); err != nil {
		r.Log.Error(err, "Cannot sync NetworkPolicy items")
		return
	}

	r.Log.Info("Starting processing of Limit Ranges", "items", len(instance.Spec.LimitRanges.Items))
	if err = r.syncLimitRanges(instance); err != nil {
		r.Log.Error(err, "Cannot sync LimitRange items")
		return
	}

	r.Log.Info("Starting processing of Resource Quotas", "items", len(instance.Spec.ResourceQuota.Items))
	if err = r.syncResourceQuotas(instance); err != nil {
		r.Log.Error(err, "Cannot sync ResourceQuota items")
		return
	}

	r.Log.Info("Ensuring RoleBindings for Owners and Tenant")
	if err = r.syncRoleBindings(instance); err != nil {
		r.Log.Error(err, "Cannot sync RoleBindings items")
		return
	}

	r.Log.Info("Ensuring Namespace count")
	if err = r.ensureNamespaceCount(instance); err != nil {
		r.Log.Error(err, "Cannot sync Namespace count")
		return
	}

	r.Log.Info("Tenant reconciling completed")
	return ctrl.Result{}, err
}

func (r *Manager) updateTenantStatus(tnt *capsulev1beta1.Tenant) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		if tnt.IsCordoned() {
			tnt.Status.State = capsulev1beta1.TenantStateCordoned
		} else {
			tnt.Status.State = capsulev1beta1.TenantStateActive
		}

		return r.Client.Status().Update(context.Background(), tnt)
	})
}
