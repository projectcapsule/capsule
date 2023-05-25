// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	"github.com/clastix/capsule/pkg/api"
)

//nolint:maintidx
func generateTenantsSpecs() (Tenant, capsulev1beta1.Tenant) {
	var namespaceQuota int32 = 5

	nodeSelector := map[string]string{
		"foo": "bar",
	}
	v1alpha1AdditionalMetadataSpec := &AdditionalMetadata{
		Labels: map[string]string{
			"foo": "bar",
		},
		Annotations: map[string]string{
			"foo": "bar",
		},
	}
	v1alpha1AllowedListSpec := &api.AllowedListSpec{
		Exact: []string{"foo", "bar"},
		Regex: "^foo*",
	}
	v1beta1AdditionalMetadataSpec := &api.AdditionalMetadataSpec{
		Labels: map[string]string{
			"foo": "bar",
		},
		Annotations: map[string]string{
			"foo": "bar",
		},
	}
	v1beta1NamespaceOptions := &capsulev1beta1.NamespaceOptions{
		Quota:              &namespaceQuota,
		AdditionalMetadata: v1beta1AdditionalMetadataSpec,
	}
	v1beta1ServiceOptions := &api.ServiceOptions{
		AdditionalMetadata: v1beta1AdditionalMetadataSpec,
		AllowedServices: &api.AllowedServices{
			NodePort:     pointer.Bool(false),
			ExternalName: pointer.Bool(false),
			LoadBalancer: pointer.Bool(false),
		},
		ExternalServiceIPs: &api.ExternalServiceIPsSpec{
			Allowed: []api.AllowedIP{"192.168.0.1"},
		},
	}
	v1beta2AllowedListSpec := &api.SelectorAllowedListSpec{
		AllowedListSpec: api.AllowedListSpec{
			Exact: []string{"foo", "bar"},
			Regex: "^foo*",
		},
	}
	networkPolicies := []networkingv1.NetworkPolicySpec{
		{
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"foo": "tenant-resources",
								},
							},
						},
						{
							PodSelector: &metav1.LabelSelector{},
						},
						{
							IPBlock: &networkingv1.IPBlock{
								CIDR: "192.168.0.0/12",
							},
						},
					},
				},
			},
		},
	}
	limitRanges := []corev1.LimitRangeSpec{
		{
			Limits: []corev1.LimitRangeItem{
				{
					Type: corev1.LimitTypePod,
					Min: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceCPU:    resource.MustParse("50m"),
						corev1.ResourceMemory: resource.MustParse("5Mi"),
					},
					Max: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
				},
			},
		},
	}
	resourceQuotas := []corev1.ResourceQuotaSpec{
		{
			Hard: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceLimitsCPU:      resource.MustParse("8"),
				corev1.ResourceLimitsMemory:   resource.MustParse("16Gi"),
				corev1.ResourceRequestsCPU:    resource.MustParse("8"),
				corev1.ResourceRequestsMemory: resource.MustParse("16Gi"),
			},
			Scopes: []corev1.ResourceQuotaScope{
				corev1.ResourceQuotaScopeNotTerminating,
			},
		},
	}

	v1beta1Tnt := capsulev1beta1.Tenant{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "alice",
			Labels: map[string]string{
				"foo": "bar",
			},
			Annotations: map[string]string{
				"foo": "bar",
			},
		},
		Spec: capsulev1beta1.TenantSpec{
			Owners: capsulev1beta1.OwnerListSpec{
				{
					Kind: "User",
					Name: "alice",
					ProxyOperations: []capsulev1beta1.ProxySettings{
						{
							Kind:       "IngressClasses",
							Operations: []capsulev1beta1.ProxyOperation{"List", "Update", "Delete"},
						},
						{
							Kind:       "Nodes",
							Operations: []capsulev1beta1.ProxyOperation{"Update", "Delete"},
						},
						{
							Kind:       "StorageClasses",
							Operations: []capsulev1beta1.ProxyOperation{"Update", "Delete"},
						},
					},
				},
				{
					Kind: "User",
					Name: "bob",
					ProxyOperations: []capsulev1beta1.ProxySettings{
						{
							Kind:       "IngressClasses",
							Operations: []capsulev1beta1.ProxyOperation{"Update"},
						},
						{
							Kind:       "StorageClasses",
							Operations: []capsulev1beta1.ProxyOperation{"List"},
						},
					},
				},
				{
					Kind: "User",
					Name: "jack",
					ProxyOperations: []capsulev1beta1.ProxySettings{
						{
							Kind:       "IngressClasses",
							Operations: []capsulev1beta1.ProxyOperation{"Delete"},
						},
						{
							Kind:       "Nodes",
							Operations: []capsulev1beta1.ProxyOperation{"Delete"},
						},
						{
							Kind:       "StorageClasses",
							Operations: []capsulev1beta1.ProxyOperation{"List"},
						},
						{
							Kind:       "PriorityClasses",
							Operations: []capsulev1beta1.ProxyOperation{"List"},
						},
					},
				},
				{
					Kind: "Group",
					Name: "owner-foo",
					ProxyOperations: []capsulev1beta1.ProxySettings{
						{
							Kind:       "IngressClasses",
							Operations: []capsulev1beta1.ProxyOperation{"List"},
						},
					},
				},
				{
					Kind: "Group",
					Name: "owner-bar",
					ProxyOperations: []capsulev1beta1.ProxySettings{
						{
							Kind:       "IngressClasses",
							Operations: []capsulev1beta1.ProxyOperation{"List"},
						},
						{
							Kind:       "StorageClasses",
							Operations: []capsulev1beta1.ProxyOperation{"Delete"},
						},
					},
				},
				{
					Kind: "ServiceAccount",
					Name: "system:serviceaccount:oil-production:default",
					ProxyOperations: []capsulev1beta1.ProxySettings{
						{
							Kind:       "Nodes",
							Operations: []capsulev1beta1.ProxyOperation{"Update"},
						},
					},
				},
				{
					Kind: "ServiceAccount",
					Name: "system:serviceaccount:gas-production:gas",
					ProxyOperations: []capsulev1beta1.ProxySettings{
						{
							Kind:       "StorageClasses",
							Operations: []capsulev1beta1.ProxyOperation{"Update"},
						},
					},
				},
			},
			NamespaceOptions: v1beta1NamespaceOptions,
			ServiceOptions:   v1beta1ServiceOptions,
			StorageClasses:   &v1beta2AllowedListSpec.AllowedListSpec,
			IngressOptions: capsulev1beta1.IngressOptions{
				HostnameCollisionScope: api.HostnameCollisionScopeDisabled,
				AllowedClasses:         &v1beta2AllowedListSpec.AllowedListSpec,
				AllowedHostnames:       &v1beta2AllowedListSpec.AllowedListSpec,
			},
			ContainerRegistries: &v1beta2AllowedListSpec.AllowedListSpec,
			NodeSelector:        nodeSelector,
			NetworkPolicies: api.NetworkPolicySpec{
				Items: networkPolicies,
			},
			LimitRanges: api.LimitRangesSpec{
				Items: limitRanges,
			},
			ResourceQuota: api.ResourceQuotaSpec{
				Scope: api.ResourceQuotaScopeNamespace,
				Items: resourceQuotas,
			},
			AdditionalRoleBindings: []api.AdditionalRoleBindingsSpec{
				{
					ClusterRoleName: "crds-rolebinding",
					Subjects: []rbacv1.Subject{
						{
							Kind:     "Group",
							APIGroup: rbacv1.GroupName,
							Name:     "system:authenticated",
						},
					},
				},
			},
			ImagePullPolicies: []api.ImagePullPolicySpec{"Always", "IfNotPresent"},
			PriorityClasses: &api.AllowedListSpec{
				Exact: []string{"default"},
				Regex: "^tier-.*$",
			},
		},
		Status: capsulev1beta1.TenantStatus{
			Size:       1,
			Namespaces: []string{"foo", "bar"},
		},
	}

	v1alpha1Tnt := Tenant{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "alice",
			Labels: map[string]string{
				"foo": "bar",
			},
			Annotations: map[string]string{
				"foo":                                "bar",
				podAllowedImagePullPolicyAnnotation:  "Always,IfNotPresent",
				enableExternalNameAnnotation:         "false",
				enableNodePortsAnnotation:            "false",
				enableLoadBalancerAnnotation:         "false",
				podPriorityAllowedAnnotation:         "default",
				podPriorityAllowedRegexAnnotation:    "^tier-.*$",
				ownerGroupsAnnotation:                "owner-foo,owner-bar",
				ownerUsersAnnotation:                 "bob,jack",
				ownerServiceAccountAnnotation:        "system:serviceaccount:oil-production:default,system:serviceaccount:gas-production:gas",
				enableNodeUpdateAnnotation:           "alice,system:serviceaccount:oil-production:default",
				enableNodeDeletionAnnotation:         "alice,jack",
				enableStorageClassListingAnnotation:  "bob,jack",
				enableStorageClassUpdateAnnotation:   "alice,system:serviceaccount:gas-production:gas",
				enableStorageClassDeletionAnnotation: "alice,owner-bar",
				enableIngressClassListingAnnotation:  "alice,owner-foo,owner-bar",
				enableIngressClassUpdateAnnotation:   "alice,bob",
				enableIngressClassDeletionAnnotation: "alice,jack",
				enablePriorityClassListingAnnotation: "jack",
				resourceQuotaScopeAnnotation:         "Namespace",
				ingressHostnameCollisionScope:        "Disabled",
			},
		},
		Spec: TenantSpec{
			Owner: OwnerSpec{
				Name: "alice",
				Kind: "User",
			},
			NamespaceQuota:      &namespaceQuota,
			NamespacesMetadata:  v1alpha1AdditionalMetadataSpec,
			ServicesMetadata:    v1alpha1AdditionalMetadataSpec,
			StorageClasses:      v1alpha1AllowedListSpec,
			IngressClasses:      v1alpha1AllowedListSpec,
			IngressHostnames:    v1alpha1AllowedListSpec,
			ContainerRegistries: v1alpha1AllowedListSpec,
			NodeSelector:        nodeSelector,
			NetworkPolicies:     networkPolicies,
			LimitRanges:         limitRanges,
			ResourceQuota:       resourceQuotas,
			AdditionalRoleBindings: []api.AdditionalRoleBindingsSpec{
				{
					ClusterRoleName: "crds-rolebinding",
					Subjects: []rbacv1.Subject{
						{
							Kind:     "Group",
							APIGroup: rbacv1.GroupName,
							Name:     "system:authenticated",
						},
					},
				},
			},
			ExternalServiceIPs: &api.ExternalServiceIPsSpec{
				Allowed: []api.AllowedIP{"192.168.0.1"},
			},
		},
		Status: TenantStatus{
			Size:       1,
			Namespaces: []string{"foo", "bar"},
		},
	}

	return v1alpha1Tnt, v1beta1Tnt
}

func TestConversionHub_ConvertTo(t *testing.T) {
	v1beta1ConvertedTnt := capsulev1beta1.Tenant{}

	v1alpha1Tnt, v1beta1tnt := generateTenantsSpecs()
	err := v1alpha1Tnt.ConvertTo(&v1beta1ConvertedTnt)

	if assert.NoError(t, err) {
		sort.Slice(v1beta1tnt.Spec.Owners, func(i, j int) bool {
			return v1beta1tnt.Spec.Owners[i].Name < v1beta1tnt.Spec.Owners[j].Name
		})
		sort.Slice(v1beta1ConvertedTnt.Spec.Owners, func(i, j int) bool {
			return v1beta1ConvertedTnt.Spec.Owners[i].Name < v1beta1ConvertedTnt.Spec.Owners[j].Name
		})

		for _, owner := range v1beta1tnt.Spec.Owners {
			sort.Slice(owner.ProxyOperations, func(i, j int) bool {
				return owner.ProxyOperations[i].Kind < owner.ProxyOperations[j].Kind
			})
		}

		for _, owner := range v1beta1ConvertedTnt.Spec.Owners {
			sort.Slice(owner.ProxyOperations, func(i, j int) bool {
				return owner.ProxyOperations[i].Kind < owner.ProxyOperations[j].Kind
			})
		}

		assert.Equal(t, v1beta1tnt, v1beta1ConvertedTnt)
	}
}

func TestConversionHub_ConvertFrom(t *testing.T) {
	v1alpha1ConvertedTnt := Tenant{}

	v1alpha1Tnt, v1beta1tnt := generateTenantsSpecs()

	err := v1alpha1ConvertedTnt.ConvertFrom(&v1beta1tnt)
	if assert.NoError(t, err) {
		assert.EqualValues(t, v1alpha1Tnt, v1alpha1ConvertedTnt)
	}
}
