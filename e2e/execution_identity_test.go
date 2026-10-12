// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package e2e

import (
	rbacv1 "k8s.io/api/rbac/v1"

	apimeta "github.com/projectcapsule/capsule/pkg/api/meta"
)

func managedTargetServiceAccountReference(
	namespace,
	name string,
) *apimeta.NamespacedRFC1123ObjectReferenceWithNamespace {
	return &apimeta.NamespacedRFC1123ObjectReferenceWithNamespace{
		Name:      apimeta.RFC1123Name(name),
		Namespace: apimeta.RFC1123SubdomainName(namespace),
	}
}

func grantManagedTargetServiceAccount(namespace, name, targetNamespace string, configMapVerbs []string) {
	ensureServiceAccount(namespace, name)
	bindServiceAccountToNamespacedResource(
		namespace,
		name,
		targetNamespace,
		[]string{"configmaps"},
		configMapVerbs,
	)
	bindServiceAccountToClusterResources(
		namespace,
		name,
		name+"-namespaces",
		name+"-namespaces-binding",
		[]rbacv1.PolicyRule{{
			APIGroups: []string{""},
			Resources: []string{"namespaces"},
			Verbs:     []string{"get"},
		}},
	)
}
