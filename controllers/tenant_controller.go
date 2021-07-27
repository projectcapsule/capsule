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
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	"github.com/clastix/capsule/controllers/rbac"
)

// TenantReconciler reconciles a Tenant object
type TenantReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func (r *TenantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta1.Tenant{}).
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

	if instance.Spec.NetworkPolicies != nil {
		r.Log.Info("Starting processing of Network Policies", "items", len(instance.Spec.NetworkPolicies.Items))
		if err = r.syncNetworkPolicies(instance); err != nil {
			r.Log.Error(err, "Cannot sync NetworkPolicy items")
			return
		}
	}

	if instance.Spec.LimitRanges != nil {
		r.Log.Info("Starting processing of Limit Ranges", "items", len(instance.Spec.LimitRanges.Items))
		if err = r.syncLimitRanges(instance); err != nil {
			r.Log.Error(err, "Cannot sync LimitRange items")
			return
		}
	}

	if instance.Spec.ResourceQuota != nil {
		r.Log.Info("Starting processing of Resource Quotas", "items", len(instance.Spec.ResourceQuota.Items))
		if err = r.syncResourceQuotas(instance); err != nil {
			r.Log.Error(err, "Cannot sync ResourceQuota items")
			return
		}
	}

	r.Log.Info("Ensuring additional RoleBindings for owner")
	if err = r.syncAdditionalRoleBindings(instance); err != nil {
		r.Log.Error(err, "Cannot sync additional RoleBindings items")
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
func (r *TenantReconciler) pruningResources(ns string, keys []string, obj client.Object) (err error) {
	var capsuleLabel string
	if capsuleLabel, err = capsulev1beta1.GetTypeLabel(obj); err != nil {
		return
	}

	selector := labels.NewSelector()

	var exists *labels.Requirement
	if exists, err = labels.NewRequirement(capsuleLabel, selection.Exists, []string{}); err != nil {
		return
	}
	selector = selector.Add(*exists)

	if len(keys) > 0 {
		var notIn *labels.Requirement
		if notIn, err = labels.NewRequirement(capsuleLabel, selection.NotIn, keys); err != nil {
			return err
		}

		selector = selector.Add(*notIn)
	}

	r.Log.Info("Pruning objects with label selector " + selector.String())

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return r.DeleteAllOf(context.TODO(), obj, &client.DeleteAllOfOptions{
			ListOptions: client.ListOptions{
				LabelSelector: selector,
				Namespace:     ns,
			},
			DeleteOptions: client.DeleteOptions{},
		})
	})
}

// Serial ResourceQuota processing is expensive: using Go routines we can speed it up.
// In case of multiple errors these are logged properly, returning a generic error since we have to repush back the
// reconciliation loop.
func (r *TenantReconciler) resourceQuotasUpdate(resourceName corev1.ResourceName, actual, limit resource.Quantity, list ...corev1.ResourceQuota) (err error) {
	group := new(errgroup.Group)

	for _, item := range list {
		rq := item

		group.Go(func() (err error) {
			found := &corev1.ResourceQuota{}
			if err = r.Get(context.TODO(), types.NamespacedName{Namespace: rq.Namespace, Name: rq.Name}, found); err != nil {
				return
			}

			return retry.RetryOnConflict(retry.DefaultBackoff, func() (retryErr error) {
				_, retryErr = controllerutil.CreateOrUpdate(context.TODO(), r.Client, found, func() error {
					// Ensuring annotation map is there to avoid uninitialized map error and
					// assigning the overall usage
					if found.Annotations == nil {
						found.Annotations = make(map[string]string)
					}
					found.Labels = rq.Labels
					found.Annotations[capsulev1beta1.UsedQuotaFor(resourceName)] = actual.String()
					found.Annotations[capsulev1beta1.HardQuotaFor(resourceName)] = limit.String()
					// Updating the Resource according to the actual.Cmp result
					found.Spec.Hard = rq.Spec.Hard
					return nil
				})

				return retryErr
			})
		})
	}

	if err = group.Wait(); err != nil {
		// We had an error and we mark the whole transaction as failed
		// to process it another time according to the Tenant controller back-off factor.
		r.Log.Error(err, "Cannot update outer ResourceQuotas", "resourceName", resourceName.String())
		err = fmt.Errorf("update of outer ResourceQuota items has failed: %s", err.Error())
	}

	return err
}

func (r *TenantReconciler) syncAdditionalRoleBinding(tenant *capsulev1beta1.Tenant, ns string, keys []string, hashFn func(binding capsulev1beta1.AdditionalRoleBindingsSpec) string) (err error) {
	var tenantLabel, roleBindingLabel string

	if tenantLabel, err = capsulev1beta1.GetTypeLabel(&capsulev1beta1.Tenant{}); err != nil {
		return
	}

	if roleBindingLabel, err = capsulev1beta1.GetTypeLabel(&rbacv1.RoleBinding{}); err != nil {
		return
	}

	if err = r.pruningResources(ns, keys, &rbacv1.RoleBinding{}); err != nil {
		return
	}

	for i, roleBinding := range tenant.Spec.AdditionalRoleBindings {
		roleBindingHashLabel := hashFn(roleBinding)

		target := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("capsule-%s-%d-%s", tenant.Name, i, roleBinding.ClusterRoleName),
				Namespace: ns,
			},
		}

		var res controllerutil.OperationResult
		res, err = controllerutil.CreateOrUpdate(context.TODO(), r.Client, target, func() error {
			target.ObjectMeta.Labels = map[string]string{
				tenantLabel:      tenant.Name,
				roleBindingLabel: roleBindingHashLabel,
			}
			target.RoleRef = rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     roleBinding.ClusterRoleName,
			}
			target.Subjects = roleBinding.Subjects

			return controllerutil.SetControllerReference(tenant, target, r.Scheme)
		})

		r.emitEvent(tenant, target.GetNamespace(), res, fmt.Sprintf("Ensuring additional RoleBinding %s", target.GetName()), err)

		if err != nil {
			r.Log.Error(err, "Cannot sync Additional RoleBinding")
		}
		r.Log.Info(fmt.Sprintf("Additional RoleBindings sync result: %s", string(res)), "name", target.Name, "namespace", target.Namespace)
		if err != nil {
			return
		}
	}

	return nil
}

// Additional Role Bindings can be used in many ways: applying Pod Security Policies or giving
// access to CRDs or specific API groups.
func (r *TenantReconciler) syncAdditionalRoleBindings(tenant *capsulev1beta1.Tenant) (err error) {
	// hashing the RoleBinding name due to DNS RFC-1123 applied to Kubernetes labels
	hashFn := func(binding capsulev1beta1.AdditionalRoleBindingsSpec) string {
		h := fnv.New64a()

		_, _ = h.Write([]byte(binding.ClusterRoleName))

		for _, sub := range binding.Subjects {
			_, _ = h.Write([]byte(sub.Kind + sub.Name))
		}

		return fmt.Sprintf("%x", h.Sum64())
	}
	// getting requested Role Binding keys
	var keys []string
	for _, i := range tenant.Spec.AdditionalRoleBindings {
		keys = append(keys, hashFn(i))
	}

	group := new(errgroup.Group)

	for _, ns := range tenant.Status.Namespaces {
		namespace := ns

		group.Go(func() error {
			return r.syncAdditionalRoleBinding(tenant, namespace, keys, hashFn)
		})
	}

	return group.Wait()
}

func (r *TenantReconciler) syncResourceQuota(tenant *capsulev1beta1.Tenant, namespace string, keys []string) (err error) {
	// getting ResourceQuota labels for the mutateFn
	var tenantLabel, typeLabel string

	if tenantLabel, err = capsulev1beta1.GetTypeLabel(&capsulev1beta1.Tenant{}); err != nil {
		return err
	}

	if typeLabel, err = capsulev1beta1.GetTypeLabel(&corev1.ResourceQuota{}); err != nil {
		return err
	}
	// Pruning resource of non-requested resources
	if err = r.pruningResources(namespace, keys, &corev1.ResourceQuota{}); err != nil {
		return err
	}

	for index, resQuota := range tenant.Spec.ResourceQuota.Items {
		target := &corev1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("capsule-%s-%d", tenant.Name, index),
				Namespace: namespace,
			},
		}

		var res controllerutil.OperationResult
		err = retry.RetryOnConflict(retry.DefaultBackoff, func() (retryErr error) {
			res, retryErr = controllerutil.CreateOrUpdate(context.TODO(), r.Client, target, func() (err error) {
				target.SetLabels(map[string]string{
					tenantLabel: tenant.Name,
					typeLabel:   strconv.Itoa(index),
				})
				target.Spec.Scopes = resQuota.Scopes
				target.Spec.ScopeSelector = resQuota.ScopeSelector
				// In case of Namespace scope for the ResourceQuota we can easily apply the bare specification
				if tenant.Spec.ResourceQuota.Scope == capsulev1beta1.ResourceQuotaScopeNamespace {
					target.Spec.Hard = resQuota.Hard
				}

				return controllerutil.SetControllerReference(tenant, target, r.Scheme)
			})

			return retryErr
		})

		r.emitEvent(tenant, target.GetNamespace(), res, fmt.Sprintf("Ensuring ResourceQuota %s", target.GetName()), err)

		r.Log.Info("Resource Quota sync result: "+string(res), "name", target.Name, "namespace", target.Namespace)

		if err != nil {
			return
		}
	}

	return nil
}

// When the Resource Budget assigned to a Tenant is Tenant-scoped we have to rely on the ResourceQuota resources to
// represent the resource quota for the single Tenant rather than the single Namespace,
// so abusing of this API although its Namespaced scope.
//
// Since a Namespace could take-up all the available resource quota, the Namespace ResourceQuota will be a 1:1 mapping
// to the Tenant one: in first time Capsule is going to sum all the analogous ResourceQuota resources on other Tenant
// namespaces to check if the Tenant quota has been exceeded or not, reusing the native Kubernetes policy putting the
// .Status.Used value as the .Hard value.
// This will trigger following reconciliations but that's ok: the mutateFn will re-use the same business logic, letting
// the mutateFn along with the CreateOrUpdate to don't perform the update since resources are identical.
//
// In case of Namespace-scoped Resource Budget, we're just replicating the resources across all registered Namespaces.
func (r *TenantReconciler) syncResourceQuotas(tenant *capsulev1beta1.Tenant) (err error) {
	// getting ResourceQuota labels for the mutateFn
	var tenantLabel, typeLabel string

	if tenantLabel, err = capsulev1beta1.GetTypeLabel(&capsulev1beta1.Tenant{}); err != nil {
		return err
	}

	if typeLabel, err = capsulev1beta1.GetTypeLabel(&corev1.ResourceQuota{}); err != nil {
		return err
	}

	if tenant.Spec.ResourceQuota.Scope == capsulev1beta1.ResourceQuotaScopeTenant {
		group := new(errgroup.Group)

		for i, q := range tenant.Spec.ResourceQuota.Items {
			index := i

			resourceQuota := q

			group.Go(func() (scopeErr error) {
				// Calculating the Resource Budget at Tenant scope just if this is put in place.
				// Requirement to list ResourceQuota of the current Tenant
				var tntRequirement *labels.Requirement
				if tntRequirement, scopeErr = labels.NewRequirement(tenantLabel, selection.Equals, []string{tenant.Name}); scopeErr != nil {
					r.Log.Error(scopeErr, "Cannot build ResourceQuota Tenant requirement")
				}
				// Requirement to list ResourceQuota for the current index
				var indexRequirement *labels.Requirement
				if indexRequirement, scopeErr = labels.NewRequirement(typeLabel, selection.Equals, []string{strconv.Itoa(index)}); scopeErr != nil {
					r.Log.Error(scopeErr, "Cannot build ResourceQuota index requirement")
				}
				// Listing all the ResourceQuota according to the said requirements.
				// These are required since Capsule is going to sum all the used quota to
				// sum them and get the Tenant one.
				list := &corev1.ResourceQuotaList{}
				if scopeErr = r.List(context.TODO(), list, &client.ListOptions{LabelSelector: labels.NewSelector().Add(*tntRequirement).Add(*indexRequirement)}); scopeErr != nil {
					r.Log.Error(scopeErr, "Cannot list ResourceQuota", "tenantFilter", tntRequirement.String(), "indexFilter", indexRequirement.String())
					return
				}
				// Iterating over all the options declared for the ResourceQuota,
				// summing all the used quota across different Namespaces to determinate
				// if we're hitting a Hard quota at Tenant level.
				// For this case, we're going to block the Quota setting the Hard as the
				// used one.
				for name, hardQuota := range resourceQuota.Hard {
					r.Log.Info("Desired hard " + name.String() + " quota is " + hardQuota.String())

					// Getting the whole usage across all the Tenant Namespaces
					var quantity resource.Quantity
					for _, item := range list.Items {
						quantity.Add(item.Status.Used[name])
					}
					r.Log.Info("Computed " + name.String() + " quota for the whole Tenant is " + quantity.String())

					switch quantity.Cmp(resourceQuota.Hard[name]) {
					case 0:
						// The Tenant is matching exactly the Quota:
						// falling through next case since we have to block further
						// resource allocations.
						fallthrough
					case 1:
						// The Tenant is OverQuota:
						// updating all the related ResourceQuota with the current
						// used Quota to block further creations.
						for item := range list.Items {
							if _, ok := list.Items[item].Status.Used[name]; ok {
								list.Items[item].Spec.Hard[name] = list.Items[item].Status.Used[name]
							} else {
								um := make(map[corev1.ResourceName]resource.Quantity)
								um[name] = resource.Quantity{}
								list.Items[item].Spec.Hard = um
							}
						}
					default:
						// The Tenant is respecting the Hard quota:
						// restoring the default one for all the elements,
						// also for the reconciled one.
						for item := range list.Items {
							if list.Items[item].Spec.Hard == nil {
								list.Items[item].Spec.Hard = map[corev1.ResourceName]resource.Quantity{}
							}
							list.Items[item].Spec.Hard[name] = resourceQuota.Hard[name]
						}
					}
					if scopeErr = r.resourceQuotasUpdate(name, quantity, resourceQuota.Hard[name], list.Items...); scopeErr != nil {
						r.Log.Error(scopeErr, "cannot proceed with outer ResourceQuota")
						return
					}
				}
				return
			})
		}
		// Waiting the update of all ResourceQuotas
		if err = group.Wait(); err != nil {
			return
		}
	}
	// getting requested ResourceQuota keys
	keys := make([]string, 0, len(tenant.Spec.ResourceQuota.Items))

	for i := range tenant.Spec.ResourceQuota.Items {
		keys = append(keys, strconv.Itoa(i))
	}

	group := new(errgroup.Group)

	for _, ns := range tenant.Status.Namespaces {
		namespace := ns

		group.Go(func() error {
			return r.syncResourceQuota(tenant, namespace, keys)
		})
	}

	return group.Wait()
}

func (r *TenantReconciler) syncLimitRange(tenant *capsulev1beta1.Tenant, namespace string, keys []string) (err error) {
	// getting LimitRange labels for the mutateFn
	var tenantLabel, limitRangeLabel string

	if tenantLabel, err = capsulev1beta1.GetTypeLabel(&capsulev1beta1.Tenant{}); err != nil {
		return
	}
	if limitRangeLabel, err = capsulev1beta1.GetTypeLabel(&corev1.LimitRange{}); err != nil {
		return
	}

	if err = r.pruningResources(namespace, keys, &corev1.LimitRange{}); err != nil {
		return
	}

	for i, spec := range tenant.Spec.LimitRanges.Items {
		target := &corev1.LimitRange{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("capsule-%s-%d", tenant.Name, i),
				Namespace: namespace,
			},
		}

		var res controllerutil.OperationResult
		res, err = controllerutil.CreateOrUpdate(context.TODO(), r.Client, target, func() (err error) {
			target.ObjectMeta.Labels = map[string]string{
				tenantLabel:     tenant.Name,
				limitRangeLabel: strconv.Itoa(i),
			}
			target.Spec = spec
			return controllerutil.SetControllerReference(tenant, target, r.Scheme)
		})

		r.emitEvent(tenant, target.GetNamespace(), res, fmt.Sprintf("Ensuring LimitRange %s", target.GetName()), err)

		r.Log.Info("LimitRange sync result: "+string(res), "name", target.Name, "namespace", target.Namespace)
		if err != nil {
			return
		}
	}

	return
}

// Ensuring all the LimitRange are applied to each Namespace handled by the Tenant.
func (r *TenantReconciler) syncLimitRanges(tenant *capsulev1beta1.Tenant) error {
	// getting requested LimitRange keys
	keys := make([]string, 0, len(tenant.Spec.LimitRanges.Items))

	for i := range tenant.Spec.LimitRanges.Items {
		keys = append(keys, strconv.Itoa(i))
	}

	group := new(errgroup.Group)

	for _, ns := range tenant.Status.Namespaces {
		namespace := ns

		group.Go(func() error {
			return r.syncLimitRange(tenant, namespace, keys)
		})
	}

	return group.Wait()
}

func (r *TenantReconciler) syncNamespaceMetadata(namespace string, tnt *capsulev1beta1.Tenant) (err error) {
	var res controllerutil.OperationResult

	err = retry.RetryOnConflict(retry.DefaultBackoff, func() (conflictErr error) {
		ns := &corev1.Namespace{}
		if conflictErr = r.Client.Get(context.TODO(), types.NamespacedName{Name: namespace}, ns); err != nil {
			return
		}

		capsuleLabel, _ := capsulev1beta1.GetTypeLabel(&capsulev1beta1.Tenant{})

		res, conflictErr = controllerutil.CreateOrUpdate(context.TODO(), r.Client, ns, func() error {
			annotations := make(map[string]string)

			if tnt.Spec.NamespacesMetadata != nil {
				for k, v := range tnt.Spec.NamespacesMetadata.AdditionalAnnotations {
					annotations[k] = v
				}
			}

			if tnt.Spec.NodeSelector != nil {
				var selector []string
				for k, v := range tnt.Spec.NodeSelector {
					selector = append(selector, fmt.Sprintf("%s=%s", k, v))
				}
				annotations["scheduler.alpha.kubernetes.io/node-selector"] = strings.Join(selector, ",")
			}

			if tnt.Spec.IngressClasses != nil {
				if len(tnt.Spec.IngressClasses.Exact) > 0 {
					annotations[capsulev1beta1.AvailableIngressClassesAnnotation] = strings.Join(tnt.Spec.IngressClasses.Exact, ",")
				}
				if len(tnt.Spec.IngressClasses.Regex) > 0 {
					annotations[capsulev1beta1.AvailableIngressClassesRegexpAnnotation] = tnt.Spec.IngressClasses.Regex
				}
			}

			if tnt.Spec.StorageClasses != nil {
				if len(tnt.Spec.StorageClasses.Exact) > 0 {
					annotations[capsulev1beta1.AvailableStorageClassesAnnotation] = strings.Join(tnt.Spec.StorageClasses.Exact, ",")
				}
				if len(tnt.Spec.StorageClasses.Regex) > 0 {
					annotations[capsulev1beta1.AvailableStorageClassesRegexpAnnotation] = tnt.Spec.StorageClasses.Regex
				}
			}

			if tnt.Spec.ContainerRegistries != nil {
				if len(tnt.Spec.ContainerRegistries.Exact) > 0 {
					annotations[capsulev1beta1.AllowedRegistriesAnnotation] = strings.Join(tnt.Spec.ContainerRegistries.Exact, ",")
				}
				if len(tnt.Spec.ContainerRegistries.Regex) > 0 {
					annotations[capsulev1beta1.AllowedRegistriesRegexpAnnotation] = tnt.Spec.ContainerRegistries.Regex
				}
			}

			ns.SetAnnotations(annotations)

			newLabels := map[string]string{
				"name":       namespace,
				capsuleLabel: tnt.GetName(),
			}

			if tnt.Spec.NamespacesMetadata != nil {
				for k, v := range tnt.Spec.NamespacesMetadata.AdditionalLabels {
					newLabels[k] = v
				}
			}

			ns.SetLabels(newLabels)

			return nil
		})

		return
	})

	r.emitEvent(tnt, namespace, res, "Ensuring Namespace metadata", err)

	return
}

// Ensuring all annotations are applied to each Namespace handled by the Tenant.
func (r *TenantReconciler) syncNamespaces(tenant *capsulev1beta1.Tenant) (err error) {
	group := new(errgroup.Group)

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

func (r *TenantReconciler) syncNetworkPolicy(tenant *capsulev1beta1.Tenant, namespace string, keys []string) (err error) {
	if err = r.pruningResources(namespace, keys, &networkingv1.NetworkPolicy{}); err != nil {
		return
	}
	// getting NetworkPolicy labels for the mutateFn
	var tenantLabel, networkPolicyLabel string

	if tenantLabel, err = capsulev1beta1.GetTypeLabel(&capsulev1beta1.Tenant{}); err != nil {
		return
	}

	if networkPolicyLabel, err = capsulev1beta1.GetTypeLabel(&networkingv1.NetworkPolicy{}); err != nil {
		return
	}

	for i, spec := range tenant.Spec.NetworkPolicies.Items {
		target := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("capsule-%s-%d", tenant.Name, i),
				Namespace: namespace,
			},
		}

		var res controllerutil.OperationResult
		res, err = controllerutil.CreateOrUpdate(context.TODO(), r.Client, target, func() (err error) {
			target.SetLabels(map[string]string{
				tenantLabel:        tenant.Name,
				networkPolicyLabel: strconv.Itoa(i),
			})
			target.Spec = spec

			return controllerutil.SetControllerReference(tenant, target, r.Scheme)
		})

		r.emitEvent(tenant, target.GetNamespace(), res, fmt.Sprintf("Ensuring NetworkPolicy %s", target.GetName()), err)

		r.Log.Info("Network Policy sync result: "+string(res), "name", target.Name, "namespace", target.Namespace)

		if err != nil {
			return
		}
	}

	return
}

// Ensuring all the NetworkPolicies are applied to each Namespace handled by the Tenant.
func (r *TenantReconciler) syncNetworkPolicies(tenant *capsulev1beta1.Tenant) error {
	// getting requested NetworkPolicy keys
	keys := make([]string, 0, len(tenant.Spec.NetworkPolicies.Items))

	for i := range tenant.Spec.NetworkPolicies.Items {
		keys = append(keys, strconv.Itoa(i))
	}

	group := new(errgroup.Group)

	for _, ns := range tenant.Status.Namespaces {
		namespace := ns

		group.Go(func() error {
			return r.syncNetworkPolicy(tenant, namespace, keys)
		})
	}

	return group.Wait()
}

// Each Tenant owner needs the admin Role attached to each Namespace, otherwise no actions on it can be performed.
// Since RBAC is based on deny all first, some specific actions like editing Capsule resources are going to be blocked
// via Dynamic Admission Webhooks.
// TODO(prometherion): we could create a capsule:admin role rather than hitting webhooks for each action
func (r *TenantReconciler) ownerRoleBinding(tenant *capsulev1beta1.Tenant) error {
	// getting RoleBinding label for the mutateFn
	var subjects []rbacv1.Subject

	tl, err := capsulev1beta1.GetTypeLabel(&capsulev1beta1.Tenant{})
	if err != nil {
		return err
	}

	newLabels := map[string]string{tl: tenant.Name}

	for _, owner := range tenant.Spec.Owners {
		if owner.Kind == "ServiceAccount" {
			splitName := strings.Split(owner.Name, ":")
			subjects = append(subjects, rbacv1.Subject{
				Kind:      owner.Kind.String(),
				Name:      splitName[len(splitName)-1],
				Namespace: splitName[len(splitName)-2],
			})
		} else {
			subjects = append(subjects, rbacv1.Subject{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     owner.Kind.String(),
				Name:     owner.Name,
			})
		}
	}

	list := make(map[types.NamespacedName]rbacv1.RoleRef)

	for _, i := range tenant.Status.Namespaces {
		list[types.NamespacedName{Namespace: i, Name: "namespace:admin"}] = rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "admin",
		}
		list[types.NamespacedName{Namespace: i, Name: "namespace-deleter"}] = rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     rbac.DeleterRoleName,
		}
	}

	for namespacedName, roleRef := range list {
		target := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      namespacedName.Name,
				Namespace: namespacedName.Namespace,
			},
		}

		var res controllerutil.OperationResult
		res, err = controllerutil.CreateOrUpdate(context.TODO(), r.Client, target, func() (err error) {
			target.ObjectMeta.Labels = newLabels
			target.Subjects = subjects
			target.RoleRef = roleRef
			return controllerutil.SetControllerReference(tenant, target, r.Scheme)
		})

		r.emitEvent(tenant, target.GetNamespace(), res, fmt.Sprintf("Ensuring Capsule RoleBinding %s", target.GetName()), err)

		r.Log.Info("Role Binding sync result: "+string(res), "name", target.Name, "namespace", target.Namespace)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *TenantReconciler) ensureNamespaceCount(tenant *capsulev1beta1.Tenant) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		tenant.Status.Size = uint(len(tenant.Status.Namespaces))

		found := &capsulev1beta1.Tenant{}
		if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: tenant.GetName()}, found); err != nil {
			return err
		}

		found.Status.Size = tenant.Status.Size

		return r.Client.Status().Update(context.TODO(), found, &client.UpdateOptions{})
	})
}

func (r *TenantReconciler) emitEvent(object runtime.Object, namespace string, res controllerutil.OperationResult, msg string, err error) {
	var eventType = corev1.EventTypeNormal
	if err != nil {
		eventType = corev1.EventTypeWarning
		res = "Error"
	}

	r.Recorder.AnnotatedEventf(object, map[string]string{"OperationResult": string(res)}, eventType, namespace, msg)
}

func (r *TenantReconciler) collectNamespaces(tenant *capsulev1beta1.Tenant) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		list := &corev1.NamespaceList{}
		err = r.Client.List(context.TODO(), list, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector(".metadata.ownerReferences[*].capsule", tenant.GetName()),
		})

		if err != nil {
			return
		}

		_, err = controllerutil.CreateOrUpdate(context.TODO(), r.Client, tenant.DeepCopy(), func() error {
			tenant.AssignNamespaces(list.Items)

			return r.Client.Status().Update(context.TODO(), tenant, &client.UpdateOptions{})
		})
		return
	})
}

func (r *TenantReconciler) updateTenantStatus(tnt *capsulev1beta1.Tenant) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		if tnt.IsCordoned() {
			tnt.Status.State = capsulev1beta1.TenantStateCordoned
		} else {
			tnt.Status.State = capsulev1beta1.TenantStateActive
		}

		return r.Client.Status().Update(context.Background(), tnt)
	})
}
