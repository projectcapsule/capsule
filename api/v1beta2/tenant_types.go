// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/misc"
)

// TenantSpec defines the desired state of Tenant.
type TenantSpec struct {
	// Specify Permissions for the Tenant.
	// +optional
	Permissions Permissions `json:"permissions,omitzero"`
	// Specify enforcement specifications for the scope of the Tenant.
	//  We are moving all configuration enforcement. per namespace into a rule construct.
	//  It's currently not final.
	//
	// Read More: https://projectcapsule.dev/docs/tenants/rules/
	//+optional
	Rules []*NamespaceRule `json:"rules,omitzero"`

	// Specifies the owners of the Tenant.
	// Optional
	Owners api.OwnerListSpec `json:"owners,omitempty"`
	// Specifies options for the Namespaces, such as additional metadata or maximum number of namespaces allowed for that Tenant. Once the namespace quota assigned to the Tenant has been reached, the Tenant owner cannot create further namespaces. Optional.
	NamespaceOptions *NamespaceOptions `json:"namespaceOptions,omitempty"`
	// Specifies options for the Service, such as additional metadata or block of certain type of Services. Optional.
	ServiceOptions *api.ServiceOptions `json:"serviceOptions,omitempty"`
	// Specifies options for the Pods deployed in the Tenant namespaces, such as additional metadata.
	PodOptions *api.PodOptions `json:"podOptions,omitempty"`
	// Specifies the allowed StorageClasses assigned to the Tenant.
	// Capsule assures that all PersistentVolumeClaim resources created in the Tenant can use only one of the allowed StorageClasses.
	// A default value can be specified, and all the PersistentVolumeClaim resources created will inherit the declared class.
	// Optional.
	StorageClasses *api.DefaultAllowedListSpec `json:"storageClasses,omitempty"`
	// Specifies options for the Ingress resources, such as allowed hostnames and IngressClass. Optional.
	// +optional
	IngressOptions IngressOptions `json:"ingressOptions,omitzero"`
	// Specifies the label to control the placement of pods on a given pool of worker nodes. All namespaces created within the Tenant will have the node selector annotation. This annotation tells the Kubernetes scheduler to place pods on the nodes having the selector label. Optional.
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Specifies a list of ResourceQuota resources assigned to the Tenant. The assigned values are inherited by any namespace created in the Tenant. The Capsule operator aggregates ResourceQuota at Tenant level, so that the hard quota is never crossed for the given Tenant. This permits the Tenant owner to consume resources in the Tenant regardless of the namespace. Optional.
	// +optional
	ResourceQuota api.ResourceQuotaSpec `json:"resourceQuotas,omitzero"`
	// Specifies additional RoleBindings assigned to the Tenant. Capsule will ensure that all namespaces in the Tenant always contain the RoleBinding for the given ClusterRole. Optional.
	AdditionalRoleBindings []api.AdditionalRoleBindingsSpec `json:"additionalRoleBindings,omitempty"`
	// Specifies the allowed RuntimeClasses assigned to the Tenant.
	// Capsule assures that all Pods resources created in the Tenant can use only one of the allowed RuntimeClasses.
	// Optional.
	RuntimeClasses *api.DefaultAllowedListSpec `json:"runtimeClasses,omitempty"`
	// Specifies the allowed priorityClasses assigned to the Tenant.
	// Capsule assures that all Pods resources created in the Tenant can use only one of the allowed PriorityClasses.
	// A default value can be specified, and all the Pod resources created will inherit the declared class.
	// Optional.
	PriorityClasses *api.DefaultAllowedListSpec `json:"priorityClasses,omitempty"`
	// Specifies options for the DeviceClass resources.
	DeviceClasses *api.SelectorAllowedListSpec `json:"deviceClasses,omitempty"`
	// Specifies options for the GatewayClass resources.
	// +optional
	GatewayOptions GatewayOptions `json:"gatewayOptions,omitzero"`
	// Toggling the Tenant resources cordoning, when enable resources cannot be deleted.
	//+kubebuilder:default:=false
	Cordoned bool `json:"cordoned,omitempty"`
	// Prevent accidental deletion of the Tenant.
	// When enabled, the deletion request will be declined.
	//+kubebuilder:default:=false
	PreventDeletion bool `json:"preventDeletion,omitempty"`
	// Use this if you want to disable/enable the Tenant name prefix to specific Tenants, overriding global forceTenantPrefix in CapsuleConfiguration.
	// When set to 'true', it enforces Namespaces created for this Tenant to be named with the Tenant name prefix,
	// separated by a dash (i.e. for Tenant 'foo', namespace names must be prefixed with 'foo-'),
	// this is useful to avoid Namespace name collision.
	// When set to 'false', it allows Namespaces created for this Tenant to be named anything.
	// Overrides CapsuleConfiguration global forceTenantPrefix for the Tenant only.
	// If unset, Tenant uses CapsuleConfiguration's forceTenantPrefix
	// Optional
	ForceTenantPrefix *bool `json:"forceTenantPrefix,omitempty"`

	// Deprecated: Use Enforcement.Registries instead
	//
	// Specifies the trusted Image Registries assigned to the Tenant. Capsule assures that all Pods resources created in the Tenant can use only one of the allowed trusted registries. Optional.
	ContainerRegistries *api.AllowedListSpec `json:"containerRegistries,omitempty"`
	// Deprecated: Use Enforcement.Registries instead
	//
	// Specify the allowed values for the imagePullPolicies option in Pod resources. Capsule assures that all Pod resources created in the Tenant can use only one of the allowed policy. Optional.
	ImagePullPolicies []api.ImagePullPolicySpec `json:"imagePullPolicies,omitempty"`

	// Deprecated: Use Tenant Replications instead (https://projectcapsule.dev/docs/replications/)
	//
	// Specifies the NetworkPolicies assigned to the Tenant. The assigned NetworkPolicies are inherited by any namespace created in the Tenant. Optional.
	// +optional
	NetworkPolicies api.NetworkPolicySpec `json:"networkPolicies,omitzero"`
	// Deprecated: Use Tenant Replications instead (https://projectcapsule.dev/docs/replications/)
	//
	// Specifies the resource min/max usage restrictions to the Tenant. The assigned values are inherited by any namespace created in the Tenant. Optional.
	// +optional
	LimitRanges api.LimitRangesSpec `json:"limitRanges,omitzero"`
}

type Permissions struct {
	// Matches TenantOwner objects which are promoted to owners of this tenant
	// The elements are OR operations and independent. You can see the resulting Tenant Owners
	// in the Status.Owners specification of the Tenant.
	MatchOwners []*metav1.LabelSelector `json:"matchOwners,omitempty"`
}

func (p *Permissions) ListMatchingOwners(
	ctx context.Context,
	c client.Client,
	tnt string,
	opts ...client.ListOption,
) ([]*TenantOwner, error) {
	defaultSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			meta.NewTenantLabel: tnt,
		},
	}

	return misc.ListBySelectors[*TenantOwner](ctx, c, &TenantOwnerList{}, append(p.MatchOwners, defaultSelector))
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=tnt
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.conditions[?(@.type==\"Cordoned\")].reason",description="The actual state of the Tenant"
// +kubebuilder:printcolumn:name="Namespace quota",type="integer",JSONPath=".spec.namespaceOptions.quota",description="The max amount of Namespaces can be created"
// +kubebuilder:printcolumn:name="Namespace count",type="integer",JSONPath=".status.size",description="The total amount of Namespaces in use"
// +kubebuilder:printcolumn:name="Node selector",type="string",JSONPath=".spec.nodeSelector",description="Node Selector applied to Pods"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="Reconcile Status for the tenant"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description="Reconcile Message for the tenant"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age"
// Tenant is the Schema for the tenants API.
type Tenant struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec TenantSpec `json:"spec"`

	// +optional
	Status TenantStatus `json:"status,omitzero"`
}

func (in *Tenant) GetNamespaces() (res []string) {
	res = make([]string, 0, len(in.Status.Namespaces))

	res = append(res, in.Status.Namespaces...)

	return res
}

// +kubebuilder:object:root=true

// TenantList contains a list of Tenant.
type TenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`

	Items []Tenant `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Tenant{}, &TenantList{})
}
