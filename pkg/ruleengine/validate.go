// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package ruleengine

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8svalidation "k8s.io/apimachinery/pkg/util/validation"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
	workloadruntime "github.com/projectcapsule/capsule/pkg/runtime/workloads"
)

func ValidateRuleStatusBody(
	mapper k8smeta.RESTMapper,
	bodies []*rules.NamespaceRuleBodyNamespace,
) error {
	quotaNames := make(map[string]string)

	for i, rule := range bodies {
		if rule == nil {
			continue
		}

		if err := validateAudience(i, rule.Audience); err != nil {
			return err
		}

		if err := validateQuotaRules(i, rule.Quota, quotaNames); err != nil {
			return err
		}

		if rule.Enforce == nil {
			continue
		}

		if err := validateWorkloadRules(i, rule.Enforce.Workloads); err != nil {
			return err
		}

		if err := validateServiceRules(i, rule.Enforce.Services); err != nil {
			return err
		}

		if err := validateIngressRules(i, rule.Enforce.Ingress); err != nil {
			return err
		}

		if err := validateMetadataRules(i, rule.Enforce.Metadata, mapper); err != nil {
			return err
		}
	}

	return nil
}

func validateQuotaRules(ruleIndex int, quotas []rules.ResourceQuotaRule, names map[string]string) error {
	for quotaIndex, quota := range quotas {
		path := fmt.Sprintf("rules[%d].quota[%d]", ruleIndex, quotaIndex)
		if errs := k8svalidation.IsDNS1123Label(quota.Name); len(errs) > 0 {
			return fmt.Errorf("%s.name %q is invalid: %s", path, quota.Name, strings.Join(errs, "; "))
		}

		if previous, found := names[quota.Name]; found {
			return fmt.Errorf(
				"%s.name %q is invalid: quota name is already used by %s",
				path,
				quota.Name,
				previous,
			)
		}

		names[quota.Name] = path

		if len(quota.Hard) == 0 {
			return fmt.Errorf("%s.hard is invalid: at least one resource is required", path)
		}

		for name, quantity := range quota.Hard {
			if quantity.Sign() < 0 {
				return fmt.Errorf(
					"rules[%d].quota[%d].hard[%q] is invalid: quantity must not be negative",
					ruleIndex,
					quotaIndex,
					name,
				)
			}
		}
	}

	return nil
}

func validateIngressRules(
	ruleIndex int,
	ingress rules.NamespaceRuleEnforceIngressBody,
) error {
	if len(ingress.Types) == 0 && len(ingress.Hostnames) > 0 {
		return fmt.Errorf(
			"rules[%d].enforce.ingress.types is invalid: types must be configured when hostnames are configured",
			ruleIndex,
		)
	}

	if len(ingress.Types) > 0 && len(ingress.Hostnames) == 0 {
		return fmt.Errorf(
			"rules[%d].enforce.ingress.hostnames is invalid: hostnames must be configured when types are configured",
			ruleIndex,
		)
	}

	for i, resourceType := range ingress.Types {
		switch resourceType {
		case rules.IngressTypeIngress, rules.IngressTypeRoute,
			rules.IngressTypeListenerSet,
			rules.IngressTypeHTTPRoute,
			rules.IngressTypeGateway,
			rules.IngressTypeTLSRoute,
			rules.IngressTypeGRPCRoute:
		default:
			return fmt.Errorf(
				"rules[%d].enforce.ingress.types[%d] %q is invalid: unsupported ingress resource type",
				ruleIndex,
				i,
				resourceType,
			)
		}
	}

	for i, hostname := range ingress.Hostnames {
		if err := validateExpressionMatch(
			hostname,
			fmt.Sprintf("rules[%d].enforce.ingress.hostnames[%d]", ruleIndex, i),
		); err != nil {
			return err
		}
	}

	return nil
}

func validateAudience(ruleIndex int, audience []rules.Audience) error {
	for i, subject := range audience {
		path := fmt.Sprintf("rules[%d].audience[%d]", ruleIndex, i)
		if strings.TrimSpace(subject.Name) == "" {
			return fmt.Errorf("%s.name is invalid: name is empty", path)
		}

		switch subject.Kind {
		case rules.AudienceKindUser, rules.AudienceKindGroup, rules.AudienceKindServiceAccount:
		case rules.AudienceKindCustom:
			switch rules.CustomAudience(subject.Name) {
			case rules.CustomAudienceCapsuleUser, rules.CustomAudienceAdministrator, rules.CustomAudienceTenantOwner, rules.CustomAudienceController:
			default:
				return fmt.Errorf("%s.name %q is invalid: unsupported custom audience", path, subject.Name)
			}
		default:
			return fmt.Errorf("%s.kind %q is invalid: unsupported audience kind", path, subject.Kind)
		}
	}

	return nil
}

func validateWorkloadRules(
	ruleIndex int,
	workloads rules.NamespaceRuleEnforceWorkloadsBody,
) error {
	for j, target := range workloads.Targets {
		switch target {
		case rules.DeprecatedValidateImages,
			rules.ValidatePod,
			rules.ValidateInitContainers,
			rules.ValidateEphemeralContainers,
			rules.ValidateContainers,
			rules.ValidateVolumes:
		default:
			return fmt.Errorf(
				"rules[%d].enforce.workloads.targets[%d] %q is invalid: unsupported workload target",
				ruleIndex,
				j,
				target,
			)
		}
	}

	if err := validateWorkloadResourceRules(ruleIndex, workloads); err != nil {
		return err
	}

	for j, registry := range workloads.Registries {
		if err := validateExpression(
			registry.Expression,
			fmt.Sprintf("rules[%d].enforce.workloads.registries[%d].exp", ruleIndex, j),
		); err != nil {
			return err
		}
	}

	for j, scheduler := range workloads.Schedulers {
		if err := validateExpression(
			scheduler.Expression,
			fmt.Sprintf("rules[%d].enforce.workloads.schedulers[%d].exp", ruleIndex, j),
		); err != nil {
			return err
		}
	}

	return nil
}

func validateWorkloadResourceRules(
	ruleIndex int,
	workloads rules.NamespaceRuleEnforceWorkloadsBody,
) error {
	resources := workloads.Resources
	if resources == nil {
		return nil
	}

	path := fmt.Sprintf("rules[%d].enforce.workloads.resources", ruleIndex)
	if len(resources.Requests) == 0 && len(resources.Limits) == 0 {
		return fmt.Errorf("%s is invalid: at least one request or limit policy is required", path)
	}

	podTarget, err := validateWorkloadResourceTargets(path, workloads.Targets)
	if err != nil {
		return err
	}

	if err := validateWorkloadRequestPolicies(path, resources.Requests, podTarget); err != nil {
		return err
	}

	return validateWorkloadLimitPolicies(path, resources, podTarget)
}

func validateWorkloadResourceTargets(
	path string,
	targets []rules.WorkloadValidationTarget,
) (bool, error) {
	podTarget := false

	for _, target := range targets {
		switch target {
		case rules.ValidatePod:
			podTarget = true
		case rules.ValidateContainers, rules.ValidateInitContainers:
		case rules.ValidateEphemeralContainers, rules.ValidateVolumes, rules.DeprecatedValidateImages:
			return false, fmt.Errorf(
				"%s is invalid: workload target %q does not support resource policies",
				path,
				target,
			)
		}
	}

	return podTarget, nil
}

func validateWorkloadRequestPolicies(
	path string,
	policies map[corev1.ResourceName]rules.WorkloadResourceRequestPolicy,
	podTarget bool,
) error {
	for name, policy := range policies {
		policyPath := fmt.Sprintf("%s.requests[%q]", path, name)
		if err := validateWorkloadResourceName(name, podTarget); err != nil {
			return fmt.Errorf("%s is invalid: %w", policyPath, err)
		}

		switch policy.Policy {
		case rules.WorkloadResourceRequestPolicyPreserve,
			rules.WorkloadResourceRequestPolicyRemove:
			if policy.Value != nil {
				return fmt.Errorf("%s.value is invalid: value is only supported by the Default policy", policyPath)
			}
		case rules.WorkloadResourceRequestPolicyDefault:
			if err := validateDefaultResourceQuantity(policyPath, policy.Value); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s.policy %q is invalid: unsupported request policy", policyPath, policy.Policy)
		}
	}

	return nil
}

func validateWorkloadLimitPolicies(
	path string,
	resources *rules.WorkloadResourceRules,
	podTarget bool,
) error {
	one := resource.MustParse("1")

	for name, policy := range resources.Limits {
		policyPath := fmt.Sprintf("%s.limits[%q]", path, name)
		if err := validateWorkloadResourceName(name, podTarget); err != nil {
			return fmt.Errorf("%s is invalid: %w", policyPath, err)
		}

		switch policy.Policy {
		case rules.WorkloadResourceLimitPolicyPreserve,
			rules.WorkloadResourceLimitPolicyRemove,
			rules.WorkloadResourceLimitPolicyMatchRequest:
			if policy.Value != nil {
				return fmt.Errorf(
					"%s.value is invalid: value is only supported by the Default and Ratio policies",
					policyPath,
				)
			}
		case rules.WorkloadResourceLimitPolicyDefault:
			if err := validateDefaultResourceQuantity(policyPath, policy.Value); err != nil {
				return err
			}
		case rules.WorkloadResourceLimitPolicyRatio:
			if policy.Value == nil {
				return fmt.Errorf("%s.value is invalid: Ratio requires a value", policyPath)
			}

			if !workloadruntime.RatioSupportedResource(name) {
				return fmt.Errorf(
					"%s.policy is invalid: Ratio is only supported for cpu, memory, and ephemeral-storage",
					policyPath,
				)
			}

			if policy.Value.Cmp(one) < 0 {
				return fmt.Errorf("%s.value is invalid: Ratio must be greater than or equal to 1", policyPath)
			}
		default:
			return fmt.Errorf("%s.policy %q is invalid: unsupported limit policy", policyPath, policy.Policy)
		}

		if requestPolicy, found := resources.Requests[name]; found &&
			requestPolicy.Policy == rules.WorkloadResourceRequestPolicyRemove &&
			(policy.Policy == rules.WorkloadResourceLimitPolicyMatchRequest ||
				policy.Policy == rules.WorkloadResourceLimitPolicyRatio) {
			return fmt.Errorf(
				"%s is invalid: %s requires a request which is removed by the request policy",
				policyPath,
				policy.Policy,
			)
		}
	}

	return nil
}

func validateDefaultResourceQuantity(path string, value *resource.Quantity) error {
	if value == nil {
		return fmt.Errorf("%s.value is invalid: Default requires a value", path)
	}

	if value.Sign() < 0 {
		return fmt.Errorf("%s.value is invalid: quantity must not be negative", path)
	}

	return nil
}

func validateWorkloadResourceName(name corev1.ResourceName, podTarget bool) error {
	if errs := k8svalidation.IsQualifiedName(string(name)); len(errs) > 0 {
		return fmt.Errorf("resource name is invalid: %s", strings.Join(errs, "; "))
	}

	if !podTarget {
		return nil
	}

	if name == corev1.ResourceCPU || name == corev1.ResourceMemory ||
		strings.HasPrefix(string(name), corev1.ResourceHugePagesPrefix) {
		return nil
	}

	return fmt.Errorf("resource %q is not supported by pod-level resources", name)
}

func validateServiceRules(
	ruleIndex int,
	services rules.NamespaceRuleEnforceServicesBody,
) error {
	for j, serviceType := range services.Types {
		if err := validateServiceType(serviceType); err != nil {
			return fmt.Errorf(
				"rules[%d].enforce.services.types[%d] %q is invalid: %w",
				ruleIndex,
				j,
				serviceType,
				err,
			)
		}
	}

	if services.LoadBalancers != nil {
		for j, cidr := range services.LoadBalancers.CIDRs {
			if err := validateCIDR(cidr); err != nil {
				return fmt.Errorf(
					"rules[%d].enforce.services.loadBalancers.cidrs[%d] %q is invalid: %w",
					ruleIndex,
					j,
					cidr,
					err,
				)
			}
		}
	}

	if services.ExternalNames != nil {
		for j, hostname := range services.ExternalNames.Hostnames {
			if err := validateExpressionMatch(
				hostname,
				fmt.Sprintf("rules[%d].enforce.services.externalNames.hostnames[%d]", ruleIndex, j),
			); err != nil {
				return err
			}
		}
	}

	if services.NodePorts != nil {
		for j, portRange := range services.NodePorts.Ports {
			if err := validateNodePortRange(portRange); err != nil {
				return fmt.Errorf(
					"rules[%d].enforce.services.nodePorts.ports[%d] is invalid: %w",
					ruleIndex,
					j,
					err,
				)
			}
		}
	}

	return nil
}

func validateMetadataRules(
	ruleIndex int,
	metadata []rules.MetadataRule,
	mapper k8smeta.RESTMapper,
) error {
	for j, rule := range metadata {
		fieldPath := fmt.Sprintf("rules[%d].enforce.metadata[%d]", ruleIndex, j)

		if rule.HasWildcard() && metadataRuleHasManagedValues(rule) {
			return fmt.Errorf("%s is invalid: managed metadata requires concrete apiGroups and kinds", fieldPath)
		}

		if err := validateMetadataTargets(fieldPath, rule, mapper); err != nil {
			return err
		}

		for key, policy := range rule.Labels {
			if err := validateMetadataKey(key); err != nil {
				return fmt.Errorf(
					"%s.labels[%q] is invalid: %w",
					fieldPath,
					key,
					err,
				)
			}

			if err := validateMutableMetadataKey(key, policy); err != nil {
				return fmt.Errorf("%s.labels[%q] is invalid: %w", fieldPath, key, err)
			}

			for k, matcher := range policy.Values {
				if err := validateExpressionMatch(
					matcher,
					fmt.Sprintf("%s.labels[%q].values[%d]", fieldPath, key, k),
				); err != nil {
					return err
				}
			}
		}

		for key, policy := range rule.Annotations {
			if err := validateMetadataKey(key); err != nil {
				return fmt.Errorf(
					"%s.annotations[%q] is invalid: %w",
					fieldPath,
					key,
					err,
				)
			}

			if err := validateMutableMetadataKey(key, policy); err != nil {
				return fmt.Errorf("%s.annotations[%q] is invalid: %w", fieldPath, key, err)
			}

			for k, matcher := range policy.Values {
				if err := validateExpressionMatch(
					matcher,
					fmt.Sprintf("%s.annotations[%q].values[%d]", fieldPath, key, k),
				); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func metadataRuleHasManagedValues(rule rules.MetadataRule) bool {
	for _, policy := range rule.Labels {
		if policy.Managed != nil {
			return true
		}
	}

	for _, policy := range rule.Annotations {
		if policy.Managed != nil {
			return true
		}
	}

	return false
}

func validateMutableMetadataKey(key string, policy rules.MetadataValueRule) error {
	if policy.Default == nil && policy.Managed == nil {
		return nil
	}

	if errs := k8svalidation.IsQualifiedName(strings.TrimSpace(key)); len(errs) > 0 {
		return errors.New("default and managed require a concrete metadata key")
	}

	return nil
}

func validateMetadataKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("key is empty")
	}

	if !strings.ContainsAny(key, "*[](){}+?|^$\\") {
		if errs := k8svalidation.IsQualifiedName(key); len(errs) > 0 {
			return errors.New(strings.Join(errs, ", "))
		}
	}

	expression := rules.MetadataKeyExpression(key)
	if _, err := regexp.Compile(expression.Expression); err != nil {
		return fmt.Errorf("invalid key expression %q: %w", key, err)
	}

	return nil
}

func validateExpressionMatch(match runtime.ExpressionMatch, fieldPath string) error {
	if err := validateExpression(match.Expression, fieldPath+".exp"); err != nil {
		return err
	}

	return nil
}

func validateExpression(expression string, fieldPath string) error {
	if strings.TrimSpace(expression) == "" {
		return nil
	}

	if _, err := regexp.Compile(expression); err != nil {
		return fmt.Errorf("%s %q is invalid: %w", fieldPath, expression, err)
	}

	return nil
}

func validateServiceType(serviceType rules.ServiceType) error {
	switch serviceType {
	case rules.ServiceTypeClusterIP,
		rules.ServiceTypeNodePort,
		rules.ServiceTypeLoadBalancer,
		rules.ServiceTypeExternalName:
		return nil
	default:
		return fmt.Errorf("unsupported service type")
	}
}

func validateCIDR(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("CIDR is empty")
	}

	if !strings.Contains(raw, "/") {
		ip := net.ParseIP(raw)
		if ip == nil {
			return fmt.Errorf("must be a valid IP or CIDR")
		}

		return nil
	}

	if _, _, err := net.ParseCIDR(raw); err != nil {
		return err
	}

	return nil
}

func validateNodePortRange(portRange rules.ServiceNodePortRange) error {
	if portRange.From < 1 || portRange.From > 65535 {
		return fmt.Errorf("from %d must be between 1 and 65535", portRange.From)
	}

	if portRange.To < 1 || portRange.To > 65535 {
		return fmt.Errorf("to %d must be between 1 and 65535", portRange.To)
	}

	if portRange.From > portRange.To {
		return fmt.Errorf("from %d must be lower than or equal to %d", portRange.From, portRange.To)
	}

	return nil
}

func validateMetadataTargets(
	fieldPath string,
	rule rules.MetadataRule,
	mapper k8smeta.RESTMapper,
) error {
	if len(rule.Kinds) == 0 {
		return fmt.Errorf("%s.kinds is invalid: at least one kind must be configured", fieldPath)
	}

	for i, kind := range rule.Kinds {
		kind = strings.TrimSpace(kind)
		if kind == "" {
			return fmt.Errorf("%s.kinds[%d] is invalid: kind is empty", fieldPath, i)
		}
	}

	if mapper == nil {
		return nil
	}

	if err := rule.ValidateKnownKindsWithScope(mapper, fieldPath, func(
		gvk schema.GroupVersionKind,
		scope k8smeta.RESTScope,
	) bool {
		return scope.Name() == k8smeta.RESTScopeNameNamespace ||
			(gvk.Group == "" && gvk.Version == "v1" && gvk.Kind == "Namespace")
	}); err != nil {
		return err
	}

	return nil
}
