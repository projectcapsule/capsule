// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

func generateTenantsSpecs() (Tenant, capsulev1beta1.Tenant) {
	var namespaceQuota int32 = 5
	var nodeSelector = map[string]string{
		"foo": "bar",
	}
	var v1alpha1AdditionalMetadataSpec = &AdditionalMetadataSpec{
		AdditionalLabels: map[string]string{
			"foo": "bar",
		},
		AdditionalAnnotations: map[string]string{
			"foo": "bar",
		},
	}
	var v1alpha1AllowedListSpec = &AllowedListSpec{
		Exact: []string{"foo", "bar"},
		Regex: "^foo*",
	}
	var v1beta1AdditionalMetadataSpec = &capsulev1beta1.AdditionalMetadataSpec{
		AdditionalLabels: map[string]string{
			"foo": "bar",
		},
		AdditionalAnnotations: map[string]string{
			"foo": "bar",
		},
	}
	var v1beta1AllowedListSpec = &capsulev1beta1.AllowedListSpec{
		Exact: []string{"foo", "bar"},
		Regex: "^foo*",
	}
	var networkPolicies = []networkingv1.NetworkPolicySpec{
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
	var limitRanges = []corev1.LimitRangeSpec{
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
	var resourceQuotas = []corev1.ResourceQuotaSpec{
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

	var v1beta1Tnt = capsulev1beta1.Tenant{
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
			Owner: capsulev1beta1.OwnerSpec{
				Name: "alice",
				Kind: "User",
			},
			NamespaceQuota:      &namespaceQuota,
			NamespacesMetadata:  v1beta1AdditionalMetadataSpec,
			ServicesMetadata:    v1beta1AdditionalMetadataSpec,
			StorageClasses:      v1beta1AllowedListSpec,
			IngressClasses:      v1beta1AllowedListSpec,
			IngressHostnames:    v1beta1AllowedListSpec,
			ContainerRegistries: v1beta1AllowedListSpec,
			NodeSelector:        nodeSelector,
			NetworkPolicies: &capsulev1beta1.NetworkPolicySpec{
				Items: networkPolicies,
			},
			LimitRanges: &capsulev1beta1.LimitRangesSpec{
				Items: limitRanges,
			},
			ResourceQuota: &capsulev1beta1.ResourceQuotaSpec{
				Items: resourceQuotas,
			},
			AdditionalRoleBindings: []capsulev1beta1.AdditionalRoleBindingsSpec{
				{
					ClusterRoleName: "crds-rolebinding",
					Subjects: []rbacv1.Subject{
						{
							Kind:     "Group",
							APIGroup: "rbac.authorization.k8s.io",
							Name:     "system:authenticated",
						},
					},
				},
			},
			ExternalServiceIPs: &capsulev1beta1.ExternalServiceIPsSpec{
				Allowed: []capsulev1beta1.AllowedIP{"192.168.0.1"},
			},
			ImagePullPolicies: []capsulev1beta1.ImagePullPolicySpec{"Always", "IfNotPresent"},
			PriorityClasses: &capsulev1beta1.AllowedListSpec{
				Exact: []string{"default"},
				Regex: "^tier-.*$",
			},
			EnableNodePorts: false,
		},
		Status: capsulev1beta1.TenantStatus{
			Size:       1,
			Namespaces: []string{"foo", "bar"},
		},
	}

	var v1alpha1Tnt = Tenant{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "alice",
			Labels: map[string]string{
				"foo": "bar",
			},
			Annotations: map[string]string{
				"foo":                               "bar",
				podAllowedImagePullPolicyAnnotation: "Always,IfNotPresent",
				enableNodePortsAnnotation:           "false",
				podPriorityAllowedAnnotation:        "default",
				podPriorityAllowedRegexAnnotation:   "^tier-.*$",
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
			AdditionalRoleBindings: []AdditionalRoleBindingsSpec{
				{
					ClusterRoleName: "crds-rolebinding",
					Subjects: []rbacv1.Subject{
						{
							Kind:     "Group",
							APIGroup: "rbac.authorization.k8s.io",
							Name:     "system:authenticated",
						},
					},
				},
			},
			ExternalServiceIPs: &ExternalServiceIPsSpec{
				Allowed: []AllowedIP{"192.168.0.1"},
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
	var v1beta1ConvertedTnt = capsulev1beta1.Tenant{}

	v1alpha1Tnt, v1beta1tnt := generateTenantsSpecs()
	err := v1alpha1Tnt.ConvertTo(&v1beta1ConvertedTnt)
	if assert.NoError(t, err) {
		assert.Equal(t, v1beta1ConvertedTnt, v1beta1tnt)
	}
}

func TestConversionHub_ConvertFrom(t *testing.T) {
	var v1alpha1ConvertedTnt = Tenant{}
	v1alpha1Tnt, v1beta1tnt := generateTenantsSpecs()

	err := v1alpha1ConvertedTnt.ConvertFrom(&v1beta1tnt)
	if assert.NoError(t, err) {
		assert.Equal(t, v1alpha1ConvertedTnt, v1alpha1Tnt)
	}
}
