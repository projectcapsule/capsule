// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package customquota

import (
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func quantityLedgerKeyForMatchedQuota(item evaluatedQuota) types.NamespacedName {
	if item.IsGlobal {
		return types.NamespacedName{
			Name:      item.Name,
			Namespace: configuration.ControllerNamespace(),
		}
	}

	return types.NamespacedName{
		Name:      item.Name,
		Namespace: item.Namespace,
	}
}

func buildReservation(
	req admission.Request,
	u unstructured.Unstructured,
	usage resource.Quantity,
) capsulev1beta2.QuantityLedgerReservation {
	namespace := u.GetNamespace()
	if namespace == "" {
		namespace = req.Namespace
	}

	name := u.GetName()
	if name == "" {
		name = req.Name
	}

	now := metav1.Now()
	expiresAt := metav1.NewTime(now.Add(15 * time.Second))

	return capsulev1beta2.QuantityLedgerReservation{
		ID:    string(req.UID),
		Usage: usage.DeepCopy(),
		ObjectRef: capsulev1beta2.QuantityLedgerObjectRef{
			APIGroup:   req.Kind.Group,
			APIVersion: req.Kind.Version,
			Kind:       req.Kind.Kind,
			Namespace:  namespace,
			Name:       name,
			UID:        u.GetUID(),
		},
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: &expiresAt,
	}
}

func allKeys[K comparable, V any](a map[K]V, b map[K]V) []K {
	out := make([]K, 0, len(a)+len(b))
	seen := make(map[K]struct{}, len(a)+len(b))

	for k := range a {
		seen[k] = struct{}{}

		out = append(out, k)
	}

	for k := range b {
		if _, ok := seen[k]; ok {
			continue
		}

		out = append(out, k)
	}

	return out
}

func sourcesChanged(a, b []capsulev1beta2.CustomQuotaSpecSource) bool {
	if len(a) != len(b) {
		return true
	}

	for i := range a {
		if a[i].Group != b[i].Group ||
			a[i].Version != b[i].Version ||
			a[i].Kind != b[i].Kind ||
			a[i].Path != b[i].Path ||
			a[i].Operation != b[i].Operation {
			return true
		}
	}

	return false
}
