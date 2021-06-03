// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/controllers/rbac"
)

// TenantReconciler reconciles a Tenant object
type TenantReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

func (r *TenantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1alpha1.Tenant{}).
		Owns(&corev1.Namespace{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&corev1.LimitRange{}).
		Owns(&corev1.ResourceQuota{}).
		Owns(&rbacv1.RoleBinding{}).
		Complete(r)
}

func (r TenantReconciler) Reconcile(ctx context.Context, request ctrl.Request) (result ctrl.Result, err error) {
	r.Log = r.Log.WithValues("Request.Name", request.Name)

	// Fetch the Tenant instance
	instance := &capsulev1alpha1.Tenant{}
	err = r.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Request object not found, could have been deleted after reconcile request")
			return reconcile.Result{}, nil
		}
		r.Log.Error(err, "Error reading the object")
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

	r.Log.Info("Starting processing of Network Policies", "items", len(instance.Spec.NetworkPolicies))
	if err = r.syncNetworkPolicies(instance); err != nil {
		r.Log.Error(err, "Cannot sync NetworkPolicy items")
		return
	}

	r.Log.Info("Starting processing of Limit Ranges", "items", len(instance.Spec.LimitRanges))
	if err = r.syncLimitRanges(instance); err != nil {
		r.Log.Error(err, "Cannot sync LimitRange items")
		return
	}

	r.Log.Info("Starting processing of Resource Quotas", "items", len(instance.Spec.ResourceQuota))
	if err = r.syncResourceQuotas(instance); err != nil {
		r.Log.Error(err, "Cannot sync ResourceQuota items")
		return
	}

	r.Log.Info("Ensuring PSP for owner")
	if err = r.syncAdditionalRoleBindings(instance); err != nil {
		r.Log.Error(err, "Cannot sync additional Role Bindings items")
		return
	}

	r.Log.Info("Ensuring RoleBinding for owner")
	if err = r.ownerRoleBinding(instance); err != nil {
		r.Log.Error(err, "Cannot sync owner RoleBinding")
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

// pruningResources is taking care of removing the no more requested sub-resources as LimitRange, ResourceQuota or
// NetworkPolicy using the "exists" and "notin" LabelSelector to perform an outer-join removal.
func (r *TenantReconciler) pruningResources(ns string, keys []string, obj client.Object) error {
	capsuleLabel, err := capsulev1alpha1.GetTypeLabel(obj)
	if err != nil {
		return err
	}

	s := labels.NewSelector()

	exists, err := labels.NewRequirement(capsuleLabel, selection.Exists, []string{})
	if err != nil {
		return err
	}
	s = s.Add(*exists)

	if len(keys) > 0 {
		var notIn *labels.Requirement
		notIn, err = labels.NewRequirement(capsuleLabel, selection.NotIn, keys)
		if err != nil {
			return err
		}
		s = s.Add(*notIn)
	}

	r.Log.Info("Pruning objects with label selector " + s.String())
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return r.DeleteAllOf(context.TODO(), obj, &client.DeleteAllOfOptions{
			ListOptions: client.ListOptions{
				LabelSelector: s,
				Namespace:     ns,
			},
			DeleteOptions: client.DeleteOptions{},
		})
	})
}

// Serial ResourceQuota processing is expensive: using Go routines we can speed it up.
// In case of multiple errors these are logged properly, returning a generic error since we have to repush back the
// reconciliation loop.
func (r *TenantReconciler) resourceQuotasUpdate(resourceName corev1.ResourceName, actual, limit resource.Quantity, list ...corev1.ResourceQuota) error {
	g := errgroup.Group{}

	for _, item := range list {
		rq := item
		g.Go(func() error {
			found := &corev1.ResourceQuota{}
			if err := r.Get(context.TODO(), types.NamespacedName{Namespace: rq.Namespace, Name: rq.Name}, found); err != nil {
				return err
			}

			return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				_, err := controllerutil.CreateOrUpdate(context.TODO(), r.Client, found, func() error {
					// Ensuring annotation map is there to avoid uninitialized map error and
					// assigning the overall usage
					if found.Annotations == nil {
						found.Annotations = make(map[string]string)
					}
					found.Labels = rq.Labels
					found.Annotations[capsulev1alpha1.UsedQuotaFor(resourceName)] = actual.String()
					found.Annotations[capsulev1alpha1.HardQuotaFor(resourceName)] = limit.String()
					// Updating the Resource according to the actual.Cmp result
					found.Spec.Hard = rq.Spec.Hard
					return nil
				})
				return err
			})
		})
	}

	var err error
	if err = g.Wait(); err != nil {
		// We had an error and we mark the whole transaction as failed
		// to process it another time according to the Tenant controller back-off factor.
		r.Log.Error(err, "Cannot update outer ResourceQuotas", "resourceName", resourceName.String())
		err = fmt.Errorf("update of outer ResourceQuota items has failed: %s", err.Error())
	}

	return err
}

// Additional Role Bindings can be used in many ways: applying Pod Security Policies or giving
// access to CRDs or specific API groups.
func (r *TenantReconciler) syncAdditionalRoleBindings(tenant *capsulev1alpha1.Tenant) (err error) {
	// hashing the RoleBinding name due to DNS RFC-1123 applied to Kubernetes labels
	hash := func(value string) string {
		h := fnv.New64a()
		_, _ = h.Write([]byte(value))
		return fmt.Sprintf("%x", h.Sum64())
	}
	// getting requested Role Binding keys
	var keys []string
	for _, i := range tenant.Spec.AdditionalRoleBindings {
		keys = append(keys, hash(i.ClusterRoleName))
	}

	var tl, ll string
	tl, err = capsulev1alpha1.GetTypeLabel(&capsulev1alpha1.Tenant{})
	if err != nil {
		return
	}
	ll, err = capsulev1alpha1.GetTypeLabel(&rbacv1.RoleBinding{})
	if err != nil {
		return
	}

	for _, ns := range tenant.Status.Namespaces {
		if err = r.pruningResources(ns, keys, &rbacv1.RoleBinding{}); err != nil {
			return err
		}
		for _, i := range tenant.Spec.AdditionalRoleBindings {
			lv := hash(i.ClusterRoleName)
			rb := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("capsule-%s-%s", tenant.Name, i.ClusterRoleName),
					Namespace: ns,
				},
			}
			var res controllerutil.OperationResult
			res, err = controllerutil.CreateOrUpdate(context.TODO(), r.Client, rb, func() error {
				rb.ObjectMeta.Labels = map[string]string{
					tl: tenant.Name,
					ll: lv,
				}
				rb.RoleRef = rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     i.ClusterRoleName,
				}
				rb.Subjects = i.Subjects
				return controllerutil.SetControllerReference(tenant, rb, r.Scheme)
			})
			if err != nil {
				r.Log.Error(err, "Cannot sync Additional RoleBinding")
			}
			r.Log.Info(fmt.Sprintf("Additional RoleBindings sync result: %s", string(res)), "name", rb.Name, "namespace", rb.Namespace)
			if err != nil {
				return
			}
		}
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
func (r *TenantReconciler) syncResourceQuotas(tenant *capsulev1alpha1.Tenant) error {
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
			res, err := controllerutil.CreateOrUpdate(context.TODO(), r.Client, target, func() (err error) {
				// Requirement to list ResourceQuota of the current Tenant
				tr, err := labels.NewRequirement(tenantLabel, selection.Equals, []string{tenant.Name})
				if err != nil {
					r.Log.Error(err, "Cannot build ResourceQuota Tenant requirement")
				}
				// Requirement to list ResourceQuota for the current index
				ir, err := labels.NewRequirement(typeLabel, selection.Equals, []string{strconv.Itoa(i)})
				if err != nil {
					r.Log.Error(err, "Cannot build ResourceQuota index requirement")
				}

				// Listing all the ResourceQuota according to the said requirements.
				// These are required since Capsule is going to sum all the used quota to
				// sum them and get the Tenant one.
				rql := &corev1.ResourceQuotaList{}
				err = r.List(context.TODO(), rql, &client.ListOptions{
					LabelSelector: labels.NewSelector().Add(*tr).Add(*ir),
				})
				if err != nil {
					r.Log.Error(err, "Cannot list ResourceQuota", "tenantFilter", tr.String(), "indexFilter", ir.String())
					return err
				}

				// Iterating over all the options declared for the ResourceQuota,
				// summing all the used quota across different Namespaces to determinate
				// if we're hitting a Hard quota at Tenant level.
				// For this case, we're going to block the Quota setting the Hard as the
				// used one.
				for rn, rq := range q.Hard {
					r.Log.Info("Desired hard " + rn.String() + " quota is " + rq.String())

					// Getting the whole usage across all the Tenant Namespaces
					var qt resource.Quantity
					for _, rq := range rql.Items {
						qt.Add(rq.Status.Used[rn])
					}
					r.Log.Info("Computed " + rn.String() + " quota for the whole Tenant is " + qt.String())

					switch qt.Cmp(q.Hard[rn]) {
					case 0:
						// The Tenant is matching exactly the Quota:
						// falling through next case since we have to block further
						// resource allocations.
						fallthrough
					case 1:
						// The Tenant is OverQuota:
						// updating all the related ResourceQuota with the current
						// used Quota to block further creations.
						for i := range rql.Items {
							if _, ok := rql.Items[i].Status.Used[rn]; ok {
								rql.Items[i].Spec.Hard[rn] = rql.Items[i].Status.Used[rn]
							} else {
								um := make(map[corev1.ResourceName]resource.Quantity)
								um[rn] = resource.Quantity{}
								rql.Items[i].Spec.Hard = um
							}
						}
					default:
						// The Tenant is respecting the Hard quota:
						// restoring the default one for all the elements,
						// also for the reconciled one.
						for i := range rql.Items {
							if rql.Items[i].Spec.Hard == nil {
								rql.Items[i].Spec.Hard = map[corev1.ResourceName]resource.Quantity{}
							}
							rql.Items[i].Spec.Hard[rn] = q.Hard[rn]
						}
						target.Spec = q
					}
					if err := r.resourceQuotasUpdate(rn, qt, q.Hard[rn], rql.Items...); err != nil {
						r.Log.Error(err, "cannot proceed with outer ResourceQuota")
						return err
					}
				}
				return controllerutil.SetControllerReference(tenant, target, r.Scheme)
			})
			r.Log.Info("Resource Quota sync result: "+string(res), "name", target.Name, "namespace", target.Namespace)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// Ensuring all the LimitRange are applied to each Namespace handled by the Tenant.
func (r *TenantReconciler) syncLimitRanges(tenant *capsulev1alpha1.Tenant) error {
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
			res, err := controllerutil.CreateOrUpdate(context.TODO(), r.Client, t, func() (err error) {
				t.ObjectMeta.Labels = map[string]string{
					tl: tenant.Name,
					ll: strconv.Itoa(i),
				}
				t.Spec = spec
				return controllerutil.SetControllerReference(tenant, t, r.Scheme)
			})
			r.Log.Info("LimitRange sync result: "+string(res), "name", t.Name, "namespace", t.Namespace)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *TenantReconciler) syncNamespaceMetadata(namespace string, tnt *capsulev1alpha1.Tenant) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		ns := &corev1.Namespace{}
		if err = r.Client.Get(context.TODO(), types.NamespacedName{Name: namespace}, ns); err != nil {
			return
		}

		a := tnt.Spec.NamespacesMetadata.AdditionalAnnotations
		if a == nil {
			a = make(map[string]string)
		}
		if tnt.Spec.NodeSelector != nil {
			var selector []string
			for k, v := range tnt.Spec.NodeSelector {
				selector = append(selector, fmt.Sprintf("%s=%s", k, v))
			}
			a["scheduler.alpha.kubernetes.io/node-selector"] = strings.Join(selector, ",")
		}
		if tnt.Spec.IngressClasses != nil {
			if len(tnt.Spec.IngressClasses.Exact) > 0 {
				a[capsulev1alpha1.AvailableIngressClassesAnnotation] = strings.Join(tnt.Spec.IngressClasses.Exact, ",")
			}
			if len(tnt.Spec.IngressClasses.Regex) > 0 {
				a[capsulev1alpha1.AvailableIngressClassesRegexpAnnotation] = tnt.Spec.IngressClasses.Regex
			}
		}
		if tnt.Spec.StorageClasses != nil {
			if len(tnt.Spec.StorageClasses.Exact) > 0 {
				a[capsulev1alpha1.AvailableStorageClassesAnnotation] = strings.Join(tnt.Spec.StorageClasses.Exact, ",")
			}
			if len(tnt.Spec.StorageClasses.Regex) > 0 {
				a[capsulev1alpha1.AvailableStorageClassesRegexpAnnotation] = tnt.Spec.StorageClasses.Regex
			}
		}
		if tnt.Spec.ContainerRegistries != nil {
			if len(tnt.Spec.ContainerRegistries.Exact) > 0 {
				a[capsulev1alpha1.AllowedRegistriesAnnotation] = strings.Join(tnt.Spec.ContainerRegistries.Exact, ",")
			}
			if len(tnt.Spec.ContainerRegistries.Regex) > 0 {
				a[capsulev1alpha1.AllowedRegistriesRegexpAnnotation] = tnt.Spec.ContainerRegistries.Regex
			}
		}
		ns.SetAnnotations(a)

		l := tnt.Spec.NamespacesMetadata.AdditionalLabels
		if l == nil {
			l = make(map[string]string)
		}
		l["name"] = namespace
		capsuleLabel, _ := capsulev1alpha1.GetTypeLabel(&capsulev1alpha1.Tenant{})
		l[capsuleLabel] = tnt.GetName()
		ns.SetLabels(l)

		return r.Client.Update(context.TODO(), ns, &client.UpdateOptions{})
	})
}

// Ensuring all annotations are applied to each Namespace handled by the Tenant.
func (r *TenantReconciler) syncNamespaces(tenant *capsulev1alpha1.Tenant) (err error) {
	group := errgroup.Group{}

	for _, item := range tenant.Status.Namespaces {
		namespace := item
		group.Go(func() error {
			return r.syncNamespaceMetadata(namespace, tenant)
		})
	}

	if err = group.Wait(); err != nil {
		r.Log.Error(err, "Cannot sync Namespaces")
		err = fmt.Errorf("cannot sync Namespaces: %s", err.Error())
	}
	return
}

// Ensuring all the NetworkPolicies are applied to each Namespace handled by the Tenant.
func (r *TenantReconciler) syncNetworkPolicies(tenant *capsulev1alpha1.Tenant) error {
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
			res, err := controllerutil.CreateOrUpdate(context.TODO(), r.Client, t, func() (err error) {
				t.Spec = spec
				return controllerutil.SetControllerReference(tenant, t, r.Scheme)
			})
			r.Log.Info("Network Policy sync result: "+string(res), "name", t.Name, "namespace", t.Namespace)
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
func (r *TenantReconciler) ownerRoleBinding(tenant *capsulev1alpha1.Tenant) error {
	// getting RoleBinding label for the mutateFn
	tl, err := capsulev1alpha1.GetTypeLabel(&capsulev1alpha1.Tenant{})
	if err != nil {
		return err
	}

	l := map[string]string{tl: tenant.Name}
	s := []rbacv1.Subject{
		{
			Kind: tenant.Spec.Owner.Kind.String(),
			Name: tenant.Spec.Owner.Name,
		},
	}

	rbl := make(map[types.NamespacedName]rbacv1.RoleRef)
	for _, i := range tenant.Status.Namespaces {
		rbl[types.NamespacedName{Namespace: i, Name: "namespace:admin"}] = rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "admin",
		}
		rbl[types.NamespacedName{Namespace: i, Name: "namespace-deleter"}] = rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     rbac.DeleterRoleName,
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
		res, err = controllerutil.CreateOrUpdate(context.TODO(), r.Client, target, func() (err error) {
			target.ObjectMeta.Labels = l
			target.Subjects = s
			target.RoleRef = rr
			return controllerutil.SetControllerReference(tenant, target, r.Scheme)
		})
		r.Log.Info("Role Binding sync result: "+string(res), "name", target.Name, "namespace", target.Namespace)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *TenantReconciler) ensureNamespaceCount(tenant *capsulev1alpha1.Tenant) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		tenant.Status.Size = uint(len(tenant.Status.Namespaces))
		found := &capsulev1alpha1.Tenant{}
		if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: tenant.GetName()}, found); err != nil {
			return err
		}
		found.Status.Size = tenant.Status.Size
		return r.Client.Status().Update(context.TODO(), found, &client.UpdateOptions{})
	})
}

func (r *TenantReconciler) collectNamespaces(tenant *capsulev1alpha1.Tenant) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		nl := &corev1.NamespaceList{}
		err = r.Client.List(context.TODO(), nl, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".metadata.ownerReferences[*].capsule", tenant.GetName()),
		})
		if err != nil {
			return
		}
		_, err = controllerutil.CreateOrUpdate(context.TODO(), r.Client, tenant.DeepCopy(), func() error {
			tenant.AssignNamespaces(nl.Items)
			return r.Client.Status().Update(context.TODO(), tenant, &client.UpdateOptions{})
		})
		return
	})
}
