package tenant

import (
	"context"
	"fmt"
	"hash/fnv"

	"golang.org/x/sync/errgroup"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

// Sync the dynamic Tenant Owner specific cluster-roles and additional ClusterRole Bindings, which can be used in many ways:
// applying Pod Security Policies or giving access to CRDs or specific API groups.
func (r *Manager) syncClusterRoleBindings(ctx context.Context, tenant *capsulev1beta1.Tenant) (err error) {
	// hashing the ClusterRoleBinding name due to DNS RFC-1123 applied to Kubernetes labels
	hashFn := func(binding capsulev1beta1.AdditionalRoleBindingsSpec) string {
		h := fnv.New64a()

		_, _ = h.Write([]byte(binding.ClusterRoleName))

		for _, sub := range binding.Subjects {
			_, _ = h.Write([]byte(sub.Kind + sub.Name))
		}

		return fmt.Sprintf("%x", h.Sum64())
	}
	// getting requested Role Binding keys
	keys := make([]string, 0, len(tenant.Spec.Owners))
	// Generating for dynamic tenant owners cluster roles
	for _, owner := range tenant.Spec.Owners {
		for _, clusterRoleName := range owner.GetClusterRoles(*tenant) {

			cr := r.ownerClusterRoleBindings(owner, clusterRoleName)

			keys = append(keys, hashFn(cr))
		}
	}

	group := new(errgroup.Group)

	group.Go(func() error {
		return r.syncClusterRoleBinding(ctx, tenant, keys, hashFn)
	})

	return group.Wait()
}

func (r *Manager) syncClusterRoleBinding(ctx context.Context, tenant *capsulev1beta1.Tenant, keys []string, hashFn func(binding capsulev1beta1.AdditionalRoleBindingsSpec) string) (err error) {

	var tenantLabel string

	var clusterRoleBindingLabel string

	if tenantLabel, err = capsulev1beta1.GetTypeLabel(&capsulev1beta1.Tenant{}); err != nil {
		return
	}

	if clusterRoleBindingLabel, err = capsulev1beta1.GetTypeLabel(&rbacv1.ClusterRoleBinding{}); err != nil {
		return
	}

	if err = r.pruningClusterResources(ctx, keys, &rbacv1.ClusterRoleBinding{}); err != nil {
		return
	}

	var clusterRoleBindings []capsulev1beta1.AdditionalRoleBindingsSpec

	for _, owner := range tenant.Spec.Owners {
		for _, clusterRoleName := range owner.GetClusterRoles(*tenant) {
			clusterRoleBindings = append(clusterRoleBindings, r.ownerClusterRoleBindings(owner, clusterRoleName))
		}
	}

	for i, clusterRoleBinding := range clusterRoleBindings {

		clusterRoleBindingHashLabel := hashFn(clusterRoleBinding)

		target := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("capsule-%s-%d-%s", tenant.Name, i, clusterRoleBinding.ClusterRoleName),
			},
		}

		var res controllerutil.OperationResult
		res, err = controllerutil.CreateOrUpdate(ctx, r.Client, target, func() error {
			target.ObjectMeta.Labels = map[string]string{
				tenantLabel:             tenant.Name,
				clusterRoleBindingLabel: clusterRoleBindingHashLabel,
			}
			target.RoleRef = rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     clusterRoleBinding.ClusterRoleName,
			}
			target.Subjects = clusterRoleBinding.Subjects

			return controllerutil.SetControllerReference(tenant, target, r.Client.Scheme())
		})

		// TODO: find appropriate event Namespace.
		r.emitEvent(tenant, target.GetNamespace(), res, fmt.Sprintf("Ensuring ClusterRoleBinding %s", target.GetName()), err)

		if err != nil {
			r.Log.Error(err, "Cannot sync ClusterRoleBinding")
		}

		r.Log.Info(fmt.Sprintf("ClusterRoleBinding sync result: %s", string(res)), "name", target.Name, "namespace", target.Namespace)

		if err != nil {
			return
		}
	}

	return nil
}
