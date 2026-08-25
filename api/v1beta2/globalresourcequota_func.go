// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package v1beta2

import (
	"crypto/sha256"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	runtimequota "github.com/projectcapsule/capsule/pkg/runtime/quota"
)

func (q *GlobalResourceQuota) GetResourceQuotaName() string {
	sum := sha256.Sum256([]byte(q.Name))

	return fmt.Sprintf("capsule-global-quota-%x", sum[:10])
}

func (q *GlobalResourceQuota) GetLedgerName() string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s/%s", q.Name, q.UID))

	return fmt.Sprintf("global-resource-quota-%x", sum[:16])
}

func (q *GlobalResourceQuota) AssignNamespaces(namespaces []corev1.Namespace) {
	names := make([]string, 0, len(namespaces))

	for i := range namespaces {
		ns := &namespaces[i]
		if ns.Status.Phase == corev1.NamespaceActive && ns.DeletionTimestamp == nil {
			names = append(names, ns.Name)
		}
	}

	sort.Strings(names)
	q.Status.Namespaces = names
	q.Status.NamespaceSize = uint(len(names))
}

func (q *GlobalResourceQuota) CalculateAvailable() {
	available := make(corev1.ResourceList, len(q.Status.Total.Hard))

	for name, hard := range q.Status.Total.Hard {
		value := hard.DeepCopy()
		value.Sub(q.Status.Total.Used[name])
		runtimequota.ClampQuantityToZero(&value)
		available[name] = value
	}

	q.Status.Total.Available = available
}

func ZeroResourceList(resources corev1.ResourceList) corev1.ResourceList {
	out := make(corev1.ResourceList, len(resources))
	for name := range resources {
		out[name] = *resource.NewQuantity(0, resource.DecimalSI)
	}

	return out
}
