// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

//nolint:nestif,cyclop,maintidx
package v1beta2

import (
	"fmt"
	"strconv"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/conversion"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	"github.com/clastix/capsule/pkg/api"
)

//nolint:gocyclo
func (in *Tenant) ConvertFrom(raw conversion.Hub) error {
	src, ok := raw.(*capsulev1beta1.Tenant)
	if !ok {
		return fmt.Errorf("expected *capsulev1beta1.Tenant, got %T", raw)
	}

	annotations := src.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	in.ObjectMeta = src.ObjectMeta
	in.Spec.Owners = make(OwnerListSpec, 0, len(src.Spec.Owners))

	for index, owner := range src.Spec.Owners {
		proxySettings := make([]ProxySettings, 0, len(owner.ProxyOperations))

		for _, proxyOp := range owner.ProxyOperations {
			ops := make([]ProxyOperation, 0, len(proxyOp.Operations))

			for _, op := range proxyOp.Operations {
				ops = append(ops, ProxyOperation(op))
			}

			proxySettings = append(proxySettings, ProxySettings{
				Kind:       ProxyServiceKind(proxyOp.Kind),
				Operations: ops,
			})
		}

		in.Spec.Owners = append(in.Spec.Owners, OwnerSpec{
			Kind:            OwnerKind(owner.Kind),
			Name:            owner.Name,
			ClusterRoles:    owner.GetRoles(*src, index),
			ProxyOperations: proxySettings,
		})
	}

	if nsOpts := src.Spec.NamespaceOptions; nsOpts != nil {
		in.Spec.NamespaceOptions = &NamespaceOptions{}

		in.Spec.NamespaceOptions.Quota = src.Spec.NamespaceOptions.Quota

		if metadata := nsOpts.AdditionalMetadata; metadata != nil {
			in.Spec.NamespaceOptions.AdditionalMetadata = metadata
		}

		if value, found := annotations[capsulev1beta1.ForbiddenNamespaceLabelsAnnotation]; found {
			in.Spec.NamespaceOptions.ForbiddenLabels.Exact = strings.Split(value, ",")

			delete(annotations, capsulev1beta1.ForbiddenNamespaceLabelsAnnotation)
		}

		if value, found := annotations[capsulev1beta1.ForbiddenNamespaceLabelsRegexpAnnotation]; found {
			in.Spec.NamespaceOptions.ForbiddenLabels.Regex = value

			delete(annotations, capsulev1beta1.ForbiddenNamespaceLabelsRegexpAnnotation)
		}

		if value, found := annotations[capsulev1beta1.ForbiddenNamespaceAnnotationsAnnotation]; found {
			in.Spec.NamespaceOptions.ForbiddenAnnotations.Exact = strings.Split(value, ",")

			delete(annotations, capsulev1beta1.ForbiddenNamespaceAnnotationsAnnotation)
		}

		if value, found := annotations[capsulev1beta1.ForbiddenNamespaceAnnotationsRegexpAnnotation]; found {
			in.Spec.NamespaceOptions.ForbiddenAnnotations.Regex = value

			delete(annotations, capsulev1beta1.ForbiddenNamespaceAnnotationsRegexpAnnotation)
		}
	}

	if svcOpts := src.Spec.ServiceOptions; svcOpts != nil {
		in.Spec.ServiceOptions = &api.ServiceOptions{}

		if metadata := svcOpts.AdditionalMetadata; metadata != nil {
			in.Spec.ServiceOptions.AdditionalMetadata = metadata
		}

		if allowed := svcOpts.AllowedServices; allowed != nil {
			in.Spec.ServiceOptions.AllowedServices = allowed
		}

		if externalIPs := svcOpts.ExternalServiceIPs; externalIPs != nil {
			in.Spec.ServiceOptions.ExternalServiceIPs = externalIPs
		}
	}

	if sc := src.Spec.StorageClasses; sc != nil {
		in.Spec.StorageClasses = &api.AllowedListSpec{
			Exact: sc.Exact,
			Regex: sc.Regex,
		}
	}

	if scope := src.Spec.IngressOptions.HostnameCollisionScope; len(scope) > 0 {
		in.Spec.IngressOptions.HostnameCollisionScope = scope
	}

	v, found := annotations[capsulev1beta1.DenyWildcard]
	if found {
		value, err := strconv.ParseBool(v)
		if err == nil {
			in.Spec.IngressOptions.AllowWildcardHostnames = value

			delete(annotations, capsulev1beta1.DenyWildcard)
		}
	}

	if ingressClass := src.Spec.IngressOptions.AllowedClasses; ingressClass != nil {
		in.Spec.IngressOptions.AllowedClasses = &api.AllowedListSpec{
			Exact: ingressClass.Exact,
			Regex: ingressClass.Regex,
		}
	}

	if hostnames := src.Spec.IngressOptions.AllowedHostnames; hostnames != nil {
		in.Spec.IngressOptions.AllowedClasses = &api.AllowedListSpec{
			Exact: hostnames.Exact,
			Regex: hostnames.Regex,
		}
	}

	if allowed := src.Spec.ContainerRegistries; allowed != nil {
		in.Spec.ContainerRegistries = &api.AllowedListSpec{
			Exact: allowed.Exact,
			Regex: allowed.Regex,
		}
	}

	in.Spec.NodeSelector = src.Spec.NodeSelector

	if items := src.Spec.NetworkPolicies.Items; len(items) > 0 {
		in.Spec.NetworkPolicies.Items = items
	}

	if items := src.Spec.LimitRanges.Items; len(items) > 0 {
		in.Spec.LimitRanges.Items = items
	}

	if scope := src.Spec.ResourceQuota.Scope; len(scope) > 0 {
		in.Spec.ResourceQuota.Scope = ResourceQuotaScope(scope)
	}

	if items := src.Spec.ResourceQuota.Items; len(items) > 0 {
		in.Spec.ResourceQuota.Items = items
	}

	in.Spec.AdditionalRoleBindings = make([]AdditionalRoleBindingsSpec, 0, len(src.Spec.AdditionalRoleBindings))
	for _, rb := range src.Spec.AdditionalRoleBindings {
		in.Spec.AdditionalRoleBindings = append(in.Spec.AdditionalRoleBindings, AdditionalRoleBindingsSpec{
			ClusterRoleName: rb.ClusterRoleName,
			Subjects:        rb.Subjects,
		})
	}

	in.Spec.ImagePullPolicies = src.Spec.ImagePullPolicies

	if allowed := src.Spec.PriorityClasses; allowed != nil {
		in.Spec.PriorityClasses = &api.AllowedListSpec{
			Exact: allowed.Exact,
			Regex: allowed.Regex,
		}
	}

	if v, found := annotations["capsule.clastix.io/cordon"]; found {
		value, err := strconv.ParseBool(v)
		if err == nil {
			delete(annotations, "capsule.clastix.io/cordon")
		}

		in.Spec.Cordoned = value
	}

	if _, found := annotations[capsulev1beta1.ProtectedTenantAnnotation]; found {
		in.Spec.PreventDeletion = true

		delete(annotations, capsulev1beta1.ProtectedTenantAnnotation)
	}

	in.SetAnnotations(annotations)

	in.Status.Namespaces = src.Status.Namespaces
	in.Status.Size = src.Status.Size
	in.Status.State = tenantState(src.Status.State)

	return nil
}

func (in *Tenant) ConvertTo(raw conversion.Hub) error {
	dst, ok := raw.(*capsulev1beta1.Tenant)
	if !ok {
		return fmt.Errorf("expected *capsulev1beta1.Tenant, got %T", raw)
	}

	annotations := in.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	dst.ObjectMeta = in.ObjectMeta
	dst.Spec.Owners = make(capsulev1beta1.OwnerListSpec, 0, len(in.Spec.Owners))

	for index, owner := range in.Spec.Owners {
		proxySettings := make([]capsulev1beta1.ProxySettings, 0, len(owner.ProxyOperations))

		for _, proxyOp := range owner.ProxyOperations {
			ops := make([]capsulev1beta1.ProxyOperation, 0, len(proxyOp.Operations))

			for _, op := range proxyOp.Operations {
				ops = append(ops, capsulev1beta1.ProxyOperation(op))
			}

			proxySettings = append(proxySettings, capsulev1beta1.ProxySettings{
				Kind:       capsulev1beta1.ProxyServiceKind(proxyOp.Kind),
				Operations: ops,
			})
		}

		dst.Spec.Owners = append(dst.Spec.Owners, capsulev1beta1.OwnerSpec{
			Kind:            capsulev1beta1.OwnerKind(owner.Kind),
			Name:            owner.Name,
			ProxyOperations: proxySettings,
		})

		if clusterRoles := owner.ClusterRoles; len(clusterRoles) > 0 {
			annotations[fmt.Sprintf("%s/%d", capsulev1beta1.ClusterRoleNamesAnnotation, index)] = strings.Join(owner.ClusterRoles, ",")
		}
	}

	if nsOpts := in.Spec.NamespaceOptions; nsOpts != nil {
		dst.Spec.NamespaceOptions = &capsulev1beta1.NamespaceOptions{}

		if quota := nsOpts.Quota; quota != nil {
			dst.Spec.NamespaceOptions.Quota = quota
		}

		if metadata := nsOpts.AdditionalMetadata; metadata != nil {
			dst.Spec.NamespaceOptions.AdditionalMetadata = metadata
		}

		if exact := nsOpts.ForbiddenAnnotations.Exact; len(exact) > 0 {
			annotations[capsulev1beta1.ForbiddenNamespaceAnnotationsAnnotation] = strings.Join(exact, ",")
		}

		if regex := nsOpts.ForbiddenAnnotations.Regex; len(regex) > 0 {
			annotations[capsulev1beta1.ForbiddenNamespaceAnnotationsRegexpAnnotation] = regex
		}

		if exact := nsOpts.ForbiddenLabels.Exact; len(exact) > 0 {
			annotations[capsulev1beta1.ForbiddenNamespaceLabelsAnnotation] = strings.Join(exact, ",")
		}

		if regex := nsOpts.ForbiddenLabels.Regex; len(regex) > 0 {
			annotations[capsulev1beta1.ForbiddenNamespaceLabelsRegexpAnnotation] = regex
		}
	}

	if svcOpts := in.Spec.ServiceOptions; svcOpts != nil {
		dst.Spec.ServiceOptions = &capsulev1beta1.ServiceOptions{}

		if metadata := svcOpts.AdditionalMetadata; metadata != nil {
			dst.Spec.ServiceOptions.AdditionalMetadata = metadata
		}

		if allowed := svcOpts.AllowedServices; allowed != nil {
			dst.Spec.ServiceOptions.AllowedServices = allowed
		}

		if externalIPs := svcOpts.ExternalServiceIPs; externalIPs != nil {
			dst.Spec.ServiceOptions.ExternalServiceIPs = svcOpts.ExternalServiceIPs
		}
	}

	if allowed := in.Spec.StorageClasses; allowed != nil {
		dst.Spec.StorageClasses = allowed
	}

	dst.Spec.IngressOptions.HostnameCollisionScope = in.Spec.IngressOptions.HostnameCollisionScope

	if allowed := in.Spec.IngressOptions.AllowedClasses; allowed != nil {
		dst.Spec.IngressOptions.AllowedClasses = allowed
	}

	if allowed := in.Spec.IngressOptions.AllowedHostnames; allowed != nil {
		dst.Spec.IngressOptions.AllowedHostnames = allowed
	}

	annotations[capsulev1beta1.DenyWildcard] = fmt.Sprintf("%t", in.Spec.IngressOptions.AllowWildcardHostnames)

	if allowed := in.Spec.ContainerRegistries; allowed != nil {
		dst.Spec.ContainerRegistries = allowed
	}

	dst.Spec.NodeSelector = in.Spec.NodeSelector

	dst.Spec.NetworkPolicies = capsulev1beta1.NetworkPolicySpec{
		Items: in.Spec.NetworkPolicies.Items,
	}

	dst.Spec.LimitRanges = capsulev1beta1.LimitRangesSpec{
		Items: in.Spec.LimitRanges.Items,
	}

	dst.Spec.ResourceQuota = capsulev1beta1.ResourceQuotaSpec{
		Scope: capsulev1beta1.ResourceQuotaScope(in.Spec.ResourceQuota.Scope),
		Items: in.Spec.ResourceQuota.Items,
	}

	dst.Spec.AdditionalRoleBindings = make([]api.AdditionalRoleBindingsSpec, 0, len(in.Spec.AdditionalRoleBindings))
	for _, item := range in.Spec.AdditionalRoleBindings {
		dst.Spec.AdditionalRoleBindings = append(dst.Spec.AdditionalRoleBindings, api.AdditionalRoleBindingsSpec{
			ClusterRoleName: item.ClusterRoleName,
			Subjects:        item.Subjects,
		})
	}

	dst.Spec.ImagePullPolicies = make([]api.ImagePullPolicySpec, 0, len(in.Spec.ImagePullPolicies))
	for _, item := range in.Spec.ImagePullPolicies {
		dst.Spec.ImagePullPolicies = append(dst.Spec.ImagePullPolicies, item)
	}

	if allowed := in.Spec.PriorityClasses; allowed != nil {
		dst.Spec.PriorityClasses = allowed
	}

	dst.SetAnnotations(annotations)

	return nil
}
