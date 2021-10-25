package tenant

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"

	"golang.org/x/sync/errgroup"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	"github.com/clastix/capsule/controllers/rbac"
)

// Additional Role Bindings can be used in many ways: applying Pod Security Policies or giving
// access to CRDs or specific API groups.
func (r *Manager) syncAdditionalRoleBindings(tenant *capsulev1beta1.Tenant) (err error) {
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

func (r *Manager) syncAdditionalRoleBinding(tenant *capsulev1beta1.Tenant, ns string, keys []string, hashFn func(binding capsulev1beta1.AdditionalRoleBindingsSpec) string) (err error) {
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

// Each Tenant owner needs the admin Role attached to each Namespace, otherwise no actions on it can be performed.
// Since RBAC is based on deny all first, some specific actions like editing Capsule resources are going to be blocked
// via Dynamic Admission Webhooks.
// TODO(prometherion): we could create a capsule:admin role rather than hitting webhooks for each action
func (r *Manager) ownerRoleBinding(tenant *capsulev1beta1.Tenant) error {
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
