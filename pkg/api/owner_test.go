package api_test

import (
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/projectcapsule/capsule/pkg/api"
)

func TestCoreOwnerSpec_ToAdditionalRolebindings(t *testing.T) {
	tests := []struct {
		name string
		in   api.CoreOwnerSpec
		want []api.AdditionalRoleBindingsSpec
	}{
		{
			name: "no cluster roles yields empty slice",
			in: api.CoreOwnerSpec{
				UserSpec: api.UserSpec{
					Kind: api.UserOwner,
					Name: "alice",
				},
				ClusterRoles: nil,
			},
			want: []api.AdditionalRoleBindingsSpec{},
		},
		{
			name: "one role creates one binding with subject",
			in: api.CoreOwnerSpec{
				UserSpec: api.UserSpec{
					Kind: api.UserOwner,
					Name: "alice",
				},
				ClusterRoles: []string{"admin"},
			},
			want: []api.AdditionalRoleBindingsSpec{
				{
					ClusterRoleName: "admin",
					Subjects: []rbacv1.Subject{
						{APIGroup: rbacv1.GroupName, Kind: "User", Name: "alice"},
					},
				},
			},
		},
		{
			name: "multiple roles create one binding per role (preserves order)",
			in: api.CoreOwnerSpec{
				UserSpec: api.UserSpec{
					Kind: api.GroupOwner,
					Name: "devops",
				},
				ClusterRoles: []string{"view", "edit"},
			},
			want: []api.AdditionalRoleBindingsSpec{
				{
					ClusterRoleName: "view",
					Subjects: []rbacv1.Subject{
						{APIGroup: rbacv1.GroupName, Kind: "Group", Name: "devops"},
					},
				},
				{
					ClusterRoleName: "edit",
					Subjects: []rbacv1.Subject{
						{APIGroup: rbacv1.GroupName, Kind: "Group", Name: "devops"},
					},
				},
			},
		},
		{
			name: "serviceaccount subject is split correctly in bindings",
			in: api.CoreOwnerSpec{
				UserSpec: api.UserSpec{
					Kind: api.ServiceAccountOwner,
					Name: "system:serviceaccount:capsule-system:capsule",
				},
				ClusterRoles: []string{"admin", "service-admin"},
			},
			want: []api.AdditionalRoleBindingsSpec{
				{
					ClusterRoleName: "admin",
					Subjects: []rbacv1.Subject{
						{Kind: "ServiceAccount", Namespace: "capsule-system", Name: "capsule"},
					},
				},
				{
					ClusterRoleName: "service-admin",
					Subjects: []rbacv1.Subject{
						{Kind: "ServiceAccount", Namespace: "capsule-system", Name: "capsule"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.ToAdditionalRolebindings()

			if len(got) != len(tt.want) {
				t.Fatalf("expected %d bindings, got %d: %#v", len(tt.want), len(got), got)
			}

			for i := range tt.want {
				if got[i].ClusterRoleName != tt.want[i].ClusterRoleName {
					t.Fatalf("binding[%d].ClusterRoleName: expected %q, got %q", i, tt.want[i].ClusterRoleName, got[i].ClusterRoleName)
				}

				if len(got[i].Subjects) != len(tt.want[i].Subjects) {
					t.Fatalf("binding[%d].Subjects length: expected %d, got %d", i, len(tt.want[i].Subjects), len(got[i].Subjects))
				}

				for j := range tt.want[i].Subjects {
					if got[i].Subjects[j] != tt.want[i].Subjects[j] {
						t.Fatalf("binding[%d].Subjects[%d]: expected %#v, got %#v", i, j, tt.want[i].Subjects[j], got[i].Subjects[j])
					}
				}
			}
		})
	}
}
