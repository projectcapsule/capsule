// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

func namespaceHasFinalizers(ctx context.Context, c client.Reader, namespace string) *admission.Response {
	if namespace == "" {
		return nil
	}

	ns := &corev1.Namespace{}
	if err := c.Get(ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
		return ad.ErroredResponse(err)
	}

	if ns.DeletionTimestamp == nil {
		return nil
	}

	terminating, err := tenant.NamespaceIsPendingUnmanagedTerminationByStatus(ctx, c, ns)
	if err != nil {
		return ad.ErroredResponse(err)
	}

	if terminating {
		msg := "namespace is terminating and has remaining finalizers; delaying deletion of managed resource"
		resp := admission.Denied(msg)

		return &resp
	}

	return nil
}
