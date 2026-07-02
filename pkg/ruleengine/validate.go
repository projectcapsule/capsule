// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package ruleengine

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"

	k8smeta "k8s.io/apimachinery/pkg/api/meta"
	k8svalidation "k8s.io/apimachinery/pkg/util/validation"

	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
)

func ValidateRuleStatusBody(
	mapper k8smeta.RESTMapper,
	bodies []*rules.NamespaceRuleBodyNamespace,
) error {
	for i, rule := range bodies {
		if rule == nil || rule.Enforce == nil {
			continue
		}

		if err := validateWorkloadRules(i, rule.Enforce.Workloads); err != nil {
			return err
		}

		if err := validateServiceRules(i, rule.Enforce.Services); err != nil {
			return err
		}

		if err := validateMetadataRules(i, rule.Enforce.Metadata, mapper); err != nil {
			return err
		}
	}

	return nil
}

func validateWorkloadRules(
	ruleIndex int,
	workloads rules.NamespaceRuleEnforceWorkloadsBody,
) error {
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

func validateMetadataKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("key is empty")
	}

	if errs := k8svalidation.IsQualifiedName(key); len(errs) > 0 {
		return errors.New(strings.Join(errs, ", "))
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

	if err := rule.ValidateKnownKinds(mapper, fieldPath); err != nil {
		return err
	}

	return nil
}
