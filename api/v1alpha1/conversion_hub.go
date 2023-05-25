// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	"github.com/clastix/capsule/pkg/api"
)

const (
	resourceQuotaScopeAnnotation = "capsule.clastix.io/resource-quota-scope"

	podAllowedImagePullPolicyAnnotation = "capsule.clastix.io/allowed-image-pull-policy"

	podPriorityAllowedAnnotation      = "priorityclass.capsule.clastix.io/allowed"
	podPriorityAllowedRegexAnnotation = "priorityclass.capsule.clastix.io/allowed-regex"

	enableNodePortsAnnotation    = "capsule.clastix.io/enable-node-ports"
	enableExternalNameAnnotation = "capsule.clastix.io/enable-external-name"
	enableLoadBalancerAnnotation = "capsule.clastix.io/enable-loadbalancer-service"

	ownerGroupsAnnotation         = "owners.capsule.clastix.io/group"
	ownerUsersAnnotation          = "owners.capsule.clastix.io/user"
	ownerServiceAccountAnnotation = "owners.capsule.clastix.io/serviceaccount"

	enableNodeListingAnnotation           = "capsule.clastix.io/enable-node-listing"
	enableNodeUpdateAnnotation            = "capsule.clastix.io/enable-node-update"
	enableNodeDeletionAnnotation          = "capsule.clastix.io/enable-node-deletion"
	enableStorageClassListingAnnotation   = "capsule.clastix.io/enable-storageclass-listing"
	enableStorageClassUpdateAnnotation    = "capsule.clastix.io/enable-storageclass-update"
	enableStorageClassDeletionAnnotation  = "capsule.clastix.io/enable-storageclass-deletion"
	enableIngressClassListingAnnotation   = "capsule.clastix.io/enable-ingressclass-listing"
	enableIngressClassUpdateAnnotation    = "capsule.clastix.io/enable-ingressclass-update"
	enableIngressClassDeletionAnnotation  = "capsule.clastix.io/enable-ingressclass-deletion"
	enablePriorityClassListingAnnotation  = "capsule.clastix.io/enable-priorityclass-listing"
	enablePriorityClassUpdateAnnotation   = "capsule.clastix.io/enable-priorityclass-update"
	enablePriorityClassDeletionAnnotation = "capsule.clastix.io/enable-priorityclass-deletion"

	ingressHostnameCollisionScope = "ingress.capsule.clastix.io/hostname-collision-scope"
)

func (in *Tenant) convertV1Alpha1OwnerToV1Beta1() capsulev1beta1.OwnerListSpec {
	serviceKindToAnnotationMap := map[capsulev1beta1.ProxyServiceKind][]string{
		capsulev1beta1.NodesProxy:           {enableNodeListingAnnotation, enableNodeUpdateAnnotation, enableNodeDeletionAnnotation},
		capsulev1beta1.StorageClassesProxy:  {enableStorageClassListingAnnotation, enableStorageClassUpdateAnnotation, enableStorageClassDeletionAnnotation},
		capsulev1beta1.IngressClassesProxy:  {enableIngressClassListingAnnotation, enableIngressClassUpdateAnnotation, enableIngressClassDeletionAnnotation},
		capsulev1beta1.PriorityClassesProxy: {enablePriorityClassListingAnnotation, enablePriorityClassUpdateAnnotation, enablePriorityClassDeletionAnnotation},
	}
	annotationToOperationMap := map[string]capsulev1beta1.ProxyOperation{
		enableNodeListingAnnotation:           capsulev1beta1.ListOperation,
		enableNodeUpdateAnnotation:            capsulev1beta1.UpdateOperation,
		enableNodeDeletionAnnotation:          capsulev1beta1.DeleteOperation,
		enableStorageClassListingAnnotation:   capsulev1beta1.ListOperation,
		enableStorageClassUpdateAnnotation:    capsulev1beta1.UpdateOperation,
		enableStorageClassDeletionAnnotation:  capsulev1beta1.DeleteOperation,
		enableIngressClassListingAnnotation:   capsulev1beta1.ListOperation,
		enableIngressClassUpdateAnnotation:    capsulev1beta1.UpdateOperation,
		enableIngressClassDeletionAnnotation:  capsulev1beta1.DeleteOperation,
		enablePriorityClassListingAnnotation:  capsulev1beta1.ListOperation,
		enablePriorityClassUpdateAnnotation:   capsulev1beta1.UpdateOperation,
		enablePriorityClassDeletionAnnotation: capsulev1beta1.DeleteOperation,
	}
	annotationToOwnerKindMap := map[string]capsulev1beta1.OwnerKind{
		ownerUsersAnnotation:          capsulev1beta1.UserOwner,
		ownerGroupsAnnotation:         capsulev1beta1.GroupOwner,
		ownerServiceAccountAnnotation: capsulev1beta1.ServiceAccountOwner,
	}

	annotations := in.GetAnnotations()

	operations := make(map[string]map[capsulev1beta1.ProxyServiceKind][]capsulev1beta1.ProxyOperation)

	for serviceKind, operationAnnotations := range serviceKindToAnnotationMap {
		for _, operationAnnotation := range operationAnnotations {
			val, ok := annotations[operationAnnotation]
			if ok {
				for _, owner := range strings.Split(val, ",") {
					if _, exists := operations[owner]; !exists {
						operations[owner] = make(map[capsulev1beta1.ProxyServiceKind][]capsulev1beta1.ProxyOperation)
					}

					operations[owner][serviceKind] = append(operations[owner][serviceKind], annotationToOperationMap[operationAnnotation])
				}
			}
		}
	}

	var owners capsulev1beta1.OwnerListSpec

	getProxySettingsForOwner := func(ownerName string) (settings []capsulev1beta1.ProxySettings) {
		ownerOperations, ok := operations[ownerName]
		if ok {
			for k, v := range ownerOperations {
				settings = append(settings, capsulev1beta1.ProxySettings{
					Kind:       k,
					Operations: v,
				})
			}
		}

		return
	}

	owners = append(owners, capsulev1beta1.OwnerSpec{
		Kind:            capsulev1beta1.OwnerKind(in.Spec.Owner.Kind),
		Name:            in.Spec.Owner.Name,
		ProxyOperations: getProxySettingsForOwner(in.Spec.Owner.Name),
	})

	for ownerAnnotation, ownerKind := range annotationToOwnerKindMap {
		val, ok := annotations[ownerAnnotation]
		if ok {
			for _, owner := range strings.Split(val, ",") {
				owners = append(owners, capsulev1beta1.OwnerSpec{
					Kind:            ownerKind,
					Name:            owner,
					ProxyOperations: getProxySettingsForOwner(owner),
				})
			}
		}
	}

	return owners
}

//nolint:gocognit,gocyclo,cyclop,maintidx
func (in *Tenant) ConvertTo(dstRaw conversion.Hub) error {
	dst, ok := dstRaw.(*capsulev1beta1.Tenant)
	if !ok {
		return fmt.Errorf("expected type *capsulev1beta1.Tenant, got %T", dst)
	}

	annotations := in.GetAnnotations()

	// ObjectMeta
	dst.ObjectMeta = in.ObjectMeta

	// Spec
	if in.Spec.NamespaceQuota != nil {
		if dst.Spec.NamespaceOptions == nil {
			dst.Spec.NamespaceOptions = &capsulev1beta1.NamespaceOptions{}
		}

		dst.Spec.NamespaceOptions.Quota = in.Spec.NamespaceQuota
	}

	dst.Spec.NodeSelector = in.Spec.NodeSelector

	dst.Spec.Owners = in.convertV1Alpha1OwnerToV1Beta1()

	if in.Spec.NamespacesMetadata != nil {
		if dst.Spec.NamespaceOptions == nil {
			dst.Spec.NamespaceOptions = &capsulev1beta1.NamespaceOptions{}
		}

		dst.Spec.NamespaceOptions.AdditionalMetadata = &api.AdditionalMetadataSpec{
			Labels:      in.Spec.NamespacesMetadata.Labels,
			Annotations: in.Spec.NamespacesMetadata.Annotations,
		}
	}

	if in.Spec.ServicesMetadata != nil {
		if dst.Spec.ServiceOptions == nil {
			dst.Spec.ServiceOptions = &api.ServiceOptions{}
		}

		dst.Spec.ServiceOptions.AdditionalMetadata = &api.AdditionalMetadataSpec{
			Labels:      in.Spec.ServicesMetadata.Labels,
			Annotations: in.Spec.ServicesMetadata.Annotations,
		}
	}

	if in.Spec.StorageClasses != nil {
		dst.Spec.StorageClasses = in.Spec.StorageClasses
	}

	if v, annotationOk := in.Annotations[ingressHostnameCollisionScope]; annotationOk {
		switch v {
		case string(api.HostnameCollisionScopeCluster), string(api.HostnameCollisionScopeTenant), string(api.HostnameCollisionScopeNamespace):
			dst.Spec.IngressOptions.HostnameCollisionScope = api.HostnameCollisionScope(v)
		default:
			dst.Spec.IngressOptions.HostnameCollisionScope = api.HostnameCollisionScopeDisabled
		}
	}

	if in.Spec.IngressClasses != nil {
		dst.Spec.IngressOptions.AllowedClasses = &api.AllowedListSpec{
			Exact: in.Spec.IngressClasses.Exact,
			Regex: in.Spec.IngressClasses.Regex,
		}
	}

	if in.Spec.IngressHostnames != nil {
		dst.Spec.IngressOptions.AllowedHostnames = &api.AllowedListSpec{
			Exact: in.Spec.IngressHostnames.Exact,
			Regex: in.Spec.IngressHostnames.Regex,
		}
	}

	if in.Spec.ContainerRegistries != nil {
		dst.Spec.ContainerRegistries = &api.AllowedListSpec{
			Exact: in.Spec.ContainerRegistries.Exact,
			Regex: in.Spec.ContainerRegistries.Regex,
		}
	}

	if len(in.Spec.NetworkPolicies) > 0 {
		dst.Spec.NetworkPolicies = api.NetworkPolicySpec{
			Items: in.Spec.NetworkPolicies,
		}
	}

	if len(in.Spec.LimitRanges) > 0 {
		dst.Spec.LimitRanges = api.LimitRangesSpec{
			Items: in.Spec.LimitRanges,
		}
	}

	if len(in.Spec.ResourceQuota) > 0 {
		dst.Spec.ResourceQuota = api.ResourceQuotaSpec{
			Scope: func() api.ResourceQuotaScope {
				if v, annotationOk := in.GetAnnotations()[resourceQuotaScopeAnnotation]; annotationOk {
					switch v {
					case string(api.ResourceQuotaScopeNamespace):
						return api.ResourceQuotaScopeNamespace
					case string(api.ResourceQuotaScopeTenant):
						return api.ResourceQuotaScopeTenant
					}
				}

				return api.ResourceQuotaScopeTenant
			}(),
			Items: in.Spec.ResourceQuota,
		}
	}

	dst.Spec.AdditionalRoleBindings = in.Spec.AdditionalRoleBindings

	if in.Spec.ExternalServiceIPs != nil {
		if dst.Spec.ServiceOptions == nil {
			dst.Spec.ServiceOptions = &api.ServiceOptions{}
		}

		dst.Spec.ServiceOptions.ExternalServiceIPs = in.Spec.ExternalServiceIPs
	}

	pullPolicies, ok := annotations[podAllowedImagePullPolicyAnnotation]
	if ok {
		for _, policy := range strings.Split(pullPolicies, ",") {
			dst.Spec.ImagePullPolicies = append(dst.Spec.ImagePullPolicies, api.ImagePullPolicySpec(policy))
		}
	}

	priorityClasses := api.AllowedListSpec{}

	priorityClassAllowed, ok := annotations[podPriorityAllowedAnnotation]

	if ok {
		priorityClasses.Exact = strings.Split(priorityClassAllowed, ",")
	}

	priorityClassesRegexp, ok := annotations[podPriorityAllowedRegexAnnotation]

	if ok {
		priorityClasses.Regex = priorityClassesRegexp
	}

	if !reflect.ValueOf(priorityClasses).IsZero() {
		dst.Spec.PriorityClasses = &priorityClasses
	}

	enableNodePorts, ok := annotations[enableNodePortsAnnotation]
	if ok {
		val, err := strconv.ParseBool(enableNodePorts)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("unable to parse %s annotation on tenant %s", enableNodePortsAnnotation, in.GetName()))
		}

		if dst.Spec.ServiceOptions == nil {
			dst.Spec.ServiceOptions = &api.ServiceOptions{}
		}

		if dst.Spec.ServiceOptions.AllowedServices == nil {
			dst.Spec.ServiceOptions.AllowedServices = &api.AllowedServices{}
		}

		dst.Spec.ServiceOptions.AllowedServices.NodePort = pointer.Bool(val)
	}

	enableExternalName, ok := annotations[enableExternalNameAnnotation]
	if ok {
		val, err := strconv.ParseBool(enableExternalName)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("unable to parse %s annotation on tenant %s", enableExternalNameAnnotation, in.GetName()))
		}

		if dst.Spec.ServiceOptions == nil {
			dst.Spec.ServiceOptions = &api.ServiceOptions{}
		}

		if dst.Spec.ServiceOptions.AllowedServices == nil {
			dst.Spec.ServiceOptions.AllowedServices = &api.AllowedServices{}
		}

		dst.Spec.ServiceOptions.AllowedServices.ExternalName = pointer.Bool(val)
	}

	loadBalancerService, ok := annotations[enableLoadBalancerAnnotation]
	if ok {
		val, err := strconv.ParseBool(loadBalancerService)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("unable to parse %s annotation on tenant %s", enableLoadBalancerAnnotation, in.GetName()))
		}

		if dst.Spec.ServiceOptions == nil {
			dst.Spec.ServiceOptions = &api.ServiceOptions{}
		}

		if dst.Spec.ServiceOptions.AllowedServices == nil {
			dst.Spec.ServiceOptions.AllowedServices = &api.AllowedServices{}
		}

		dst.Spec.ServiceOptions.AllowedServices.LoadBalancer = pointer.Bool(val)
	}
	// Status
	dst.Status = capsulev1beta1.TenantStatus{
		Size:       in.Status.Size,
		Namespaces: in.Status.Namespaces,
	}
	// Remove unneeded annotations
	delete(dst.ObjectMeta.Annotations, podAllowedImagePullPolicyAnnotation)
	delete(dst.ObjectMeta.Annotations, podPriorityAllowedAnnotation)
	delete(dst.ObjectMeta.Annotations, podPriorityAllowedRegexAnnotation)
	delete(dst.ObjectMeta.Annotations, enableNodePortsAnnotation)
	delete(dst.ObjectMeta.Annotations, enableExternalNameAnnotation)
	delete(dst.ObjectMeta.Annotations, enableLoadBalancerAnnotation)
	delete(dst.ObjectMeta.Annotations, ownerGroupsAnnotation)
	delete(dst.ObjectMeta.Annotations, ownerUsersAnnotation)
	delete(dst.ObjectMeta.Annotations, ownerServiceAccountAnnotation)
	delete(dst.ObjectMeta.Annotations, enableNodeListingAnnotation)
	delete(dst.ObjectMeta.Annotations, enableNodeUpdateAnnotation)
	delete(dst.ObjectMeta.Annotations, enableNodeDeletionAnnotation)
	delete(dst.ObjectMeta.Annotations, enableStorageClassListingAnnotation)
	delete(dst.ObjectMeta.Annotations, enableStorageClassUpdateAnnotation)
	delete(dst.ObjectMeta.Annotations, enableStorageClassDeletionAnnotation)
	delete(dst.ObjectMeta.Annotations, enableIngressClassListingAnnotation)
	delete(dst.ObjectMeta.Annotations, enableIngressClassUpdateAnnotation)
	delete(dst.ObjectMeta.Annotations, enableIngressClassDeletionAnnotation)
	delete(dst.ObjectMeta.Annotations, enablePriorityClassListingAnnotation)
	delete(dst.ObjectMeta.Annotations, enablePriorityClassUpdateAnnotation)
	delete(dst.ObjectMeta.Annotations, enablePriorityClassDeletionAnnotation)
	delete(dst.ObjectMeta.Annotations, resourceQuotaScopeAnnotation)
	delete(dst.ObjectMeta.Annotations, ingressHostnameCollisionScope)

	return nil
}

//nolint:gocognit,gocyclo,cyclop
func (in *Tenant) convertV1Beta1OwnerToV1Alpha1(src *capsulev1beta1.Tenant) {
	ownersAnnotations := map[string][]string{
		ownerGroupsAnnotation:         nil,
		ownerUsersAnnotation:          nil,
		ownerServiceAccountAnnotation: nil,
	}

	proxyAnnotations := map[string][]string{
		enableNodeListingAnnotation:          nil,
		enableNodeUpdateAnnotation:           nil,
		enableNodeDeletionAnnotation:         nil,
		enableStorageClassListingAnnotation:  nil,
		enableStorageClassUpdateAnnotation:   nil,
		enableStorageClassDeletionAnnotation: nil,
		enableIngressClassListingAnnotation:  nil,
		enableIngressClassUpdateAnnotation:   nil,
		enableIngressClassDeletionAnnotation: nil,
	}

	for i, owner := range src.Spec.Owners {
		if i == 0 {
			in.Spec.Owner = OwnerSpec{
				Name: owner.Name,
				Kind: Kind(owner.Kind),
			}
		} else {
			switch owner.Kind {
			case capsulev1beta1.UserOwner:
				ownersAnnotations[ownerUsersAnnotation] = append(ownersAnnotations[ownerUsersAnnotation], owner.Name)
			case capsulev1beta1.GroupOwner:
				ownersAnnotations[ownerGroupsAnnotation] = append(ownersAnnotations[ownerGroupsAnnotation], owner.Name)
			case capsulev1beta1.ServiceAccountOwner:
				ownersAnnotations[ownerServiceAccountAnnotation] = append(ownersAnnotations[ownerServiceAccountAnnotation], owner.Name)
			}
		}

		for _, setting := range owner.ProxyOperations {
			switch setting.Kind {
			case capsulev1beta1.NodesProxy:
				for _, operation := range setting.Operations {
					switch operation {
					case capsulev1beta1.ListOperation:
						proxyAnnotations[enableNodeListingAnnotation] = append(proxyAnnotations[enableNodeListingAnnotation], owner.Name)
					case capsulev1beta1.UpdateOperation:
						proxyAnnotations[enableNodeUpdateAnnotation] = append(proxyAnnotations[enableNodeUpdateAnnotation], owner.Name)
					case capsulev1beta1.DeleteOperation:
						proxyAnnotations[enableNodeDeletionAnnotation] = append(proxyAnnotations[enableNodeDeletionAnnotation], owner.Name)
					}
				}
			case capsulev1beta1.PriorityClassesProxy:
				for _, operation := range setting.Operations {
					switch operation {
					case capsulev1beta1.ListOperation:
						proxyAnnotations[enablePriorityClassListingAnnotation] = append(proxyAnnotations[enablePriorityClassListingAnnotation], owner.Name)
					case capsulev1beta1.UpdateOperation:
						proxyAnnotations[enablePriorityClassUpdateAnnotation] = append(proxyAnnotations[enablePriorityClassUpdateAnnotation], owner.Name)
					case capsulev1beta1.DeleteOperation:
						proxyAnnotations[enablePriorityClassDeletionAnnotation] = append(proxyAnnotations[enablePriorityClassDeletionAnnotation], owner.Name)
					}
				}
			case capsulev1beta1.StorageClassesProxy:
				for _, operation := range setting.Operations {
					switch operation {
					case capsulev1beta1.ListOperation:
						proxyAnnotations[enableStorageClassListingAnnotation] = append(proxyAnnotations[enableStorageClassListingAnnotation], owner.Name)
					case capsulev1beta1.UpdateOperation:
						proxyAnnotations[enableStorageClassUpdateAnnotation] = append(proxyAnnotations[enableStorageClassUpdateAnnotation], owner.Name)
					case capsulev1beta1.DeleteOperation:
						proxyAnnotations[enableStorageClassDeletionAnnotation] = append(proxyAnnotations[enableStorageClassDeletionAnnotation], owner.Name)
					}
				}
			case capsulev1beta1.IngressClassesProxy:
				for _, operation := range setting.Operations {
					switch operation {
					case capsulev1beta1.ListOperation:
						proxyAnnotations[enableIngressClassListingAnnotation] = append(proxyAnnotations[enableIngressClassListingAnnotation], owner.Name)
					case capsulev1beta1.UpdateOperation:
						proxyAnnotations[enableIngressClassUpdateAnnotation] = append(proxyAnnotations[enableIngressClassUpdateAnnotation], owner.Name)
					case capsulev1beta1.DeleteOperation:
						proxyAnnotations[enableIngressClassDeletionAnnotation] = append(proxyAnnotations[enableIngressClassDeletionAnnotation], owner.Name)
					}
				}
			}
		}
	}

	for k, v := range ownersAnnotations {
		if len(v) > 0 {
			in.Annotations[k] = strings.Join(v, ",")
		}
	}

	for k, v := range proxyAnnotations {
		if len(v) > 0 {
			in.Annotations[k] = strings.Join(v, ",")
		}
	}
}

//nolint:cyclop
func (in *Tenant) ConvertFrom(srcRaw conversion.Hub) error {
	src, ok := srcRaw.(*capsulev1beta1.Tenant)
	if !ok {
		return fmt.Errorf("expected *capsulev1beta1.Tenant, got %T", srcRaw)
	}

	// ObjectMeta
	in.ObjectMeta = src.ObjectMeta

	// Spec
	if src.Spec.NamespaceOptions != nil && src.Spec.NamespaceOptions.Quota != nil {
		in.Spec.NamespaceQuota = src.Spec.NamespaceOptions.Quota
	}

	in.Spec.NodeSelector = src.Spec.NodeSelector

	if in.Annotations == nil {
		in.Annotations = make(map[string]string)
	}

	in.convertV1Beta1OwnerToV1Alpha1(src)

	if src.Spec.NamespaceOptions != nil && src.Spec.NamespaceOptions.AdditionalMetadata != nil {
		in.Spec.NamespacesMetadata = &AdditionalMetadata{
			Labels:      src.Spec.NamespaceOptions.AdditionalMetadata.Labels,
			Annotations: src.Spec.NamespaceOptions.AdditionalMetadata.Annotations,
		}
	}

	if src.Spec.ServiceOptions != nil && src.Spec.ServiceOptions.AdditionalMetadata != nil {
		in.Spec.ServicesMetadata = &AdditionalMetadata{
			Labels:      src.Spec.ServiceOptions.AdditionalMetadata.Labels,
			Annotations: src.Spec.ServiceOptions.AdditionalMetadata.Annotations,
		}
	}

	if src.Spec.StorageClasses != nil {
		in.Spec.StorageClasses = src.Spec.StorageClasses
	}

	in.Annotations[ingressHostnameCollisionScope] = string(src.Spec.IngressOptions.HostnameCollisionScope)

	if src.Spec.IngressOptions.AllowedClasses != nil {
		in.Spec.IngressClasses = src.Spec.IngressOptions.AllowedClasses
	}

	if src.Spec.IngressOptions.AllowedHostnames != nil {
		in.Spec.IngressHostnames = src.Spec.IngressOptions.AllowedHostnames
	}

	if src.Spec.ContainerRegistries != nil {
		in.Spec.ContainerRegistries = src.Spec.ContainerRegistries
	}

	if len(src.Spec.NetworkPolicies.Items) > 0 {
		in.Spec.NetworkPolicies = src.Spec.NetworkPolicies.Items
	}

	if len(src.Spec.LimitRanges.Items) > 0 {
		in.Spec.LimitRanges = src.Spec.LimitRanges.Items
	}

	if len(src.Spec.ResourceQuota.Items) > 0 {
		in.Annotations[resourceQuotaScopeAnnotation] = string(src.Spec.ResourceQuota.Scope)
		in.Spec.ResourceQuota = src.Spec.ResourceQuota.Items
	}

	in.Spec.AdditionalRoleBindings = src.Spec.AdditionalRoleBindings

	if src.Spec.ServiceOptions != nil && src.Spec.ServiceOptions.ExternalServiceIPs != nil {
		in.Spec.ExternalServiceIPs = src.Spec.ServiceOptions.ExternalServiceIPs
	}

	if len(src.Spec.ImagePullPolicies) != 0 {
		var pullPolicies []string

		for _, policy := range src.Spec.ImagePullPolicies {
			pullPolicies = append(pullPolicies, string(policy))
		}

		in.Annotations[podAllowedImagePullPolicyAnnotation] = strings.Join(pullPolicies, ",")
	}

	if src.Spec.PriorityClasses != nil {
		if len(src.Spec.PriorityClasses.Exact) != 0 {
			in.Annotations[podPriorityAllowedAnnotation] = strings.Join(src.Spec.PriorityClasses.Exact, ",")
		}

		if src.Spec.PriorityClasses.Regex != "" {
			in.Annotations[podPriorityAllowedRegexAnnotation] = src.Spec.PriorityClasses.Regex
		}
	}

	if src.Spec.ServiceOptions != nil && src.Spec.ServiceOptions.AllowedServices != nil {
		if src.Spec.ServiceOptions.AllowedServices.NodePort != nil {
			in.Annotations[enableNodePortsAnnotation] = strconv.FormatBool(*src.Spec.ServiceOptions.AllowedServices.NodePort)
		}

		if src.Spec.ServiceOptions.AllowedServices.ExternalName != nil {
			in.Annotations[enableExternalNameAnnotation] = strconv.FormatBool(*src.Spec.ServiceOptions.AllowedServices.ExternalName)
		}

		if src.Spec.ServiceOptions.AllowedServices.LoadBalancer != nil {
			in.Annotations[enableLoadBalancerAnnotation] = strconv.FormatBool(*src.Spec.ServiceOptions.AllowedServices.LoadBalancer)
		}
	}

	// Status
	in.Status = TenantStatus{
		Size:       src.Status.Size,
		Namespaces: src.Status.Namespaces,
	}

	return nil
}
