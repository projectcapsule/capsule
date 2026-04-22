// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0
package customquota

import (
	"context"
	"sort"
	"time"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	controller "github.com/projectcapsule/capsule/internal/controllers/customquotas"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	index "github.com/projectcapsule/capsule/pkg/runtime/indexers/customquota"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
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
		if a[i].GroupVersionKind.Group != b[i].GroupVersionKind.Group ||
			a[i].GroupVersionKind.Version != b[i].GroupVersionKind.Version ||
			a[i].GroupVersionKind.Kind != b[i].GroupVersionKind.Kind ||
			a[i].Path != b[i].Path ||
			a[i].Operation != b[i].Operation {
			return true
		}
	}

	return false
}

func touchQuantityLedger(
	ctx context.Context,
	c client.Client,
	item types.NamespacedName,
) error {
	return meta.TriggerRequestReconcileAnnotation(
		ctx,
		c,
		capsulev1beta2.GroupVersion.WithKind("QuantityLedger"),
		item,
	)
}

type touchedQuotaRef struct {
	Key       string
	Name      string
	Namespace string
	IsGlobal  bool
}

func previouslyReferencedQuotas(
	ctx context.Context,
	c client.Client,
	oldUID types.UID,
	newUID types.UID,
	req admission.Request,
) ([]touchedQuotaRef, error) {
	seen := map[string]touchedQuotaRef{}

	addCustom := func(uid types.UID, namespace string) error {
		if uid == "" || namespace == "" {
			return nil
		}

		list := &capsulev1beta2.CustomQuotaList{}
		if err := c.List(ctx, list,
			client.InNamespace(namespace),
			client.MatchingFields{
				index.ObjectUIDIndexerFieldName: string(uid),
			},
		); err != nil {
			return err
		}

		for _, item := range list.Items {
			key := controller.MakeCustomQuotaCacheKey(item.Namespace, item.Name)
			seen[key] = touchedQuotaRef{
				Key:       key,
				Name:      item.Name,
				Namespace: item.Namespace,
				IsGlobal:  false,
			}
		}

		return nil
	}

	addGlobal := func(uid types.UID) error {
		if uid == "" {
			return nil
		}

		list := &capsulev1beta2.GlobalCustomQuotaList{}
		if err := c.List(ctx, list, client.MatchingFields{
			index.ObjectUIDIndexerFieldName: string(uid),
		}); err != nil {
			return err
		}

		for _, item := range list.Items {
			key := controller.MakeGlobalCustomQuotaCacheKey(item.Name)
			seen[key] = touchedQuotaRef{
				Key:       key,
				Name:      item.Name,
				Namespace: "",
				IsGlobal:  true,
			}
		}

		return nil
	}

	if err := addCustom(oldUID, req.Namespace); err != nil {
		return nil, err
	}
	if err := addCustom(newUID, req.Namespace); err != nil {
		return nil, err
	}
	if err := addGlobal(oldUID); err != nil {
		return nil, err
	}
	if err := addGlobal(newUID); err != nil {
		return nil, err
	}

	out := make([]touchedQuotaRef, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsGlobal != out[j].IsGlobal {
			return out[i].IsGlobal
		}
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})

	return out, nil
}
