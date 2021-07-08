// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

const (
	podAllowedImagePullPolicyAnnotation = "capsule.clastix.io/allowed-image-pull-policy"

	podPriorityAllowedAnnotation      = "priorityclass.capsule.clastix.io/allowed"
	podPriorityAllowedRegexAnnotation = "priorityclass.capsule.clastix.io/allowed-regex"

	enableNodePortsAnnotation = "capsule.clastix.io/enable-node-ports"

	ownerGroupsAnnotation         = "owners.capsule.clastix.io/group"
	ownerUsersAnnotation          = "owners.capsule.clastix.io/user"
	ownerServiceAccountAnnotation = "owners.capsule.clastix.io/serviceaccount"

	enableNodeListingAnnotation          = "capsule.clastix.io/enable-node-listing"
	enableNodeUpdateAnnotation           = "capsule.clastix.io/enable-node-update"
	enableNodeDeletionAnnotation         = "capsule.clastix.io/enable-node-deletion"
	enableStorageClassListingAnnotation  = "capsule.clastix.io/enable-storageclass-listing"
	enableStorageClassUpdateAnnotation   = "capsule.clastix.io/enable-storageclass-update"
	enableStorageClassDeletionAnnotation = "capsule.clastix.io/enable-storageclass-deletion"
	enableIngressClassListingAnnotation  = "capsule.clastix.io/enable-ingressclass-listing"
	enableIngressClassUpdateAnnotation   = "capsule.clastix.io/enable-ingressclass-update"
	enableIngressClassDeletionAnnotation = "capsule.clastix.io/enable-ingressclass-deletion"

	listOperation   = "List"
	updateOperation = "Update"
	deleteOperation = "Delete"

	nodesServiceKind          = "Nodes"
	storageClassesServiceKind = "StorageClasses"
	ingressClassesServiceKind = "IngressClasses"
)

func (t *Tenant) convertV1Alpha1OwnerToV1Beta1() []capsulev1beta1.OwnerSpec {
	var serviceKindToAnnotationMap = map[capsulev1beta1.ProxyServiceKind][]string{
		nodesServiceKind:          {enableNodeListingAnnotation, enableNodeUpdateAnnotation, enableNodeDeletionAnnotation},
		storageClassesServiceKind: {enableStorageClassListingAnnotation, enableStorageClassUpdateAnnotation, enableStorageClassDeletionAnnotation},
		ingressClassesServiceKind: {enableIngressClassListingAnnotation, enableIngressClassUpdateAnnotation, enableIngressClassDeletionAnnotation},
	}
	var annotationToOperationMap = map[string]capsulev1beta1.ProxyOperation{
		enableNodeListingAnnotation:          listOperation,
		enableNodeUpdateAnnotation:           updateOperation,
		enableNodeDeletionAnnotation:         deleteOperation,
		enableStorageClassListingAnnotation:  listOperation,
		enableStorageClassUpdateAnnotation:   updateOperation,
		enableStorageClassDeletionAnnotation: deleteOperation,
		enableIngressClassListingAnnotation:  listOperation,
		enableIngressClassUpdateAnnotation:   updateOperation,
		enableIngressClassDeletionAnnotation: deleteOperation,
	}
	var annotationToOwnerKindMap = map[string]capsulev1beta1.OwnerKind{
		ownerUsersAnnotation:          "User",
		ownerGroupsAnnotation:         "Group",
		ownerServiceAccountAnnotation: "ServiceAccount",
	}
	annotations := t.GetAnnotations()

	var operations = make(map[string]map[capsulev1beta1.ProxyServiceKind][]capsulev1beta1.ProxyOperation)

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

	var owners []capsulev1beta1.OwnerSpec

	var getProxySettingsForOwner = func(ownerName string) (settings []capsulev1beta1.ProxySettings) {
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
		Kind:            capsulev1beta1.OwnerKind(t.Spec.Owner.Kind),
		Name:            t.Spec.Owner.Name,
		ProxyOperations: getProxySettingsForOwner(t.Spec.Owner.Name),
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

func (t *Tenant) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*capsulev1beta1.Tenant)
	annotations := t.GetAnnotations()

	// ObjectMeta
	dst.ObjectMeta = t.ObjectMeta

	// Spec
	dst.Spec.NamespaceQuota = t.Spec.NamespaceQuota
	dst.Spec.NodeSelector = t.Spec.NodeSelector

	dst.Spec.Owners = t.convertV1Alpha1OwnerToV1Beta1()

	if t.Spec.NamespacesMetadata != nil {
		dst.Spec.NamespacesMetadata = &capsulev1beta1.AdditionalMetadataSpec{
			AdditionalLabels:      t.Spec.NamespacesMetadata.AdditionalLabels,
			AdditionalAnnotations: t.Spec.NamespacesMetadata.AdditionalAnnotations,
		}
	}
	if t.Spec.ServicesMetadata != nil {
		dst.Spec.ServicesMetadata = &capsulev1beta1.AdditionalMetadataSpec{
			AdditionalLabels:      t.Spec.ServicesMetadata.AdditionalLabels,
			AdditionalAnnotations: t.Spec.ServicesMetadata.AdditionalAnnotations,
		}
	}
	if t.Spec.StorageClasses != nil {
		dst.Spec.StorageClasses = &capsulev1beta1.AllowedListSpec{
			Exact: t.Spec.StorageClasses.Exact,
			Regex: t.Spec.StorageClasses.Regex,
		}
	}
	if t.Spec.IngressClasses != nil {
		dst.Spec.IngressClasses = &capsulev1beta1.AllowedListSpec{
			Exact: t.Spec.IngressClasses.Exact,
			Regex: t.Spec.IngressClasses.Regex,
		}
	}
	if t.Spec.IngressHostnames != nil {
		dst.Spec.IngressHostnames = &capsulev1beta1.AllowedListSpec{
			Exact: t.Spec.IngressHostnames.Exact,
			Regex: t.Spec.IngressHostnames.Regex,
		}
	}
	if t.Spec.ContainerRegistries != nil {
		dst.Spec.ContainerRegistries = &capsulev1beta1.AllowedListSpec{
			Exact: t.Spec.ContainerRegistries.Exact,
			Regex: t.Spec.ContainerRegistries.Regex,
		}
	}
	if len(t.Spec.NetworkPolicies) > 0 {
		dst.Spec.NetworkPolicies = &capsulev1beta1.NetworkPolicySpec{
			Items: t.Spec.NetworkPolicies,
		}
	}
	if len(t.Spec.LimitRanges) > 0 {
		dst.Spec.LimitRanges = &capsulev1beta1.LimitRangesSpec{
			Items: t.Spec.LimitRanges,
		}
	}
	if len(t.Spec.ResourceQuota) > 0 {
		dst.Spec.ResourceQuota = &capsulev1beta1.ResourceQuotaSpec{
			Items: t.Spec.ResourceQuota,
		}
	}
	if len(t.Spec.AdditionalRoleBindings) > 0 {
		for _, rb := range t.Spec.AdditionalRoleBindings {
			dst.Spec.AdditionalRoleBindings = append(dst.Spec.AdditionalRoleBindings, capsulev1beta1.AdditionalRoleBindingsSpec{
				ClusterRoleName: rb.ClusterRoleName,
				Subjects:        rb.Subjects,
			})
		}
	}
	if t.Spec.ExternalServiceIPs != nil {
		var allowedIPs []capsulev1beta1.AllowedIP
		for _, IP := range t.Spec.ExternalServiceIPs.Allowed {
			allowedIPs = append(allowedIPs, capsulev1beta1.AllowedIP(IP))
		}

		dst.Spec.ExternalServiceIPs = &capsulev1beta1.ExternalServiceIPsSpec{
			Allowed: allowedIPs,
		}
	}

	pullPolicies, ok := annotations[podAllowedImagePullPolicyAnnotation]
	if ok {
		for _, policy := range strings.Split(pullPolicies, ",") {
			dst.Spec.ImagePullPolicies = append(dst.Spec.ImagePullPolicies, capsulev1beta1.ImagePullPolicySpec(policy))
		}
	}

	priorityClasses := capsulev1beta1.AllowedListSpec{}

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
			return errors.Wrap(err, fmt.Sprintf("unable to parse %s annotation on tenant %s", enableNodePortsAnnotation, t.GetName()))
		}
		dst.Spec.EnableNodePorts = val
	}

	// Status
	dst.Status = capsulev1beta1.TenantStatus{
		Size:       t.Status.Size,
		Namespaces: t.Status.Namespaces,
	}

	// Remove unneeded annotations
	delete(dst.ObjectMeta.Annotations, podAllowedImagePullPolicyAnnotation)
	delete(dst.ObjectMeta.Annotations, podPriorityAllowedAnnotation)
	delete(dst.ObjectMeta.Annotations, podPriorityAllowedRegexAnnotation)
	delete(dst.ObjectMeta.Annotations, enableNodePortsAnnotation)
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

	return nil
}

func (t *Tenant) convertV1Beta1OwnerToV1Alpha1(src *capsulev1beta1.Tenant) {
	var ownersAnnotations = map[string][]string{
		ownerGroupsAnnotation:         nil,
		ownerUsersAnnotation:          nil,
		ownerServiceAccountAnnotation: nil,
	}

	var proxyAnnotations = map[string][]string{
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
			t.Spec.Owner = OwnerSpec{
				Name: owner.Name,
				Kind: Kind(owner.Kind),
			}
		} else {
			switch owner.Kind {
			case "User":
				ownersAnnotations[ownerUsersAnnotation] = append(ownersAnnotations[ownerUsersAnnotation], owner.Name)
			case "Group":
				ownersAnnotations[ownerGroupsAnnotation] = append(ownersAnnotations[ownerGroupsAnnotation], owner.Name)
			case "ServiceAccount":
				ownersAnnotations[ownerServiceAccountAnnotation] = append(ownersAnnotations[ownerServiceAccountAnnotation], owner.Name)
			}
		}
		for _, setting := range owner.ProxyOperations {
			switch setting.Kind {
			case nodesServiceKind:
				for _, operation := range setting.Operations {
					switch operation {
					case listOperation:
						proxyAnnotations[enableNodeListingAnnotation] = append(proxyAnnotations[enableNodeListingAnnotation], owner.Name)
					case updateOperation:
						proxyAnnotations[enableNodeUpdateAnnotation] = append(proxyAnnotations[enableNodeUpdateAnnotation], owner.Name)
					case deleteOperation:
						proxyAnnotations[enableNodeDeletionAnnotation] = append(proxyAnnotations[enableNodeDeletionAnnotation], owner.Name)
					}
				}
			case storageClassesServiceKind:
				for _, operation := range setting.Operations {
					switch operation {
					case listOperation:
						proxyAnnotations[enableStorageClassListingAnnotation] = append(proxyAnnotations[enableStorageClassListingAnnotation], owner.Name)
					case updateOperation:
						proxyAnnotations[enableStorageClassUpdateAnnotation] = append(proxyAnnotations[enableStorageClassUpdateAnnotation], owner.Name)
					case deleteOperation:
						proxyAnnotations[enableStorageClassDeletionAnnotation] = append(proxyAnnotations[enableStorageClassDeletionAnnotation], owner.Name)
					}
				}
			case ingressClassesServiceKind:
				for _, operation := range setting.Operations {
					switch operation {
					case listOperation:
						proxyAnnotations[enableIngressClassListingAnnotation] = append(proxyAnnotations[enableIngressClassListingAnnotation], owner.Name)
					case updateOperation:
						proxyAnnotations[enableIngressClassUpdateAnnotation] = append(proxyAnnotations[enableIngressClassUpdateAnnotation], owner.Name)
					case deleteOperation:
						proxyAnnotations[enableIngressClassDeletionAnnotation] = append(proxyAnnotations[enableIngressClassDeletionAnnotation], owner.Name)
					}
				}
			}
		}
	}

	for k, v := range ownersAnnotations {
		if len(v) > 0 {
			t.Annotations[k] = strings.Join(v, ",")
		}
	}
	for k, v := range proxyAnnotations {
		if len(v) > 0 {
			t.Annotations[k] = strings.Join(v, ",")
		}
	}
}

func (t *Tenant) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*capsulev1beta1.Tenant)

	// ObjectMeta
	t.ObjectMeta = src.ObjectMeta

	// Spec
	t.Spec.NamespaceQuota = src.Spec.NamespaceQuota
	t.Spec.NodeSelector = src.Spec.NodeSelector

	if t.Annotations == nil {
		t.Annotations = make(map[string]string)
	}

	t.convertV1Beta1OwnerToV1Alpha1(src)

	if src.Spec.NamespacesMetadata != nil {
		t.Spec.NamespacesMetadata = &AdditionalMetadataSpec{
			AdditionalLabels:      src.Spec.NamespacesMetadata.AdditionalLabels,
			AdditionalAnnotations: src.Spec.NamespacesMetadata.AdditionalAnnotations,
		}
	}
	if src.Spec.ServicesMetadata != nil {
		t.Spec.ServicesMetadata = &AdditionalMetadataSpec{
			AdditionalLabels:      src.Spec.ServicesMetadata.AdditionalLabels,
			AdditionalAnnotations: src.Spec.ServicesMetadata.AdditionalAnnotations,
		}
	}
	if src.Spec.StorageClasses != nil {
		t.Spec.StorageClasses = &AllowedListSpec{
			Exact: src.Spec.StorageClasses.Exact,
			Regex: src.Spec.StorageClasses.Regex,
		}
	}
	if src.Spec.IngressClasses != nil {
		t.Spec.IngressClasses = &AllowedListSpec{
			Exact: src.Spec.IngressClasses.Exact,
			Regex: src.Spec.IngressClasses.Regex,
		}
	}
	if src.Spec.IngressHostnames != nil {
		t.Spec.IngressHostnames = &AllowedListSpec{
			Exact: src.Spec.IngressHostnames.Exact,
			Regex: src.Spec.IngressHostnames.Regex,
		}
	}
	if src.Spec.ContainerRegistries != nil {
		t.Spec.ContainerRegistries = &AllowedListSpec{
			Exact: src.Spec.ContainerRegistries.Exact,
			Regex: src.Spec.ContainerRegistries.Regex,
		}
	}
	if src.Spec.NetworkPolicies != nil {
		t.Spec.NetworkPolicies = src.Spec.NetworkPolicies.Items
	}
	if src.Spec.LimitRanges != nil {
		t.Spec.LimitRanges = src.Spec.LimitRanges.Items
	}
	if src.Spec.ResourceQuota != nil {
		t.Spec.ResourceQuota = src.Spec.ResourceQuota.Items
	}
	if len(src.Spec.AdditionalRoleBindings) > 0 {
		for _, rb := range src.Spec.AdditionalRoleBindings {
			t.Spec.AdditionalRoleBindings = append(t.Spec.AdditionalRoleBindings, AdditionalRoleBindingsSpec{
				ClusterRoleName: rb.ClusterRoleName,
				Subjects:        rb.Subjects,
			})
		}
	}
	if src.Spec.ExternalServiceIPs != nil {
		var allowedIPs []AllowedIP
		for _, IP := range src.Spec.ExternalServiceIPs.Allowed {
			allowedIPs = append(allowedIPs, AllowedIP(IP))
		}

		t.Spec.ExternalServiceIPs = &ExternalServiceIPsSpec{
			Allowed: allowedIPs,
		}
	}
	if len(src.Spec.ImagePullPolicies) != 0 {
		var pullPolicies []string
		for _, policy := range src.Spec.ImagePullPolicies {
			pullPolicies = append(pullPolicies, string(policy))
		}
		t.Annotations[podAllowedImagePullPolicyAnnotation] = strings.Join(pullPolicies, ",")
	}

	if src.Spec.PriorityClasses != nil {
		if len(src.Spec.PriorityClasses.Exact) != 0 {
			t.Annotations[podPriorityAllowedAnnotation] = strings.Join(src.Spec.PriorityClasses.Exact, ",")
		}
		if src.Spec.PriorityClasses.Regex != "" {
			t.Annotations[podPriorityAllowedRegexAnnotation] = src.Spec.PriorityClasses.Regex
		}
	}

	t.Annotations[enableNodePortsAnnotation] = strconv.FormatBool(src.Spec.EnableNodePorts)

	// Status
	t.Status = TenantStatus{
		Size:       src.Status.Size,
		Namespaces: src.Status.Namespaces,
	}

	return nil
}
