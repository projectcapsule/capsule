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
	podPriorityAllowedAnnotation        = "priorityclass.capsule.clastix.io/allowed"
	podPriorityAllowedRegexAnnotation   = "priorityclass.capsule.clastix.io/allowed-regex"
	enableNodePortsAnnotation           = "capsule.clastix.io/enable-node-ports"
)

func (t *Tenant) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*capsulev1beta1.Tenant)
	annotations := t.GetAnnotations()

	// ObjectMeta
	dst.ObjectMeta = t.ObjectMeta

	// Spec
	dst.Spec.NamespaceQuota = t.Spec.NamespaceQuota
	dst.Spec.NodeSelector = t.Spec.NodeSelector

	dst.Spec.Owner = capsulev1beta1.OwnerSpec{
		Name: t.Spec.Owner.Name,
		Kind: capsulev1beta1.Kind(t.Spec.Owner.Kind),
	}

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

	return nil
}

func (t *Tenant) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*capsulev1beta1.Tenant)

	// ObjectMeta
	t.ObjectMeta = src.ObjectMeta

	// Spec
	t.Spec.NamespaceQuota = src.Spec.NamespaceQuota
	t.Spec.NodeSelector = src.Spec.NodeSelector

	t.Spec.Owner = OwnerSpec{
		Name: src.Spec.Owner.Name,
		Kind: Kind(src.Spec.Owner.Kind),
	}

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
