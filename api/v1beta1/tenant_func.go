// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
)

func (in *Tenant) IsCordoned() bool {
	if v, ok := in.Labels["capsule.clastix.io/cordon"]; ok && v == "enabled" {
		return true
	}

	return false
}

func (in *Tenant) IsFull() bool {
	// we don't have limits on assigned Namespaces
	if in.Spec.NamespaceOptions == nil || in.Spec.NamespaceOptions.Quota == nil {
		return false
	}

	return len(in.Status.Namespaces) >= int(*in.Spec.NamespaceOptions.Quota)
}

func (in *Tenant) AssignNamespaces(namespaces []corev1.Namespace) {
	var l []string

	for _, ns := range namespaces {
		if ns.Status.Phase == corev1.NamespaceActive {
			l = append(l, ns.GetName())
		}
	}

	sort.Strings(l)

	in.Status.Namespaces = l
	in.Status.Size = uint(len(l))
}

func (in *Tenant) GetOwnerProxySettings(name string, kind OwnerKind) []ProxySettings {
	return in.Spec.Owners.FindOwner(name, kind).ProxyOperations
}
