/*
Copyright 2020 Clastix Labs.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
