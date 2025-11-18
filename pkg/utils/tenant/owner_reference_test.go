// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func TestIsTenantOwnerReference(t *testing.T) {
	capsuleGroup := capsulev1beta2.GroupVersion.Group

	tests := []struct {
		name string
		or   metav1.OwnerReference
		want bool
	}{
		{
			name: "valid tenant ownerRef with exact group and version",
			or: metav1.OwnerReference{
				APIVersion: capsuleGroup + "/v1beta2",
				Kind:       ObjectReferenceTenantKind,
				Name:       "my-tenant",
			},
			want: true,
		},
		{
			name: "valid tenant ownerRef with same group but different version",
			or: metav1.OwnerReference{
				APIVersion: capsuleGroup + "/v1",
				Kind:       ObjectReferenceTenantKind,
				Name:       "my-tenant",
			},
			want: true, // we intentionally only check the group, not the version
		},
		{
			name: "wrong group",
			or: metav1.OwnerReference{
				APIVersion: "other.group.io/v1beta2",
				Kind:       ObjectReferenceTenantKind,
				Name:       "my-tenant",
			},
			want: false,
		},
		{
			name: "wrong kind",
			or: metav1.OwnerReference{
				APIVersion: capsuleGroup + "/v1beta2",
				Kind:       "Namespace",
				Name:       "my-tenant",
			},
			want: false,
		},
		{
			name: "empty APIVersion",
			or: metav1.OwnerReference{
				APIVersion: "",
				Kind:       ObjectReferenceTenantKind,
				Name:       "my-tenant",
			},
			want: false,
		},
		{
			name: "APIVersion without slash (only version)",
			or: metav1.OwnerReference{
				APIVersion: "v1beta2",
				Kind:       ObjectReferenceTenantKind,
				Name:       "my-tenant",
			},
			want: false,
		},
		{
			name: "APIVersion with empty group",
			or: metav1.OwnerReference{
				APIVersion: "/v1beta2",
				Kind:       ObjectReferenceTenantKind,
				Name:       "my-tenant",
			},
			want: false,
		},
		{
			name: "APIVersion with empty version",
			or: metav1.OwnerReference{
				APIVersion: "",
				Kind:       ObjectReferenceTenantKind,
				Name:       "my-tenant",
			},
			want: false,
		},
		{
			name: "APIVersion with extra slash in version (still ok as long as group matches)",
			or: metav1.OwnerReference{
				APIVersion: capsuleGroup + "/v1beta2/extra",
				Kind:       ObjectReferenceTenantKind,
				Name:       "my-tenant",
			},
			want: false,
		},
		{
			name: "completely unrelated ownerRef",
			or: metav1.OwnerReference{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       "cm",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt // capture
		t.Run(tt.name, func(t *testing.T) {
			got := IsTenantOwnerReference(tt.or)
			if got != tt.want {
				t.Fatalf("IsTenantOwnerReference(%+v) = %v, want %v", tt.or, got, tt.want)
			}
		})
	}
}

func TestIsTenantOwnerReferenceForTenant(t *testing.T) {
	capsuleGroup := capsulev1beta2.GroupVersion.Group

	tests := []struct {
		name   string
		or     metav1.OwnerReference
		want   bool
		tenant *capsulev1beta2.Tenant
	}{
		{
			name: "valid tenant ownerRef with exact group and version (same tenant)",
			or: metav1.OwnerReference{
				APIVersion: capsuleGroup + "/v1beta2",
				Kind:       ObjectReferenceTenantKind,
				Name:       "my-tenant",
			},
			tenant: &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-tenant",
				},
			},
			want: true,
		},
		{
			name: "valid tenant ownerRef with exact group and version (different tenant)",
			or: metav1.OwnerReference{
				APIVersion: capsuleGroup + "/v1beta2",
				Kind:       ObjectReferenceTenantKind,
				Name:       "my-tenant",
			},
			tenant: &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-tenant-2",
				},
			},
			want: false,
		},
		{
			name: "valid tenant ownerRef with same group but different version (same tenant)",
			or: metav1.OwnerReference{
				APIVersion: capsuleGroup + "/v1",
				Kind:       ObjectReferenceTenantKind,
				Name:       "my-tenant",
			},
			tenant: &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-tenant",
				},
			},
			want: true, // we intentionally only check the group, not the version
		},
		{
			name: "valid tenant ownerRef with same group but different version (different tenant)",
			or: metav1.OwnerReference{
				APIVersion: capsuleGroup + "/v1",
				Kind:       ObjectReferenceTenantKind,
				Name:       "my-tenant",
			},
			tenant: &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-tenant-2",
				},
			},
			want: false, // we intentionally only check the group, not the version
		},
		{
			name: "wrong group",
			or: metav1.OwnerReference{
				APIVersion: "other.group.io/v1beta2",
				Kind:       ObjectReferenceTenantKind,
				Name:       "my-tenant",
			},
			tenant: &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-tenant",
				},
			},
			want: false,
		},
		{
			name: "wrong kind",
			or: metav1.OwnerReference{
				APIVersion: capsuleGroup + "/v1beta2",
				Kind:       "Namespace",
				Name:       "my-tenant",
			},
			tenant: &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-tenant",
				},
			},
			want: false,
		},
		{
			name: "empty APIVersion",
			or: metav1.OwnerReference{
				APIVersion: "",
				Kind:       ObjectReferenceTenantKind,
				Name:       "my-tenant",
			},
			tenant: &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-tenant",
				},
			},
			want: false,
		},
		{
			name: "empty tenant",
			or: metav1.OwnerReference{
				APIVersion: capsuleGroup + "/v1",
				Kind:       ObjectReferenceTenantKind,
				Name:       "my-tenant",
			},
			tenant: nil,
			want:   false,
		},
		{
			name: "APIVersion without slash (only version)",
			or: metav1.OwnerReference{
				APIVersion: "v1beta2",
				Kind:       ObjectReferenceTenantKind,
				Name:       "my-tenant",
			},
			tenant: &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-tenant",
				},
			},
			want: false,
		},
		{
			name: "APIVersion with empty group",
			or: metav1.OwnerReference{
				APIVersion: "/v1beta2",
				Kind:       ObjectReferenceTenantKind,
				Name:       "my-tenant",
			},
			tenant: &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-tenant",
				},
			},
			want: false,
		},
		{
			name: "APIVersion with empty version",
			or: metav1.OwnerReference{
				APIVersion: "",
				Kind:       ObjectReferenceTenantKind,
				Name:       "my-tenant",
			},
			tenant: &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-tenant",
				},
			},
			want: false,
		},
		{
			name: "APIVersion with extra slash in version (still ok as long as group matches)",
			or: metav1.OwnerReference{
				APIVersion: capsuleGroup + "/v1beta2/extra",
				Kind:       ObjectReferenceTenantKind,
				Name:       "my-tenant",
			},
			tenant: &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-tenant",
				},
			},
			want: false,
		},
		{
			name: "completely unrelated ownerRef",
			or: metav1.OwnerReference{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       "my-tenant",
			},
			tenant: &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-tenant",
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt // capture
		t.Run(tt.name, func(t *testing.T) {
			got := IsTenantOwnerReferenceForTenant(tt.or, tt.tenant)
			if got != tt.want {
				t.Fatalf("IsTenantOwnerReference(%+v) = %v, want %v", tt.or, got, tt.want)
			}
		})
	}
}
