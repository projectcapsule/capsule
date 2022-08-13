package tenant

import (
	"context"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

const (
	ImpersonatorRoleName = "capsule-tenant-impersonator"
)

// Sync the Tenant Owner specific cluster-roles.
// When the Tenant is configured GitOpsReady additional (Cluster)Roles are created, then bound.
func (r *Manager) syncRoles(ctx context.Context, tenant *capsulev1beta1.Tenant) (err error) {

	// If the Tenant will be reconciled the GitOps-way,
	// Tenant Owners might be machine GitOps reconciler identities.
	if tenant.Spec.GitOpsReady {
		for _, owner := range tenant.Spec.Owners {
			if err = r.ensureOwnerRole(ctx, tenant, &owner, ImpersonatorRoleName); err != nil {
				r.Log.Error(err, "Reconciliation for ClusterRole failed", "ClusterRole", ImpersonatorRoleName)
				return err
			}
		}
	}

	return
}

func (r *Manager) ensureOwnerRole(ctx context.Context, tenant *capsulev1beta1.Tenant, owner *capsulev1beta1.OwnerSpec, roleName string) (err error) {
	switch roleName {
	case ImpersonatorRoleName:
		clusterRole := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: roleName + "-" + tenant.Name + "-" + owner.Name,
			},
		}

		resource := "users"
		if owner.Kind == capsulev1beta1.GroupOwner {
			resource = "groups"
		}

		resourceName := owner.Name
		if owner.Kind == capsulev1beta1.ServiceAccountOwner {
			resourceName = "system:serviceaccount:" + tenant.Namespace + ":" + owner.Name
		}

		_, err = controllerutil.CreateOrUpdate(ctx, r.Client, clusterRole, func() error {
			clusterRole.Rules = []rbacv1.PolicyRule{
				{
					APIGroups:     []string{""},
					Resources:     []string{resource},
					Verbs:         []string{"impersonate"},
					ResourceNames: []string{resourceName},
				},
			}

			return nil
		})
	}

	return
}
