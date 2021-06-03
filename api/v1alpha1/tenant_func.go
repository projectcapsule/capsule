// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
)

func (t *Tenant) IsFull() bool {
	// we don't have limits on assigned Namespaces
	if t.Spec.NamespaceQuota == nil {
		return false
	}
	return len(t.Status.Namespaces) >= int(*t.Spec.NamespaceQuota)
}

func (t *Tenant) AssignNamespaces(namespaces []corev1.Namespace) {
	var l []string
	for _, ns := range namespaces {
		if ns.Status.Phase == corev1.NamespaceActive {
			l = append(l, ns.GetName())
		}
	}
	sort.Strings(l)

	t.Status.Namespaces = l
	t.Status.Size = uint(len(l))
}
