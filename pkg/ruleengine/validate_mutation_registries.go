// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package ruleengine

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func validateRegistryMutation(path string, registry rules.WorkloadRegistryMutation) error {
	switch registry.ImagePullPolicy {
	case "", corev1.PullAlways, corev1.PullIfNotPresent, corev1.PullNever:
	default:
		return fmt.Errorf("%s.imagePullPolicy: unsupported pull policy %q", path, registry.ImagePullPolicy)
	}

	if len(registry.ImagePullSecrets) > 64 {
		return fmt.Errorf("%s.imagePullSecrets: at most 64 secret references are supported", path)
	}

	seen := make(map[string]struct{}, len(registry.ImagePullSecrets))

	for i, secret := range registry.ImagePullSecrets {
		if !strings.Contains(secret.Name, "{{") {
			if errors := validation.IsDNS1123Subdomain(secret.Name); len(errors) > 0 {
				return fmt.Errorf("%s.imagePullSecrets[%d].name: invalid secret name %q: %s", path, i, secret.Name, strings.Join(errors, "; "))
			}
		}

		if _, found := seen[secret.Name]; found {
			return fmt.Errorf("%s.imagePullSecrets[%d].name: duplicate secret name %q", path, i, secret.Name)
		}

		seen[secret.Name] = struct{}{}
	}

	return nil
}
