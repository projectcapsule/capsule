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

package tenant

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	capsulev1alpha1 "github.com/clastix/capsule/pkg/apis/capsule/v1alpha1"
)

// Add creates a new Tenant Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, &ReconcileTenant{
		client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
		logger: log.Log.WithName("controller_tenant"),
	})
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("tenant-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Tenant
	err = c.Watch(&source.Kind{Type: &capsulev1alpha1.Tenant{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for controlled resources
	for _, r := range []runtime.Object{&networkingv1.NetworkPolicy{}, &corev1.LimitRange{}, &corev1.ResourceQuota{}, &rbacv1.RoleBinding{}} {
		err = c.Watch(&source.Kind{Type: r}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &capsulev1alpha1.Tenant{},
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// ReconcileTenant reconciles a Tenant object
type ReconcileTenant struct {
	client client.Client
	scheme *runtime.Scheme
	logger logr.Logger
}

// Reconcile reads that state of the cluster for a Tenant object and makes changes based on the state read
// and what is in the Tenant.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileTenant) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	r.logger = log.Log.WithName("controller_tenant").WithValues("Request.Name", request.Name)

	// Fetch the Tenant instance
	instance := &capsulev1alpha1.Tenant{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			r.logger.Info("Request object not found, could have been deleted after reconcile request")
			return reconcile.Result{}, nil
		}
		r.logger.Error(err, "Error reading the object")
		return reconcile.Result{}, err
	}

	r.logger.Info("Starting processing of Network Policies", "items", len(instance.Spec.NetworkPolicies))
	if err := r.syncNetworkPolicies(instance); err != nil {
		r.logger.Error(err, "Cannot sync NetworkPolicy items")
		return reconcile.Result{}, err
	}

	r.logger.Info("Starting processing of Node Selector")
	if err := r.ensureNodeSelector(instance); err != nil {
		r.logger.Error(err, "Cannot sync Namespaces Node Selector items")
		return reconcile.Result{}, err
	}

	r.logger.Info("Starting processing of Limit Ranges", "items", len(instance.Spec.LimitRanges))
	if err := r.syncLimitRanges(instance); err != nil {
		r.logger.Error(err, "Cannot sync LimitRange items")
		return reconcile.Result{}, err
	}

	r.logger.Info("Starting processing of Resource Quotas", "items", len(instance.Spec.ResourceQuota))
	if err := r.syncResourceQuotas(instance); err != nil {
		r.logger.Error(err, "Cannot sync ResourceQuota items")
		return reconcile.Result{}, err
	}

	r.logger.Info("Ensuring RoleBinding for owner")
	if err := r.ownerRoleBinding(instance); err != nil {
		r.logger.Error(err, "Cannot sync owner RoleBinding")
		return reconcile.Result{}, err
	}

	r.logger.Info("Tenant reconciling completed")
	return reconcile.Result{}, nil
}

// pruningResources is taking care of removing the no more requested sub-resources as LimitRange, ResourceQuota or
// NetworkPolicy using the "notin" LabelSelector to perform an outer-join removal.
func (r *ReconcileTenant) pruningResources(ns string, keys []string, obj runtime.Object) error {
	capsuleLabel, err := capsulev1alpha1.GetTypeLabel(obj)
	if err != nil {
		return err
	}
	req, err := labels.NewRequirement(capsuleLabel, selection.NotIn, keys)
	if err != nil {
		return err
	}
	r.logger.Info("Pruning objects with label selector " + req.String())
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return r.client.DeleteAllOf(context.TODO(), obj, &client.DeleteAllOfOptions{
			ListOptions: client.ListOptions{
				LabelSelector: labels.NewSelector().Add(*req),
				Namespace:     ns,
			},
			DeleteOptions: client.DeleteOptions{},
		})
	})
	if err != nil {
		return err
	}

	return nil
}

// We're relying on the ResourceQuota resource to represent the resource quota for the single Tenant rather than the
// single Namespace, so abusing of this API although its Namespaced scope.
// Since a Namespace could take-up all the available resource quota, the Namespace ResourceQuota will be a 1:1 mapping
// to the Tenant one: in a second time Capsule is going to sum all the analogous ResourceQuota resources on other Tenant
// namespaces to check if the Tenant quota has been exceeded or not, reusing the native Kubernetes policy putting the
// .Status.Used value as the .Hard value.
// This will trigger a following reconciliation but that's ok: the mutateFn will re-use the same business logic, letting
// the mutateFn along with the CreateOrUpdate to don't perform the update since resources are identical.
func (r *ReconcileTenant) syncResourceQuotas(tenant *capsulev1alpha1.Tenant) error {
	// getting requested ResourceQuota keys
	keys := make([]string, 0, len(tenant.Spec.ResourceQuota))
	for i := range tenant.Spec.ResourceQuota {
		keys = append(keys, strconv.Itoa(i))
	}

	// getting ResourceQuota labels for the mutateFn
	tenantLabel, err := capsulev1alpha1.GetTypeLabel(&capsulev1alpha1.Tenant{})
	if err != nil {
		return err
	}
	typeLabel, err := capsulev1alpha1.GetTypeLabel(&corev1.ResourceQuota{})
	if err != nil {
		return err
	}

	for _, ns := range tenant.Status.Namespaces {
		if err := r.pruningResources(ns, keys, &corev1.ResourceQuota{}); err != nil {
			return err
		}
		for i, q := range tenant.Spec.ResourceQuota {
			target := &corev1.ResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name:        fmt.Sprintf("capsule-%s-%d", tenant.Name, i),
					Namespace:   ns,
					Annotations: make(map[string]string),
					Labels: map[string]string{
						tenantLabel: tenant.Name,
						typeLabel:   strconv.Itoa(i),
					},
				},
			}
			res, err := controllerutil.CreateOrUpdate(context.TODO(), r.client, target, func() (err error) {
				// Requirement to list ResourceQuota of the current Tenant
				tr, err := labels.NewRequirement(tenantLabel, selection.Equals, []string{tenant.Name})
				if err != nil {
					r.logger.Error(err, "Cannot build ResourceQuota Tenant requirement")
				}
				// Requirement to list ResourceQuota for the current index
				ir, err := labels.NewRequirement(typeLabel, selection.Equals, []string{strconv.Itoa(i)})
				if err != nil {
					r.logger.Error(err, "Cannot build ResourceQuota index requirement")
				}

				// Listing all the ResourceQuota according to the said requirements.
				// These are required since Capsule is going to sum all the used quota to
				// sum them and get the Tenant one.
				rql := &corev1.ResourceQuotaList{}
				err = r.client.List(context.TODO(), rql, &client.ListOptions{
					LabelSelector: labels.NewSelector().Add(*tr).Add(*ir),
				})
				if err != nil {
					r.logger.Error(err, "Cannot list ResourceQuota", "tenantFilter", tr.String(), "indexFilter", ir.String())
					return err
				}

				// Iterating over all the options declared for the ResourceQuota,
				// summing all the used quota across different Namespaces to determinate
				// if we're hitting a Hard quota at Tenant level.
				// For this case, we're going to block the Quota setting the Hard as the
				// used one.
				for rn, rq := range q.Hard {
					r.logger.Info("Desired hard " + rn.String() + " quota is " + rq.String())

					// Getting the whole usage across all the Tenant Namespaces
					var qt resource.Quantity
					for _, rq := range rql.Items {
						qt.Add(rq.Status.Used[rn])
					}
					r.logger.Info("Computed " + rn.String() + " quota for the whole Tenant is " + qt.String())

					switch qt.Cmp(q.Hard[rn]) {
					case 1:
						// The Tenant is OverQuota:
						// updating all the related ResourceQuota with the current
						// used Quota to block further creations.
						for i := range rql.Items {
							rql.Items[i].Spec.Hard[rn] = rql.Items[i].Status.Used[rn]
						}
						println("")
					default:
						// The Tenant is respecting the Hard quota:
						// restoring the default one for all the elements,
						// also for the reconciliated one.
						for i := range rql.Items {
							rql.Items[i].Spec.Hard[rn] = q.Hard[rn]
						}
						target.Spec = q
					}

					// Updating all outer join ResourceQuota adding the Used for the current Resource
					// TODO(prometherion): this is too expensive, should be performed via a recursion
					for _, oj := range rql.Items {
						err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
							_ = r.client.Get(context.TODO(), types.NamespacedName{Namespace: oj.Namespace, Name: oj.Name}, &oj)
							if oj.Annotations == nil {
								oj.Annotations = make(map[string]string)
							}
							oj.Annotations[capsulev1alpha1.UsedQuotaFor(rn)] = qt.String()
							return r.client.Update(context.TODO(), &oj, &client.UpdateOptions{})
						})
						if err != nil {
							return err
						}
					}
				}
				return controllerutil.SetControllerReference(tenant, target, r.scheme)
			})
			r.logger.Info("Resource Quota sync result: "+string(res), "name", target.Name, "namespace", target.Namespace)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// Ensuring all the LimitRange are applied to each Namespace handled by the Tenant.
func (r *ReconcileTenant) syncLimitRanges(tenant *capsulev1alpha1.Tenant) error {
	// getting requested LimitRange keys
	keys := make([]string, 0, len(tenant.Spec.LimitRanges))
	for i := range tenant.Spec.LimitRanges {
		keys = append(keys, strconv.Itoa(i))
	}

	// getting LimitRange labels for the mutateFn
	tl, err := capsulev1alpha1.GetTypeLabel(&capsulev1alpha1.Tenant{})
	if err != nil {
		return err
	}
	ll, err := capsulev1alpha1.GetTypeLabel(&corev1.LimitRange{})
	if err != nil {
		return err
	}

	for _, ns := range tenant.Status.Namespaces {
		if err := r.pruningResources(ns, keys, &corev1.LimitRange{}); err != nil {
			return err
		}
		for i, spec := range tenant.Spec.LimitRanges {
			t := &corev1.LimitRange{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("capsule-%s-%d", tenant.Name, i),
					Namespace: ns,
				},
			}
			res, err := controllerutil.CreateOrUpdate(context.TODO(), r.client, t, func() (err error) {
				t.ObjectMeta.Labels = map[string]string{
					tl: tenant.Name,
					ll: strconv.Itoa(i),
				}
				t.Spec = spec
				return controllerutil.SetControllerReference(tenant, t, r.scheme)
			})
			r.logger.Info("LimitRange sync result: "+string(res), "name", t.Name, "namespace", t.Namespace)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// Ensuring all the NetworkPolicies are applied to each Namespace handled by the Tenant.
func (r *ReconcileTenant) syncNetworkPolicies(tenant *capsulev1alpha1.Tenant) error {
	// getting requested NetworkPolicy keys
	keys := make([]string, 0, len(tenant.Spec.NetworkPolicies))
	for i := range tenant.Spec.NetworkPolicies {
		keys = append(keys, strconv.Itoa(i))
	}

	// getting NetworkPolicy labels for the mutateFn
	tl, err := capsulev1alpha1.GetTypeLabel(&capsulev1alpha1.Tenant{})
	if err != nil {
		return err
	}
	nl, err := capsulev1alpha1.GetTypeLabel(&networkingv1.NetworkPolicy{})
	if err != nil {
		return err
	}

	for _, ns := range tenant.Status.Namespaces {
		if err := r.pruningResources(ns, keys, &networkingv1.NetworkPolicy{}); err != nil {
			return err
		}
		for i, spec := range tenant.Spec.NetworkPolicies {
			t := &networkingv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("capsule-%s-%d", tenant.Name, i),
					Namespace: ns,
					Labels: map[string]string{
						tl: tenant.Name,
						nl: strconv.Itoa(i),
					},
				},
			}
			res, err := controllerutil.CreateOrUpdate(context.TODO(), r.client, t, func() (err error) {
				t.Spec = spec
				return controllerutil.SetControllerReference(tenant, t, r.scheme)
			})
			r.logger.Info("Network Policy sync result: "+string(res), "name", t.Name, "namespace", t.Namespace)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// Each Tenant owner needs the admin Role attached to each Namespace, otherwise no actions on it can be performed.
// Since RBAC is based on deny all first, some specific actions like editing Capsule resources are going to be blocked
// via Dynamic Admission Webhooks.
// TODO(prometherion): we could create a capsule:admin role rather than hitting webhooks for each action
func (r *ReconcileTenant) ownerRoleBinding(tenant *capsulev1alpha1.Tenant) error {
	// getting RoleBinding label for the mutateFn
	tl, err := capsulev1alpha1.GetTypeLabel(&capsulev1alpha1.Tenant{})
	if err != nil {
		return err
	}

	l := map[string]string{tl: tenant.Name}
	s := []rbacv1.Subject{
		{
			Kind: "User",
			Name: tenant.Spec.Owner,
		},
	}

	rbl := make(map[types.NamespacedName]rbacv1.RoleRef)
	for _, i := range tenant.Status.Namespaces {
		rbl[types.NamespacedName{Namespace: i, Name: "namespace:admin"}] = rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "admin",
		}
		rbl[types.NamespacedName{Namespace: i, Name: "namespace:deleter"}] = rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "namespace:deleter",
		}
	}

	for nn, rr := range rbl {
		target := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
			},
		}

		var res controllerutil.OperationResult
		res, err = controllerutil.CreateOrUpdate(context.TODO(), r.client, target, func() (err error) {
			target.ObjectMeta.Labels = l
			target.Subjects = s
			target.RoleRef = rr
			return controllerutil.SetControllerReference(tenant, target, r.scheme)
		})
		r.logger.Info("Role Binding sync result: "+string(res), "name", target.Name, "namespace", target.Namespace)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ReconcileTenant) ensureNodeSelector(tenant *capsulev1alpha1.Tenant) (err error) {
	if tenant.Spec.NodeSelector == nil {
		return
	}

	for _, namespace := range tenant.Status.Namespaces {
		selectorMap := tenant.Spec.NodeSelector
		if selectorMap == nil {
			return
		}

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}

		var res controllerutil.OperationResult
		res, err = controllerutil.CreateOrUpdate(context.TODO(), r.client, ns, func() error {
			if ns.Annotations == nil {
				ns.Annotations = make(map[string]string)
			}
			var selector []string
			for k, v := range selectorMap {
				selector = append(selector, fmt.Sprintf("%s=%s", k, v))
			}
			ns.Annotations["scheduler.alpha.kubernetes.io/node-selector"] = strings.Join(selector, ",")
			return nil
		})
		r.logger.Info("Namespace Node  sync result: "+string(res), "name", ns.Name)
		if err != nil {
			return err
		}
	}

	return
}
