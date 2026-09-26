// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
)

// Protection must not prevent Kubernetes from deleting the contents of a
// terminating namespace. Only DELETE handlers use this exception; ordinary
// authorized requests and dependency errors retain their original response.
// The reader must be authoritative: a cached terminating namespace may have
// been deleted and replaced by an active namespace with the same name.
func allowTerminatingNamespaceDeletion(ctx context.Context, reader client.Reader, req admission.Request, response *admission.Response) *admission.Response {
	if response == nil || response.Result == nil || response.Result.Code != http.StatusForbidden || req.Namespace == "" || reader == nil {
		return response
	}

	terminating, err := namespaceTerminating(ctx, reader, req.Namespace)
	if err = client.IgnoreNotFound(err); err != nil {
		return ad.ErroredResponse(err)
	}

	if terminating {
		return nil
	}

	return response
}

func namespaceTerminating(ctx context.Context, reader client.Reader, namespace string) (bool, error) {
	ns := &corev1.Namespace{}
	if err := reader.Get(ctx, client.ObjectKey{Name: namespace}, ns); err != nil {
		return false, err
	}

	return ns.DeletionTimestamp != nil || ns.Status.Phase == corev1.NamespaceTerminating, nil
}
