// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquota

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type GlobalTargetReference struct{}

func (o GlobalTargetReference) Object() client.Object {
	return &capsulev1beta2.GlobalCustomQuota{}
}

func (o GlobalTargetReference) Field() string {
	return TargetIndexerFieldName
}

func (o GlobalTargetReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		tr := object.(*capsulev1beta2.GlobalCustomQuota) //nolint:forcetypeassert

		// Index on Spec.Sources (declared intent) rather than Status.Targets
		// (reconciled state) so the webhook can match quotas immediately after
		// creation, before the controller has had a chance to reconcile.
		targets := make([]string, 0, len(tr.Spec.Sources))

		for _, src := range tr.Spec.Sources {
			gvk := src.GroupVersionKind()
			targets = append(targets, metav1.GroupVersionKind{
				Group:   gvk.Group,
				Version: gvk.Version,
				Kind:    gvk.Kind,
			}.String())
		}

		return targets
	}
}
